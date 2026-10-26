// Package engine reads processes from process engines for the pages that
// show them (ADR-0129): the newest version of each process, its BPMN model,
// and counts of where its instances are; and their forms and decisions
// (ADR-0143): each form as it is saved, with its form-js schema, and the
// newest deployed version of each decision, with its DMN model. It speaks
// the API of atlas (github.com/pblumer/atlas). The credential comes from
// the environment and is sent to the engine only.
package engine

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
	"github.com/dbuchs7/wikiwikibangbang/internal/connector"
	"github.com/dbuchs7/wikiwikibangbang/internal/site"
)

// Errors of a read. Use errors.Is; the message says more, never the
// credential.
var (
	// ErrNoCredential: the engine's variable is not set.
	ErrNoCredential = errors.New("the engine's credential is not set")
	// ErrRefused: the engine refused the credential.
	ErrRefused = errors.New("the engine refused the credential")
	// ErrNotFound: the engine has no such process, form or decision for
	// the credential.
	ErrNotFound = errors.New("not found at the engine")
	// ErrUnavailable: the engine did not answer, or not as expected.
	ErrUnavailable = errors.New("the engine is not available")
)

// Timeout bounds one read of an engine.
const Timeout = 15 * time.Second

// AllowPrivateVar, set to "on", lets readers reach engines in the server's
// own network. As for connectors (ADR-0127), it is the operator's decision,
// so it comes from the environment; without it an engine must be on the
// public internet.
const AllowPrivateVar = "WWBB_ENGINE_ALLOW_PRIVATE"

// TokenVar names the environment variable that holds an engine's
// credential, e.g. WWBB_ENGINE_ATLAS_PROD_TOKEN: the ID in capitals, other
// characters as _, named as a connector's (ADR-0077).
func TokenVar(id string) string {
	return "WWBB_ENGINE_" + strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r - 'a' + 'A'
		case r >= '0' && r <= '9':
			return r
		}
		return '_'
	}, id) + "_TOKEN"
}

// Reader reads engines.
type Reader struct {
	getenv func(string) string
	http   *http.Client
}

// NewReader returns a reader that takes credentials from getenv
// (os.Getenv when nil) and calls the engines with client. Without one, it
// makes one with the Timeout, through the environment's proxy, kept to
// public addresses unless AllowPrivateVar allows others.
func NewReader(getenv func(string) string, client *http.Client) *Reader {
	if getenv == nil {
		getenv = os.Getenv
	}
	if client == nil {
		var transport http.RoundTripper = &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: Timeout,
			MaxIdleConnsPerHost:   4,
			IdleConnTimeout:       90 * time.Second,
		}
		if getenv(AllowPrivateVar) != "on" {
			transport = connector.PublicOnly(transport)
		}
		client = &http.Client{Timeout: Timeout, Transport: transport}
	}
	// An engine's API does not redirect; following one would send the
	// credential where the administrator did not point it.
	c := *client
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Reader{getenv: getenv, http: &c}
}

// HasCredential reports whether the engine's variable is set.
func (r *Reader) HasCredential(id string) bool { return r.getenv(TokenVar(id)) != "" }

// Process is one version of a process.
type Process struct {
	// ID is the process's BPMN ID, which a process block names.
	ID         string    `json:"process"`
	Name       string    `json:"name"`
	Version    int       `json:"version"`
	DeployedAt time.Time `json:"deployedAt"`
	// key is the engine's key of this version; it stays on the server.
	key uint64
}

// Diagram is the newest version of a process with its BPMN model.
type Diagram struct {
	Process
	XML string `json:"xml"`
}

// Operations says where the instances of one version of a process are.
type Operations struct {
	Process string `json:"process"`
	Version int    `json:"version"`
	// Instances are running, Finished have ended, Tokens wait on elements,
	// Incidents of them are held by an incident.
	Instances int `json:"instances"`
	Finished  int `json:"finished"`
	Tokens    int `json:"tokens"`
	Incidents int `json:"incidents"`
	// Exact is false when the engine could count incidents only in part.
	Exact    bool      `json:"exact"`
	Elements []Element `json:"elements"`
}

