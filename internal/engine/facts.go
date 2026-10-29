package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/site"
)

// EventsVar names the environment variable of the credential an engine's
// event feed is followed with (ADR-0151), e.g.
// WWBB_ENGINE_ATLAS_PROD_EVENTS_TOKEN; on atlas a token of the scope
// events.
func EventsVar(id string) string {
	return strings.TrimSuffix(TokenVar(id), "_TOKEN") + "_EVENTS_TOKEN"
}

// HasEventsCredential reports whether the engine's events variable is set.
func (r *Reader) HasEventsCredential(id string) bool { return r.getenv(EventsVar(id)) != "" }

// ErrCursorGone is a cursor older than what the engine's feed keeps: what
// happened after it is no longer told, so the open tasks are compared
// instead.
var ErrCursorGone = errors.New("the engine's feed no longer holds what follows the cursor")

// The kinds of a task fact.
const (
	FactCreated    = "created"
	FactAssigned   = "assigned"
	FactUnassigned = "unassigned"
	FactCompleted  = "completed"
	FactCancelled  = "cancelled"
)

// TaskFact is a turn in the life of a user task, as the engine's feed
// tells it: never who holds the task, nor its form or values.
type TaskFact struct {
	Kind string
	Key  uint64
	// Process, Element and Name are the task's, where the engine still
	// knows its process; Groups those it is offered to.
	Process, Element, Name string
	Groups                 []string
	Due                    time.Time
}

// factsPage is how many events one request of the feed asks for.
const factsPage = 1000

// TaskFacts reads one page of the engine's feed after the cursor, with the
// events credential, and answers its task facts, in order, the cursor to
// read on from and whether more follow at once. An empty cursor reads
// from the oldest event the feed keeps. Events of other kinds are passed
// over.
func (r *Reader) TaskFacts(ctx context.Context, e site.Engine, after string) (facts []TaskFact, next string, more bool, err error) {
	q := url.Values{"limit": {strconv.Itoa(factsPage)}}
	if after != "" {
		q.Set("after", after)
	}
	var page struct {
		Events []struct {
			Type string          `json:"type"`
			Data json.RawMessage `json:"data"`
		} `json:"events"`
		Next string `json:"next"`
		More bool   `json:"more"`
	}
	rq := request{method: http.MethodGet, path: "/api/v1/events?" + q.Encode(), accept: "application/json", tokenVar: EventsVar(e.ID), feed: true}
	if err := r.call(ctx, e, rq, func(_ string, body io.Reader) error { return json.NewDecoder(body).Decode(&page) }); err != nil {
		return nil, "", false, err
	}
	for _, ev := range page.Events {
		kind, ok := strings.CutPrefix(ev.Type, "atlas.task.")
		if !ok {
			continue
		}
		var d struct {
			TaskKey         uint64 `json:"taskKey"`
			ProcessID       string `json:"processId"`
			ElementID       string `json:"elementId"`
			Name            string `json:"name"`
			CandidateGroups string `json:"candidateGroups"`
			DueDate         string `json:"dueDate"`
		}
		if json.Unmarshal(ev.Data, &d) != nil || d.TaskKey == 0 {
			continue
		}
		switch kind {
		case FactCreated, FactAssigned, FactUnassigned, FactCompleted, FactCancelled:
		default:
			continue
		}
		f := TaskFact{Kind: kind, Key: d.TaskKey, Process: d.ProcessID, Element: d.ElementID, Name: strings.TrimSpace(d.Name)}
		for _, g := range strings.Split(d.CandidateGroups, ",") {
			if g = strings.TrimSpace(g); g != "" {
				f.Groups = append(f.Groups, g)
			}
		}
		if t, err := time.Parse(time.RFC3339, d.DueDate); err == nil {
			f.Due = t.UTC()
		}
		facts = append(facts, f)
	}
	if page.Next == "" {
		page.Next = after
	}
	return facts, page.Next, page.More, nil
}

// OpenTask reads one open task with the task list credential; a task that
// ended is ErrNotFound.
func (r *Reader) OpenTask(ctx context.Context, e site.Engine, key uint64) (Task, error) {
	var got wireTask
	rq := request{method: http.MethodGet, path: taskPath(key), accept: "application/json", tokenVar: TaskListVar(e.ID)}
	if err := r.call(ctx, e, rq, func(_ string, body io.Reader) error { return json.NewDecoder(body).Decode(&got) }); err != nil {
		return Task{}, err
	}
	if got.Key == 0 {
		return Task{}, fmt.Errorf("%w: it answered no task", ErrUnavailable)
	}
	return got.task(), nil
}
