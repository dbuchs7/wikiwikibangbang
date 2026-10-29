package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
	"github.com/dbuchs7/wikiwikibangbang/internal/site"
)

// TaskListVar names the environment variable that holds the credential an
// engine's open user tasks are listed with (ADR-0151), e.g.
// WWBB_ENGINE_ATLAS_PROD_TASKLIST_TOKEN. On atlas it is a token of the
// scope tasklist an administrator minted, so it lists every open task and
// can do nothing else.
func TaskListVar(id string) string {
	return strings.TrimSuffix(TokenVar(id), "_TOKEN") + "_TASKLIST_TOKEN"
}

// HasTaskListCredential reports whether the engine's task list variable is
// set.
func (r *Reader) HasTaskListCredential(id string) bool { return r.getenv(TaskListVar(id)) != "" }

// Task is an open user task of an engine, as a task link reads it.
type Task struct {
	Key                uint64
	ProcessInstanceKey uint64
	ElementInstanceKey uint64
	// Process and Element are the BPMN IDs of the process and the task;
	// Name is the task's name.
	Process, Element, Name string
	// Assignee is who holds it, as the engine names people: a username.
	Assignee string
	// Groups are the groups it is offered to.
	Groups []string
	// Form is the form the task names; empty without one.
	Form     string
	Priority int
	// Due and Created are zero when unknown.
	Due, Created time.Time
	// Wizard is "public" for a step of a public wizard, which belongs to
	// an anonymous filler and is never linked.
	Wizard string
}

// wireTask is a user task as the engine answers it.
type wireTask struct {
	Key                uint64 `json:"key"`
	ProcessInstanceKey uint64 `json:"processInstanceKey"`
	ElementInstanceKey uint64 `json:"elementInstanceKey"`
	ProcessID          string `json:"processId"`
	ElementID          string `json:"elementId"`
	Name               string `json:"name"`
	Assignee           string `json:"assignee"`
	CandidateGroups    string `json:"candidateGroups"`
	FormID             string `json:"formId"`
	Priority           int    `json:"priority"`
	DueDate            int64  `json:"dueDate"`
	CreatedAt          int64  `json:"createdAt"`
	Wizard             string `json:"wizard"`
}

func (it wireTask) task() Task {
	t := Task{Key: it.Key, ProcessInstanceKey: it.ProcessInstanceKey, ElementInstanceKey: it.ElementInstanceKey,
		Process: it.ProcessID, Element: it.ElementID, Name: strings.TrimSpace(it.Name), Assignee: it.Assignee,
		Form: it.FormID, Priority: it.Priority, Wizard: it.Wizard}
	for _, g := range strings.Split(it.CandidateGroups, ",") {
		if g = strings.TrimSpace(g); g != "" {
			t.Groups = append(t.Groups, g)
		}
	}
	if it.DueDate > 0 {
		t.Due = time.UnixMilli(it.DueDate).UTC()
	}
	if it.CreatedAt > 0 {
		t.Created = time.UnixMilli(it.CreatedAt).UTC()
	}
	return t
}

// tasksPage is how many tasks one request of the list asks for.
const tasksPage = 500

// OpenTasks lists the engine's open user tasks with the task list
// credential, newest first, in pages, up to budget EngineOpenTasks.
// complete is false when the budget cut the list: the tasks beyond it are
// not known to be open or not.
func (r *Reader) OpenTasks(ctx context.Context, e site.Engine) (tasks []Task, complete bool, err error) {
	return r.openTasks(ctx, e, "")
}

// OpenTasksAs lists the open user tasks a person sees, with the access
// token of the person's grant, as OpenTasks lists them all: the engine
// decides which they are (ADR-0151).
func (r *Reader) OpenTasksAs(ctx context.Context, e site.Engine, bearer string) (tasks []Task, complete bool, err error) {
	if bearer == "" {
		return nil, false, fmt.Errorf("%w: no grant", ErrNoCredential)
	}
	return r.openTasks(ctx, e, bearer)
}

func (r *Reader) openTasks(ctx context.Context, e site.Engine, bearer string) (tasks []Task, complete bool, err error) {
	limit := budget.Get().EngineOpenTasks
	before := ""
	for {
		q := url.Values{"limit": {strconv.Itoa(tasksPage)}}
		if before != "" {
			q.Set("before", before)
		}
		var page struct {
			Items      []wireTask `json:"items"`
			Truncated  bool       `json:"truncated"`
			NextCursor string     `json:"nextCursor"`
		}
		rq := request{method: http.MethodGet, path: "/api/v1/tasks?" + q.Encode(), accept: "application/json", tokenVar: TaskListVar(e.ID), bearer: bearer}
		if err := r.call(ctx, e, rq, func(_ string, body io.Reader) error { return json.NewDecoder(body).Decode(&page) }); err != nil {
			return nil, false, err
		}
		for _, it := range page.Items {
			if it.Key == 0 {
				continue
			}
			if len(tasks) == limit {
				return tasks, false, nil
			}
			tasks = append(tasks, it.task())
		}
		if !page.Truncated || page.NextCursor == "" {
			return tasks, true, nil
		}
		if page.NextCursor == before {
			return nil, false, fmt.Errorf("%w: the task list did not move past %s", ErrUnavailable, before)
		}
		before = page.NextCursor
	}
}

// TaskURL is where the engine shows a task in its own task app.
func TaskURL(e site.Engine, key uint64) string {
	return e.BaseURL + "/#/tasks/t/" + strconv.FormatUint(key, 10)
}

// ClientIDVar and ClientSecretVar name the environment variables of this
// server's registration as an OAuth client at an engine (ADR-0151), e.g.
// WWBB_ENGINE_ATLAS_PROD_CLIENT_ID: people connect to the engine through
// it.
func ClientIDVar(id string) string {
	return strings.TrimSuffix(TokenVar(id), "_TOKEN") + "_CLIENT_ID"
}

// ClientSecretVar: see ClientIDVar.
func ClientSecretVar(id string) string {
	return strings.TrimSuffix(TokenVar(id), "_TOKEN") + "_CLIENT_SECRET"
}

// Person is who a person's grant belongs to at an engine: the engine's ID
// of the account, its username, name and mail address, and the names of
// its groups.
type Person struct {
	ID, Username, Name, Email string
	Groups                    []string
}

// Me reads who the access token of a person's grant belongs to, from
// GET /api/v1/auth/me.
func (r *Reader) Me(ctx context.Context, e site.Engine, bearer string) (Person, error) {
	var got struct {
		User struct {
			ID          string `json:"id"`
			Username    string `json:"username"`
			Email       string `json:"email"`
			DisplayName string `json:"displayName"`
			Groups      []struct {
				Name string `json:"name"`
			} `json:"groups"`
		} `json:"user"`
	}
	err := r.call(ctx, e, request{method: http.MethodGet, path: "/api/v1/auth/me", accept: "application/json", bearer: bearer},
		func(_ string, body io.Reader) error { return json.NewDecoder(body).Decode(&got) })
	if err != nil {
		return Person{}, err
	}
	u := got.User
	if u.ID == "" {
		return Person{}, fmt.Errorf("%w: it did not say who the grant belongs to", ErrUnavailable)
	}
	p := Person{ID: u.ID, Username: u.Username, Name: strings.TrimSpace(u.DisplayName), Email: u.Email}
	for _, g := range u.Groups {
		if g.Name != "" {
			p.Groups = append(p.Groups, g.Name)
		}
	}
	return p, nil
}
