package site

import (
	"cmp"
	"fmt"
	"maps"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// Connector keeps a space in step with an external source (ADR-0077,
// ADR-0127): which space it fills, with items of which type, which of
// their fields take which of the source's values, how often, who owns new
// entries, and the settings of its kind. Its ID names the source in the
// origin of the items it writes (ADR-0023), so it does not change.
type Connector struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Kind          string `json:"kind"`
	Enabled       bool   `json:"enabled"`
	Space         string `json:"space"`
	IntervalHours int    `json:"intervalHours"`
	// Steward, a user ID, becomes the owner of new entries until someone
	// else is set; empty leaves them without one.
	Steward string `json:"steward"`
	// Type is the type of the items it writes; empty is page (ADR-0093).
	// The type must have a text, and each field it requires must be
	// mapped.
	Type string `json:"type,omitempty"`
	// Fields maps a field of the type to the name of a value the source
	// gives for each entry (ADR-0127); the run converts it to the field's
	// kind. Fields not named are left as people set them.
	Fields map[string]string `json:"fields,omitempty"`
	// CKAN holds the settings of a connector of the kind ckan.
	CKAN *CKAN `json:"ckan,omitempty"`
	// JSON holds the settings of a connector of the kind json.
	JSON *JSONSource `json:"json,omitempty"`
}

// JSONSource is any source that answers JSON over http (ADR-0127): the
// address of a list of entries, and where in it each entry's ID, title,
// link and text are. Its defaults read the connectors' own feed format,
// {"entries": [{"id", "title", "url", "text", ...}], "next": "..."}, which
// an adapter for any other system can write.
type JSONSource struct {
	URL string `json:"url"`
	// Items is the path to the list of entries in the answer; empty is
	// the answer itself. A path names keys and list positions joined by
	// dots, e.g. result.items or data.0.rows.
	Items string `json:"items"`
	// ID, Title, Link and Text are paths within an entry. ID and Title
	// are required; Text is written as the page's paragraphs.
	ID    string `json:"id"`
	Title string `json:"title"`
	Link  string `json:"link,omitempty"`
	Text  string `json:"text,omitempty"`
	// Next, if set, is the path to the address of the next page of the
	// list, in the answer; it is followed up to 50 pages.
	Next string `json:"next,omitempty"`
	// Values are the further values of an entry, by name, with the path
	// to each: shown in the page's table and mapped to fields by Fields.
	Values map[string]string `json:"values,omitempty"`
	// Language is the one of the page's own words; empty is German.
	Language string `json:"language,omitempty"`
}

// CKAN is a source that runs CKAN, such as opendata.swiss (ADR-0075):
// every resource of a data set in one of the formats becomes an entry.
type CKAN struct {
	// BaseURL is the CKAN action API, e.g.
	// https://ckan.opendata.swiss/api/3/action.
	BaseURL string `json:"baseURL"`
	// PortalURL prefixes a data set's name to link to its page, the entry
	// in the system of record.
	PortalURL string `json:"portalURL"`
	// Formats are the resource formats that count as an interface.
	Formats []string `json:"formats"`
	// Organizations, if any, limit the connector to these publishers, by
	// their names in the source.
	Organizations []string `json:"organizations"`
	// Language is the one a page is written in: the source's texts in it
	// where it has them, and the page's own words (ADR-0080). Empty is
	// German, the first language of opendata.swiss.
	Language string `json:"language,omitempty"`
}

// ConnectorLanguages are the languages a connector writes its pages in.
var ConnectorLanguages = []string{"de", "fr", "it", "en"}

// The kinds of source a connector reads: CKAN, and any JSON over http.
const (
	KindCKAN = "ckan"
	KindJSON = "json"
)

// ConnectorKinds are the kinds of source a connector reads.
var ConnectorKinds = []string{KindCKAN, KindJSON}

// maxConnectors bounds how many connectors a site keeps.
const maxConnectors = 20

