package connector

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

// Entry is one entry of a source, in the form every reader produces
// (ADR-0127): one item in the connector's space. For a CKAN source it is a
// data set with interfaces (ADR-0080).
type Entry struct {
	// SourceID is the data set's ID, which identifies the entry.
	SourceID string
	Dataset  string // the data set's name
	Title    string
	// Publisher, Contact and DatasetURL head its page.
	Publisher  string
	Contact    string
	DatasetURL string
	// Body is the page written from everything the source says of the
	// data set; every run replaces it.
	Body []item.Block
	// Values are what the source gives of the entry by name, for the
	// connector's mapping to the fields of its type (ADR-0127).
	Values map[string]string

	// The fields below serve sources whose entries form a tree and whose
	// content is read only when it changed: a Git repository's Markdown
	// files (ADR-0138).

	// Parent is the SourceID of the entry whose page holds this one's
	// page; empty puts it at the top of the space. Only a new page is
	// placed: people may move a page, and a run leaves it there.
	Parent string
	// Version is the source's version the entry was read at, e.g. a
	// commit; it is kept as the origin's version when the content changed.
	Version string
	// Blob identifies the content in the source without reading it, e.g.
	// a Git blob ID; a page whose kept raw content has this ID is
	// unchanged. Empty means the content must be read to tell.
	Blob string
	// Load reads the entry's content: its title, its page, the raw
	// content kept in the origin, and its pictures. Apply calls it for a
	// new entry, or when Blob does not tell that the content is unchanged;
	// nil means Title and Body are the content.
	Load func() (Content, error)
}

// ErrLeftOut is an entry Load leaves out for what it is, e.g. a file too
// large: reading it again changes nothing until the source changes, so a
// run that left it out is still remembered as having read the source.
var ErrLeftOut = errors.New("connector: left out")

// leftOut is an entry left out for what it is (ErrLeftOut), saying why.
type leftOut struct{ why string }

func (e leftOut) Error() string      { return e.why }
func (leftOut) Is(target error) bool { return target == ErrLeftOut }

// Content is an entry's content as Load reads it.
type Content struct {
	Title string
	Body  []item.Block
	// Raw is the source's own text, kept in the origin (sealed, ADR-0026)
	// to tell a changed source from text people edited here.
	Raw string
	// Pictures are files the page shows: each replaces the block at its
	// index with an image of it once it is attached.
	Pictures []Picture
	// Links map the addresses the page links to that are other entries of
	// the same source to their SourceIDs: such a link becomes a mention of
	// the entry's page (D2).
	Links map[string]string
}

// Picture is a file a page shows, read from the source.
type Picture struct {
	Block   int
	Name    string
	Caption string
	Data    []byte
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
		e.Body = page(d, apis, others, e, lang, source)
		e.Values = ckanValues(e, apis)
		out = append(out, e)
	}
	return out, problems
}

// ckanValues are a CKAN entry's values by name (site.CKANValues), those
// it has.
func ckanValues(e Entry, apis []Resource) map[string]string {
	var formats []string
	for _, r := range apis {
		if f := normalFormat(r.Format); !slices.Contains(formats, f) {
			formats = append(formats, f)
		}
	}
	slices.Sort(formats)
	v := map[string]string{"dataset": e.Dataset, "publisher": e.Publisher, "contact": e.Contact, "url": e.DatasetURL, "formats": strings.Join(formats, ", ")}
	maps.DeleteFunc(v, func(_, s string) bool { return s == "" })
	return v
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
