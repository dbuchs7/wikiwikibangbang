// Package connector keeps catalog spaces in step with external sources
// of interfaces (ADR-0077). A connector, set up by an administrator in the
// site settings, reads its source on a schedule; every interface it finds
// becomes an item of the type interface, matched to the source by origin
// (ADR-0023) and held to the catalog rules of ADR-0074. What the catalog
// adds here — owner, review, notes — is kept by every run.
//
// The first kind of source is CKAN, as opendata.swiss runs it with the
// Swiss DCAT-AP profile (ADR-0075): every resource of a data set whose
// format counts as an interface — an API, a WMS, a SPARQL endpoint —
// becomes an entry. Its multilingual fields come as an object of
// languages or as text; this package reads both, so a change of
// representation costs a field, not the run.
package connector

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/site"
)

// maxAnswer bounds what is read of one answer of the CKAN API: a page of
// data sets with their resources.
const maxAnswer = 64 << 20

// pageRows is how many data sets one search request asks for.
const pageRows = 500

// maxDatasets bounds a run, so that a misbehaving source cannot make it
// endless: opendata.swiss holds some ten thousand data sets in all.
const maxDatasets = 50000

// Text is a multilingual text: an object of languages, or text alone.
type Text map[string]string

// UnmarshalJSON reads an object of languages, text, or text that holds
// such an object, as some fields of the portal do.
func (t *Text) UnmarshalJSON(data []byte) error {
	*t = Text{}
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		s = strings.TrimSpace(s)
		if strings.HasPrefix(s, "{") {
			var m map[string]any
			if json.Unmarshal([]byte(s), &m) == nil {
				t.fill(m)
				return nil
			}
		}
		if s != "" {
			(*t)[""] = s
		}
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err == nil {
		t.fill(m)
		return nil
	}
	return nil // anything else is no text
}

func (t Text) fill(m map[string]any) {
	for k, v := range m {
		if k == "url" {
			continue // a publisher's address is no name
		}
		switch x := v.(type) {
		case string:
			if x = strings.TrimSpace(x); x != "" {
				t[k] = x
			}
		case map[string]any:
			// e.g. a publisher {"name": {...}, "url": "..."}
			if k == "name" {
				for lang, n := range x {
					if s, ok := n.(string); ok && strings.TrimSpace(s) != "" {
						t[lang] = strings.TrimSpace(s)
					}
				}
			}
		}
	}
}

// Pick is the text in German, else English, French, Italian, else any.
func (t Text) Pick() string { return t.In("de") }

// In is the text in the language lang, else German, English, French,
// Italian, else any.
func (t Text) In(lang string) string {
	for _, lang := range []string{lang, "de", "en", "fr", "it", "", "name"} {
		if s := t[lang]; s != "" {
			return s
		}
	}
	for _, s := range t {
		return s
	}
	return ""
}

// Links is a list of web addresses that may come as one text, a list, or
// a list encoded as text.
type Links []string

// UnmarshalJSON reads a list or a text, keeping http and https addresses.
func (l *Links) UnmarshalJSON(data []byte) error {
	*l = nil
	var list []string
	if err := json.Unmarshal(data, &list); err != nil {
		var s string
		if json.Unmarshal(data, &s) != nil {
			return nil
		}
		if err := json.Unmarshal([]byte(s), &list); err != nil {
			list = []string{s}
		}
	}
	for _, s := range list {
		if s = strings.TrimSpace(s); webLink(s) {
			*l = append(*l, s)
		}
	}
	return nil
}

func webLink(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && len(s) <= 2048
}

// Contact is a contact point of a data set.
type Contact struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Contacts reads a list of contact points, or one.
type Contacts []Contact

// UnmarshalJSON reads a list or a single contact point.
func (c *Contacts) UnmarshalJSON(data []byte) error {
	var list []Contact
	if err := json.Unmarshal(data, &list); err == nil {
		*c = list
		return nil
	}
	var one Contact
	if err := json.Unmarshal(data, &one); err == nil {
		*c = Contacts{one}
	}
	return nil
}

