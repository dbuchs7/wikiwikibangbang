// Package opendata mirrors the interfaces published on opendata.swiss
// into a catalog space (ADR-0075): every resource of a data set whose
// format counts as an interface — an API, a WMS, a SPARQL endpoint —
// becomes an item of the type interface, matched to its source by origin
// (ADR-0023) and held to the catalog rules of ADR-0074.
//
// The portal runs CKAN with the Swiss DCAT-AP profile. Its multilingual
// fields come as an object of languages or as text; this package reads
// both, so a change of representation costs a field, not the run.
package opendata

import (
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
func (t Text) Pick() string {
	for _, lang := range []string{"de", "en", "fr", "it", "", "name"} {
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

// Resource is one distribution of a data set.
type Resource struct {
	ID             string `json:"id"`
	Title          Text   `json:"title"`
	Format         string `json:"format"`
	URL            string `json:"url"`
	DownloadURL    string `json:"download_url"`
	Documentation  Links  `json:"documentation"`
	AccessServices Links  `json:"access_services"`
	Modified       string `json:"modified"`
}

// Dataset is a data set as package_search answers it.
type Dataset struct {
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	Title         Text          `json:"title"`
	Organization  *Organization `json:"organization"`
	Publisher     Text          `json:"publisher"`
	ContactPoints Contacts      `json:"contact_points"`
	Documentation Links         `json:"documentation"`
	Modified      string        `json:"modified"`
	Resources     []Resource    `json:"resources"`
}

// Client reads the CKAN action API.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// NewClient returns a client of the API at base.
func NewClient(base string) Client {
	return Client{BaseURL: strings.TrimSuffix(base, "/"), HTTP: &http.Client{
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

func failure(e *struct {
	Message string `json:"message"`
}) error {
	if e != nil && e.Message != "" {
		return errors.New("opendata: " + e.Message)
	}
	return errors.New("opendata: the source reports a failure")
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
			return nil, fmt.Errorf("opendata: the source reports %d data sets, more than the %d a run reads", answer.Result.Count, maxDatasets)
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
	req.Header.Set("User-Agent", "wikiwikibangbang catalog sync")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("opendata: %w", err)
	}
	defer resp.Body.Close()
	body := io.LimitReader(resp.Body, maxAnswer)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusConflict && resp.StatusCode != http.StatusBadRequest {
		return fmt.Errorf("opendata: %s answered %s", c.BaseURL, resp.Status)
	}
	// CKAN answers a failed search with an error document, 400 or 409.
	if err := json.NewDecoder(body).Decode(v); err != nil {
		return fmt.Errorf("opendata: reading the answer: %w", err)
	}
	return nil
}
