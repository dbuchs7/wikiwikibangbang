package site

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// OpenData is the synchronisation of a catalog of interfaces with
// opendata.swiss (ADR-0075): which space it fills, from where, which
// resource formats count as interfaces, and how often it runs.
type OpenData struct {
	Enabled bool   `json:"enabled"`
	Space   string `json:"space"`
	// BaseURL is the CKAN action API, e.g.
	// https://ckan.opendata.swiss/api/3/action.
	BaseURL string `json:"baseURL"`
	// PortalURL prefixes a data set's name to link to its page, the entry
	// in the system of record.
	PortalURL string `json:"portalURL"`
	// Formats are the resource formats that count as an interface.
	Formats []string `json:"formats"`
	// Organizations, if any, limit the synchronisation to these
	// publishers, by their names on opendata.swiss.
	Organizations []string `json:"organizations"`
	IntervalHours int      `json:"intervalHours"`
	// Steward, a user ID, becomes the owner of new entries until someone
	// else is set; empty leaves them without one.
	Steward string `json:"steward"`
}

// DefaultOpenData is the synchronisation until an administrator sets one.
var DefaultOpenData = OpenData{
	BaseURL:       "https://ckan.opendata.swiss/api/3/action",
	PortalURL:     "https://opendata.swiss/de/dataset/",
	Formats:       []string{"API", "WMS", "WFS", "WCS", "WMTS", "SPARQL"},
	Organizations: []string{},
	IntervalHours: 24,
}

var formatRE = regexp.MustCompile(`^[A-Z0-9][A-Z0-9 _.:/-]{0,39}$`)

var orgRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,99}$`)

// Validate checks the synchronisation's settings.
func (o OpenData) Validate() error {
	for _, u := range []string{o.BaseURL, o.PortalURL} {
		p, err := url.Parse(u)
		if err != nil || (p.Scheme != "https" && p.Scheme != "http") || p.Host == "" || p.User != nil || len(u) > 512 {
			return fmt.Errorf("%w: %q is not an http or https address", ErrInvalid, u)
		}
	}
	switch {
	case o.Enabled && o.Space == "":
		return fmt.Errorf("%w: the synchronisation needs a space", ErrInvalid)
	case len(o.Formats) == 0 || len(o.Formats) > 30:
		return fmt.Errorf("%w: name 1 to 30 formats", ErrInvalid)
	case len(o.Organizations) > 200:
		return fmt.Errorf("%w: name at most 200 organizations", ErrInvalid)
	case o.IntervalHours < 1 || o.IntervalHours > 24*31:
		return fmt.Errorf("%w: the synchronisation runs every 1 to 744 hours", ErrInvalid)
	}
	for _, f := range o.Formats {
		if !formatRE.MatchString(f) {
			return fmt.Errorf("%w: %q is not a format in capitals, e.g. WMS", ErrInvalid, f)
		}
	}
	for _, org := range o.Organizations {
		if !orgRE.MatchString(org) {
			return fmt.Errorf("%w: %q is not an organization's name, e.g. bundesamt-fur-statistik-bfs", ErrInvalid, org)
		}
	}
	return nil
}

// normalize tidies what an administrator typed: formats in capitals,
// both lists without blanks and duplicates.
func (o OpenData) normalize() OpenData {
	clean := func(in []string, f func(string) string) []string {
		out := []string{}
		for _, s := range in {
			if s = f(strings.TrimSpace(s)); s != "" && !slices.Contains(out, s) {
				out = append(out, s)
			}
		}
		return out
	}
	o.Formats = clean(o.Formats, strings.ToUpper)
	o.Organizations = clean(o.Organizations, strings.ToLower)
	o.BaseURL, o.PortalURL, o.Space = strings.TrimSpace(o.BaseURL), strings.TrimSpace(o.PortalURL), strings.TrimSpace(o.Space)
	return o
}

// OpenData returns the synchronisation with opendata.swiss.
func (s *Settings) OpenData() OpenData {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.openData == nil {
		o := DefaultOpenData
		o.Formats = slices.Clone(o.Formats)
		return o
	}
	o := *s.openData
	o.Formats, o.Organizations = slices.Clone(o.Formats), slices.Clone(o.Organizations)
	return o
}

// SetOpenData replaces the synchronisation with opendata.swiss and
// returns it as kept.
func (s *Settings) SetOpenData(actor string, o OpenData) (OpenData, error) {
	o = o.normalize()
	if err := o.Validate(); err != nil {
		return o, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return o, s.record(event{Kind: openDataSet, Actor: actor, OpenData: &o})
}
