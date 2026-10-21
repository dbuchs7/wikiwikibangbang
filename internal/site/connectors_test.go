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