// Organization is the publisher a data set belongs to.
type Organization struct {
	Name  string `json:"name"`
	Title Text   `json:"title"`
}

// Words is a list of words per language, such as keywords; a plain list
// counts as words without a language.
type Words map[string][]string

// UnmarshalJSON reads an object of languages with lists, a list, or a list
// of objects with a name (CKAN's tags).
func (w *Words) UnmarshalJSON(data []byte) error {
	*w = Words{}
	var byLang map[string][]string
	if json.Unmarshal(data, &byLang) == nil {
		for k, v := range byLang {
			(*w)[k] = v
		}
		return nil
	}
	var list []string
	if json.Unmarshal(data, &list) == nil {
		(*w)[""] = list
		return nil
	}
	var tags []struct {
		Name        string `json:"name"`
		DisplayName string `json:"display_name"`
	}
	if json.Unmarshal(data, &tags) == nil {
		for _, t := range tags {
			(*w)[""] = append((*w)[""], cmp.Or(t.DisplayName, t.Name))
		}
	}
	return nil
}

// In is the words in the language lang, else in the order of Text.In.
func (w Words) In(lang string) []string {
	for _, l := range []string{lang, "de", "en", "fr", "it", ""} {
		if len(w[l]) > 0 {
			return w[l]
		}
	}
	return nil
}

// Strings is a list of texts that may come as one text.
type Strings []string

// UnmarshalJSON reads a list of texts, or one; anything else is none.
func (s *Strings) UnmarshalJSON(data []byte) error {
	*s = nil
	var list []string
	if json.Unmarshal(data, &list) == nil {
		*s = list
		return nil
	}
	var one string
	if json.Unmarshal(data, &one) == nil && strings.TrimSpace(one) != "" {
		*s = Strings{one}
	}
	return nil
}

// Scalar is a value that may come as text or as a number.
type Scalar string

// UnmarshalJSON reads text, a number or a boolean as text.
func (v *Scalar) UnmarshalJSON(data []byte) error {
	var s string
	if json.Unmarshal(data, &s) == nil {
		*v = Scalar(s)
		return nil
	}
	var n json.Number
	if json.Unmarshal(data, &n) == nil {
		*v = Scalar(n.String())
		return nil
	}
	*v = ""
	return nil
}

// Named is a theme or a relation: a name, a title in languages, and an
// address.
type Named struct {
	Name  string `json:"name"`
	Label Text   `json:"label"`
	Title Text   `json:"title"`
	URL   string `json:"url"`
}

// Period is a temporal coverage.
type Period struct {
	Start Scalar `json:"start_date"`
	End   Scalar `json:"end_date"`
}

// Resource is one distribution of a data set.
type Resource struct {
	ID             string  `json:"id"`
	Title          Text    `json:"title"`
	Description    Text    `json:"description"`
	Format         string  `json:"format"`
	MediaType      string  `json:"media_type"`
	URL            string  `json:"url"`
	AccessURL      string  `json:"access_url"`
	DownloadURL    string  `json:"download_url"`
	Documentation  Links   `json:"documentation"`
	AccessServices Links   `json:"access_services"`
	Rights         Text    `json:"rights"`
	License        Text    `json:"license"`
	Language       Strings `json:"language"`
	ByteSize       Scalar  `json:"byte_size"`
	Issued         string  `json:"issued"`
	Modified       string  `json:"modified"`
}

