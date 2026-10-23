package server

import (
	"net/http"
	"strconv"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/audit"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/site"
	"github.com/dbuchs7/wikiwikibangbang/internal/storage"
)

// retentionRoutes are the rules for the older versions of pictures
// (ADR-0104): the album's default, set by administrators, and a rule of a
// picture's own, which takes precedence; both with a preview of what goes.
func retentionRoutes(h itemHandlers) []Route {
	a := authHandlers{d: h.d}
	return []Route{
		{Method: http.MethodGet, Pattern: "/api/album/retention", Summary: "The album's default rule for older versions of pictures: keep (newest versions kept, 0 no limit), days (versions younger kept, 0 no limit), automatic (applied every day)", Handler: http.HandlerFunc(h.albumRetention)},
		{Method: http.MethodPut, Pattern: "/api/album/retention", Summary: "Set the album's default rule for older versions: automatic, keep, days (administrators)", Handler: http.HandlerFunc(a.setAlbumRetention)},
		{Method: http.MethodGet, Pattern: "/api/album/retention/preview", Summary: "What the clean-up would remove now, by each picture's own rule or the default; keep and days try another default (administrators)", Handler: http.HandlerFunc(a.previewAlbumRetention)},
		{Method: http.MethodPost, Pattern: "/api/album/retention/run", Summary: "Clean up every picture now by its own rule or the default (administrators)", Handler: http.HandlerFunc(a.runAlbumRetention)},
		{Method: http.MethodGet, Pattern: "/api/items/{ref}/attachments/{id}/retention", Summary: "What a rule keeps and removes of a picture's versions: its own rule, or the default; keep and days try another", Handler: http.HandlerFunc(h.pictureRetention)},
		{Method: http.MethodPut, Pattern: "/api/items/{ref}/attachments/{id}/retention", Summary: "Give a picture's versions a rule of their own (keep, days), or back to the default (default: true)", Handler: http.HandlerFunc(h.setPictureRetention)},
		{Method: http.MethodPost, Pattern: "/api/items/{ref}/attachments/{id}/retention/clean", Summary: "Remove the versions of a picture its rule does not keep; keep and days use another rule this once; answers what was removed and the plan after", Handler: http.HandlerFunc(h.cleanPicture)},
	}
}

type retentionJSON struct {
	Keep int `json:"keep"`
	Days int `json:"days"`
}

func ruleJSON(r item.Retention) retentionJSON { return retentionJSON{Keep: r.Keep, Days: r.Days} }

// defaultRetention is the album's default rule as the item model takes it.
func defaultRetention(s *site.Settings) item.Retention {
	r := s.AlbumRetention()
	return item.Retention{Keep: r.Keep, Days: r.Days}
}

// isPicture is what the album shows and the rules apply to.
func isPicture(a item.Attachment) bool { return inlineTypes[a.Type] }

// ruleQuery reads keep and days from the query: a rule to try, or nil
// when neither is given.
func ruleQuery(r *http.Request) (*item.Retention, bool) {
	q := r.URL.Query()
	if q.Get("keep") == "" && q.Get("days") == "" {
		return nil, true
	}
	var rule item.Retention
	for _, f := range []struct {
		name string
		to   *int
	}{{"keep", &rule.Keep}, {"days", &rule.Days}} {
		if v := q.Get(f.name); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				return nil, false
			}
			*f.to = n
		}
	}
	return &rule, true
}

func (h itemHandlers) albumRetention(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"settings": h.d.DB.Site.AlbumRetention()})
}

func (h authHandlers) setAlbumRetention(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.adminOnly(w, r)
	if !ok {
		return
	}
	var req site.AlbumRetention
	if err := readJSON(w, r, &req); err != nil {
		badRequest(w, err)
		return
	}
	if err := (item.Retention{Keep: req.Keep, Days: req.Days}).Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.d.DB.Site.SetAlbumRetention(actor, req); err != nil {
		writeError(w, http.StatusInternalServerError, "setting not saved")
		return
	}
	h.record(audit.Entry{Event: audit.AlbumRetentionChanged, Actor: actor, Addr: clientAddr(r), Detail: map[string]string{
		"automatic": strconv.FormatBool(req.Automatic), "keep": strconv.Itoa(req.Keep), "days": strconv.Itoa(req.Days),
	}})
	writeJSON(w, http.StatusOK, map[string]any{"settings": h.d.DB.Site.AlbumRetention()})
}

