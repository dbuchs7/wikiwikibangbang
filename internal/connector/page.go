package connector

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

// labels are the words a generated page uses, by language (ADR-0080).
// English is the source; a language without its own words uses it.
type labels struct {
	generated, interfaces, dataset, others, further, field, value                     string
	format, mediaType, license, rights, languages, size, issued, modified, resourceID string
	address, access, download, documentation, accessService                           string
	publisher, contact, keywords, themes, temporal, frequency, spatial, identifier    string
	datasetID, page, landing, relation                                                string
	withdrawn                                                                         string
	// entry links an entry of a JSON source to its page there (ADR-0127).
	entry string
	// fromGit heads a page written from a Git repository (ADR-0136).
	fromGit string
}

var pageLabels = map[string]labels{
	"en": {
		generated:  "This page is written from %s by every sync and replaced by the next one. Add notes as comments: they stay.",
		interfaces: "Interfaces", dataset: "Data set", others: "Other resources", further: "Further details from the source",
		field: "Field", value: "Value",
		format: "Format", mediaType: "Media type", license: "Licence", rights: "Terms of use", languages: "Languages",
		size: "Size in bytes", issued: "Published", modified: "Modified", resourceID: "Resource ID",
		address: "Address", access: "Access", download: "Download", documentation: "Documentation", accessService: "Access service",
		publisher: "Publisher", contact: "Contact", keywords: "Keywords", themes: "Themes", temporal: "Period covered",
		frequency: "Update frequency", spatial: "Area", identifier: "Identifier", datasetID: "Data set ID",
		page: "Data set in the source", landing: "Landing page", relation: "Related", entry: "Entry in the source",
		withdrawn: "The source no longer publishes this entry. The page stays, with what was added here; it is no longer kept in step.",
		fromGit:   "This page is written from %s whenever it changes there. Add notes as comments: they stay.",
	},
	"de": {
		generated:  "Diese Seite wird bei jedem Abgleich aus %s geschrieben und vom nächsten ersetzt. Anmerkungen bitte als Kommentar: Kommentare bleiben erhalten.",
		interfaces: "Schnittstellen", dataset: "Datensatz", others: "Weitere Ressourcen", further: "Weitere Angaben der Quelle",
		field: "Angabe", value: "Wert",
		format: "Format", mediaType: "Medientyp", license: "Lizenz", rights: "Nutzungsbedingungen", languages: "Sprachen",
		size: "Grösse in Bytes", issued: "Publiziert", modified: "Geändert", resourceID: "Ressourcen-ID",
		address: "Adresse", access: "Zugang", download: "Download", documentation: "Dokumentation", accessService: "Zugangsdienst",
		publisher: "Herausgeber", contact: "Kontakt", keywords: "Stichwörter", themes: "Themen", temporal: "Zeitraum",
		frequency: "Aktualisierung", spatial: "Raum", identifier: "Kennung", datasetID: "Datensatz-ID",
		page: "Datensatz in der Quelle", landing: "Einstiegsseite", relation: "Verwandt", entry: "Eintrag in der Quelle",
		withdrawn: "Die Quelle publiziert diesen Eintrag nicht mehr. Die Seite bleibt mit allem, was hier ergänzt wurde, wird aber nicht mehr abgeglichen.",
		fromGit:   "Diese Seite wird bei jeder Änderung aus %s geschrieben. Anmerkungen bitte als Kommentar: Kommentare bleiben erhalten.",
	},
}

func wordsIn(lang string) labels {
	if l, ok := pageLabels[lang]; ok {
		return l
	}
	return pageLabels["en"]
}

// maxCell bounds the text of one table cell or one further detail, so a
// misbehaving source cannot fill a page with one value.
const maxCell = 2000

// doc builds a page's blocks.
type doc struct{ blocks []item.Block }

func (b *doc) add(k item.BlockKind, text string) {
	b.blocks = append(b.blocks, item.Block{Kind: k, Text: text})
}

func (b *doc) heading(level int, text string) {
	b.blocks = append(b.blocks, item.Block{Kind: item.Heading, Level: level, Text: oneLine(text)})
}

// paragraphs adds a text, one paragraph per line that is not empty.
func (b *doc) paragraphs(text string) {
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r", ""), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			b.add(item.Paragraph, clip(line))
		}
	}
}

// table adds the facts that have a value, under a header row.
func (b *doc) table(w labels, facts [][2]string) {
	rows := []string{w.field + "\t" + w.value}
	for _, f := range facts {
		if v := oneLine(f[1]); v != "" {
			rows = append(rows, oneLine(f[0])+"\t"+v)
		}
	}
	if len(rows) > 1 {
		b.add(item.Table, strings.Join(rows, "\n"))
	}
}

// links adds one bullet per web address, the address linked.
func (b *doc) links(links [][2]string) {
	for _, l := range links {
		label, href := oneLine(l[0]), strings.TrimSpace(l[1])
		if href == "" || !webLink(href) || !item.SafeHref(href) {
			continue
		}
		text := label + ": " + href
		start := utf8.RuneCountInString(label) + 2
		b.blocks = append(b.blocks, item.Block{Kind: item.BulletItem, Text: text,
			Marks: []item.Mark{{Type: item.MarkLink, Start: start, End: utf8.RuneCountInString(text), Href: href}}})
	}
}

// oneLine is a value as one line of at most maxCell characters, fit for a
// table cell.
func oneLine(s string) string {
	return clip(strings.Join(strings.Fields(s), " "))
}