// Dataset is a data set as package_search answers it. Extra holds every
// field of the answer this package does not read by name, so that a
// connector can show all of it (ADR-0080).
type Dataset struct {
	ID                 string                     `json:"id"`
	Name               string                     `json:"name"`
	Title              Text                       `json:"title"`
	Description        Text                       `json:"description"`
	Notes              Text                       `json:"notes"`
	Organization       *Organization              `json:"organization"`
	Publisher          Text                       `json:"publisher"`
	ContactPoints      Contacts                   `json:"contact_points"`
	Documentation      Links                      `json:"documentation"`
	Keywords           Words                      `json:"keywords"`
	Tags               Words                      `json:"tags"`
	Groups             []Named                    `json:"groups"`
	LicenseTitle       Text                       `json:"license_title"`
	Temporals          []Period                   `json:"temporals"`
	AccrualPeriodicity string                     `json:"accrual_periodicity"`
	Language           Strings                    `json:"language"`
	Spatial            Text                       `json:"spatial"`
	Relations          []Named                    `json:"relations"`
	LandingPage        string                     `json:"url"`
	Identifier         string                     `json:"identifier"`
	Issued             string                     `json:"issued"`
	Modified           string                     `json:"modified"`
	Resources          []Resource                 `json:"resources"`
	Extra              map[string]json.RawMessage `json:"-"`
}

// readByName are the fields of a data set that are read by name or are
// CKAN's own bookkeeping, and so not shown among the further details.
var readByName = []string{"id", "name", "title", "description", "notes", "organization", "publisher", "contact_points", "documentation",
	"keywords", "tags", "groups", "license_title", "license_id", "license_url", "temporals", "accrual_periodicity", "language", "spatial",
	"relations", "url", "identifier", "issued", "modified", "resources", "owner_org", "private", "state", "type", "num_resources", "num_tags",
	"revision_id", "metadata_created", "metadata_modified", "creator_user_id", "isopen", "extras", "relationships_as_object",
	"relationships_as_subject", "see_alsos", "qualified_relations", "version", "maintainer", "maintainer_email", "author", "author_email"}

// UnmarshalJSON reads the fields by name and keeps the others in Extra.
func (d *Dataset) UnmarshalJSON(data []byte) error {
	type plain Dataset
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return err
	}
	for _, k := range readByName {
		delete(all, k)
	}
	*d = Dataset(p)
	d.Extra = all
	return nil
}

// Client reads the CKAN action API.
type Client struct {
	BaseURL string
	// Token, if set, is sent as the API token CKAN expects.
	Token string
	HTTP  *http.Client
}

// NewClient returns a client of the API at base, sending token if one is
// given.
func NewClient(base, token string) Client {
	return Client{BaseURL: strings.TrimSuffix(base, "/"), Token: token, HTTP: &http.Client{
		Timeout: 2 * time.Minute,
		Transport: &http.Transport{
			Proxy:               http.ProxyFromEnvironment,
			DialContext:         (&net.Dialer{Timeout: 30 * time.Second}).DialContext,
			TLSHandshakeTimeout: 30 * time.Second,
		},
	}}
}

// orgQuery limits a search to organizations, or is empty.
func orgQuery(orgs []string) string {
	if len(orgs) == 0 {
		return ""
	}
	return "organization:(" + quote(orgs) + ")"
}

func quote(in []string) string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = strconv.Quote(s)
	}
	return strings.Join(out, " OR ")
}

// query is the search for data sets with a resource in one of the
// formats, of one of the organizations if any are named. The formats are
// the source's own values, as the search compares them exactly.
func query(formats, orgs []string) string {
	fq := "res_format:(" + quote(formats) + ")"
	if o := orgQuery(orgs); o != "" {
		fq += " AND " + o
	}
	return fq
}

// FormatCount is a resource format the source uses, with the number of
// data sets that have a resource in it.
type FormatCount struct {
	Format string `json:"format"`
	Count  int    `json:"count"`
}

