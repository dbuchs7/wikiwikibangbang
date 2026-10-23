package server

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/auth"
	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
	"github.com/dbuchs7/wikiwikibangbang/internal/export"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

// exportRoutes export pages and lists (ADR-0071): a page as a Word
// document, its tables and lists of items as CSV. A PDF is the browser's:
// the UI prints a page without its surroundings.
func exportRoutes(h itemHandlers) []Route {
	return []Route{
		{Method: http.MethodGet, Pattern: "/api/items/{ref}/export/docx", Summary: "The item as a Word document: title, properties and body with its pictures and the items it embeds; children=true adds every page below it, as a handbook", Handler: http.HandlerFunc(h.exportDOCX)},
		{Method: http.MethodGet, Pattern: "/api/items/{ref}/export/html", Summary: "The item as a page to print as PDF, with what it embeds; children=true adds every page below it, as a handbook", Handler: http.HandlerFunc(h.exportHTML)},
		{Method: http.MethodGet, Pattern: "/api/items/{ref}/tables/{n}", Summary: "The item's n-th table (from 1) as CSV", Handler: http.HandlerFunc(h.exportTable)},
		{Method: http.MethodGet, Pattern: "/api/items.csv", Summary: "Readable items as CSV, with the filters of /api/items: key, title, space, type, state, labels and every property", Handler: http.HandlerFunc(h.exportItems)},
		{Method: http.MethodGet, Pattern: "/api/search.csv", Summary: "A full-text search as CSV (q; space, type, state, label), best first", Handler: http.HandlerFunc(h.exportSearch)},
	}
}

// exportWords are the labels of an export in each UI language.
var exportWords = map[string]map[string]string{
	"de": {"key": "Schlüssel", "title": "Titel", "space": "Bereich", "type": "Typ", "state": "Status", "labels": "Labels", "restricted": "(eingeschränkt)", "table": "Tabelle", "contents": "Inhalt"},
	"en": {"key": "Key", "title": "Title", "space": "Space", "type": "Type", "state": "State", "labels": "Labels", "restricted": "(restricted)", "table": "table", "contents": "Contents"},
}

func (h itemHandlers) lang(r *http.Request) string {
	if l := h.d.DB.Accounts.Language(auth.UserID(who(r))); exportWords[l] != nil {
		return l
	}
	return "en"
}

// attach sends a file to be saved under name.
func attach(w http.ResponseWriter, contentType, name string) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	w.Header().Set("Cache-Control", "no-store")
}

// fileName makes a file name of a key and a title.
func fileName(key, title, ext string) string {
	clean := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) || r < 0x20 {
			return -1
		}
		return r
	}, title)
	if len([]rune(clean)) > 80 {
		clean = string([]rune(clean)[:80])
	}
	return strings.TrimSpace(key+" "+strings.TrimSpace(clean)) + ext
}

