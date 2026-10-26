package site

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// TestEnginesAreKeptAndReplayed: process engines (ADR-0126) are set,
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