// Formats returns the resource formats of the source's data sets, of the
// organizations if any are named, the most frequent first: what the
// source calls its formats, which a run's formats are matched against.
func (c Client) Formats(ctx context.Context, orgs []string) ([]FormatCount, error) {
	q := url.Values{"rows": {"0"}, "facet.field": {`["res_format"]`}, "facet.limit": {"-1"}}
	if o := orgQuery(orgs); o != "" {
		q.Set("fq", o)
	}
	var answer struct {
		Success bool `json:"success"`
		Error   *struct {
			Message string `json:"message"`
		} `json:"error"`
		Result struct {
			Facets       map[string]map[string]int `json:"facets"`
			SearchFacets map[string]struct {
				Items []struct {
					Name  string `json:"name"`
					Count int    `json:"count"`
				} `json:"items"`
			} `json:"search_facets"`
		} `json:"result"`
	}
	if err := c.get(ctx, "/package_search?"+q.Encode(), &answer); err != nil {
		return nil, err
	}
	if !answer.Success {
		return nil, failure(answer.Error)
	}
	counts := map[string]int{}
	for name, n := range answer.Result.Facets["res_format"] {
		counts[name] = n
	}
	for _, it := range answer.Result.SearchFacets["res_format"].Items {
		counts[it.Name] = max(counts[it.Name], it.Count)
	}
	out := []FormatCount{}
	for name, n := range counts {
		if strings.TrimSpace(name) != "" {
			out = append(out, FormatCount{Format: name, Count: n})
		}
	}
	slices.SortFunc(out, func(a, b FormatCount) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		return strings.Compare(a.Format, b.Format)
	})
	return out, nil
}

// Match picks the source's formats that the wanted ones name, compared
// without regard to case and with the usual spellings of publishers
// (aliases).
func Match(available []FormatCount, wanted []string) []string {
	want := map[string]bool{}
	for _, f := range wanted {
		want[normalFormat(f)] = true
	}
	var out []string
	for _, a := range available {
		if want[normalFormat(a.Format)] {
			out = append(out, a.Format)
		}
	}
	return out
}

// UnknownOrganizations returns the names among orgs that are no
// organization of the source. A source that does not list its
// organizations is taken at its word: the error is returned, and the
// caller goes on.
func (c Client) UnknownOrganizations(ctx context.Context, orgs []string) ([]string, error) {
	if len(orgs) == 0 {
		return nil, nil
	}
	var answer struct {
		Success bool `json:"success"`
		Error   *struct {
			Message string `json:"message"`
		} `json:"error"`
		Result []string `json:"result"`
	}
	if err := c.get(ctx, "/organization_list?"+url.Values{"limit": {"1000"}}.Encode(), &answer); err != nil {
		return nil, err
	}
	if !answer.Success {
		return nil, failure(answer.Error)
	}
	var unknown []string
	for _, o := range orgs {
		if !slices.Contains(answer.Result, o) {
			unknown = append(unknown, o)
		}
	}
	return unknown, nil
}

func failure(e *struct {
	Message string `json:"message"`
}) error {
	if e != nil && e.Message != "" {
		return errors.New("connector: " + e.Message)
	}
	return errors.New("connector: the source reports a failure")
}

// Datasets returns every data set with a resource in one of the formats,
// as the source writes them (see Formats and Match), of the organizations
// if any are named, in a stable order.
func (c Client) Datasets(ctx context.Context, formats, orgs []string) ([]Dataset, error) {
	if len(formats) == 0 {
		return nil, nil
	}
	var out []Dataset
	for start := 0; ; start += pageRows {
		q := url.Values{"fq": {query(formats, orgs)}, "rows": {strconv.Itoa(pageRows)}, "start": {strconv.Itoa(start)}, "sort": {"name asc"}}
		var answer struct {
			Success bool `json:"success"`
			Error   *struct {
				Message string `json:"message"`
			} `json:"error"`
			Result struct {
				Count   int       `json:"count"`
				Results []Dataset `json:"results"`
			} `json:"result"`
		}
		if err := c.get(ctx, "/package_search?"+q.Encode(), &answer); err != nil {
			return nil, err
		}
		if !answer.Success {
			return nil, failure(answer.Error)
		}
		out = append(out, answer.Result.Results...)
		switch {
		case answer.Result.Count > maxDatasets:
			return nil, fmt.Errorf("connector: the source reports %d data sets, more than the %d a run reads", answer.Result.Count, maxDatasets)
		case len(answer.Result.Results) == 0 || len(out) >= answer.Result.Count:
			return out, nil
		}
	}
}