// propertyText writes a property's value for a reader: user IDs as names.
func propertyText(v any, kind item.FieldKind, names map[auth.UserID]string) string {
	switch {
	case v == nil:
		return ""
	case kind == item.KindUser:
		if n, ok := names[auth.UserID(fmt.Sprint(v))]; ok {
			return n
		}
	}
	switch x := v.(type) {
	case bool:
		return strconv.FormatBool(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case time.Time:
		return x.Format("2006-01-02")
	}
	return fmt.Sprint(v)
}

// userIDs collects the user IDs among items' user properties.
func userIDs(items []item.Item, types func(string) item.TypeDef) []auth.UserID {
	var ids []auth.UserID
	for _, it := range items {
		for _, f := range types(it.Type).Fields {
			if v, ok := it.Properties[f.Name]; ok && f.Kind == item.KindUser && v != nil {
				ids = append(ids, auth.UserID(fmt.Sprint(v)))
			}
		}
	}
	return ids
}

// exportDocument gathers what a page export writes, through the reader's
// session (D10): the page, with children its whole subtree as a handbook,
// the items their bodies embed as far as the reader may read them, and
// their pictures, read as files only when withData is set.
func (h itemHandlers) exportDocument(r *http.Request, children, withData bool) (export.Document, item.Item, error) {
	words := exportWords[h.lang(r)]
	doc := export.Document{Embeds: map[item.ID]export.Page{}, Restricted: words["restricted"]}
	var root item.Item
	type file struct {
		owner item.ID
		a     item.Attachment
	}
	var files []file
	var err error
	h.d.DB.Read(func(s *item.Store) {
		x := s.As(who(r))
		if root, err = resolveItem(x, r.PathValue("ref")); err != nil {
			return
		}
		limit := budget.Get().PageItems
		type node struct {
			it    item.Item
			depth int
		}
		// The subtree in reading order: depth first, in sibling order.
		var order []node
		var walk func(it item.Item, depth int)
		walk = func(it item.Item, depth int) {
			order = append(order, node{it, depth})
			if (!children && depth == 0) || len(order) > limit {
				return
			}
			kids, _ := x.Tree(it.Space, it.ID)
			for _, k := range kids {
				kid, err := x.Get(k.Item.ID)
				if err == nil {
					walk(kid, depth+1)
				}
			}
		}
		walk(root, 0)
		if len(order) > limit {
			err = errTooMany
			return
		}
		// The items the bodies embed, a few levels deep.
		embeds := map[item.ID]item.Item{}
		next := []item.Item{}
		for _, n := range order {
			next = append(next, n.it)
		}
		for level := 0; level < 3 && len(next) > 0 && len(embeds) < limit; level++ {
			var found []item.Item
			for _, it := range next {
				for _, b := range it.Body.Blocks {
					if b.Kind != item.Embed {
						continue
					}
					if _, seen := embeds[b.Ref]; seen {
						continue
					}
					if e, err := x.Get(b.Ref); err == nil {
						embeds[b.Ref] = e
						found = append(found, e)
					}
				}
			}
			next = found
		}
		all := make([]item.Item, 0, len(order)+len(embeds))
		for _, n := range order {
			all = append(all, n.it)
		}
		for _, e := range embeds {
			all = append(all, e)
		}
		names := h.d.DB.Accounts.Usernames(userIDs(all, s.Type))
		pageOf := func(it item.Item, depth int) export.Page {
			p := export.Page{ID: it.ID, Key: it.Key, Title: it.Title, Body: it.Body.Blocks, Depth: depth, Images: map[item.ID]export.Image{},
				Facts: [][2]string{{words["key"], it.Key}, {words["type"], it.Type}}}
			if it.State != "" {
				p.Facts = append(p.Facts, [2]string{words["state"], it.State})
			}
			for _, f := range s.Type(it.Type).Fields {
				if v, ok := it.Properties[f.Name]; ok && v != nil {
					p.Facts = append(p.Facts, [2]string{f.Name, propertyText(v, f.Kind, names)})
				}
			}
			if len(it.Labels) > 0 {
				p.Facts = append(p.Facts, [2]string{words["labels"], strings.Join(it.Labels, ", ")})
			}
			if kids, err := x.Tree(it.Space, it.ID); err == nil {
				for _, k := range kids {
					p.Children = append(p.Children, k.Item.Title)
				}
			}
			shown := map[item.ID]item.BlockKind{}
			for _, b := range it.Body.Blocks {
				if b.Kind == item.Image || b.Kind == item.Video {
					shown[b.Ref] = b.Kind
				}
			}
			if atts, err := x.Attachments(it.ID); err == nil {
				for _, a := range atts {
					if kind, ok := shown[a.ID]; ok && !a.Deleted {
						p.Images[a.ID] = export.Image{Name: a.Name, Type: a.Type}
						// A video is named, not copied into the document (ADR-0097).
						if kind == item.Image {
							files = append(files, file{it.ID, a})
						}
					}
				}
			}
			return p
		}
		for _, n := range order {
			doc.Pages = append(doc.Pages, pageOf(n.it, n.depth))
		}
		for id, e := range embeds {
			doc.Embeds[id] = pageOf(e, 0)
		}
		if children {
			doc.Contents = words["contents"]
		}
	})
	if err != nil || !withData {
		return doc, root, err
	}
	// The pictures are read after the store's lock.
	data := map[[2]item.ID][]byte{}
	for _, f := range files {
		rc, err := h.d.DB.OpenAttachment(f.owner, f.a.ID)
		if err != nil {
			continue // shown by its caption alone
		}
		b, err := io.ReadAll(io.LimitReader(rc, budget.Get().AttachmentBytes))
		rc.Close()
		if err == nil {
			data[[2]item.ID{f.owner, f.a.ID}] = b
		}
	}
	fill := func(p export.Page) {
		for id, img := range p.Images {
			img.Data = data[[2]item.ID{p.ID, id}]
			p.Images[id] = img
		}
	}
	for _, p := range doc.Pages {
		fill(p)
	}
	for _, e := range doc.Embeds {
		fill(e)
	}
	return doc, root, nil
}

// exportFailed answers an export that could not be gathered.
func (h itemHandlers) exportFailed(w http.ResponseWriter, err error) {
	if errors.Is(err, errTooMany) {
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("a handbook holds at most %d pages", budget.Get().PageItems))
		return
	}
	h.fail(w, err)
}

func (h itemHandlers) exportDOCX(w http.ResponseWriter, r *http.Request) {
	children := r.URL.Query().Get("children") == "true"
	doc, it, err := h.exportDocument(r, children, true)
	if err != nil {
		h.exportFailed(w, err)
		return
	}
	attach(w, "application/vnd.openxmlformats-officedocument.wordprocessingml.document", fileName(it.Key, it.Title, ".docx"))
	if err := export.DOCX(w, doc); err != nil {
		h.log.Error("export failed", "item", it.ID, "err", err)
	}
}

// ExportPolicy is the content security policy of a page's printable form:
// no script at all, its stylesheet and pictures from this server, and
// framed only by the UI, which prints it.
const ExportPolicy = "default-src 'none'; style-src 'self'; img-src 'self'; media-src 'self'; script-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'self'"

