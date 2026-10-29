package engineconn

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// memSealer seals by xor with a key per ID, enough to tell sealed from
// plain and a destroyed key from a kept one.
type memSealer struct {
	keys     map[string]byte
	shredded []string
}

func (m *memSealer) Seal(key string, pt, aad []byte) ([]byte, error) {
	if _, ok := m.keys[key]; !ok {
		m.keys[key] = byte(len(m.keys) + 1)
	}
	out := append([]byte{}, aad...)
	out = append(out, 0)
	for _, b := range pt {
		out = append(out, b^m.keys[key])
	}
	return out, nil
}

func (m *memSealer) Open(key string, ct, aad []byte) ([]byte, error) {
	k, ok := m.keys[key]
	if !ok {
		return nil, errors.New("destroyed")
	}
	if !bytes.HasPrefix(ct, append(append([]byte{}, aad...), 0)) {
		return nil, errors.New("does not authenticate")
	}
	var out []byte
	for _, b := range ct[len(aad)+1:] {
		out = append(out, b^k)
	}
	return out, nil
}

func (m *memSealer) Shred(keys ...string) error {
	for _, key := range keys {
		delete(m.keys, key)
		m.shredded = append(m.shredded, key)
	}
	return nil
}

type journal struct{ records [][]byte }

func (j *journal) append(b []byte) error {
	j.records = append(j.records, append([]byte{}, b...))
	return nil
}

func newConns(t *testing.T, j *journal, sl *memSealer) *Connections {
	t.Helper()
	n := 0
	c, err := New(j.records, j.append, sl, func() string { n++; return fmt.Sprintf("k%d-%d", len(j.records), n) })
	if err != nil {
		t.Fatal(err)
	}
	return c
}

var anna = Profile{Subject: "u-17", Username: "anna", Name: "Anna Muster", Email: "anna@example.org", Groups: []string{"hr"}}

// TestAConnectionKeepsItsTokenSealedAndEndsWithItsKey covers ADR-0151's
// connections: a connection names who its person is at the engine; its
// refresh token is never written in plaintext, is replaced by a renewal
// and read back after a restart; ending it destroys its key, so neither
// the journal nor a copy of it can give the token again.
func TestAConnectionKeepsItsTokenSealedAndEndsWithItsKey(t *testing.T) {
	j, sl := &journal{}, &memSealer{keys: map[string]byte{}}
	c := newConns(t, j, sl)
	if _, err := c.Connect("bert", "atlas", Profile{Username: "x"}, "r"); err == nil {
		t.Error("a connection without the engine's ID of its person was kept")
	}
	got, err := c.Connect("bert", "atlas", anna, "atlasor_first")
	if err != nil || got.Username != "anna" || got.Engine != "atlas" || got.Connected.IsZero() {
		t.Fatalf("Connect = %+v, %v", got, err)
	}
	if err := c.Renewed("bert", "atlas", "atlasor_second", nil); err != nil {
		t.Fatal(err)
	}
	for _, r := range j.records {
		if strings.Contains(string(r), "atlasor_") {
			t.Fatalf("a refresh token in plaintext: %s", r)
		}
	}
	if err := c.Renewed("carla", "atlas", "x", nil); !errors.Is(err, ErrNotConnected) {
		t.Errorf("renewing nobody's connection = %v", err)
	}

	again := newConns(t, j, sl)
	if tok, err := again.Refresh("bert", "atlas"); err != nil || tok != "atlasor_second" {
		t.Fatalf("after a restart, Refresh = %q, %v", tok, err)
	}
	if cs := again.Of("bert", ""); len(cs) != 1 || cs[0].Email != "anna@example.org" || cs[0].Groups[0] != "hr" {
		t.Fatalf("Of = %+v", cs)
	}
	if again.Count() != 1 || len(again.Keys()) != 1 {
		t.Errorf("count %d, keys %v", again.Count(), again.Keys())
	}
	key := again.Keys()[0]

	if err := again.End("bert", "atlas", "disconnected"); err != nil {
		t.Fatal(err)
	}
	if len(sl.shredded) != 1 || sl.shredded[0] != key {
		t.Errorf("destroyed %v, want the connection's key %s", sl.shredded, key)
	}
	if _, ok := again.Get("bert", "atlas"); ok {
		t.Error("an ended connection is still there")
	}
	if _, err := again.Refresh("bert", "atlas"); !errors.Is(err, ErrNotConnected) {
		t.Errorf("Refresh of an ended connection = %v", err)
	}
	if err := again.End("bert", "atlas", "twice"); !errors.Is(err, ErrNotConnected) {
		t.Errorf("ending twice = %v", err)
	}
	if third := newConns(t, j, sl); third.Count() != 0 {
		t.Error("an ended connection came back after a restart")
	}
}