// OpenDataSwiss is the connector to opendata.swiss as it starts: the
// first template, and the ID the synchronisation of ADR-0075 continues
// under.
const OpenDataSwiss = "opendata.swiss"

// ConnectorTemplates are what a new connector starts from, one per kind.
func ConnectorTemplates() []Connector {
	return []Connector{{ID: OpenDataSwiss, Name: "opendata.swiss", Kind: KindCKAN, IntervalHours: 24, CKAN: &CKAN{
		BaseURL:       "https://ckan.opendata.swiss/api/3/action",
		PortalURL:     "https://opendata.swiss/de/dataset/",
		Formats:       []string{"API", "WMS", "WFS", "WCS", "WMTS", "SPARQL"},
		Organizations: []string{},
		Language:      "de",
	}}, {Name: "JSON", Kind: KindJSON, IntervalHours: 24, JSON: &JSONSource{
		URL: "https://", Items: "entries", ID: "id", Title: "title", Link: "url", Text: "text", Next: "next",
		Values: map[string]string{}, Language: "de",
	}}}
}

var connectorIDRE = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,39}$`)

// formatRE accepts a format as publishers name it, e.g. "WMS",
// "ESRI REST" or "OGC API – FEATURES", kept in capitals.
var formatRE = regexp.MustCompile(`^[\p{Lu}\p{N}][\p{Lu}\p{N} _.:/()+&–-]{0,59}$`)

var orgRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,99}$`)

// pathRE accepts a path into a JSON answer: keys of letters, digits, _, -,
// @ and $, or list positions, joined by dots.
var pathRE = regexp.MustCompile(`^[A-Za-z0-9_@$-]+(\.[A-Za-z0-9_@$-]+){0,15}$`)

// nameRE accepts a field's or a value's name.
var nameRE = regexp.MustCompile(`^[\p{L}\p{N}_][\p{L}\p{N}_ .-]{0,63}$`)

// Validate checks a connector's settings.
func (c Connector) Validate() error {
	switch {
	case !connectorIDRE.MatchString(c.ID):
		return fmt.Errorf("%w: %q is not a connector ID: lower-case letters, digits, dots and hyphens, at most 40", ErrInvalid, c.ID)
	case strings.TrimSpace(c.Name) == "" || strings.ContainsAny(c.Name, "<>\n") || len([]rune(c.Name)) > 80:
		return fmt.Errorf("%w: the name is a single line of 1 to 80 characters", ErrInvalid)
	case !slices.Contains(ConnectorKinds, c.Kind):
		return fmt.Errorf("%w: %q is not a kind of connector (%s)", ErrInvalid, c.Kind, strings.Join(ConnectorKinds, ", "))
	case c.Enabled && c.Space == "":
		return fmt.Errorf("%w: the connector needs a space", ErrInvalid)
	case c.IntervalHours < 1 || c.IntervalHours > 24*31:
		return fmt.Errorf("%w: the connector runs every 1 to 744 hours", ErrInvalid)
	}
	if c.Type != "" && !nameRE.MatchString(c.Type) {
		return fmt.Errorf("%w: %q is not a type's name", ErrInvalid, c.Type)
	}
	if len(c.Fields) > 50 {
		return fmt.Errorf("%w: map at most 50 fields", ErrInvalid)
	}
	for field, value := range c.Fields {
		if !nameRE.MatchString(field) || !nameRE.MatchString(value) {
			return fmt.Errorf("%w: %q from %q is not a field and a value's name", ErrInvalid, field, value)
		}
	}
	var err error
	switch c.Kind {
	case KindCKAN:
		if c.CKAN == nil {
			return fmt.Errorf("%w: a CKAN connector needs its source", ErrInvalid)
		}
		err = c.CKAN.validate()
	case KindJSON:
		if c.JSON == nil {
			return fmt.Errorf("%w: a JSON connector needs its source", ErrInvalid)
		}
		err = c.JSON.validate()
	}
	if err != nil {
		return err
	}
	given := c.Values()
	for _, field := range slices.Sorted(maps.Keys(c.Fields)) {
		if !slices.Contains(given, c.Fields[field]) {
			return fmt.Errorf("%w: the source gives no value %q for the field %q (%s)", ErrInvalid, c.Fields[field], field, strings.Join(given, ", "))
		}
	}
	return nil
}