func (c Client) get(ctx context.Context, path string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "wikiwikibangbang connector")
	if c.Token != "" {
		req.Header.Set("Authorization", c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("connector: %w", err)
	}
	defer resp.Body.Close()
	body := io.LimitReader(resp.Body, maxAnswer)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusConflict && resp.StatusCode != http.StatusBadRequest {
		return fmt.Errorf("connector: %s answered %s", c.BaseURL, resp.Status)
	}
	// CKAN answers a failed search with an error document, 400 or 409.
	if err := json.NewDecoder(body).Decode(v); err != nil {
		return fmt.Errorf("connector: reading the answer: %w", err)
	}
	return nil
}

// reading is what a connector read from its source: the entries, and
// what to report of how it went. apply is false when the run must not
// change the catalog, e.g. when no format was found to read.
type reading struct {
	apply    bool
	datasets int
	formats  []string
	entries  []Entry
	problems []string
}

func (rd *reading) problem(format string, args ...any) {
	if len(rd.problems) < problemsShown {
		rd.problems = append(rd.problems, fmt.Sprintf(format, args...))
	}
}

// readCKAN reads the interfaces of a CKAN source. The source's own
// spelling of the formats is asked first: its search compares them
// exactly, so "api" would not find "API". When the search finds data
// sets but none of their resources is an interface, or finds none where
// the source counts some, the reading says so with what it saw, as the
// cause is then in the source's data, not in the settings.
func readCKAN(ctx context.Context, c site.Connector, client Client) (reading, error) {
	var rd reading
	k := c.CKAN
	// Publishers are named by their short names in the source; a name the
	// source does not know would leave the search empty without a word.
	if unknown, err := client.UnknownOrganizations(ctx, k.Organizations); err == nil && len(unknown) > 0 {
		rd.problem("the source knows no organization %s; name publishers as the source's addresses do, e.g. bundesamt-fur-statistik-bfs, or leave the list empty for all",
			strings.Join(unknown, ", "))
		if len(unknown) == len(k.Organizations) {
			return rd, nil
		}
	}
	available, err := client.Formats(ctx, k.Organizations)
	if err != nil {
		return rd, err
	}
	rd.formats = Match(available, k.Formats)
	if len(rd.formats) == 0 {
		rd.problem("no format of the source is in the list %s; the source's formats are %s — choose the ones that are interfaces",
			strings.Join(k.Formats, ", "), describe(available, 25))
		return rd, nil
	}
	counted := 0
	for _, a := range available {
		if slices.Contains(rd.formats, a.Format) {
			counted += a.Count
		}
	}
	datasets, err := client.Datasets(ctx, rd.formats, k.Organizations)
	if err != nil {
		return rd, err
	}
	rd.apply, rd.datasets = true, len(datasets)
	var problems []string
	rd.entries, problems = Entries(datasets, k.Formats, k.PortalURL, cmp.Or(k.Language, "de"), c.Name)
	for _, p := range problems {
		rd.problem("%s", p)
	}
	switch {
	case len(datasets) == 0 && counted > 0:
		rd.problem("the source counts %d data sets in the formats %s, but its search for them answered none", counted, strings.Join(rd.formats, ", "))
	case len(datasets) > 0 && len(rd.entries) == 0:
		seen := map[string]int{}
		for _, d := range datasets {
			for _, r := range d.Resources {
				seen[r.Format]++
			}
		}
		var fs []FormatCount
		for f, n := range seen {
			if strings.TrimSpace(f) == "" {
				f = "(no format)"
			}
			fs = append(fs, FormatCount{Format: f, Count: n})
		}
		slices.SortFunc(fs, func(a, b FormatCount) int { return cmp.Or(b.Count-a.Count, strings.Compare(a.Format, b.Format)) })
		rd.problem("the search found %d data sets, but none of their resources has a format of the list %s; their resources' formats are %s",
			len(datasets), strings.Join(k.Formats, ", "), describe(fs, 25))
	}
	return rd, nil
}