// Element counts the tokens of one BPMN element: waiting there, held there
// by an incident, and passed through it.
type Element struct {
	ID        string `json:"element"`
	Tokens    int    `json:"tokens"`
	Incidents int    `json:"incidents"`
	Passed    int    `json:"passed"`
}

// Processes returns the newest version of each process the credential may
// read, by name.
func (r *Reader) Processes(ctx context.Context, e site.Engine) ([]Process, error) {
	var all []struct {
		Key        uint64 `json:"key"`
		ProcessID  string `json:"processId"`
		Name       string `json:"name"`
		Version    int    `json:"version"`
		DeployedAt int64  `json:"deployedAt"`
	}
	if err := r.get(ctx, e, "/api/v1/processes", func(body io.Reader) error { return json.NewDecoder(body).Decode(&all) }); err != nil {
		return nil, err
	}
	newest := map[string]Process{}
	for _, p := range all {
		if p.ProcessID == "" {
			continue
		}
		if n, ok := newest[p.ProcessID]; !ok || p.Version > n.Version {
			newest[p.ProcessID] = Process{ID: p.ProcessID, Name: p.Name, Version: p.Version, DeployedAt: time.Unix(p.DeployedAt, 0).UTC(), key: p.Key}
		}
	}
	out := make([]Process, 0, len(newest))
	for _, p := range newest {
		out = append(out, p)
	}
	slices.SortFunc(out, func(a, b Process) int {
		return cmp.Or(strings.Compare(strings.ToLower(cmp.Or(a.Name, a.ID)), strings.ToLower(cmp.Or(b.Name, b.ID))), strings.Compare(a.ID, b.ID))
	})
	return out, nil
}

// newest returns the newest version of one process.
func (r *Reader) newest(ctx context.Context, e site.Engine, process string) (Process, error) {
	all, err := r.Processes(ctx, e)
	if err != nil {
		return Process{}, err
	}
	i := slices.IndexFunc(all, func(p Process) bool { return p.ID == process })
	if i < 0 {
		return Process{}, fmt.Errorf("%w: process %s", ErrNotFound, process)
	}
	return all[i], nil
}

// Diagram returns the newest version of a process with its BPMN model.
func (r *Reader) Diagram(ctx context.Context, e site.Engine, process string) (Diagram, error) {
	p, err := r.newest(ctx, e, process)
	if err != nil {
		return Diagram{}, err
	}
	var xml strings.Builder
	err = r.get(ctx, e, "/api/v1/processes/"+strconv.FormatUint(p.key, 10)+"/xml", func(body io.Reader) error {
		_, err := io.Copy(&xml, body)
		return err
	})
	return Diagram{Process: p, XML: xml.String()}, err
}

// Operations returns where the instances of the newest version of a
// process are. Nothing else of the engine's answer is passed on: no
// incident messages, no instance keys, no variables.
func (r *Reader) Operations(ctx context.Context, e site.Engine, process string) (Operations, error) {
	p, err := r.newest(ctx, e, process)
	if err != nil {
		return Operations{}, err
	}
	var rt struct {
		Instances int `json:"instances"`
		Finished  int `json:"finished"`
		Tokens    int `json:"tokens"`
		Elements  []struct {
			ElementID  string `json:"elementId"`
			Tokens     int    `json:"tokens"`
			Visits     int    `json:"visits"`
			Incidents  int    `json:"incidents"`
			Terminated int    `json:"terminated"`
		} `json:"elements"`
		IncidentTotal       int  `json:"incidentTotal"`
		IncidentCountsExact bool `json:"incidentCountsExact"`
	}
	err = r.get(ctx, e, "/api/v1/processes/"+strconv.FormatUint(p.key, 10)+"/runtime", func(body io.Reader) error { return json.NewDecoder(body).Decode(&rt) })
	if err != nil {
		return Operations{}, err
	}
	out := Operations{Process: p.ID, Version: p.Version, Instances: rt.Instances, Finished: rt.Finished, Tokens: rt.Tokens,
		Incidents: rt.IncidentTotal, Exact: rt.IncidentCountsExact, Elements: []Element{}}
	for _, el := range rt.Elements {
		out.Elements = append(out.Elements, Element{ID: el.ElementID, Tokens: el.Tokens, Incidents: el.Incidents, Passed: max(el.Visits-el.Tokens-el.Terminated, 0)})
	}
	return out, nil
}