// TestConnectingAgainReplacesTheConnection: one connection per person and
// engine; the replaced one's key is destroyed, and a renewal recorded for
// it by a round that ran meanwhile does not bring its token back.
func TestConnectingAgainReplacesTheConnection(t *testing.T) {
	j, sl := &journal{}, &memSealer{keys: map[string]byte{}}
	c := newConns(t, j, sl)
	if _, err := c.Connect("bert", "atlas", anna, "atlasor_old"); err != nil {
		t.Fatal(err)
	}
	old := c.Keys()[0]
	if _, err := c.Connect("bert", "atlas", Profile{Subject: "u-18", Username: "anna2"}, "atlasor_new"); err != nil {
		t.Fatal(err)
	}
	if c.Count() != 1 || len(sl.shredded) != 1 || sl.shredded[0] != old {
		t.Fatalf("count %d, destroyed %v", c.Count(), sl.shredded)
	}
	// A renewal of the old connection, as journaled late.
	j.records = append(j.records, []byte(`{"v":1,"kind":"renewed","user":"bert","engine":"atlas","key":"`+old+`","sealed":"AA=="}`))
	again := newConns(t, j, sl)
	if tok, err := again.Refresh("bert", "atlas"); err != nil || tok != "atlasor_new" {
		t.Fatalf("Refresh = %q, %v", tok, err)
	}
	if cn, _ := again.Get("bert", "atlas"); cn.Username != "anna2" {
		t.Errorf("profile %+v", cn)
	}
	if _, err := New([][]byte{[]byte(`{"v":2}`)}, j.append, sl, nil); err == nil {
		t.Error("a record of an unknown version was read")
	}
	if _, err := New([][]byte{[]byte(`{"v":1,"kind":"connected","user":"x","engine":"y"}`)}, j.append, sl, nil); err == nil {
		t.Error("a connection without its token was read")
	}
}

// TestEndingAnEngineEndsItsConnections and the journal is compacted once
// renewals make it long.
func TestEndingAnEngineEndsItsConnectionsAndTheJournalIsCompacted(t *testing.T) {
	j, sl := &journal{}, &memSealer{keys: map[string]byte{}}
	c := newConns(t, j, sl)
	for _, u := range []string{"bert", "vera"} {
		if _, err := c.Connect(u, "atlas", anna, "atlasor_"+u); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.Connect("bert", "other", anna, "atlasor_other"); err != nil {
		t.Fatal(err)
	}
	if c.Compacted() != nil {
		t.Error("a short journal is compacted")
	}
	for i := range 120 {
		if err := c.Renewed("vera", "atlas", fmt.Sprintf("atlasor_%d", i), nil); err != nil {
			t.Fatal(err)
		}
	}
	records := c.Compacted()
	if len(records) != 3 {
		t.Fatalf("compacted to %d records, want 3", len(records))
	}
	c.Rewritten(len(records))
	if c.Compacted() != nil {
		t.Error("compacted again at once")
	}
	j.records = records
	if tok, err := newConns(t, j, sl).Refresh("vera", "atlas"); err != nil || tok != "atlasor_119" {
		t.Fatalf("after compaction, Refresh = %q, %v", tok, err)
	}

	users, err := c.EndEngine("atlas", "the engine was removed")
	if err != nil || strings.Join(users, ",") != "bert,vera" {
		t.Fatalf("EndEngine = %v, %v", users, err)
	}
	if cs := c.Of("", ""); len(cs) != 1 || cs[0].Engine != "other" {
		t.Errorf("left %+v", cs)
	}
}
