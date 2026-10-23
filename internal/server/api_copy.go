package server

import (
	"fmt"
	"net/http"

	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

// copyRoutes copy a page, with its child pages if asked (ADR-0067).
func copyRoutes(h itemHandlers) []Route {
	return []Route{
		{Method: http.MethodPost, Pattern: "/api/items/{ref}/copy", Summary: "Copy the item to space and parent, under title, with its child pages when children is true: body, properties, labels, access and files; not comments or history", Handler: http.HandlerFunc(h.copyItem)},
	}
}

// copyTask is one item to copy and where its copy goes.
type copyTask struct {
	src    item.ID
	parent item.ID
	title  string // empty keeps the source's
}

func (h itemHandlers) copyItem(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Space    string  `json:"space"`
		Parent   item.ID `json:"parent"`
		Title    string  `json:"title"`
		Children bool    `json:"children"`
	}
	if !decode(w, r, &req) {
		return
	}
	var root item.Item
	var err error
	h.d.DB.Read(func(s *item.Store) { root, err = resolveItem(s.As(who(r)), r.PathValue("ref")) })
	if err != nil {
		h.fail(w, err)
		return
	}
	// The whole subtree is known before anything is made, so a copy into
	// its own subtree does not copy itself again, and its size is bounded.
	limit := budget.Get().PageItems
	type node struct{ src, srcParent item.ID }
	nodes := []node{{src: root.ID}}
	if req.Children {
		h.d.DB.Read(func(s *item.Store) {
			x := s.As(who(r))
			for i := 0; i < len(nodes) && len(nodes) <= limit; i++ {
				kids, err := x.Tree(root.Space, nodes[i].src)
				if err != nil {
					continue
				}
				for _, k := range kids {
					nodes = append(nodes, node{src: k.Item.ID, srcParent: nodes[i].src})
				}
			}
		})
	}
	if len(nodes) > limit {
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("a copy takes at most %d pages", limit))
		return
	}
	copies := map[item.ID]item.ID{}
	var first item.Item
	for i, n := range nodes {
		t := copyTask{src: n.src, parent: copies[n.srcParent]}
		if i == 0 {
			t.parent, t.title = req.Parent, req.Title
		}
		copied, err := h.copyOne(r, req.Space, t)
		if err != nil {
			if i == 0 {
				h.fail(w, err)
				return
			}
			writeError(w, http.StatusConflict, fmt.Sprintf("copying stopped after %d of %d pages: %v", i, len(nodes), err))
			return
		}
		if i == 0 {
			first = copied
		}
		copies[n.src] = copied.ID
	}
	made := len(nodes)
	writeJSON(w, http.StatusCreated, map[string]any{"item": map[string]any{"id": first.ID, "key": first.Key, "title": first.Title}, "copied": made})
}

// copyOne copies one item under t.parent in space, files and body
// included, and returns the copy.
func (h itemHandlers) copyOne(r *http.Request, space string, t copyTask) (item.Item, error) {
	var src item.Item
	var files []item.Attachment
	var err error
	h.d.DB.Read(func(s *item.Store) {
		x := s.As(who(r))
		if src, err = x.Get(t.src); err != nil {
			return
		}
		// A copy holds text, never a placeholder for it (ADR-0088).
		if !x.ReadsAllSecrets(t.src) {
			err = fmt.Errorf("%w: %s holds protected passages you may not read; ask for them or copy without it", item.ErrForbidden, src.Key)
			return
		}
		files, err = x.Attachments(src.ID)
	})
	if err != nil {
		return item.Item{}, err
	}
	title := t.title
	if title == "" {
		title = src.Title
	}
	var made item.Item
	err = h.d.DB.Write(func(s *item.Store) error {
		x := s.As(who(r))
		var err error
		made, err = x.Create(item.NewItem{Space: space, Type: src.Type, Parent: t.parent, Title: title, Properties: src.Properties})
		if err != nil {
			return err
		}
		if len(src.Labels) > 0 {
			if err := x.SetLabels(made.ID, src.Labels); err != nil {
				return err
			}
		}
		if err := x.CopySecrets(src.ID, made.ID); err != nil {
			return err
		}
		// A restricted page stays as restricted as its source.
		if len(src.Restriction.Read)+len(src.Restriction.Edit) > 0 {
			return x.Restrict(made.ID, src.Restriction)
		}
		return nil
	})
	if err != nil {
		return item.Item{}, err
	}
	// The files are copied first, each remembering its source (ADR-0055),
	// so the body can show them under their new IDs.
	renamed := map[item.ID]item.ID{}
	for _, f := range files {
		if f.Deleted {
			continue
		}
		content, err := h.d.DB.OpenAttachment(src.ID, f.ID)
		if err != nil {
			return made, fmt.Errorf("the file %s cannot be read: %w", f.Name, err)
		}
		a, err := h.d.DB.StoreAttachment(made.ID, content, budget.Get().AttachmentBytes)
		content.Close()
		if err != nil {
			return made, err
		}
		a.Name, a.Source = f.Name, &item.Source{Item: src.ID, Attachment: f.ID}
		err = h.d.DB.Write(func(s *item.Store) error {
			got, err := s.As(who(r)).AddAttachment(made.ID, a)
			renamed[f.ID] = got.ID
			return err
		})
		if err != nil {
			h.d.DB.DiscardAttachment(made.ID, a.ID)
			return made, err
		}
	}
	if len(src.Body.Blocks) > 0 {
		body := item.Doc{Blocks: make([]item.Block, len(src.Body.Blocks))}
		copy(body.Blocks, src.Body.Blocks)
		for i, b := range body.Blocks {
			if b.Kind == item.Image || b.Kind == item.Video {
				if to, ok := renamed[b.Ref]; ok {
					body.Blocks[i].Ref = to
				}
			}
		}
		if err := h.d.DB.Write(func(s *item.Store) error { return s.As(who(r)).CommitBody(made.ID, body) }); err != nil {
			return made, err
		}
	}
	h.d.DB.Read(func(s *item.Store) { made, _ = s.As(who(r)).Get(made.ID) })
	return made, nil
}
