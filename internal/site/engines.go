package site

import (
	"cmp"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

// Engine is a process engine whose processes pages show in their newest
// version (ADR-0126): its ID, which process blocks name and which never
// changes, a name for people, its kind and the address of its API. The
// credential is not a setting: it comes from the environment.
type Engine struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	BaseURL string `json:"baseURL"`
	// Operations allows readers to see where the engine's instances are:
	// counts per element of a process, nothing else.
	Operations bool `json:"operations"`
}

// EngineAtlas is the kind of an atlas engine (github.com/pblumer/atlas).
const EngineAtlas = "atlas"

// EngineKinds are the kinds of process engine.
var EngineKinds = []string{EngineAtlas}

// maxEngines bounds how many process engines a site keeps.
const maxEngines = 20

// Normalize tidies what an administrator typed.
func (e Engine) Normalize() Engine {
	e.ID, e.Name, e.Kind = strings.TrimSpace(e.ID), strings.TrimSpace(e.Name), strings.TrimSpace(e.Kind)
	e.BaseURL = strings.TrimRight(strings.TrimSpace(e.BaseURL), "/")
	return e
}

// Validate checks an engine's settings. Its address may be private: an
// administrator sets it, and engines run inside organisations' networks.
func (e Engine) Validate() error {
	switch {
	case !item.ValidEngineID(e.ID):
		return fmt.Errorf("%w: %q is not an engine ID: lower-case letters, digits, dots and hyphens, at most 40", ErrInvalid, e.ID)
	case e.Name == "" || strings.ContainsAny(e.Name, "<>\n") || len([]rune(e.Name)) > 80:
		return fmt.Errorf("%w: the name is a single line of 1 to 80 characters", ErrInvalid)
	case !slices.Contains(EngineKinds, e.Kind):
		return fmt.Errorf("%w: %q is not a kind of engine (%s)", ErrInvalid, e.Kind, strings.Join(EngineKinds, ", "))
	}
	u, err := url.Parse(e.BaseURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || len(e.BaseURL) > 512 {
		return fmt.Errorf("%w: %q is not the http or https address of an engine, without user, query or fragment", ErrInvalid, e.BaseURL)
	}
	return nil
}

// Engines returns the process engines by name.
func (s *Settings) Engines() []Engine {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Engine{}
	for _, e := range s.engines {
		out = append(out, e)
	}
	slices.SortFunc(out, func(a, b Engine) int { return cmp.Or(strings.Compare(a.Name, b.Name), strings.Compare(a.ID, b.ID)) })
	return out
}

// Engine returns one process engine.
func (s *Settings) Engine(id string) (Engine, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.engines[id]
	return e, ok
}

// SetEngine adds a process engine or replaces the one of its ID, and
// returns it as kept.
func (s *Settings) SetEngine(actor string, e Engine) (Engine, error) {
	e = e.Normalize()
	if err := e.Validate(); err != nil {
		return e, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.engines[e.ID]; !ok && len(s.engines) >= maxEngines {
		return e, fmt.Errorf("%w: at most %d engines", ErrInvalid, maxEngines)
	}
	return e, s.record(event{Kind: engineSet, Actor: actor, Engine: &e})
}

// RemoveEngine removes a process engine. Pages that show its processes
// keep their blocks, which then say that the engine is not set up.
func (s *Settings) RemoveEngine(actor, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.engines[id]; !ok {
		return fmt.Errorf("%w: no engine %q", ErrInvalid, id)
	}
	return s.record(event{Kind: engineRemoved, Actor: actor, Asset: id})
}
