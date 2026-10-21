package connector

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// Entry is one interface as the source describes it.
type Entry struct {
	// SourceID is the resource's ID, which identifies the entry.
	SourceID      string
	Dataset       string // the data set's name
	Title         string
	Format        string
	Endpoint      string
	Specification string
	Publisher     string
	Contact       string
	DatasetURL    string
	Modified      time.Time
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

// Entries lists the interfaces among data sets: each resource in one of
// the formats with an address. portal prefixes a data set's name to link
// to its page. Problems name what was left out and why.
func Entries(datasets []Dataset, formats []string, portal string) ([]Entry, []string) {
	want := map[string]bool{}
	for _, f := range formats {
		want[normalFormat(f)] = true
	}
	var out []Entry
	var problems []string
	seen := map[string]bool{}
	for _, d := range datasets {
		var apis []Resource
		for _, r := range d.Resources {
			if want[normalFormat(r.Format)] {
				apis = append(apis, r)
			}
		}
		for _, r := range apis {
			if r.ID == "" || seen[r.ID] {
				problems = append(problems, fmt.Sprintf("%s: a resource without an ID of its own is left out", d.Name))
				continue
			}
			seen[r.ID] = true
			e := Entry{SourceID: r.ID, Dataset: d.Name, Format: normalFormat(r.Format), Title: d.Title.Pick()}
			if e.Title == "" {
				e.Title = d.Name
			}
			if len(apis) > 1 {
				label := r.Title.Pick()
				if label == "" || label == e.Title {
					label = e.Format
				}
				e.Title += " – " + label
			}
			switch {
			case webLink(r.URL):
				e.Endpoint = r.URL
			case webLink(r.DownloadURL):
				e.Endpoint = r.DownloadURL
			}
			if docs := slices.Concat([]string(r.Documentation), []string(d.Documentation), []string(r.AccessServices)); len(docs) > 0 {
				e.Specification = docs[0]
			}
			if d.Organization != nil {
				e.Publisher = d.Organization.Title.Pick()
				if e.Publisher == "" {
					e.Publisher = d.Organization.Name
				}
			}
			if e.Publisher == "" {
				e.Publisher = d.Publisher.Pick()
			}
			var contacts []string
			for _, c := range d.ContactPoints {
				switch name, mail := strings.TrimSpace(c.Name), strings.TrimSpace(c.Email); {
				case name != "" && mail != "":
					contacts = append(contacts, name+" <"+mail+">")
				case name+mail != "":
					contacts = append(contacts, name+mail)
				}
			}
			e.Contact = strings.Join(contacts, "; ")
			if d.Name != "" && webLink(portal+d.Name) {
				e.DatasetURL = portal + d.Name
			}
			e.Modified = parseDate(r.Modified)
			if e.Modified.IsZero() {
				e.Modified = parseDate(d.Modified)
			}
			out = append(out, e)
		}
	}
	return out, problems
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
