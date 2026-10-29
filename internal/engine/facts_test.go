package engine

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/site"
)

// TestTaskFactsReadTheFeedWithTheEventsCredential covers ADR-0151's
// reading of the facts: the events credential is sent, the page's task
// facts are answered in order — the others passed over —, with the cursor
// to read on from, and a cursor the feed no longer holds is ErrCursorGone.
func TestTaskFactsReadTheFeedWithTheEventsCredential(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Header.Get("Authorization") != "Bearer follower" || r.URL.Path != "/api/v1/events":
			w.WriteHeader(http.StatusForbidden)
		case r.URL.Query().Get("after") == "3":
			w.WriteHeader(http.StatusGone)
			_, _ = w.Write([]byte(`{"error":"dropped","oldest":"7"}`))
		case r.URL.Query().Get("after") == "":
			_, _ = w.Write([]byte(`{"events":[
				{"specversion":"1.0","id":"a","type":"atlas.task.created","data":{"taskKey":18446744073709551000,"processId":"urlaub","elementId":"genehmigen","name":" Urlaub ","candidateGroups":"hr, Team Lead","dueDate":"2026-11-02T15:30:00Z","assigned":false}},
				{"specversion":"1.0","id":"b","type":"atlas.order.line.fulfilled","data":{"taskKey":1}},
				{"specversion":"1.0","id":"c","type":"atlas.task.cancelled","data":{"taskKey":12}},
				{"specversion":"1.0","id":"d","type":"atlas.task.renamed","data":{"taskKey":13}},
				{"specversion":"1.0","id":"e","type":"atlas.task.completed","data":{}}],
				"next":"42","more":true}`))
		default:
			_, _ = w.Write([]byte(`{"events":[],"next":"","more":false}`))
		}
	}))
	t.Cleanup(srv.Close)
	r := NewReader(func(k string) string { return map[string]string{EventsVar("atlas"): "follower"}[k] }, srv.Client())
	e := site.Engine{ID: "atlas", Kind: site.EngineAtlas, BaseURL: srv.URL}
	if !r.HasEventsCredential("atlas") || r.HasEventsCredential("other") {
		t.Error("HasEventsCredential")
	}
	facts, next, more, err := r.TaskFacts(context.Background(), e, "")
	if err != nil || next != "42" || !more || len(facts) != 2 {
		t.Fatalf("TaskFacts = %+v %q %v %v", facts, next, more, err)
	}
	f := facts[0]
	if f.Kind != FactCreated || f.Key != 18446744073709551000 || f.Process != "urlaub" || f.Element != "genehmigen" || f.Name != "Urlaub" ||
		len(f.Groups) != 2 || f.Groups[1] != "Team Lead" || !f.Due.Equal(time.Date(2026, 11, 2, 15, 30, 0, 0, time.UTC)) {
		t.Errorf("created = %+v", f)
	}
	if facts[1].Kind != FactCancelled || facts[1].Key != 12 {
		t.Errorf("cancelled = %+v", facts[1])
	}
	// An empty page keeps the cursor it was read after.
	if _, next, more, err := r.TaskFacts(context.Background(), e, "42"); err != nil || next != "42" || more {
		t.Errorf("an empty page = %q %v %v", next, more, err)
	}
	if _, _, _, err := r.TaskFacts(context.Background(), e, "3"); !errors.Is(err, ErrCursorGone) {
		t.Errorf("a dropped cursor = %v", err)
	}
	if _, _, _, err := NewReader(func(string) string { return "" }, srv.Client()).TaskFacts(context.Background(), e, ""); !errors.Is(err, ErrNoCredential) {
		t.Errorf("without the credential = %v", err)
	}
}
