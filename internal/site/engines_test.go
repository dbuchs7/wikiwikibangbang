package site

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// TestEnginesAreKeptAndReplayed: process engines (ADR-0129) are set,
// listed by name, removed and replayed from the journal; what an
// administrator types is tidied, and what is not an engine is refused.
func TestEnginesAreKeptAndReplayed(t *testing.T) {
	var records [][]byte
	j := func(r []byte) error { records = append(records, r); return nil }
	s, _ := New(nil, j)
	if got := s.Engines(); len(got) != 0 {
		t.Fatalf("engines before any: %+v", got)
	}
	kept, err := s.SetEngine("admin", Engine{ID: " prod ", Name: " Atlas production ", Kind: EngineAtlas, BaseURL: " https://atlas.example.org/ ", Operations: true})
	if err != nil {
		t.Fatal(err)
	}
	if kept.ID != "prod" || kept.Name != "Atlas production" || kept.BaseURL != "https://atlas.example.org" {
		t.Fatalf("normalized %+v", kept)
	}
	if _, err := s.SetEngine("admin", Engine{ID: "test", Name: "Atlas test", Kind: EngineAtlas, BaseURL: "http://10.0.0.7:8080"}); err != nil {
		t.Fatal(err)
	}
	if got := s.Engines(); len(got) != 2 || got[0].Name != "Atlas production" || got[1].ID != "test" {
		t.Fatalf("by name %+v", got)
	}
	if err := s.RemoveEngine("admin", "test"); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveEngine("admin", "test"); !errors.Is(err, ErrInvalid) {
		t.Errorf("removed twice: %v", err)
	}
	r, err := New(records, j)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := r.Engine("prod"); !ok || got != kept || len(r.Engines()) != 1 {
		t.Fatalf("replayed %+v %v", r.Engines(), ok)
	}
	if _, err := New([][]byte{[]byte(`{"v":1,"kind":"EngineSet"}`)}, j); err == nil {
		t.Error("an engine event without an engine replayed")
	}

	good := Engine{ID: "atlas", Name: "Atlas", Kind: EngineAtlas, BaseURL: "https://atlas.example.org/base"}
	for name, change := range map[string]func(*Engine){
		"no ID":           func(e *Engine) { e.ID = "" },
		"ID in capitals":  func(e *Engine) { e.ID = "Atlas" },
		"ID too long":     func(e *Engine) { e.ID = strings.Repeat("a", 41) },
		"no name":         func(e *Engine) { e.Name = " " },
		"name with <":     func(e *Engine) { e.Name = "<b>" },
		"name too long":   func(e *Engine) { e.Name = strings.Repeat("n", 81) },
		"unknown kind":    func(e *Engine) { e.Kind = "other" },
		"no address":      func(e *Engine) { e.BaseURL = "" },
		"not http":        func(e *Engine) { e.BaseURL = "ftp://atlas.example.org" },
		"no host":         func(e *Engine) { e.BaseURL = "https://" },
		"with a user":     func(e *Engine) { e.BaseURL = "https://user:secret@atlas.example.org" },
		"with a query":    func(e *Engine) { e.BaseURL = "https://atlas.example.org/?token=x" },
		"with a fragment": func(e *Engine) { e.BaseURL = "https://atlas.example.org/#x" },
		"address too long": func(e *Engine) {
			e.BaseURL = "https://atlas.example.org/" + strings.Repeat("p", 512)
		},
	} {
		e := good
		change(&e)
		if _, err := s.SetEngine("admin", e); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	for i := len(s.Engines()); i < maxEngines; i++ {
		e := good
		e.ID = fmt.Sprintf("e%d", i)
		if _, err := s.SetEngine("admin", e); err != nil {
			t.Fatal(err)
		}
	}
	more := good
	more.ID = "one-more"
	if _, err := s.SetEngine("admin", more); !errors.Is(err, ErrInvalid) {
		t.Errorf("more than %d engines: %v", maxEngines, err)
	}
	if _, err := s.SetEngine("admin", Engine{ID: "prod", Name: "Renamed", Kind: EngineAtlas, BaseURL: "https://atlas.example.org"}); err != nil {
		t.Errorf("an engine changed at the limit: %v", err)
	}
}

// TestTaskLinksAreKeptReplayedAndGoWithTheirEngine (ADR-0151): an engine's
// task links are tidied, kept, replayed and removed with the engine; what
// is no rule is refused.
func TestTaskLinksAreKeptReplayedAndGoWithTheirEngine(t *testing.T) {
	var records [][]byte
	j := func(r []byte) error { records = append(records, r); return nil }
	s, _ := New(nil, j)
	if got := s.TaskLinks("prod"); got.Enabled || len(got.Rules) != 0 || got.Engine != "prod" {
		t.Fatalf("links before any: %+v", got)
	}
	rule := TaskRule{ID: " holidays ", Processes: []string{" urlaub ", "urlaub", ""}, Groups: []string{"hr"},
		Space: " PLAT ", Type: "engine-task", Labels: []string{"atlas", "{process}"}, DueField: " due ",
		Completed: "done", Cancelled: "cancelled"}
	if _, err := s.SetTaskLinks("admin", TaskLinks{Engine: "prod", Enabled: true, Rules: []TaskRule{rule}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("links of an engine that does not exist: %v", err)
	}
	if _, err := s.SetEngine("admin", Engine{ID: "prod", Name: "Atlas", Kind: EngineAtlas, BaseURL: "https://atlas.example.org"}); err != nil {
		t.Fatal(err)
	}
	kept, err := s.SetTaskLinks("admin", TaskLinks{Engine: " prod ", Enabled: true, Rules: []TaskRule{rule}})
	if err != nil {
		t.Fatal(err)
	}
	r0 := kept.Rules[0]
	if kept.Engine != "prod" || r0.ID != "holidays" || r0.Space != "PLAT" || r0.DueField != "due" ||
		fmt.Sprint(r0.Processes) != "[urlaub]" || fmt.Sprint(r0.Labels) != "[atlas {process}]" {
		t.Fatalf("tidied %+v", kept)
	}
	got := s.TaskLinks("prod")
	got.Rules[0].ID = "changed"
	if s.TaskLinks("prod").Rules[0].ID != "holidays" {
		t.Error("TaskLinks hands out its own rules")
	}
	replayed, err := New(records, j)
	if err != nil {
		t.Fatal(err)
	}
	if l := replayed.TaskLinks("prod"); !l.Enabled || len(l.Rules) != 1 || l.Rules[0].ID != "holidays" {
		t.Fatalf("replayed %+v", l)
	}
	if err := s.RemoveEngine("admin", "prod"); err != nil {
		t.Fatal(err)
	}
	if l := s.TaskLinks("prod"); l.Enabled || len(l.Rules) != 0 {
		t.Errorf("links outlived their engine: %+v", l)
	}
	if _, err := New([][]byte{[]byte(`{"v":1,"kind":"TaskLinksSet"}`)}, j); err == nil {
		t.Error("a task links event without links replayed")
	}

	if _, err := s.SetEngine("admin", Engine{ID: "prod", Name: "Atlas", Kind: EngineAtlas, BaseURL: "https://atlas.example.org"}); err != nil {
		t.Fatal(err)
	}
	many := make([]TaskRule, maxTaskRules+1)
	for i := range many {
		many[i] = TaskRule{ID: fmt.Sprintf("r%d", i), Space: "PLAT", Type: "t", Completed: "d", Cancelled: "c"}
	}
	for name, links := range map[string]TaskLinks{
		"too many rules": {Engine: "prod", Rules: many},
		"no rule ID":     {Engine: "prod", Rules: []TaskRule{{Space: "PLAT", Type: "t", Completed: "d", Cancelled: "c"}}},
		"a rule twice":   {Engine: "prod", Rules: []TaskRule{many[0], many[0]}},
		"no space":       {Engine: "prod", Rules: []TaskRule{{ID: "a", Type: "t", Completed: "d", Cancelled: "c"}}},
		"no end states":  {Engine: "prod", Rules: []TaskRule{{ID: "a", Space: "PLAT", Type: "t"}}},
		"a long value":   {Engine: "prod", Rules: []TaskRule{{ID: "a", Space: "PLAT", Type: "t", Completed: "d", Cancelled: "c", Groups: []string{strings.Repeat("g", 201)}}}},
		"two lines":      {Engine: "prod", Rules: []TaskRule{{ID: "a", Space: "PLAT", Type: "t", Completed: "d", Cancelled: "c", Labels: []string{"a\nb"}}}},
		"too many values": {Engine: "prod", Rules: []TaskRule{{ID: "a", Space: "PLAT", Type: "t", Completed: "d", Cancelled: "c",
			Elements: distinct(51)}}},
	} {
		if _, err := s.SetTaskLinks("admin", links); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// distinct is n different values.
func distinct(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("e%d", i)
	}
	return out
}