// Form is a form the engine keeps, as it is saved now: atlas keeps one
// version of a form, not a history.
type Form struct {
	// ID is the form's ID, which a form block names and a user task binds.
	ID      string    `json:"form"`
	Name    string    `json:"name"`
	SavedAt time.Time `json:"savedAt"`
}

// FormSchema is a form with its form-js schema, a JSON object passed on as
// the engine answered it.
type FormSchema struct {
	Form
	Schema json.RawMessage `json:"schema"`
}

// Forms returns the forms the credential may see, by name.
func (r *Reader) Forms(ctx context.Context, e site.Engine) ([]Form, error) {
	var all []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		SavedAt int64  `json:"savedAt"`
	}
	if err := r.get(ctx, e, "/api/v1/forms", func(body io.Reader) error { return json.NewDecoder(body).Decode(&all) }); err != nil {
		return nil, err
	}
	out := make([]Form, 0, len(all))
	for _, f := range all {
		if f.ID != "" {
			out = append(out, Form{ID: f.ID, Name: f.Name, SavedAt: time.Unix(f.SavedAt, 0).UTC()})
		}
	}
	slices.SortFunc(out, func(a, b Form) int {
		return cmp.Or(strings.Compare(strings.ToLower(cmp.Or(a.Name, a.ID)), strings.ToLower(cmp.Or(b.Name, b.ID))), strings.Compare(a.ID, b.ID))
	})
	return out, nil
}

// Form returns a form with its schema. atlas answers any form by its ID to
// whoever is signed in, as a task needs it; the form is first looked up in
// the list, so that a block shows only a form the credential may see, as
// the picker offers it.
func (r *Reader) Form(ctx context.Context, e site.Engine, id string) (FormSchema, error) {
	all, err := r.Forms(ctx, e)
	if err != nil {
		return FormSchema{}, err
	}
	i := slices.IndexFunc(all, func(f Form) bool { return f.ID == id })
	if i < 0 {
		return FormSchema{}, fmt.Errorf("%w: form %s", ErrNotFound, id)
	}
	var got struct {
		Schema json.RawMessage `json:"schema"`
	}
	if err := r.get(ctx, e, "/api/v1/forms/"+url.PathEscape(id), func(body io.Reader) error { return json.NewDecoder(body).Decode(&got) }); err != nil {
		return FormSchema{}, err
	}
	if s := bytes.TrimSpace(got.Schema); len(s) == 0 || s[0] != '{' {
		return FormSchema{}, fmt.Errorf("%w: the schema of form %s is no JSON object", ErrUnavailable, id)
	}
	return FormSchema{Form: all[i], Schema: got.Schema}, nil
}

// Decision is the newest deployed version of a decision.
type Decision struct {
	// ID is the decision's ID as the engine lists it, which a decision
	// block names; atlas lists a decision by its name when it has one.
	ID string `json:"decision"`
	// Model is the name of the DMN model the decision is deployed in; one
	// model may hold several decisions.
	Model      string    `json:"model"`
	Version    int       `json:"version"`
	DeployedAt time.Time `json:"deployedAt"`
	// key is the engine's key of the deployment; it stays on the server.
	key uint64
}

// DecisionModel is the newest deployed version of a decision with the DMN
// model it is deployed in.
type DecisionModel struct {
	Decision
	XML string `json:"xml"`
}