// CKANValues are the values a CKAN source gives of each entry, by name,
// for the mapping to fields (ADR-0127).
var CKANValues = []string{"dataset", "publisher", "contact", "url", "formats"}

// Values are the names of the values the connector's source gives of
// each entry, which Fields maps from.
func (c Connector) Values() []string {
	switch {
	case c.Kind == KindCKAN:
		return CKANValues
	case c.Kind == KindJSON && c.JSON != nil:
		return slices.Sorted(maps.Keys(c.JSON.Values))
	}
	return nil
}

func (j JSONSource) validate() error {
	if !httpURL(j.URL) {
		return fmt.Errorf("%w: %q is not an http or https address", ErrInvalid, j.URL)
	}
	switch {
	case j.ID == "" || j.Title == "":
		return fmt.Errorf("%w: name where an entry's ID and title are", ErrInvalid)
	case len(j.Values) > 50:
		return fmt.Errorf("%w: name at most 50 values", ErrInvalid)
	case j.Language != "" && !slices.Contains(ConnectorLanguages, j.Language):
		return fmt.Errorf("%w: %q is not a language of the pages (%s)", ErrInvalid, j.Language, strings.Join(ConnectorLanguages, ", "))
	}
	for _, p := range []string{j.Items, j.ID, j.Title, j.Link, j.Text, j.Next} {
		if p != "" && !pathRE.MatchString(p) {
			return fmt.Errorf("%w: %q is not a path, e.g. result.items or id", ErrInvalid, p)
		}
	}
	for name, p := range j.Values {
		if !nameRE.MatchString(name) || !pathRE.MatchString(p) {
			return fmt.Errorf("%w: %q at %q is not a value's name and path", ErrInvalid, name, p)
		}
	}
	return nil
}

// httpURL reports whether u is an http or https address without
// credentials in it.
func httpURL(u string) bool {
	p, err := url.Parse(u)
	return err == nil && (p.Scheme == "https" || p.Scheme == "http") && p.Host != "" && p.User == nil && len(u) <= 512
}

func (k CKAN) validate() error {
	for _, u := range []string{k.BaseURL, k.PortalURL} {
		if !httpURL(u) {
			return fmt.Errorf("%w: %q is not an http or https address", ErrInvalid, u)
		}
	}
	switch {
	case len(k.Formats) == 0 || len(k.Formats) > 60:
		return fmt.Errorf("%w: name 1 to 60 formats", ErrInvalid)
	case len(k.Organizations) > 200:
		return fmt.Errorf("%w: name at most 200 organizations", ErrInvalid)
	case k.Language != "" && !slices.Contains(ConnectorLanguages, k.Language):
		return fmt.Errorf("%w: %q is not a language of the pages (%s)", ErrInvalid, k.Language, strings.Join(ConnectorLanguages, ", "))
	}
	for _, f := range k.Formats {
		if !formatRE.MatchString(f) {
			return fmt.Errorf("%w: %q is not a format, e.g. WMS or ESRI REST", ErrInvalid, f)
		}
	}
	for _, org := range k.Organizations {
		if !orgRE.MatchString(org) {
			return fmt.Errorf("%w: %q is not an organization's name, e.g. bundesamt-fur-statistik-bfs", ErrInvalid, org)
		}
	}
	return nil
}

