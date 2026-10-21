package connector

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

// Entry is one data set with interfaces, as the source describes it: one
// page in the catalog (ADR-0080).
type Entry struct {
	// SourceID is the data set's ID, which identifies the entry.
	SourceID string
	Dataset  string // the data set's name
	Title    string
	// Format lists the formats of its interfaces, e.g. "WMS, WFS".
	Format        string
	Endpoint      string
	Specification string
	Publisher     string
	Contact       string
	DatasetURL    string
	Modified      time.Time
	// Body is the page written from everything the source says of the
	// data set; every run replaces it.
	Body []item.Block
	// Resources are the IDs of its interfaces, which named entries of
	// their own before ADR-0080.
	Resources []string
}

// aliases map formats as publishers write them to the portal's names.
var aliases = map[string]string{
	"REST": "API", "REST API": "API", "OGC:WMS": "WMS", "WMS_SRVC": "WMS", "OGC:WFS": "WFS", "WFS_SRVC": "WFS",
	"WCS_SRVC": "WCS", "WMTS_SRVC": "WMTS", "SPARQL-QUERY": "SPARQL", "SPARQLQ": "SPARQL",
}

func normalFormat(f string) string {
	f = strings.ToUpper(strings.TrimSpace(f))
	if a, ok := aliases[f]; ok {
		return a
	}
	return f
}

// Entries lists the data sets that have interfaces — resources in one of
// the formats — as entries, one each. portal prefixes a data set's name to
// link to its page; lang is the language its page is written in.
// Problems name what was left out and why.
func Entries(datasets []Dataset, formats []string, portal, lang, source string) ([]Entry, []string) {
	want := map[string]bool{}
	for _, f := range formats {
		want[normalFormat(f)] = true
	}
	var out []Entry
	var problems []string
	seen := map[string]bool{}
	for _, d := range datasets {
		var apis, others []Resource
		for _, r := range d.Resources {
			if want[normalFormat(r.Format)] {
				apis = append(apis, r)
			} else {
				others = append(others, r)
			}
		}
		if len(apis) == 0 {
			continue
		}
		if d.ID == "" || seen[d.ID] {
			problems = append(problems, fmt.Sprintf("%s: a data set without an ID of its own is left out", d.Name))
			continue
		}
		seen[d.ID] = true
		e := Entry{SourceID: d.ID, Dataset: d.Name, Title: d.Title.In(lang)}
		if e.Title == "" {
			e.Title = d.Name
		}
		var fs []string
		for _, r := range apis {
			if f := normalFormat(r.Format); !slices.Contains(fs, f) {
				fs = append(fs, f)
			}
			if r.ID != "" {
				e.Resources = append(e.Resources, r.ID)
			}
			if e.Endpoint == "" {
				e.Endpoint = address(r)
			}
			if e.Specification == "" {
				if docs := slices.Concat([]string(r.Documentation), []string(r.AccessServices)); len(docs) > 0 {
					e.Specification = docs[0]
				}
			}
			if m := parseDate(r.Modified); m.After(e.Modified) {
				e.Modified = m
			}
		}
		e.Format = strings.Join(fs, ", ")
		if e.Specification == "" && len(d.Documentation) > 0 {
			e.Specification = d.Documentation[0]
		}
		if d.Organization != nil {
			e.Publisher = cmp.Or(d.Organization.Title.In(lang), d.Organization.Name)
		}
		if e.Publisher == "" {
			e.Publisher = d.Publisher.In(lang)
		}
		e.Contact = contacts(d)
		if d.Name != "" && webLink(portal+d.Name) {
			e.DatasetURL = portal + d.Name
		}
		if m := parseDate(d.Modified); m.After(e.Modified) {
			e.Modified = m
		}
		e.Body = page(d, apis, others, e, lang, source)
		out = append(out, e)
	}
	return out, problems
}

// address is where a resource is reached: its address, else its access or
// download address.
func address(r Resource) string {
	for _, u := range []string{r.URL, r.AccessURL, r.DownloadURL} {
		if webLink(u) {
			return u
		}
	}
	return ""
}

func contacts(d Dataset) string {
	var out []string
	for _, c := range d.ContactPoints {
		switch name, mail := strings.TrimSpace(c.Name), strings.TrimSpace(c.Email); {
		case name != "" && mail != "":
			out = append(out, name+" <"+mail+">")
		case name+mail != "":
			out = append(out, name+mail)
		}
	}
	return strings.Join(out, "; ")
}

// parseDate reads the dates the portal gives: ISO 8601 with or without a
// time, or DD.MM.YYYY; the day is kept.
func parseDate(s string) time.Time {
	s = strings.TrimSpace(s)
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05.999999", "2006-01-02T15:04:05", time.DateOnly, "02.01.2006"} {
		if t, err := time.Parse(layout, s); err == nil {
			return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
		}
	}
	return time.Time{}
}