// Decisions returns the newest deployed version of each decision the
// credential may read, by ID.
func (r *Reader) Decisions(ctx context.Context, e site.Engine) ([]Decision, error) {
	var all []struct {
		Key        uint64 `json:"key"`
		DecisionID string `json:"decisionId"`
		Version    int    `json:"version"`
		ModelName  string `json:"modelName"`
		DeployedAt int64  `json:"deployedAt"`
	}
	if err := r.get(ctx, e, "/api/v1/decision-deployments", func(body io.Reader) error { return json.NewDecoder(body).Decode(&all) }); err != nil {
		return nil, err
	}
	newest := map[string]Decision{}
	for _, d := range all {
		if d.DecisionID == "" {
			continue
		}
		if n, ok := newest[d.DecisionID]; !ok || d.Version > n.Version {
			newest[d.DecisionID] = Decision{ID: d.DecisionID, Model: d.ModelName, Version: d.Version, DeployedAt: time.Unix(d.DeployedAt, 0).UTC(), key: d.Key}
		}
	}
	out := make([]Decision, 0, len(newest))
	for _, d := range newest {
		out = append(out, d)
	}
	slices.SortFunc(out, func(a, b Decision) int {
		return cmp.Or(strings.Compare(strings.ToLower(a.ID), strings.ToLower(b.ID)), strings.Compare(a.ID, b.ID))
	})
	return out, nil
}

// DecisionModel returns the newest deployed version of a decision with the
// DMN model it is deployed in.
func (r *Reader) DecisionModel(ctx context.Context, e site.Engine, id string) (DecisionModel, error) {
	all, err := r.Decisions(ctx, e)
	if err != nil {
		return DecisionModel{}, err
	}
	i := slices.IndexFunc(all, func(d Decision) bool { return d.ID == id })
	if i < 0 {
		return DecisionModel{}, fmt.Errorf("%w: decision %s", ErrNotFound, id)
	}
	var xml strings.Builder
	err = r.get(ctx, e, "/api/v1/decision-deployments/"+strconv.FormatUint(all[i].key, 10)+"/xml", func(body io.Reader) error {
		_, err := io.Copy(&xml, body)
		return err
	})
	return DecisionModel{Decision: all[i], XML: xml.String()}, err
}

// get reads path of the engine and hands the answer to read, at most
// budget EngineResponseBytes of it.
func (r *Reader) get(ctx context.Context, e site.Engine, path string, read func(io.Reader) error) error {
	if e.Kind != site.EngineAtlas {
		return fmt.Errorf("%w: kind %q is not read", ErrUnavailable, e.Kind)
	}
	token := r.getenv(TokenVar(e.ID))
	if token == "" {
		return fmt.Errorf("%w: set %s", ErrNoCredential, TokenVar(e.ID))
	}
	u, err := url.Parse(e.BaseURL + path)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json, application/xml")
	res, err := r.http.Do(req)
	if errors.Is(err, connector.ErrPrivateAddress) {
		return fmt.Errorf("%w: %s is not on the public internet; set %s=on to allow engines in the server's own network", ErrUnavailable, u.Host, AllowPrivateVar)
	}
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
		return fmt.Errorf("%w (%d)", ErrRefused, res.StatusCode)
	case res.StatusCode == http.StatusNotFound:
		return fmt.Errorf("%w (%s)", ErrNotFound, path)
	case res.StatusCode != http.StatusOK:
		return fmt.Errorf("%w: it answered %d", ErrUnavailable, res.StatusCode)
	}
	limit := budget.Get().EngineResponseBytes
	body := &capped{r: io.LimitReader(res.Body, limit+1), left: limit}
	if err := read(body); err != nil {
		if body.over {
			return fmt.Errorf("%w: its answer is larger than %d bytes", ErrUnavailable, limit)
		}
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if body.over {
		return fmt.Errorf("%w: its answer is larger than %d bytes", ErrUnavailable, limit)
	}
	return nil
}

// capped reads at most left bytes and fails beyond them.
type capped struct {
	r    io.Reader
	left int64
	over bool
}

func (c *capped) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.left -= int64(n)
	if c.left < 0 {
		c.over = true
		return 0, errors.New("too large")
	}
	return n, err
}
