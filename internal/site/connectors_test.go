package site

import (
	"errors"
	"fmt"
	"slices"
	"testing"
)

func openDataTemplate() Connector { return ConnectorTemplates()[0] }

func TestConnectorsAreKeptAndReplayed(t *testing.T) {
	var records [][]byte
	j := func(r []byte) error { records = append(records, r); return nil }
	s, _ := New(nil, j)
	if got := s.Connectors(); len(got) != 0 {
		t.Fatalf("connectors before any: %+v", got)
	}
	c := openDataTemplate()
	c.Enabled, c.Space = true, " API "
	c.CKAN.Formats, c.CKAN.Organizations = []string{"api", " wms", "API", ""}, []string{"BFS", "bfs", "swisstopo"}
	kept, err := s.SetConnector("admin", c)
	if err != nil {
		t.Fatal(err)
	}
	if kept.Space != "API" || !slices.Equal(kept.CKAN.Formats, []string{"API", "WMS"}) || !slices.Equal(kept.CKAN.Organizations, []string{"bfs", "swisstopo"}) {
		t.Fatalf("normalized %+v", kept)
	}
	other := openDataTemplate()
	other.ID, other.Name = "geo", "Geodienste"
	if _, err := s.SetConnector("admin", other); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Connector(OpenDataSwiss)
	got.CKAN.Formats[0] = "CHANGED"
	if again, _ := s.Connector(OpenDataSwiss); again.CKAN.Formats[0] != "API" {
		t.Fatal("a caller changed the settings")
	}
	if names := s.Connectors(); len(names) != 2 || names[0].Name != "Geodienste" {
		t.Fatalf("by name %+v", names)
	}
	if err := s.RemoveConnector("admin", "geo"); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveConnector("admin", "geo"); !errors.Is(err, ErrInvalid) {
		t.Errorf("removed twice: %v", err)
	}
	r, err := New(records, j)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := r.Connector(OpenDataSwiss); !ok || !slices.Equal(got.CKAN.Formats, kept.CKAN.Formats) || got.Space != "API" || len(r.Connectors()) != 1 {
		t.Fatalf("replayed %+v %v", r.Connectors(), ok)
	}
	for _, bad := range []string{`{"v":1,"kind":"ConnectorSet"}`, `{"v":1,"kind":"OpenDataSet"}`} {
		if _, err := New([][]byte{[]byte(bad)}, j); err == nil {
			t.Errorf("%s replayed", bad)
		}
	}
}