func clip(s string) string {
	if utf8.RuneCountInString(s) <= maxCell {
		return s
	}
	return string([]rune(s)[:maxCell-1]) + "…"
}

// day is a date as the day it names, or the text as given.
func day(s string) string {
	if t := parseDate(s); !t.IsZero() {
		return t.Format("2006-01-02")
	}
	return s
}

// lastSegment names a value given as an address by its last part, e.g. a
// frequency .../frequency/ANNUAL as ANNUAL.
func lastSegment(s string) string {
	if webLink(s) {
		if i := strings.LastIndex(strings.TrimRight(s, "/"), "/"); i >= 0 {
			return strings.TrimRight(s, "/")[i+1:]
		}
	}
	return s
}

// page writes everything the source says of a data set with interfaces:
// what it is, each interface, the data set's details, its other
// resources, and every further field of the answer (ADR-0080).
func page(d Dataset, apis, others []Resource, e Entry, lang, source string) []item.Block {
	w := wordsIn(lang)
	var b doc
	b.blocks = append(b.blocks, item.Block{Kind: item.Panel, Tone: "info", Text: fmt.Sprintf(w.generated, source)})
	b.paragraphs(cmp.Or(d.Description.In(lang), d.Notes.In(lang)))

	b.heading(2, w.interfaces)
	for _, r := range apis {
		b.heading(3, cmp.Or(r.Title.In(lang), normalFormat(r.Format)))
		b.paragraphs(r.Description.In(lang))
		b.table(w, [][2]string{
			{w.format, r.Format}, {w.mediaType, r.MediaType}, {w.license, r.License.In(lang)}, {w.rights, r.Rights.In(lang)},
			{w.languages, strings.Join(r.Language, ", ")}, {w.size, string(r.ByteSize)}, {w.issued, day(r.Issued)},
			{w.modified, day(r.Modified)}, {w.resourceID, r.ID},
		})
		links := [][2]string{{w.address, r.URL}, {w.access, r.AccessURL}, {w.download, r.DownloadURL}}
		for _, u := range r.Documentation {
			links = append(links, [2]string{w.documentation, u})
		}
		for _, u := range r.AccessServices {
			links = append(links, [2]string{w.accessService, u})
		}
		b.links(dedupe(links))
	}

	b.heading(2, w.dataset)
	var themes, periods []string
	for _, g := range d.Groups {
		if t := cmp.Or(g.Title.In(lang), g.Label.In(lang), g.Name); t != "" {
			themes = append(themes, t)
		}
	}
	for _, p := range d.Temporals {
		if p.Start != "" || p.End != "" {
			periods = append(periods, strings.TrimSpace(day(string(p.Start))+" – "+day(string(p.End))))
		}
	}
	b.table(w, [][2]string{
		{w.publisher, e.Publisher}, {w.contact, e.Contact},
		{w.keywords, strings.Join(slices.Concat(d.Keywords.In(lang), d.Tags.In(lang)), ", ")},
		{w.themes, strings.Join(themes, ", ")}, {w.license, d.LicenseTitle.In(lang)}, {w.temporal, strings.Join(periods, "; ")},
		{w.frequency, lastSegment(d.AccrualPeriodicity)}, {w.issued, day(d.Issued)}, {w.modified, day(d.Modified)},
		{w.languages, strings.Join(d.Language, ", ")}, {w.spatial, d.Spatial.In(lang)}, {w.identifier, d.Identifier},
		{w.datasetID, d.ID},
	})
	links := [][2]string{{w.page, e.DatasetURL}, {w.landing, d.LandingPage}}
	for _, u := range d.Documentation {
		links = append(links, [2]string{w.documentation, u})
	}
	for _, r := range d.Relations {
		links = append(links, [2]string{cmp.Or(r.Label.In(lang), r.Title.In(lang), w.relation), r.URL})
	}
	b.links(dedupe(links))

	var more [][2]string
	for _, r := range others {
		more = append(more, [2]string{cmp.Or(r.Title.In(lang), r.ID) + " (" + cmp.Or(r.Format, "?") + ")", address(r)})
	}
	if len(more) > 0 {
		before := len(b.blocks)
		b.heading(2, w.others)
		b.links(more)
		if len(b.blocks) == before+1 {
			b.blocks = b.blocks[:before]
		}
	}

	var further [][2]string
	for _, k := range slices.Sorted(maps.Keys(d.Extra)) {
		further = append(further, [2]string{k, rawText(d.Extra[k], lang)})
	}
	if before := len(b.blocks); len(further) > 0 {
		b.heading(2, w.further)
		b.table(w, further)
		if len(b.blocks) == before+1 {
			b.blocks = b.blocks[:before]
		}
	}
	return b.blocks
}

// dedupe drops a link whose address came before.
func dedupe(links [][2]string) [][2]string {
	var out [][2]string
	seen := map[string]bool{}
	for _, l := range links {
		if u := strings.TrimSpace(l[1]); u != "" && !seen[u] {
			seen[u] = true
			out = append(out, l)
		}
	}
	return out
}

// rawText shows a further field of the answer as text: text and numbers
// as they are, languages in the page's language, lists joined, anything
// else as compact JSON.
func rawText(raw json.RawMessage, lang string) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var t Text
	if json.Unmarshal(raw, &t) == nil && len(t) > 0 {
		if v := t.In(lang); v != "" {
			return v
		}
	}
	var list []string
	if json.Unmarshal(raw, &list) == nil {
		return strings.Join(list, ", ")
	}
	switch v := strings.TrimSpace(string(raw)); v {
	case "null", "[]", "{}", `""`:
		return ""
	default:
		return v
	}
}
