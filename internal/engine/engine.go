// Package engine reads processes from process engines for the pages that
// show them (ADR-0129): the newest version of each process, its BPMN model,
// and counts of where its instances are. It speaks the API of atlas
// (github.com/pblumer/atlas). The credential comes from the environment and
// is sent to the engine only.
package engine

import (
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
	// ErrNotFound: the engine has no such process for the credential.
	ErrNotFound = errors.New("the engine has no such process")
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
		return Process{}, fmt.Errorf("%w: %s", ErrNotFound, process)
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