// The synchronisation of ADR-0075 continues as the connector
// opendata.swiss.
func TestTheOpenDataSettingsBecomeAConnector(t *testing.T) {
	old := `{"v":1,"kind":"OpenDataSet","openData":{"enabled":true,"space":"API","baseURL":"https://ckan.example/api/3/action",` +
		`"portalURL":"https://portal.example/dataset/","formats":["WMS"],"organizations":["bfs"],"intervalHours":12,"steward":"u1"}}`
	s, err := New([][]byte{[]byte(old)}, func([]byte) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	c, ok := s.Connector(OpenDataSwiss)
	if !ok || !c.Enabled || c.Kind != KindCKAN || c.Space != "API" || c.IntervalHours != 12 || c.Steward != "u1" ||
		c.CKAN.BaseURL != "https://ckan.example/api/3/action" || !slices.Equal(c.CKAN.Formats, []string{"WMS"}) || c.Validate() != nil {
		t.Fatalf("%+v %+v", c, c.CKAN)
	}
}

func TestConnectorsAreChecked(t *testing.T) {
	s, _ := New(nil, func([]byte) error { return nil })
	for name, change := range map[string]func(*Connector){
		"no ID":        func(c *Connector) { c.ID = "" },
		"a bad ID":     func(c *Connector) { c.ID = "Open Data" },
		"no name":      func(c *Connector) { c.Name = " " },
		"a bad kind":   func(c *Connector) { c.Kind = "ftp" },
		"no source":    func(c *Connector) { c.CKAN = nil },
		"no space":     func(c *Connector) { c.Enabled, c.Space = true, "" },
		"no formats":   func(c *Connector) { c.CKAN.Formats = nil },
		"a bad format": func(c *Connector) { c.CKAN.Formats = []string{"(API)"} },
		"a bad org":    func(c *Connector) { c.CKAN.Organizations = []string{"A B"} },
		"a language":   func(c *Connector) { c.CKAN.Language = "rm" },
		"too many orgs": func(c *Connector) {
			for i := range 201 {
				c.CKAN.Organizations = append(c.CKAN.Organizations, fmt.Sprintf("org-%d", i))
			}
		},
		"too many formats": func(c *Connector) {
			for i := range 61 {
				c.CKAN.Formats = append(c.CKAN.Formats, fmt.Sprintf("F%d", i))
			}
		},
		"an interval of 0": func(c *Connector) { c.IntervalHours = 0 },
		"ftp":              func(c *Connector) { c.CKAN.BaseURL = "ftp://x.example" },
		"a password":       func(c *Connector) { c.CKAN.PortalURL = "https://u:p@x.example/" },
	} {
		bad := openDataTemplate()
		change(&bad)
		if _, err := s.SetConnector("admin", bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	for i := range maxConnectors {
		c := openDataTemplate()
		c.ID = fmt.Sprintf("c%d", i)
		if _, err := s.SetConnector("admin", c); err != nil {
			t.Fatal(err)
		}
	}
	extra := openDataTemplate()
	if _, err := s.SetConnector("admin", extra); !errors.Is(err, ErrInvalid) {
		t.Errorf("one connector too many: %v", err)
	}
	extra.ID = "c0"
	if _, err := s.SetConnector("admin", extra); err != nil {
		t.Errorf("changing one at the limit: %v", err)
	}
}

func jsonConnector() Connector {
	c := ConnectorTemplates()[1]
	c.ID, c.JSON.URL = "feed", "https://feed.example/entries"
	c.JSON.Values = map[string]string{"owner": "contact.name", "since": "published"}
	return c
}

func TestJSONConnectorsAreChecked(t *testing.T) {
	s, _ := New(nil, func([]byte) error { return nil })
	c := jsonConnector()
	c.Type, c.Fields = " api ", map[string]string{" since ": " since "}
	c.JSON.Values[" spaced "] = " a.b "
	kept, err := s.SetConnector("admin", c)
	if err != nil {
		t.Fatal(err)
	}
	if kept.Type != "api" || kept.Fields["since"] != "since" || kept.JSON.Values["spaced"] != "a.b" ||
		!slices.Equal(kept.Values(), []string{"owner", "since", "spaced"}) {
		t.Fatalf("normalized %+v %+v", kept, kept.JSON)
	}
	got, _ := s.Connector("feed")
	got.Fields["since"], got.JSON.Values["owner"] = "x", "x"
	if again, _ := s.Connector("feed"); again.Fields["since"] != "since" || again.JSON.Values["owner"] != "contact.name" {
		t.Fatal("a caller changed the settings")
	}
	if ckan := openDataTemplate(); !slices.Equal(ckan.Values(), CKANValues) || (Connector{Kind: KindJSON}).Values() != nil {
		t.Error("the values a source gives")
	}
	for name, change := range map[string]func(*Connector){
		"no JSON source":    func(c *Connector) { c.JSON = nil },
		"the template's":    func(c *Connector) { c.JSON.URL = "https://" },
		"no ID path":        func(c *Connector) { c.JSON.ID = "" },
		"no title path":     func(c *Connector) { c.JSON.Title = "" },
		"a bad path":        func(c *Connector) { c.JSON.Items = "result..items" },
		"a bad value path":  func(c *Connector) { c.JSON.Values["x"] = "a b" },
		"a bad value name":  func(c *Connector) { c.JSON.Values["<x>"] = "a" },
		"a JSON language":   func(c *Connector) { c.JSON.Language = "rm" },
		"a bad type":        func(c *Connector) { c.Type = "<api>" },
		"a bad field":       func(c *Connector) { c.Fields = map[string]string{"<f>": "since"} },
		"an unknown value":  func(c *Connector) { c.Fields = map[string]string{"owner": "nobody"} },
		"a CKAN value here": func(c *Connector) { c.Fields = map[string]string{"owner": "publisher"} },
		"too many values": func(c *Connector) {
			for i := range 51 {
				c.JSON.Values[fmt.Sprintf("v%d", i)] = "a"
			}
		},
		"too many fields": func(c *Connector) {
			c.Fields = map[string]string{}
			for i := range 51 {
				c.Fields[fmt.Sprintf("f%d", i)] = "since"
			}
		},
	} {
		bad := jsonConnector()
		change(&bad)
		if _, err := s.SetConnector("admin", bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	ckan := openDataTemplate()
	ckan.Fields = map[string]string{"owner": "publisher"}
	if _, err := s.SetConnector("admin", ckan); err != nil {
		t.Errorf("a CKAN value mapped: %v", err)
	}
	ckan.Fields = map[string]string{"owner": "since"}
	if _, err := s.SetConnector("admin", ckan); !errors.Is(err, ErrInvalid) {
		t.Errorf("a value CKAN does not give: %v", err)
	}
}

func atlassianConnector() Connector {
	c := ConnectorTemplates()[2]
	c.ID, c.Atlassian.Site = "acme", "https://acme.atlassian.net"
	c.Atlassian.Spaces, c.Atlassian.Projects = []string{"eng", " ~5f1a ", "ENG"}, []string{"plat"}
	c.Atlassian.Users = map[string]string{"acc-1": " anna "}
	c.Atlassian.Groups = map[string]string{" dev team ": "@engineering"}
	c.Atlassian.Grants = []string{" @engineering = editor", "anna=space-admin", "anna=space-admin"}
	return c
}

func TestAtlassianConnectorsAreChecked(t *testing.T) {
	s, _ := New(nil, func([]byte) error { return nil })
	c := atlassianConnector()
	c.Enabled = true // without a space: the import names its spaces
	kept, err := s.SetConnector("admin", c)
	if err != nil {
		t.Fatal(err)
	}
	a := kept.Atlassian
	if !slices.Equal(a.Spaces, []string{"ENG", "~5f1a"}) || !slices.Equal(a.Projects, []string{"PLAT"}) || a.Users["acc-1"] != "anna" ||
		a.Groups["dev team"] != "@engineering" || !slices.Equal(a.Grants, []string{"@engineering=editor", "anna=space-admin"}) || kept.Values() != nil {
		t.Fatalf("normalized %+v", a)
	}
	got, _ := s.Connector("acme")
	got.Atlassian.Spaces[0], got.Atlassian.Users["acc-1"] = "X", "x"
	if again, _ := s.Connector("acme"); again.Atlassian.Spaces[0] != "ENG" || again.Atlassian.Users["acc-1"] != "anna" {
		t.Fatal("a caller changed the settings")
	}
	for _, local := range []string{"https://acme.example/", "http://127.0.0.1:8080", "http://localhost"} {
		ok := atlassianConnector()
		ok.Atlassian.Site = local
		if _, err := s.SetConnector("admin", ok); err != nil {
			t.Errorf("%s: %v", local, err)
		}
	}
	for name, change := range map[string]func(*Connector){
		"no site":           func(c *Connector) { c.Atlassian = nil },
		"the template's":    func(c *Connector) { c.Atlassian.Site = "https://" },
		"plain http":        func(c *Connector) { c.Atlassian.Site = "http://acme.example" },
		"a path":            func(c *Connector) { c.Atlassian.Site = "https://acme.example/wiki" },
		"a password":        func(c *Connector) { c.Atlassian.Site = "https://u:p@acme.example" },
		"nothing to import": func(c *Connector) { c.Atlassian.Spaces, c.Atlassian.Projects = nil, nil },
		"a bad key":         func(c *Connector) { c.Atlassian.Spaces = []string{"A B"} },
		"a bad account":     func(c *Connector) { c.Atlassian.Users = map[string]string{"a b": "anna"} },
		"a bad username":    func(c *Connector) { c.Atlassian.Users = map[string]string{"acc": "an na"} },
		"a bad group":       func(c *Connector) { c.Atlassian.Groups = map[string]string{"dev": "engineering"} },
		"a bad grant":       func(c *Connector) { c.Atlassian.Grants = []string{"anna=owner"} },
		"a grant to no one": func(c *Connector) { c.Atlassian.Grants = []string{"@=viewer"} },
		"a space":           func(c *Connector) { c.Space = "API" },
		"a type":            func(c *Connector) { c.Type = "page" },
		"too many spaces": func(c *Connector) {
			for i := range 101 {
				c.Atlassian.Spaces = append(c.Atlassian.Spaces, fmt.Sprintf("S%d", i))
			}
		},
		"too many grants": func(c *Connector) {
			for i := range 101 {
				c.Atlassian.Grants = append(c.Atlassian.Grants, fmt.Sprintf("u%d=viewer", i))
			}
		},
	} {
		bad := atlassianConnector()
		change(&bad)
		if _, err := s.SetConnector("admin", bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestGitConnectorsAreChecked(t *testing.T) {
	s, _ := New(nil, func([]byte) error { return nil })
	c := ConnectorTemplates()[len(ConnectorTemplates())-1]
	if c.Kind != KindGit || c.IntervalHours != 1 {
		t.Fatalf("the Git template runs every hour: %+v", c)
	}
	c.ID, c.Git.URL = "docs", " https://github.com/o/r/tree/main/docs "
	c.Fields = map[string]string{"source": "url"}
	kept, err := s.SetConnector("admin", c)
	if err != nil {
		t.Fatal(err)
	}
	if kept.Git.URL != "https://github.com/o/r/tree/main/docs" || !slices.Equal(kept.Values(), GitValues) {
		t.Fatalf("normalized %+v", kept.Git)
	}
	got, _ := s.Connector("docs")
	got.Git.URL = "x"
	if again, _ := s.Connector("docs"); again.Git.URL != kept.Git.URL {
		t.Fatal("a caller changed the settings")
	}
	for name, change := range map[string]func(*Connector){
		"no Git source":     func(c *Connector) { c.Git = nil },
		"the template's":    func(c *Connector) { c.Git.URL = "https://" },
		"no repository":     func(c *Connector) { c.Git.URL = "https://example.org/docs" },
		"a Git language":    func(c *Connector) { c.Git.Language = "rm" },
		"a JSON value":      func(c *Connector) { c.Fields = map[string]string{"source": "owner"} },
		"credentials in it": func(c *Connector) { c.Git.URL = "https://u:p@github.com/o/r" },
	} {
		bad := c
		g := *c.Git
		bad.Git = &g
		change(&bad)
		if _, err := s.SetConnector("admin", bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
