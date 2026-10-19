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
			Summary: "Check a space: sensitive data, items unchanged for staleDays (365), implausible values; what one may read",
			Handler: http.HandlerFunc(h.governance)},
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
		return out, nil
	})
}