// previewLimit is how many pictures the preview of a clean-up names; it
// counts them all.
const previewLimit = 100

func (h authHandlers) previewAlbumRetention(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.adminOnly(w, r); !ok {
		return
	}
	rule, ok := ruleQuery(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "keep and days are numbers")
		return
	}
	def := defaultRetention(h.d.DB.Site)
	if rule != nil {
		if err := rule.Validate(); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		def = *rule
	}
	type entry struct {
		Item    string `json:"item"`
		Title   string `json:"title"`
		Name    string `json:"name"`
		Own     bool   `json:"own"`
		Removes int    `json:"removes"`
	}
	out := struct {
		Pictures []entry `json:"pictures"`
		Stacks   int     `json:"stacks"`
		Versions int     `json:"versions"`
	}{Pictures: []entry{}}
	h.d.DB.Read(func(s *item.Store) {
		for id, plans := range s.PlanAllRetention(def, time.Now(), isPicture) {
			it, err := s.Get(id)
			if err != nil {
				continue
			}
			for _, p := range plans {
				out.Stacks++
				out.Versions += len(p.Remove)
				if len(out.Pictures) < previewLimit {
					out.Pictures = append(out.Pictures, entry{Item: it.Key, Title: it.Title, Name: p.Keep[0].Name, Own: p.Own, Removes: len(p.Remove)})
				}
			}
		}
	})
	writeJSON(w, http.StatusOK, out)
}

func (h authHandlers) runAlbumRetention(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.adminOnly(w, r)
	if !ok {
		return
	}
	done, err := CleanAlbum(h.d.DB, actor, time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "the clean-up stopped: "+err.Error())
		return
	}
	n := 0
	for _, c := range done {
		n += c.Removed
	}
	writeJSON(w, http.StatusOK, map[string]int{"stacks": len(done), "versions": n})
}

// CleanAlbum removes the older versions of every picture its own rule or
// the album's default does not keep, and records what it did in the audit
// trail as actor (ADR-0104). The server runs it every day while the
// default says automatic.
func CleanAlbum(db *storage.DB, actor string, now time.Time) ([]item.CleanedStack, error) {
	def := defaultRetention(db.Site)
	var done []item.CleanedStack
	err := db.Write(func(s *item.Store) error {
		var err error
		done, err = s.ApplyRetention(def, now, isPicture)
		return err
	})
	if n := removedOf(done); n > 0 || err != nil {
		detail := map[string]string{"stacks": strconv.Itoa(len(done)), "versions": strconv.Itoa(n)}
		if err != nil {
			detail["error"] = err.Error()
		}
		db.Audit.Record(audit.Entry{Event: audit.AlbumCleaned, Actor: actor, Detail: detail})
	}
	return done, err
}

func removedOf(done []item.CleanedStack) (n int) {
	for _, c := range done {
		n += c.Removed
	}
	return n
}

// planJSON is a plan as the album shows it: the versions kept, those
// removed, and which kept ones only the text keeps.
type planJSON struct {
	Rule    retentionJSON    `json:"rule"`
	Own     bool             `json:"own"`
	Default retentionJSON    `json:"default"`
	Keep    []attachmentJSON `json:"keep"`
	Remove  []attachmentJSON `json:"remove"`
	Used    []item.ID        `json:"used"`
	// Versions are all versions, newest first, each with what becomes of
	// it: keep, used (kept because the text shows it) or remove.
	Versions []versionJSON `json:"versions"`
	// Removed are the versions a clean-up just removed; the rest of the
	// plan is the state after it.
	Removed []attachmentJSON `json:"removed,omitempty"`
}

