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
// version (ADR-0129): its ID, which process blocks name and which never
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

// TaskLinks are the rules by which the user tasks of an engine become
// linked tasks of the wiki (ADR-0151).
type TaskLinks struct {
	Engine string `json:"engine"`
	// Enabled switches the synchronisation on; rules kept while it is off
	// link nothing.
	Enabled bool       `json:"enabled"`
	Rules   []TaskRule `json:"rules"`
}

// TaskRule links the tasks its filter matches: an empty list matches all.
// The first rule that matches a task links it.
type TaskRule struct {
	ID string `json:"id"`
	// Processes and Elements are BPMN IDs; Groups are candidate groups as
	// the engine names them.
	Processes []string `json:"processes,omitempty"`
	Elements  []string `json:"elements,omitempty"`
	Groups    []string `json:"groups,omitempty"`
	// Space and Type are where a linked task is made.
	Space string `json:"space"`
	Type  string `json:"type"`
	// Labels are the labels a linked task is made with; {process},
	// {element} and {group} stand for the task's.
	Labels []string `json:"labels,omitempty"`
	// DueField is the date field the task's due date goes into, if any.
	DueField string `json:"dueField,omitempty"`
	// Completed and Cancelled are the finished states an ended task's item
	// moves to.
	Completed string `json:"completed"`
	Cancelled string `json:"cancelled"`
}

// maxTaskRules bounds the rules of one engine; it is a shape of the
// settings, as maxEngines is.
const maxTaskRules = 20

// Normalize tidies what an administrator typed.
func (l TaskLinks) Normalize() TaskLinks {
	l.Engine = strings.TrimSpace(l.Engine)
	rules := make([]TaskRule, 0, len(l.Rules))
	for _, r := range l.Rules {
		r.ID, r.Space, r.Type = strings.TrimSpace(r.ID), strings.TrimSpace(r.Space), strings.TrimSpace(r.Type)
		r.DueField, r.Completed, r.Cancelled = strings.TrimSpace(r.DueField), strings.TrimSpace(r.Completed), strings.TrimSpace(r.Cancelled)
		r.Processes, r.Elements, r.Groups, r.Labels = tidyList(r.Processes), tidyList(r.Elements), tidyList(r.Groups), tidyList(r.Labels)
		rules = append(rules, r)
	}
	l.Rules = rules
	return l
}

func tidyList(in []string) []string {
	var out []string
	for _, v := range in {
		if v = strings.TrimSpace(v); v != "" && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

// Validate checks the shape of task links; that their spaces, types and
// states exist is the item store's to say.
func (l TaskLinks) Validate() error {
	if len(l.Rules) > maxTaskRules {
		return fmt.Errorf("%w: at most %d rules", ErrInvalid, maxTaskRules)
	}
	seen := map[string]bool{}
	for _, r := range l.Rules {
		switch {
		case !item.ValidEngineID(r.ID):
			return fmt.Errorf("%w: %q is not a rule ID: lower-case letters, digits, dots and hyphens, at most 40", ErrInvalid, r.ID)
		case seen[r.ID]:
			return fmt.Errorf("%w: the rule %q is there twice", ErrInvalid, r.ID)
		case r.Space == "" || r.Type == "" || r.Completed == "" || r.Cancelled == "":
			return fmt.Errorf("%w: the rule %q needs a space, a type and the states of a completed and a cancelled task", ErrInvalid, r.ID)
		}
		seen[r.ID] = true
		for _, list := range [][]string{r.Processes, r.Elements, r.Groups, r.Labels} {
			if len(list) > 50 {
				return fmt.Errorf("%w: the rule %q names more than 50 values in one list", ErrInvalid, r.ID)
			}
			for _, v := range list {
				if len(v) > 200 || strings.ContainsAny(v, "\n\r") {
					return fmt.Errorf("%w: %q in the rule %q is not one line of at most 200 characters", ErrInvalid, v, r.ID)
				}
			}
		}
	}
	return nil
}

// TaskLinks returns the task links of an engine; an engine without any has
// none, switched off.
func (s *Settings) TaskLinks(engine string) TaskLinks {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.taskLinks[engine]
	if !ok {
		return TaskLinks{Engine: engine, Rules: []TaskRule{}}
	}
	l.Rules = slices.Clone(l.Rules)
	return l
}

// SetTaskLinks replaces the task links of an engine and returns them as
// kept.
func (s *Settings) SetTaskLinks(actor string, l TaskLinks) (TaskLinks, error) {
	l = l.Normalize()
	if err := l.Validate(); err != nil {
		return l, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.engines[l.Engine]; !ok {
		return l, fmt.Errorf("%w: no engine %q", ErrInvalid, l.Engine)
	}
	return l, s.record(event{Kind: taskLinksSet, Actor: actor, TaskLinks: &l})
}
