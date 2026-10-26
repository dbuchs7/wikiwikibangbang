package server

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

// changeRoutes are the change feed and the query of one's own roles
// (ADR-0121). Both are reads, so tokens of scope read reach them.
func changeRoutes(h itemHandlers) []Route {
	get := http.MethodGet
	return []Route{
		{Method: get, Pattern: "/api/changes", Summary: "The change feed: every event of any kind, by anyone, after the event after, on what one watches (an item, " +
			"its ancestors, or a space, counting watches that held at the time) and on one's own roles, oldest first, at most limit (100 if absent); item is empty " +
			"for an item one may not read now; last is the cursor to continue from; without after, no changes and the newest event; wait holds the request " +
			"up to that many seconds until a change arrives", Handler: http.HandlerFunc(h.changes)},
		{Method: get, Pattern: "/api/me/roles", Summary: "One's own roles: the system roles held, and every space role granted, directly or through a group (via), " +
			"one entry per grant, by space and via", Handler: http.HandlerFunc(h.ownRoles)},
	}
}

type changeJSON struct {
	Seq  int       `json:"seq"`
	Item item.ID   `json:"item"`
	At   time.Time `json:"at"`
}

// queryInt reads a whole number from the query between lo and hi; def
// when it is absent.
func queryInt(w http.ResponseWriter, r *http.Request, name string, def, lo, hi int) (int, bool) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return def, true
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < lo || n > hi {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("%s must be %d to %d", name, lo, hi))
		return 0, false
	}
	return n, true
}

func (h itemHandlers) changes(w http.ResponseWriter, r *http.Request) {
	b := budget.Get()
	limit, ok := queryInt(w, r, "limit", min(100, b.ChangesPage), 1, b.ChangesPage)
	if !ok {
		return
	}
	wait, ok := queryInt(w, r, "wait", 0, 0, b.ChangesWait)
	if !ok {
		return
	}
	after, ok := queryInt(w, r, "after", -1, 0, int(^uint(0)>>1))
	if !ok {
		return
	}
	read := func() (list []item.Change, last int) {
		h.d.DB.Read(func(s *item.Store) {
			if after < 0 {
				// Without a cursor the feed starts now.
				last = s.Seq()
				return
			}
			list, last = s.As(who(r)).Changes(after, limit)
		})
		return list, last
	}
	if after < 0 {
		wait = 0
	}
	var timeout <-chan time.Time
	if wait > 0 {
		if h.waiting.Add(1) > int64(b.ChangesWaiting) {
			// Too many are waiting already: answer at once.
			wait = 0
		} else {
			t := time.NewTimer(time.Duration(wait) * time.Second)
			defer t.Stop()
			timeout = t.C
		}
		defer h.waiting.Add(-1)
	}
	for {
		// The channel is taken before the read, so a change between the
		// read and the wait wakes the wait. The store's lock is not held
		// while waiting.
		changed := h.d.DB.Changed()
		list, last := read()
		if len(list) > 0 || wait == 0 {
			out := make([]changeJSON, 0, len(list))
			for _, c := range list {
				out = append(out, changeJSON{Seq: c.Seq, Item: c.Item, At: c.At.UTC()})
			}
			writeJSON(w, http.StatusOK, map[string]any{"changes": out, "last": last})
			return
		}
		select {
		case <-changed:
		case <-timeout:
			wait = 0
		case <-r.Context().Done():
			wait = 0
		}
	}
}

func (h itemHandlers) ownRoles(w http.ResponseWriter, r *http.Request) {
	h.read(w, r, func(x *item.Session) (any, error) {
		system, grants := x.OwnRoles()
		spaces := make([]map[string]string, 0, len(grants))
		for _, g := range grants {
			spaces = append(spaces, map[string]string{"space": g.Space, "role": roleName(g.Role), "via": g.Via})
		}
		return map[string]any{"system": system, "spaces": spaces}, nil
	})
}