// Normalize tidies what an administrator typed: formats in capitals,
// lists without blanks and duplicates, mappings without empty values.
func (c Connector) Normalize() Connector {
	c.ID, c.Name, c.Space = strings.TrimSpace(c.ID), strings.TrimSpace(c.Name), strings.TrimSpace(c.Space)
	c.Type = strings.TrimSpace(c.Type)
	c.Fields = trimMap(c.Fields)
	if c.JSON != nil {
		j := *c.JSON
		for _, p := range []*string{&j.URL, &j.Items, &j.ID, &j.Title, &j.Link, &j.Text, &j.Next} {
			*p = strings.TrimSpace(*p)
		}
		j.Values = trimMap(j.Values)
		c.JSON = &j
	}
	if c.CKAN != nil {
		clean := func(in []string, f func(string) string) []string {
			out := []string{}
			for _, s := range in {
				if s = f(strings.TrimSpace(s)); s != "" && !slices.Contains(out, s) {
					out = append(out, s)
				}
			}
			return out
		}
		k := *c.CKAN
		k.Formats = clean(k.Formats, strings.ToUpper)
		k.Organizations = clean(k.Organizations, strings.ToLower)
		k.BaseURL, k.PortalURL = strings.TrimSpace(k.BaseURL), strings.TrimSpace(k.PortalURL)
		c.CKAN = &k
	}
	return c
}

// trimMap drops the entries of m with an empty key or value, trimmed.
func trimMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := map[string]string{}
	for k, v := range m {
		if k, v = strings.TrimSpace(k), strings.TrimSpace(v); k != "" && v != "" {
			out[k] = v
		}
	}
	return out
}

// clone copies a connector, so a caller's changes do not reach the
// settings.
func (c Connector) clone() Connector {
	c.Fields = maps.Clone(c.Fields)
	if c.JSON != nil {
		j := *c.JSON
		j.Values = maps.Clone(j.Values)
		c.JSON = &j
	}
	if c.CKAN != nil {
		k := *c.CKAN
		k.Formats, k.Organizations = slices.Clone(k.Formats), slices.Clone(k.Organizations)
		c.CKAN = &k
	}
	return c
}

// Connectors returns the connectors by name.
func (s *Settings) Connectors() []Connector {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Connector{}
	for _, c := range s.connectors {
		out = append(out, c.clone())
	}
	slices.SortFunc(out, func(a, b Connector) int { return cmp.Or(strings.Compare(a.Name, b.Name), strings.Compare(a.ID, b.ID)) })
	return out
}

// Connector returns one connector.
func (s *Settings) Connector(id string) (Connector, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.connectors[id]
	return c.clone(), ok
}

// SetConnector adds a connector or replaces the one of its ID, and
// returns it as kept.
func (s *Settings) SetConnector(actor string, c Connector) (Connector, error) {
	c = c.Normalize()
	if err := c.Validate(); err != nil {
		return c, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.connectors[c.ID]; !ok && len(s.connectors) >= maxConnectors {
		return c, fmt.Errorf("%w: at most %d connectors", ErrInvalid, maxConnectors)
	}
	return c, s.record(event{Kind: connectorSet, Actor: actor, Connector: &c})
}

// RemoveConnector removes a connector. The items it wrote stay, with
// their origin.
func (s *Settings) RemoveConnector(actor, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.connectors[id]; !ok {
		return fmt.Errorf("%w: no connector %q", ErrInvalid, id)
	}
	return s.record(event{Kind: connectorRemoved, Actor: actor, Asset: id})
}

// openData is the synchronisation with opendata.swiss as ADR-0075 kept
// it, before connectors; replaying it sets the connector opendata.swiss.
type openData struct {
	Enabled       bool     `json:"enabled"`
	Space         string   `json:"space"`
	BaseURL       string   `json:"baseURL"`
	PortalURL     string   `json:"portalURL"`
	Formats       []string `json:"formats"`
	Organizations []string `json:"organizations"`
	IntervalHours int      `json:"intervalHours"`
	Steward       string   `json:"steward"`
}

func (o openData) connector() Connector {
	return Connector{ID: OpenDataSwiss, Name: "opendata.swiss", Kind: KindCKAN, Enabled: o.Enabled, Space: o.Space,
		IntervalHours: o.IntervalHours, Steward: o.Steward,
		CKAN: &CKAN{BaseURL: o.BaseURL, PortalURL: o.PortalURL, Formats: o.Formats, Organizations: o.Organizations}}
}
