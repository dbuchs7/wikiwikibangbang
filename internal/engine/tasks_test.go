package engine

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
	"github.com/dbuchs7/wikiwikibangbang/internal/site"
)

// fakeTaskList answers GET /api/v1/tasks as atlas does for the token
// "lister": newest first, in pages cut by limit, continued by before.
func fakeTaskList(t *testing.T, keys []uint64) (*httptest.Server, *[]string) {
	t.Helper()
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.RequestURI())
		if r.Header.Get("Authorization") != "Bearer lister" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if r.URL.Path != "/api/v1/tasks" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		limit := 2 // the fake cuts pages short, so paging is exercised
		var before uint64
		_, _ = fmt.Sscan(r.URL.Query().Get("before"), &before)
		var page []string
		next := uint64(0)
		for _, k := range keys {
			if before != 0 && k >= before {
				continue
			}
			if len(page) == limit {
				break
			}
			page = append(page, fmt.Sprintf(`{"key":%d,"processInstanceKey":%d,"elementInstanceKey":%d,"processId":"urlaub","elementId":"genehmigen","name":" Urlaub genehmigen ","assignee":"anna","candidateGroups":"hr, finance,","formId":"antrag","priority":80,"dueDate":1793000000000,"createdAt":1792000000000%s}`,
				k, k+1, k+2, map[bool]string{true: `,"wizard":"public"`, false: ""}[k == 3]))
			next = k
		}
		truncated := next != 0 && next != keys[len(keys)-1]
		body := `{"items":[` + joinJSON(page) + `],"truncated":` + fmt.Sprint(truncated)
		if truncated {
			body += fmt.Sprintf(`,"nextCursor":"%d"`, next)
		}
		_, _ = w.Write([]byte(body + `}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &asked
}

func joinJSON(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ","
		}
		out += p
	}
	return out
}

// TestOpenTasksReadsEveryPageWithTheTaskListCredential (ADR-0151): the
// open tasks are read with the task list credential, never the engine's
// other one, page after page, and each is read as the engine gives it.
func TestOpenTasksReadsEveryPageWithTheTaskListCredential(t *testing.T) {
	srv, asked := fakeTaskList(t, []uint64{9, 7, 5, 3, 1})
	env := map[string]string{"WWBB_ENGINE_ATLAS_PROD_TASKLIST_TOKEN": "lister", "WWBB_ENGINE_ATLAS_PROD_TOKEN": "diagrams"}
	r := NewReader(func(k string) string { return env[k] }, srv.Client())
	e := site.Engine{ID: "atlas-prod", Kind: site.EngineAtlas, BaseURL: srv.URL}

	if !r.HasTaskListCredential("atlas-prod") || r.HasTaskListCredential("other") {
		t.Error("HasTaskListCredential reads the wrong variable")
	}
	tasks, complete, err := r.OpenTasks(context.Background(), e)
	if err != nil || !complete {
		t.Fatalf("OpenTasks = %v %v", complete, err)
	}
	var keys []uint64
	for _, tk := range tasks {
		keys = append(keys, tk.Key)
	}
	if !slices.Equal(keys, []uint64{9, 7, 5, 3, 1}) {
		t.Fatalf("keys = %v (asked %v)", keys, *asked)
	}
	if len(*asked) != 3 || (*asked)[1] != "/api/v1/tasks?before=7&limit=500" {
		t.Errorf("requests = %v", *asked)
	}
	got := tasks[0]
	if got.Name != "Urlaub genehmigen" || got.Process != "urlaub" || got.Element != "genehmigen" || got.Assignee != "anna" ||
		!slices.Equal(got.Groups, []string{"hr", "finance"}) || got.Form != "antrag" || got.Priority != 80 ||
		got.ProcessInstanceKey != 10 || got.ElementInstanceKey != 11 ||
		!got.Due.Equal(time.UnixMilli(1793000000000)) || !got.Created.Equal(time.UnixMilli(1792000000000)) {
		t.Errorf("task = %+v", got)
	}
	if tasks[3].Wizard != "public" || tasks[0].Wizard != "" {
		t.Errorf("wizards = %q %q", tasks[3].Wizard, tasks[0].Wizard)
	}
	if TaskURL(e, 9) != srv.URL+"/#/tasks/t/9" {
		t.Errorf("TaskURL = %s", TaskURL(e, 9))
	}
}

// TestOpenTasksStopsAtItsBudget: beyond budget EngineOpenTasks the list is
// cut, and says it is not complete.
func TestOpenTasksStopsAtItsBudget(t *testing.T) {
	b := budget.Default()
	b.EngineOpenTasks = 3
	budget.Set(b)
	t.Cleanup(func() { budget.Set(budget.Default()) })
	srv, _ := fakeTaskList(t, []uint64{9, 7, 5, 3, 1})
	r := NewReader(func(k string) string { return map[string]string{"WWBB_ENGINE_ATLAS_PROD_TASKLIST_TOKEN": "lister"}[k] }, srv.Client())
	tasks, complete, err := r.OpenTasks(context.Background(), site.Engine{ID: "atlas-prod", Kind: site.EngineAtlas, BaseURL: srv.URL})
	if err != nil || complete || len(tasks) != 3 {
		t.Fatalf("OpenTasks = %d tasks, complete %v, %v", len(tasks), complete, err)
	}
}

// TestOpenTasksNeedsItsOwnCredential: without the task list variable the
// list is not read, whatever else is set; a refused one says so.
func TestOpenTasksNeedsItsOwnCredential(t *testing.T) {
	srv, asked := fakeTaskList(t, []uint64{1})
	e := site.Engine{ID: "atlas-prod", Kind: site.EngineAtlas, BaseURL: srv.URL}
	r := NewReader(func(k string) string { return map[string]string{"WWBB_ENGINE_ATLAS_PROD_TOKEN": "diagrams"}[k] }, srv.Client())
	if _, _, err := r.OpenTasks(context.Background(), e); !errors.Is(err, ErrNoCredential) || len(*asked) != 0 {
		t.Fatalf("without the variable: %v, asked %v", err, *asked)
	}
	r = NewReader(func(k string) string { return map[string]string{"WWBB_ENGINE_ATLAS_PROD_TASKLIST_TOKEN": "wrong"}[k] }, srv.Client())
	if _, _, err := r.OpenTasks(context.Background(), e); !errors.Is(err, ErrRefused) {
		t.Fatalf("with a wrong token: %v", err)
	}
}

// TestOpenTasksRefusesAListThatDoesNotMove: a cursor answered twice would
// read for ever.
func TestOpenTasksRefusesAListThatDoesNotMove(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"items":[{"key":5}],"truncated":true,"nextCursor":"5"}`))
	}))
	t.Cleanup(srv.Close)
	r := NewReader(func(k string) string { return map[string]string{"WWBB_ENGINE_ATLAS_PROD_TASKLIST_TOKEN": "x"}[k] }, srv.Client())
	if _, _, err := r.OpenTasks(context.Background(), site.Engine{ID: "atlas-prod", Kind: site.EngineAtlas, BaseURL: srv.URL}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("a list that does not move: %v", err)
	}
}
