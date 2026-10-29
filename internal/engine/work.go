package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/dbuchs7/wikiwikibangbang/internal/site"
)

// A person works a task of an engine with the access token of their own
// grant (ADR-0151): the engine decides whether they may, and its refusal
// says why (ErrNotAllowed, ErrRejected).

func taskPath(key uint64) string { return "/api/v1/tasks/" + strconv.FormatUint(key, 10) }

// TaskAs reads an open task as the person sees it; a task that ended, or
// that the person may not see, is ErrNotFound.
func (r *Reader) TaskAs(ctx context.Context, e site.Engine, bearer string, key uint64) (Task, error) {
	var got wireTask
	err := r.call(ctx, e, request{method: http.MethodGet, path: taskPath(key), accept: "application/json", bearer: bearer, work: true},
		func(_ string, body io.Reader) error { return json.NewDecoder(body).Decode(&got) })
	if err != nil {
		return Task{}, err
	}
	if got.Key == 0 {
		return Task{}, fmt.Errorf("%w: it answered no task", ErrUnavailable)
	}
	return got.task(), nil
}

// FormAs reads a form with its schema, as the person's grant reaches it.
func (r *Reader) FormAs(ctx context.Context, e site.Engine, bearer, id string) (FormSchema, error) {
	var got struct {
		ID      string          `json:"id"`
		Name    string          `json:"name"`
		SavedAt int64           `json:"savedAt"`
		Schema  json.RawMessage `json:"schema"`
	}
	err := r.call(ctx, e, request{method: http.MethodGet, path: "/api/v1/forms/" + url.PathEscape(id), accept: "application/json", bearer: bearer, work: true},
		func(_ string, body io.Reader) error { return json.NewDecoder(body).Decode(&got) })
	if err != nil {
		return FormSchema{}, err
	}
	if s := bytes.TrimSpace(got.Schema); len(s) == 0 || s[0] != '{' {
		return FormSchema{}, fmt.Errorf("%w: the schema of form %s is no JSON object", ErrUnavailable, id)
	}
	return FormSchema{Form: Form{ID: id, Name: got.Name}, Schema: got.Schema}, nil
}

// ValuesAs reads the values a task's form opens with: the variables of
// the task's scope that the person's grant reaches, as one JSON object.
// Values the person may not read are none.
func (r *Reader) ValuesAs(ctx context.Context, e site.Engine, bearer string, t Task) (json.RawMessage, error) {
	scope := t.ElementInstanceKey
	if scope == 0 {
		scope = t.ProcessInstanceKey
	}
	var got json.RawMessage
	err := r.call(ctx, e, request{method: http.MethodGet, path: "/api/v1/instances/" + strconv.FormatUint(scope, 10) + "/variables", accept: "application/json", bearer: bearer, work: true},
		func(_ string, body io.Reader) error { return json.NewDecoder(body).Decode(&got) })
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrNotAllowed) {
		return json.RawMessage(`{}`), nil
	}
	if err != nil {
		return nil, err
	}
	if s := bytes.TrimSpace(got); len(s) == 0 || s[0] != '{' {
		return json.RawMessage(`{}`), nil
	}
	return got, nil
}

// Claim takes a task on for the person, Unclaim gives it back; both
// answer who holds it now.
func (r *Reader) Claim(ctx context.Context, e site.Engine, bearer string, key uint64) (string, error) {
	return r.assign(ctx, e, bearer, taskPath(key)+"/claim", []byte(`{}`))
}

// Unclaim: see Claim.
func (r *Reader) Unclaim(ctx context.Context, e site.Engine, bearer string, key uint64) (string, error) {
	return r.assign(ctx, e, bearer, taskPath(key)+"/unclaim", []byte(`{}`))
}

func (r *Reader) assign(ctx context.Context, e site.Engine, bearer, path string, body []byte) (string, error) {
	var got struct {
		Assignee string `json:"assignee"`
	}
	err := r.call(ctx, e, request{method: http.MethodPost, path: path, accept: "application/json", body: body, bearer: bearer, work: true},
		func(_ string, body io.Reader) error { return json.NewDecoder(body).Decode(&got) })
	return got.Assignee, err
}

// Complete completes a task as the person, with the values of its form, a
// JSON object, or none. The engine records the person as who completed
// it.
func (r *Reader) Complete(ctx context.Context, e site.Engine, bearer string, key uint64, variables json.RawMessage) error {
	if len(bytes.TrimSpace(variables)) == 0 {
		variables = json.RawMessage(`{}`)
	}
	body, err := json.Marshal(map[string]json.RawMessage{"variables": variables})
	if err != nil {
		return err
	}
	return r.call(ctx, e, request{method: http.MethodPost, path: taskPath(key) + "/complete", accept: "application/json", body: body, bearer: bearer, work: true},
		func(string, io.Reader) error { return nil })
}
