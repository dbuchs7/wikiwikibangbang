package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/collab"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/storage"
)

// Live editing sessions (ADR-0010): the server sends a session's events
// as server-sent events, clients send operations with plain POSTs.

// storeBackend is the item store as a collab.Hub sees it. The hub calls
// it with its own lock held, so nothing here may call the hub.
type storeBackend struct{ db *storage.DB }

func (b storeBackend) Access(id item.ID, principal string) (read, edit bool) {
	b.db.Read(func(s *item.Store) {
		x := s.As(principal)
		it, err := x.Get(id)
		if err != nil || it.Presence != item.Live {
			return
		}
		read, edit = true, x.MayEdit(id)
	})
	return read, edit
}

func (b storeBackend) Body(id item.ID) (item.Doc, error) {
	var doc item.Doc
	var err error
	b.db.Read(func(s *item.Store) {
		var it item.Item
		if it, err = s.Get(id); err == nil {
			doc = it.Body
		}
	})
	return doc, err
}

func (b storeBackend) Commit(id item.ID, principal string, d item.Doc) error {
	return b.db.Write(func(s *item.Store) error { return s.As(principal).CommitBody(id, d) })
}

// HeartbeatInterval keeps idle event streams open through proxies.
var HeartbeatInterval = 20 * time.Second

func liveRoutes(h itemHandlers) []Route {
	return []Route{
		{Method: http.MethodGet, Pattern: "/api/items/{ref}/live", Summary: "Join the item's live editing session: a stream of server-sent events, starting with a snapshot", Handler: http.HandlerFunc(h.liveEvents)},
		{Method: http.MethodPost, Pattern: "/api/items/{ref}/live", Summary: "Apply operations in the live session (lock, set, insert, delete, release); a release commits the body", Handler: http.HandlerFunc(h.liveOps)},
	}
}

func (h itemHandlers) resolveID(r *http.Request) (item.ID, error) {
	var id item.ID
	var err error
	h.d.DB.Read(func(s *item.Store) {
		var it item.Item
		if it, err = resolveItem(s.As(who(r)), r.PathValue("ref")); err == nil {
			id = it.ID
		}
	})
	return id, err
}

func (h itemHandlers) liveEvents(w http.ResponseWriter, r *http.Request) {
	id, err := h.resolveID(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	p, _ := PrincipalFrom(r.Context())
	sub, err := h.d.Collab.Join(id, string(p.User), p.Username)
	if err != nil {
		h.liveFail(w, err)
		return
	}
	defer sub.Leave()

	rc := http.NewResponseController(w)
	hd := w.Header()
	hd.Set("Content-Type", "text/event-stream")
	hd.Set("Cache-Control", "no-store")
	hd.Set("X-Accel-Buffering", "no") // nginx would otherwise hold events back
	w.WriteHeader(http.StatusOK)
	beat := time.NewTicker(HeartbeatInterval)
	defer beat.Stop()
	write := func(chunk string) bool {
		// The server's write timeout covers a whole response; a stream
		// extends it for every chunk instead.
		_ = rc.SetWriteDeadline(time.Now().Add(HeartbeatInterval + 10*time.Second))
		if _, err := fmt.Fprint(w, chunk); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	if !write(": joined\n\n") {
		return
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case <-beat.C:
			if !write(": ping\n\n") {
				return
			}
		case e, ok := <-sub.C:
			if !ok {
				return
			}
			data, err := json.Marshal(toLiveEventJSON(e))
			if err != nil {
				h.log.Error("encoding a live event", "err", err)
				return
			}
			if !write(fmt.Sprintf("event: %s\ndata: %s\n\n", e.Kind, data)) {
				return
			}
		}
	}
}

type liveOpJSON struct {
	Op    string    `json:"op"`
	ID    string    `json:"id"`
	After string    `json:"after"`
	Block blockJSON `json:"block"`
}

func (h itemHandlers) liveOps(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Client string       `json:"client"`
		Ops    []liveOpJSON `json:"ops"`
	}
	if !decode(w, r, &req) {
		return
	}
	id, err := h.resolveID(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	ops := make([]collab.Op, len(req.Ops))
	for i, o := range req.Ops {
		ops[i] = collab.Op{Op: o.Op, ID: o.ID, After: o.After}
		if o.Op == "set" || o.Op == "insert" {
			doc, err := toDoc([]blockJSON{o.Block}, func(string) bool { return true })
			if err != nil {
				h.fail(w, err)
				return
			}
			ops[i].Block = doc.Blocks[0]
		}
	}
	if err := h.d.Collab.Apply(id, req.Client, who(r), ops); err != nil {
		h.liveFail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h itemHandlers) liveFail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, collab.ErrLocked), errors.Is(err, collab.ErrDiverged), errors.Is(err, collab.ErrNotJoined):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, collab.ErrForbidden):
		writeError(w, http.StatusForbidden, "you may not edit this item")
	case errors.Is(err, collab.ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, collab.ErrBusy):
		writeError(w, http.StatusServiceUnavailable, err.Error())
	default:
		h.fail(w, err)
	}
}

type holderJSON struct {
	Client    string `json:"client"`
	Principal string `json:"principal"`
	Name      string `json:"name"`
}

type liveBlockJSON struct {
	ID     string      `json:"id"`
	Block  blockJSON   `json:"block"`
	Holder *holderJSON `json:"holder,omitempty"`
}

type participantJSON struct {
	Principal string `json:"principal"`
	Name      string `json:"name"`
}

type liveEventJSON struct {
	Seq          int               `json:"seq"`
	Client       string            `json:"client,omitempty"`
	Editable     bool              `json:"editable,omitempty"`
	ID           string            `json:"id,omitempty"`
	After        *string           `json:"after,omitempty"`
	Block        *blockJSON        `json:"block,omitempty"`
	Holder       *holderJSON       `json:"holder,omitempty"`
	Blocks       []liveBlockJSON   `json:"blocks,omitempty"`
	Order        []string          `json:"order,omitempty"`
	Participants []participantJSON `json:"participants,omitempty"`
	By           string            `json:"by,omitempty"`
	Name         string            `json:"name,omitempty"`
	Reason       string            `json:"reason,omitempty"`
}

func toHolderJSON(hd *collab.Holder) *holderJSON {
	if hd == nil {
		return nil
	}
	return &holderJSON{Client: hd.Client, Principal: hd.Principal, Name: hd.Name}
}

func toLiveEventJSON(e collab.Event) liveEventJSON {
	j := liveEventJSON{Seq: e.Seq, Client: e.Client, Editable: e.Editable, ID: e.ID, Holder: toHolderJSON(e.Holder), Order: e.Order,
		By: e.By, Name: e.Name, Reason: e.Reason}
	if e.Kind == collab.Insert {
		j.After = &e.After
	}
	if e.Block != nil {
		b := toBlockJSON(*e.Block)
		j.Block = &b
	}
	for _, b := range e.Blocks {
		j.Blocks = append(j.Blocks, liveBlockJSON{ID: b.ID, Block: toBlockJSON(b.Block), Holder: toHolderJSON(b.Holder)})
	}
	for _, p := range e.Participants {
		j.Participants = append(j.Participants, participantJSON{Principal: p.Principal, Name: p.Name})
	}
	return j
}
