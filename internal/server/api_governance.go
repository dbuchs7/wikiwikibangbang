package server

import (
	"net/http"
	"strconv"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

func governanceRoutes(h itemHandlers) []Route {
	return []Route{
		{Method: http.MethodGet, Pattern: "/api/spaces/{space}/governance",
			Summary: "Check a space: sensitive data, items unchanged for staleDays (365), implausible values and empty recommended fields, items due for review; what one may read",
			Handler: http.HandlerFunc(h.governance)},
		{Method: http.MethodPost, Pattern: "/api/items/{ref}/reviewed",
			Summary: "Record that an item was checked today: its type's review field is set to today's date (ADR-0074)",
			Handler: http.HandlerFunc(h.markReviewed)},
		{Method: http.MethodPost, Pattern: "/api/items/{ref}/hold/release",
			Summary: "End an item's data protection review: outcome restricted (reading is restricted to people or groups) or false-alarm (with a reason); not by who saved the findings",
			Handler: http.HandlerFunc(h.releaseHold)},
	}
}

type sensitiveJSON struct {
	Item        item.ID `json:"item"`
	Key         string  `json:"key"`
	Title       string  `json:"title"`
	Where       string  `json:"where"`
	Kind        string  `json:"kind"`
	Masked      string  `json:"masked"`
	Responsible string  `json:"responsible,omitempty"`
	Restricted  bool    `json:"restricted"`
}

type staleJSON struct {
	Item        item.ID   `json:"item"`
	Key         string    `json:"key"`
	Title       string    `json:"title"`
	LastChange  time.Time `json:"lastChange"`
	Responsible string    `json:"responsible,omitempty"`
}

type implausibleJSON struct {
	Item        item.ID `json:"item"`
	Key         string  `json:"key"`
	Title       string  `json:"title"`
	Problem     string  `json:"problem"`
	Responsible string  `json:"responsible,omitempty"`
}

// reviewDueJSON is an item due to be checked again (ADR-0074); lastReview is
// absent for one never reviewed.
type reviewDueJSON struct {
	Item        item.ID    `json:"item"`
	Key         string     `json:"key"`
	Title       string     `json:"title"`
	LastReview  *time.Time `json:"lastReview,omitempty"`
	Due         *time.Time `json:"due,omitempty"`
	Responsible string     `json:"responsible,omitempty"`
}

func toReviewsJSON(rs []item.ReviewDue) []reviewDueJSON {
	out := []reviewDueJSON{}
	for _, r := range rs {
		j := reviewDueJSON{Item: r.Item, Key: r.Key, Title: r.Title, Responsible: r.Responsible}
		if !r.LastReview.IsZero() {
			last, due := r.LastReview, r.Due
			j.LastReview, j.Due = &last, &due
		}
		out = append(out, j)
	}
	return out
}

// markReviewed records a review of an item (ADR-0074).
func (h itemHandlers) markReviewed(w http.ResponseWriter, r *http.Request) {
	h.write(w, r, http.StatusOK, func(x *item.Session, _ *item.Store) (any, error) {
		it, err := resolveItem(x, r.PathValue("ref"))
		if err != nil {
			return nil, err
		}
		if err := x.MarkReviewed(it.ID, time.Now()); err != nil {
			return nil, err
		}
		got, err := x.Get(it.ID)
		return map[string]itemJSON{"item": toItemJSON(got)}, err
	})
}

// personalJSON is an item holding indicators of a data protection
// regulation (ADR-0058).
type personalJSON struct {
	Item        item.ID         `json:"item"`
	Key         string          `json:"key"`
	Title       string          `json:"title"`
	Findings    []dpFindingJSON `json:"findings"`
	Held        bool            `json:"held"`
	Savers      []string        `json:"savers,omitempty"`
	Restricted  bool            `json:"restricted"`
	Responsible string          `json:"responsible,omitempty"`
	MayRelease  bool            `json:"mayRelease,omitempty"`
}

func toPersonalJSON(ps []item.PersonalData) []personalJSON {
	out := []personalJSON{}
	for _, p := range ps {
		out = append(out, personalJSON{Item: p.Item, Key: p.Key, Title: p.Title, Findings: toDPFindings(p.Findings), Held: p.Held,
			Savers: p.Savers, Restricted: p.Restricted, Responsible: p.Responsible, MayRelease: p.MayRelease})
	}
	return out
}

// releaseHold ends an item's data protection review (ADR-0058).
func (h itemHandlers) releaseHold(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Outcome string `json:"outcome"`
		Reason  string `json:"reason"`
	}
	if !decode(w, r, &req) {
		return
	}
	h.write(w, r, http.StatusOK, func(x *item.Session, _ *item.Store) (any, error) {
		it, err := resolveItem(x, r.PathValue("ref"))
		if err != nil {
			return nil, err
		}
		if err := x.ReleaseHold(it.ID, req.Outcome, req.Reason); err != nil {
			return nil, err
		}
		got, err := x.Get(it.ID)
		return map[string]itemJSON{"item": toItemJSON(got)}, err
	})
}

// governance answers a check of a space (ADR-0040). It scans the live
// state when asked, like search; nothing is stored.
func (h itemHandlers) governance(w http.ResponseWriter, r *http.Request) {
	days := 365
	if v := r.URL.Query().Get("staleDays"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 3650 {
			writeError(w, http.StatusBadRequest, "staleDays must be 1 to 3650")
			return
		}
		days = n
	}
	h.read(w, r, func(x *item.Session) (any, error) {
		rep, err := x.Governance(r.PathValue("space"), time.Now(), time.Duration(days)*24*time.Hour)
		if err != nil {
			return nil, err
		}
		out := map[string]any{"staleDays": days, "sensitive": []sensitiveJSON{}, "stale": []staleJSON{}, "implausible": []implausibleJSON{}}
		sens := make([]sensitiveJSON, len(rep.Sensitive))
		for i, f := range rep.Sensitive {
			sens[i] = sensitiveJSON(f)
		}
		stale := make([]staleJSON, len(rep.Stale))
		for i, s := range rep.Stale {
			stale[i] = staleJSON(s)
		}
		impl := make([]implausibleJSON, len(rep.Implausible))
		for i, p := range rep.Implausible {
			impl[i] = implausibleJSON(p)
		}
		out["sensitive"], out["stale"], out["implausible"] = sens, stale, impl
		out["personal"] = toPersonalJSON(rep.Personal)
		out["reviews"] = toReviewsJSON(rep.Reviews)
		return out, nil
	})
}
