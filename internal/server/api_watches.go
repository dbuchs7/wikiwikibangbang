package server

import (
	"net/http"
	"strconv"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/auth"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

// watchRoutes are the routes of watching and notifications (ADR-0046).
func watchRoutes(h itemHandlers) []Route {
	get, put, del, post := http.MethodGet, http.MethodPut, http.MethodDelete, http.MethodPost
	return []Route{
		{Method: put, Pattern: "/api/items/{ref}/watch", Summary: "Watch an item: be told what others change on it and of new items below it", Handler: http.HandlerFunc(h.watchItem(true))},
		{Method: del, Pattern: "/api/items/{ref}/watch", Summary: "Stop watching an item", Handler: http.HandlerFunc(h.watchItem(false))},
		{Method: put, Pattern: "/api/spaces/{space}/watch", Summary: "Watch every item of a space", Handler: http.HandlerFunc(h.watchSpace(true))},
		{Method: del, Pattern: "/api/spaces/{space}/watch", Summary: "Stop watching a space", Handler: http.HandlerFunc(h.watchSpace(false))},
		{Method: get, Pattern: "/api/me/watches", Summary: "What one watches", Handler: http.HandlerFunc(h.listWatches)},
		{Method: get, Pattern: "/api/notifications", Summary: "What others changed on what one watches, newest first, paged (before, limit), with the unread count", Handler: http.HandlerFunc(h.notifications)},
		{Method: post, Pattern: "/api/notifications/seen", Summary: "Mark the notifications up to an event (seq) as seen", Handler: http.HandlerFunc(h.notificationsSeen)},
	}
}

func (h itemHandlers) watchItem(on bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h.write(w, r, http.StatusOK, func(x *item.Session, _ *item.Store) (any, error) {
			it, err := resolveItem(x, r.PathValue("ref"))
			if err != nil {
				return nil, err
			}
			if err := x.Watch(it.ID, on); err != nil {
				return nil, err
			}
			return map[string]bool{"watching": on}, nil
		})
	}
}

func (h itemHandlers) watchSpace(on bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h.write(w, r, http.StatusOK, func(x *item.Session, _ *item.Store) (any, error) {
			if err := x.WatchSpace(r.PathValue("space"), on); err != nil {
				return nil, err
			}
			return map[string]bool{"watching": on}, nil
		})
	}
}

type watchJSON struct {
	Item  item.ID `json:"item,omitempty"`
	Key   string  `json:"key,omitempty"`
	Title string  `json:"title,omitempty"`
	Space string  `json:"space"`
}

func (h itemHandlers) listWatches(w http.ResponseWriter, r *http.Request) {
	h.read(w, r, func(x *item.Session) (any, error) {
		out := []watchJSON{}
		for _, wt := range x.Watches() {
			j := watchJSON{Item: wt.Item, Space: wt.Space}
			if wt.Item != "" {
				it, err := x.Get(wt.Item)
				if err != nil {
					continue
				}
				j.Key, j.Title, j.Space = it.Key, it.Title, it.Space
			}
			out = append(out, j)
		}
		return map[string]any{"watches": out}, nil
	})
}

type notificationJSON struct {
	Seq     int            `json:"seq"`
	At      time.Time      `json:"at"`
	Actor   string         `json:"actor"`
	Kind    item.EventKind `json:"kind"`
	Item    item.ID        `json:"item"`
	Key     string         `json:"key"`
	Title   string         `json:"title"`
	Space   string         `json:"space"`
	Created bool           `json:"created,omitempty"`
	Unread  bool           `json:"unread,omitempty"`
}

// unreadCap bounds the unread count the navigation shows ("99+").
const unreadCap = 100

// defaultStale is how long an item may stay unchanged before governance
// names it (ADR-0040).
const defaultStale = 365 * 24 * time.Hour

// findingJSON is a governance finding one is responsible for (ADR-0057).
type findingJSON struct {
	Kind   string  `json:"kind"` // sensitive, stale or implausible
	Item   item.ID `json:"item"`
	Key    string  `json:"key"`
	Title  string  `json:"title"`
	Detail string  `json:"detail,omitempty"`
	// Since is when a stale item changed last.
	Since *time.Time `json:"since,omitempty"`
}

// findingsLimit bounds the findings listed; the counts are complete.
const findingsLimit = 100

// findingsOf counts the findings one is responsible for and, with list,
// lists them.
func findingsOf(r item.Report, list bool) map[string]any {
	out := map[string]any{"sensitive": len(r.Sensitive), "stale": len(r.Stale), "implausible": len(r.Implausible)}
	if !list {
		return out
	}
	items := []findingJSON{}
	for _, f := range r.Sensitive {
		items = append(items, findingJSON{Kind: "sensitive", Item: f.Item, Key: f.Key, Title: f.Title, Detail: f.Kind + " · " + f.Where})
	}
	for _, f := range r.Implausible {
		items = append(items, findingJSON{Kind: "implausible", Item: f.Item, Key: f.Key, Title: f.Title, Detail: f.Problem})
	}
	for _, f := range r.Stale {
		since := f.LastChange
		items = append(items, findingJSON{Kind: "stale", Item: f.Item, Key: f.Key, Title: f.Title, Since: &since})
	}
	out["items"] = items[:min(len(items), findingsLimit)]
	return out
}

func (h itemHandlers) notifications(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, ok := parseLimit(w, q)
	if !ok {
		return
	}
	before := 0
	if v := q.Get("before"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "before must be the seq of a notification")
			return
		}
		before = n
	}
	seen := h.d.DB.Accounts.Seen(auth.UserID(who(r)))
	h.read(w, r, func(x *item.Session) (any, error) {
		list, more := x.Notifications(before, limit)
		out := []notificationJSON{}
		for _, n := range list {
			out = append(out, notificationJSON{Seq: n.Seq, At: n.At, Actor: n.Actor, Kind: n.Kind, Item: n.Item, Key: n.Key,
				Title: n.Title, Space: n.Space, Created: n.Created, Unread: n.Seq > seen})
		}
		res := map[string]any{"notifications": out, "unread": x.Unread(seen, unreadCap), "seen": seen,
			"findings": findingsOf(x.Findings(time.Now(), defaultStale), q.Get("findings") != "")}
		if more {
			res["next"] = list[len(list)-1].Seq
		}
		return res, nil
	})
}

func (h itemHandlers) notificationsSeen(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Seq int `json:"seq"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Seq < 0 {
		writeError(w, http.StatusBadRequest, "seq must not be negative")
		return
	}
	if err := h.d.DB.Accounts.SetSeen(auth.UserID(who(r)), req.Seq); err != nil {
		h.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