type versionJSON struct {
	attachmentJSON
	State string `json:"state"`
}

func toPlanJSON(p item.RetentionPlan, def item.Retention) planJSON {
	out := planJSON{Rule: ruleJSON(p.Rule), Own: p.Own, Default: ruleJSON(def), Keep: []attachmentJSON{}, Remove: []attachmentJSON{}, Used: p.Used}
	for _, a := range p.Keep {
		out.Keep = append(out.Keep, toAttachmentJSON(a))
	}
	for _, a := range p.Remove {
		out.Remove = append(out.Remove, toAttachmentJSON(a))
	}
	if out.Used == nil {
		out.Used = []item.ID{}
	}
	state := map[item.ID]string{}
	for _, a := range p.Remove {
		state[a.ID] = "remove"
	}
	for _, id := range p.Used {
		state[id] = "used"
	}
	out.Versions = []versionJSON{}
	for _, a := range p.Versions {
		st := state[a.ID]
		if st == "" {
			st = "keep"
		}
		out.Versions = append(out.Versions, versionJSON{toAttachmentJSON(a), st})
	}
	return out
}

func (h itemHandlers) pictureRetention(w http.ResponseWriter, r *http.Request) {
	rule, ok := ruleQuery(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "keep and days are numbers")
		return
	}
	def := defaultRetention(h.d.DB.Site)
	h.read(w, r, func(x *item.Session) (any, error) {
		it, err := resolveItem(x, r.PathValue("ref"))
		if err != nil {
			return nil, err
		}
		p, err := x.PlanRetention(it.ID, item.ID(r.PathValue("id")), rule, def, time.Now())
		if err != nil {
			return nil, err
		}
		return toPlanJSON(p, def), nil
	})
}

func (h itemHandlers) setPictureRetention(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Keep    int  `json:"keep"`
		Days    int  `json:"days"`
		Default bool `json:"default"`
	}
	if !decode(w, r, &req) {
		return
	}
	var rule *item.Retention
	if !req.Default {
		rule = &item.Retention{Keep: req.Keep, Days: req.Days}
	}
	def := defaultRetention(h.d.DB.Site)
	h.write(w, r, http.StatusOK, func(x *item.Session, _ *item.Store) (any, error) {
		it, err := resolveItem(x, r.PathValue("ref"))
		if err != nil {
			return nil, err
		}
		id := item.ID(r.PathValue("id"))
		if err := x.SetPictureRetention(it.ID, id, rule); err != nil {
			return nil, err
		}
		p, err := x.PlanRetention(it.ID, id, nil, def, time.Now())
		if err != nil {
			return nil, err
		}
		return toPlanJSON(p, def), nil
	})
}

func (h itemHandlers) cleanPicture(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Keep *int `json:"keep"`
		Days *int `json:"days"`
	}
	if r.ContentLength != 0 && !decode(w, r, &req) {
		return
	}
	var rule *item.Retention
	if req.Keep != nil || req.Days != nil {
		rule = &item.Retention{}
		if req.Keep != nil {
			rule.Keep = *req.Keep
		}
		if req.Days != nil {
			rule.Days = *req.Days
		}
	}
	def := defaultRetention(h.d.DB.Site)
	h.write(w, r, http.StatusOK, func(x *item.Session, _ *item.Store) (any, error) {
		it, err := resolveItem(x, r.PathValue("ref"))
		if err != nil {
			return nil, err
		}
		id, now := item.ID(r.PathValue("id")), time.Now()
		done, err := x.CleanVersions(it.ID, id, rule, def, now)
		if err != nil {
			return nil, err
		}
		after, err := x.PlanRetention(it.ID, id, rule, def, now)
		if err != nil {
			return nil, err
		}
		out := toPlanJSON(after, def)
		out.Removed = []attachmentJSON{}
		for _, a := range done.Remove {
			out.Removed = append(out.Removed, toAttachmentJSON(a))
		}
		return out, nil
	})
}