// exportHTML answers the page, or the handbook, as a page to print: the
// UI prints it as PDF (ADR-0071).
func (h itemHandlers) exportHTML(w http.ResponseWriter, r *http.Request) {
	children := r.URL.Query().Get("children") == "true"
	doc, it, err := h.exportDocument(r, children, false)
	if err != nil {
		h.exportFailed(w, err)
		return
	}
	hd := w.Header()
	hd.Set("Content-Type", "text/html; charset=utf-8")
	hd.Set("Content-Security-Policy", ExportPolicy)
	hd.Set("Cache-Control", "no-store")
	if err := export.HTML(w, doc, h.lang(r)); err != nil {
		h.log.Error("export failed", "item", it.ID, "err", err)
	}
}

func (h itemHandlers) exportTable(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || n < 1 {
		writeError(w, http.StatusBadRequest, "n is the number of a table, from 1")
		return
	}
	var it item.Item
	h.d.DB.Read(func(s *item.Store) { it, err = resolveItem(s.As(who(r)), r.PathValue("ref")) })
	if err != nil {
		h.fail(w, err)
		return
	}
	var tables []item.Block
	for _, b := range it.Body.Blocks {
		if b.Kind == item.Table {
			tables = append(tables, b)
		}
	}
	if n > len(tables) {
		writeError(w, http.StatusNotFound, fmt.Sprintf("the item has %d tables", len(tables)))
		return
	}
	lang := h.lang(r)
	attach(w, "text/csv; charset=utf-8", fileName(it.Key, it.Title+" "+exportWords[lang]["table"]+" "+strconv.Itoa(n), ".csv"))
	_ = export.CSV(w, export.TableRows(tables[n-1].Text), export.Separator(lang))
}

// errTooMany refuses an export over the budget.
var errTooMany = errors.New("too many items")

// itemRows writes items as rows: the fixed columns, then every property
// of their types, in the order of the types' fields.
func (h itemHandlers) itemRows(s *item.Store, items []item.Item, words map[string]string) [][]string {
	var fields []item.FieldDef
	seen := map[string]bool{}
	for _, it := range items {
		for _, f := range s.Type(it.Type).Fields {
			if !seen[f.Name] {
				seen[f.Name] = true
				fields = append(fields, f)
			}
		}
	}
	names := h.d.DB.Accounts.Usernames(userIDs(items, s.Type))
	header := []string{words["key"], words["title"], words["space"], words["type"], words["state"], words["labels"]}
	for _, f := range fields {
		header = append(header, f.Name)
	}
	rows := [][]string{header}
	for _, it := range items {
		row := []string{it.Key, it.Title, it.Space, it.Type, it.State, strings.Join(it.Labels, ", ")}
		for _, f := range fields {
			kind := f.Kind
			if i := slices.IndexFunc(s.Type(it.Type).Fields, func(g item.FieldDef) bool { return g.Name == f.Name }); i >= 0 {
				kind = s.Type(it.Type).Fields[i].Kind
			}
			row = append(row, propertyText(it.Properties[f.Name], kind, names))
		}
		rows = append(rows, row)
	}
	return rows
}

// sendRows answers a list export, or refuses one over the budget.
func (h itemHandlers) sendRows(w http.ResponseWriter, r *http.Request, base string, find func(x *item.Session) ([]item.Item, error)) {
	lang := h.lang(r)
	var rows [][]string
	var err error
	h.d.DB.Read(func(s *item.Store) {
		var items []item.Item
		if items, err = find(s.As(who(r))); err != nil {
			return
		}
		if len(items) > budget.Get().ExportRows {
			err = errTooMany
			return
		}
		rows = h.itemRows(s, items, exportWords[lang])
	})
	switch {
	case errors.Is(err, errTooMany):
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("an export holds at most %d items; narrow the list", budget.Get().ExportRows))
		return
	case err != nil:
		h.fail(w, err)
		return
	}
	attach(w, "text/csv; charset=utf-8", base+".csv")
	_ = export.CSV(w, rows, export.Separator(lang))
}

func (h itemHandlers) exportItems(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	query := item.Query{Space: q.Get("space"), Type: q.Get("type"), State: q.Get("state"), Parent: item.ID(q.Get("parent")), Label: q.Get("label"), Text: q.Get("q")}
	h.sendRows(w, r, "items", func(x *item.Session) ([]item.Item, error) { return x.Find(query), nil })
}

func (h itemHandlers) exportSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	query := item.Query{Space: q.Get("space"), Type: q.Get("type"), State: q.Get("state"), Label: q.Get("label")}
	h.sendRows(w, r, "search", func(x *item.Session) ([]item.Item, error) {
		hits, err := x.Search(q.Get("q"), query)
		items := make([]item.Item, len(hits))
		for i, hit := range hits {
			items[i] = hit.Item
		}
		return items, err
	})
}
