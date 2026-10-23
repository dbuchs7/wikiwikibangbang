package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/linkcheck"
)

// runLimit bounds one link check run as a whole.
const runLimit = 30 * time.Minute

// LinkChecks runs link checks per space and keeps the latest report of
// each in memory (ADR-0039). A nil *LinkChecks means link checking is off.
type LinkChecks struct {
	checker *linkcheck.Checker
	mu      sync.Mutex
	reports map[string]*linkReport
}

// NewLinkChecks returns link checks that use checker.
func NewLinkChecks(checker *linkcheck.Checker) *LinkChecks {
	return &LinkChecks{checker: checker, reports: map[string]*linkReport{}}
}

type linkReport struct {
	running   bool
	by        string
	started   time.Time
	finished  time.Time
	links     []item.WebLink
	results   map[string]linkcheck.Result
	checked   int
	truncated bool
}

func linkCheckRoutes(h itemHandlers) []Route {
	return []Route{
		{Method: http.MethodGet, Pattern: "/api/spaces/{space}/linkcheck", Summary: "The latest link check of a space: its broken links that one may read", Handler: http.HandlerFunc(h.linkReport)},
		{Method: http.MethodPost, Pattern: "/api/spaces/{space}/linkcheck", Summary: "Check the web links of a space (space administrators; off unless WWBB_LINKCHECK=on)", Handler: http.HandlerFunc(h.startLinkCheck)},
	}
}

func (h itemHandlers) startLinkCheck(w http.ResponseWriter, r *http.Request) {
	lc := h.d.LinkCheck
	space := r.PathValue("space")
	var links []item.WebLink
	var err error
	allowed := false
	h.d.DB.Read(func(s *item.Store) {
		x := s.As(who(r))
		if links, err = x.WebLinks(space); err == nil {
			allowed = x.MayAdminister(space)
		}
	})
	switch {
	case err != nil:
		h.fail(w, err)
		return
	case !allowed:
		writeError(w, http.StatusForbidden, "space administrators only")
		return
	case lc == nil:
		writeError(w, http.StatusConflict, "link checking is switched off; set WWBB_LINKCHECK=on")
		return
	}
	// Distinct addresses, in the order they appear, up to the budget.
	var hrefs []string
	seen := map[string]bool{}
	truncated := false
	for _, l := range links {
		if seen[l.Href] {
			continue
		}
		if len(hrefs) == budget.Get().LinkCheckLinks {
			truncated = true
			break
		}
		seen[l.Href] = true
		hrefs = append(hrefs, l.Href)
	}
	lc.mu.Lock()
	if cur := lc.reports[space]; cur != nil && cur.running {
		lc.mu.Unlock()
		writeError(w, http.StatusConflict, "a link check of this space is running")
		return
	}
	rep := &linkReport{running: true, by: who(r), started: time.Now().UTC(), links: links, truncated: truncated}
	lc.reports[space] = rep
	lc.mu.Unlock()
	h.log.Info("link check started", "space", space, "by", who(r), "links", len(hrefs))
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), runLimit)
		defer cancel()
		results := lc.checker.CheckAll(ctx, hrefs)
		lc.mu.Lock()
		rep.results, rep.checked, rep.running, rep.finished = results, len(hrefs), false, time.Now().UTC()
		lc.mu.Unlock()
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{"running": true, "links": len(hrefs), "truncated": truncated})
}

type brokenLinkJSON struct {
	Item        item.ID `json:"item"`
	Key         string  `json:"key"`
	Title       string  `json:"title"`
	Block       int     `json:"block"`
	Field       string  `json:"field,omitempty"`
	Href        string  `json:"href"`
	Status      int     `json:"status,omitempty"`
	Problem     string  `json:"problem"`
	Responsible string  `json:"responsible,omitempty"`
}

// linkReport answers the latest run of a space. Only links of items the
// caller may read now are listed, so a report made by an administrator
// discloses nothing to a reader (D10).
func (h itemHandlers) linkReport(w http.ResponseWriter, r *http.Request) {
	space := r.PathValue("space")
	lc := h.d.LinkCheck
	out := map[string]any{"enabled": lc != nil, "running": false, "broken": []brokenLinkJSON{}}
	var rep linkReport
	if lc != nil {
		lc.mu.Lock()
		if cur := lc.reports[space]; cur != nil {
			rep = *cur
		}
		lc.mu.Unlock()
	}
	h.read(w, r, func(x *item.Session) (any, error) {
		if _, err := x.WebLinks(space); err != nil {
			return nil, err // no role in the space: not found
		}
		if rep.started.IsZero() {
			return out, nil
		}
		out["running"], out["by"], out["started"], out["checked"], out["truncated"] = rep.running, rep.by, rep.started, rep.checked, rep.truncated
		if rep.running {
			return out, nil
		}
		out["finished"] = rep.finished
		broken := []brokenLinkJSON{}
		for _, l := range rep.links {
			res, ok := rep.results[l.Href]
			if !ok || res.OK {
				continue
			}
			it, err := x.Get(l.Item)
			if err != nil || it.Presence != item.Live {
				continue
			}
			// A link in a passage the reader may not read is not shown:
			// Get veils the passage, and its address with it (ADR-0088).
			if l.Block >= 0 && !textShows(it.Body, l.Href) {
				continue
			}
			broken = append(broken, brokenLinkJSON{Item: l.Item, Key: it.Key, Title: it.Title, Block: l.Block, Field: l.Field, Href: l.Href,
				Status: res.Status, Problem: res.Problem, Responsible: l.Responsible})
		}
		out["broken"] = broken
		return out, nil
	})
}

// textShows reports whether a text, as the reader gets it, still holds an
// address.
func textShows(d item.Doc, href string) bool {
	body, err := json.Marshal(d)
	if err != nil {
		return false
	}
	quoted, _ := json.Marshal(href)
	return bytes.Contains(body, quoted[1:len(quoted)-1])
}
