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
		{Method: http.MethodGet, Pattern: "/api/items/{ref}/export/docx", Summary: "The item as a Word document: title, properties and body with its pictures", Handler: http.HandlerFunc(h.exportDOCX)},
		{Method: http.MethodGet, Pattern: "/api/items/{ref}/tables/{n}", Summary: "The item's n-th table (from 1) as CSV", Handler: http.HandlerFunc(h.exportTable)},
		{Method: http.MethodGet, Pattern: "/api/items.csv", Summary: "Readable items as CSV, with the filters of /api/items: key, title, space, type, state, labels and every property", Handler: http.HandlerFunc(h.exportItems)},
		{Method: http.MethodGet, Pattern: "/api/search.csv", Summary: "A full-text search as CSV (q; space, type, state, label), best first", Handler: http.HandlerFunc(h.exportSearch)},
	}
}

// exportWords are the labels of an export in each UI language.
var exportWords = map[string]map[string]string{
	"de": {"key": "Schlüssel", "title": "Titel", "space": "Bereich", "type": "Typ", "state": "Status", "labels": "Labels", "restricted": "(eingeschränkt)", "table": "Tabelle"},
	"en": {"key": "Key", "title": "Title", "space": "Space", "type": "Type", "state": "State", "labels": "Labels", "restricted": "(restricted)", "table": "table"},
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

func (h itemHandlers) exportDOCX(w http.ResponseWriter, r *http.Request) {
	words := exportWords[h.lang(r)]
	var it item.Item
	var doc export.Document
	var files []item.Attachment
	var err error
	h.d.DB.Read(func(s *item.Store) {
		x := s.As(who(r))
		if it, err = resolveItem(x, r.PathValue("ref")); err != nil {
			return
		}
		td := s.Type(it.Type)
		names := h.d.DB.Accounts.Usernames(userIDs([]item.Item{it}, s.Type))
		doc = export.Document{Title: it.Title, Body: it.Body.Blocks,
			Facts: [][2]string{{words["key"], it.Key}, {words["type"], it.Type}}}
		if it.State != "" {
			doc.Facts = append(doc.Facts, [2]string{words["state"], it.State})
		}
		for _, f := range td.Fields {
			if v, ok := it.Properties[f.Name]; ok && v != nil {
				doc.Facts = append(doc.Facts, [2]string{f.Name, propertyText(v, f.Kind, names)})
			}
		}
		if len(it.Labels) > 0 {
			doc.Facts = append(doc.Facts, [2]string{words["labels"], strings.Join(it.Labels, ", ")})
		}
		refs := map[item.ID]string{}
		for _, b := range it.Body.Blocks {
			if b.Kind == item.Embed {
				refs[b.Ref] = words["restricted"]
				if other, err := x.Get(b.Ref); err == nil {
					refs[b.Ref] = other.Key + " " + other.Title
				}
			}
		}
		doc.Ref = func(id item.ID) string { return refs[id] }
		if kids, err := x.Tree(it.Space, it.ID); err == nil {
			for _, k := range kids {
				doc.Children = append(doc.Children, k.Item.Title)
			}
		}
		files, err = x.Attachments(it.ID)
	})
	if err != nil {
		h.fail(w, err)
		return
	}
	// The pictures the body shows are read after the store's lock.
	shown := map[item.ID]bool{}
	for _, b := range it.Body.Blocks {
		if b.Kind == item.Image {
			shown[b.Ref] = true
		}
	}
	doc.Images = map[item.ID]export.Image{}
	for _, f := range files {
		if f.Deleted || !shown[f.ID] {
			continue
		}
		rc, err := h.d.DB.OpenAttachment(it.ID, f.ID)
		if err != nil {
			continue // shown by its caption alone
		}
		data, err := io.ReadAll(io.LimitReader(rc, budget.Get().AttachmentBytes))
		rc.Close()
		if err == nil {
			doc.Images[f.ID] = export.Image{Name: f.Name, Type: f.Type, Data: data}
		}
	}
	attach(w, "application/vnd.openxmlformats-officedocument.wordprocessingml.document", fileName(it.Key, it.Title, ".docx"))
	if err := export.DOCX(w, doc); err != nil {
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
	query := item.Query{Space: q.Get("space"), Type: q.Get("type"), State: q.Get("state"), Parent: item.ID(q.Get("parent")), Label: q.Get("label")}
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
