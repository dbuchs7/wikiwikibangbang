package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/dbuchs7/orbithek/internal/site"
)

// The capabilities an engine's system grant may hold (ADR-0152), as the
// engine names its scopes: processes, forms and sketches; the list of
// open tasks; the feed of task facts.
const (
	ScopeDiagrams = "diagrams"
	ScopeTaskList = "tasklist"
	ScopeEvents   = "events"
)

// SystemGrants are the engines' system grants (ADR-0152), which credentials
// the environment does not set are taken from: whether one holds a
// capability, an access token of it, and that the engine refused one.
type SystemGrants interface {
	SystemHolds(engineID, scope string) bool
	SystemToken(ctx context.Context, e site.Engine) (string, error)
	SystemRefused(engineID string)
}

// WithSystemGrants has the reader take a credential the environment does
// not set from the engine's system grant, where it holds the capability.
func (r *Reader) WithSystemGrants(g SystemGrants) *Reader {
	r.system = g
	return r
}

// scopeOf is the capability the credential of variable v stands for.
func scopeOf(id, v string) string {
	switch v {
	case TokenVar(id):
		return ScopeDiagrams
	case TaskListVar(id):
		return ScopeTaskList
	case EventsVar(id):
		return ScopeEvents
	}
	return ""
}

// granted reports whether the credential of variable v comes from the
// engine's system grant: the environment does not set it, and the grant
// holds its capability.
func (r *Reader) granted(id, v string) bool {
	return r.getenv(v) == "" && r.system != nil && r.system.SystemHolds(id, scopeOf(id, v))
}

// ByGrant reports whether the engine's open tasks are read with its
// system grant, which tells a task that ended from one that left the
// grant's reach (ADR-0152).
func (r *Reader) ByGrant(id string) bool { return r.granted(id, TaskListVar(id)) }

// FromGrant reports, for the engine's three credentials, which come from
// its system grant rather than the environment.
func (r *Reader) FromGrant(id string) map[string]bool {
	return map[string]bool{ScopeDiagrams: r.granted(id, TokenVar(id)), ScopeTaskList: r.granted(id, TaskListVar(id)), ScopeEvents: r.granted(id, EventsVar(id))}
}

// SystemGrant is what an engine says its administrator approved for this
// server, read with the system grant from GET /api/v1/auth/me.
type SystemGrant struct {
	ClientID     string       `json:"clientId"`
	ClientName   string       `json:"clientName"`
	ApprovedBy   string       `json:"approvedBy"`
	ApprovedAt   time.Time    `json:"approvedAt"`
	Capabilities []Capability `json:"capabilities"`
	Preconsent   bool         `json:"preconsent"`
}

// Capability is one capability of a system grant and the projects it is
// confined to, none for all.
type Capability struct {
	Scope    string   `json:"scope"`
	Projects []string `json:"projects"`
}

// TaskEnded is an engine's answer that a task ended (ADR-0152): Ended is
// "completed" or "cancelled".
type TaskEnded struct{ Ended string }

func (t TaskEnded) Error() string { return "engine: the task " + t.Ended }

// TaskEnd reads what became of a task the open tasks no longer list, with
// the task list credential: nil while it is open, a TaskEnded when the
// engine says it ended, ErrNotFound when it is unknown or outside the
// reach of the credential — the engine cannot tell those apart for us.
func (r *Reader) TaskEnd(ctx context.Context, e site.Engine, key uint64) error {
	rq := request{method: http.MethodGet, path: taskPath(key), accept: "application/json", tokenVar: TaskListVar(e.ID), gone: true}
	err := r.call(ctx, e, rq, func(_ string, body io.Reader) error {
		var got wireTask
		return json.NewDecoder(body).Decode(&got)
	})
	return err
}

// goneOf reads a 410 answer to a task's read: which way it ended.
func goneOf(body io.Reader) error {
	var got struct {
		Ended string `json:"ended"`
	}
	if err := json.NewDecoder(io.LimitReader(body, 4096)).Decode(&got); err != nil || (got.Ended != "completed" && got.Ended != "cancelled") {
		return fmt.Errorf("%w: it said the task is gone, but not how it ended", ErrUnavailable)
	}
	return TaskEnded{Ended: got.Ended}
}
