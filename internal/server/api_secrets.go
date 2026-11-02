package server

import (
	"net/http"
	"strconv"
	"time"

	"github.com/dbuchs7/orbithek/internal/budget"
	"github.com/dbuchs7/orbithek/internal/item"
)

// secretRoutes protect passages of a body and decide who reads them
// (ADR-0088).
func secretRoutes(h itemHandlers) []Route {
	return []Route{
		{Method: http.MethodPost, Pattern: "/api/items/{ref}/secrets", Summary: "Protect a passage of the body: block (from 0), start and end in characters of its text; or whole blocks, a section: fromBlock and toBlock (from 0, both included), their files with them (ADR-0160); and readers (users and @groups) besides oneself; who may edit the item and reads all its protections", Handler: http.HandlerFunc(h.protect)},
		{Method: http.MethodPut, Pattern: "/api/items/{ref}/secrets/{id}", Summary: "Set who reads a protected passage besides the one who protected it (that one and the space's administrators)", Handler: http.HandlerFunc(h.setSecretReaders)},
		{Method: http.MethodDelete, Pattern: "/api/items/{ref}/secrets/{id}", Summary: "Take a passage's protection off (the one who protected it and the space's administrators)", Handler: http.HandlerFunc(h.unprotect)},
		{Method: http.MethodPost, Pattern: "/api/items/{ref}/secrets/{id}/erase", Summary: "Erase a protected passage for good: its text leaves the body and its key is destroyed, so no earlier version holds it either (the one who protected it and the space's administrators; ADR-0089)", Handler: http.HandlerFunc(h.eraseSecret)},
		{Method: http.MethodPost, Pattern: "/api/items/{ref}/secrets/{id}/request", Summary: "Ask to read a protected passage; the one who protected it and the space's administrators are told", Handler: http.HandlerFunc(h.requestSecret)},
		{Method: http.MethodPost, Pattern: "/api/items/{ref}/secrets/{id}/requests/{principal}", Summary: "Answer a request to read a protected passage: grant true lets the one who asked read it, false declines", Handler: http.HandlerFunc(h.decideSecret)},
		{Method: http.MethodGet, Pattern: "/api/me/access-requests", Summary: "The waiting requests to read a protected passage or protected blocks that one decides (one protected it, or administers its space), " +
			"on items one reads, across spaces, oldest first: at most limit (the page budget if absent) after the cursor after; total counts them all, " +
			"next continues (ADR-0161)", Handler: http.HandlerFunc(h.accessRequests)},
	}
}

type accessRequestJSON struct {
	Item      item.ID   `json:"item"`
	Key       string    `json:"key"`
	Title     string    `json:"title"`
	Space     string    `json:"space"`
	Secret    item.ID   `json:"secret"`
	Number    int       `json:"number"`
	Blocks    bool      `json:"blocks,omitempty"`
	Principal string    `json:"principal"`
	At        time.Time `json:"at"`
}

// accessRequests lists the requests to read a protection that wait on the
// caller (ADR-0161): a view of the SecretRequested events, nothing stored.
func (h itemHandlers) accessRequests(w http.ResponseWriter, r *http.Request) {
	page := budget.Get().PageItems
	limit, ok := queryInt(w, r, "limit", page, 1, page)
	if !ok {
		return
	}
	after, ok := queryInt(w, r, "after", 0, 0, int(^uint(0)>>1))
	if !ok {
		return
	}
	h.read(w, r, func(x *item.Session) (any, error) {
		list, total, next := x.AccessRequests(after, limit)
		out := make([]accessRequestJSON, 0, len(list))
		for _, a := range list {
			out = append(out, accessRequestJSON{Item: a.Item, Key: a.Key, Title: a.Title, Space: a.Space, Secret: a.Secret, Number: a.Number,
				Blocks: a.Blocks, Principal: a.Principal, At: a.At})
		}
		res := map[string]any{"requests": out, "total": total}
		if next > 0 {
			res["next"] = strconv.Itoa(next)
		}
		return res, nil
	})
}

// liveBusy answers 409 when the item's body is in a live session: changing
// it beside the session would be lost by the session's next commit.
func (h itemHandlers) liveBusy(w http.ResponseWriter, r *http.Request) bool {
	if id, err := h.resolveID(r); err == nil && h.d.Collab != nil && h.d.Collab.Busy(id) {
		writeError(w, http.StatusConflict, "the body is being edited in a live session; change the passage when it is done")
		return true
	}
	return false
}

func (h itemHandlers) protect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Block   int      `json:"block"`
		Start   int      `json:"start"`
		End     int      `json:"end"`
		Readers []string `json:"readers"`
		// FromBlock and ToBlock protect whole blocks instead (ADR-0160).
		FromBlock *int `json:"fromBlock"`
		ToBlock   *int `json:"toBlock"`
	}
	if !decode(w, r, &req) || h.liveBusy(w, r) {
		return
	}
	if (req.FromBlock == nil) != (req.ToBlock == nil) {
		writeError(w, http.StatusBadRequest, "fromBlock and toBlock come together")
		return
	}
	h.change(w, r, func(x *item.Session, _ *item.Store, it item.Item) error {
		var err error
		if req.FromBlock != nil {
			_, err = x.ProtectBlocks(it.ID, *req.FromBlock, *req.ToBlock, req.Readers)
		} else {
			_, err = x.Protect(it.ID, req.Block, req.Start, req.End, req.Readers)
		}
		return err
	})
}

func (h itemHandlers) setSecretReaders(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Readers []string `json:"readers"`
	}
	if !decode(w, r, &req) {
		return
	}
	h.change(w, r, func(x *item.Session, _ *item.Store, it item.Item) error {
		return x.SetSecretReaders(it.ID, item.ID(r.PathValue("id")), req.Readers)
	})
}

func (h itemHandlers) unprotect(w http.ResponseWriter, r *http.Request) {
	if h.liveBusy(w, r) {
		return
	}
	h.change(w, r, func(x *item.Session, _ *item.Store, it item.Item) error {
		return x.Unprotect(it.ID, item.ID(r.PathValue("id")))
	})
}

func (h itemHandlers) eraseSecret(w http.ResponseWriter, r *http.Request) {
	if h.liveBusy(w, r) {
		return
	}
	h.change(w, r, func(x *item.Session, _ *item.Store, it item.Item) error {
		return x.EraseSecret(it.ID, item.ID(r.PathValue("id")))
	})
}

func (h itemHandlers) requestSecret(w http.ResponseWriter, r *http.Request) {
	h.change(w, r, func(x *item.Session, _ *item.Store, it item.Item) error {
		return x.RequestSecret(it.ID, item.ID(r.PathValue("id")))
	})
}

func (h itemHandlers) decideSecret(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Grant bool `json:"grant"`
	}
	if !decode(w, r, &req) {
		return
	}
	h.change(w, r, func(x *item.Session, _ *item.Store, it item.Item) error {
		return x.DecideSecret(it.ID, item.ID(r.PathValue("id")), r.PathValue("principal"), req.Grant)
	})
}
