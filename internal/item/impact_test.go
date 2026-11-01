package item

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// impactIDs are the nodes of an impact walk with their distances.
func impactIDs(m Impact) []string {
	var out []string
	for _, n := range m.Nodes {
		out = append(out, n.ID+"@"+string(rune('0'+n.Distance)))
	}
	return out
}

func hop(id ID, d int) string { return string(id) + "@" + string(rune('0'+d)) }

func picAt(it Item, a ID, d int) string { return pictureNodeID(it.ID, a) + "@" + string(rune('0'+d)) }

// What depends on Home: its children, what relates to it, its picture,
// and one hop further the pictures of those and the copy of its picture;
// each with its distance and the edges that reached it (ADR-0159).
func TestImpactWalksWhatDependsOnAnItem(t *testing.T) {
	f := newStarFixture(t)
	ben := f.s.As("ben")
	m, err := ben.Impact(string(f.home.ID), ImpactAffected, 0, 1000)
	noErr(t, err)
	want := []string{hop(f.home.ID, 0), hop(f.child.ID, 1), hop(f.task.ID, 1), picAt(f.home, f.hp2, 1),
		picAt(f.task, f.tp, 2), picAt(f.child, f.cp, 2)}
	if got := impactIDs(m); !slices.Equal(got, want) {
		t.Fatalf("nodes\n got %v\nwant %v", got, want)
	}
	if !m.Complete || m.Truncated || m.Start != string(f.home.ID) || m.Space != "PLAT" || m.Direction != ImpactAffected || m.Depth != 0 {
		t.Errorf("walk %+v", m)
	}
	byID := map[string]ImpactNode{}
	for _, n := range m.Nodes {
		byID[n.ID] = n
	}
	if n := byID[string(f.task.ID)]; n.Key != f.task.Key || n.Title != "Patch" || n.Outside ||
		!slices.Equal(n.Via, []StarEdge{{From: string(f.task.ID), To: string(f.home.ID), Kind: StarRelation, Type: "depends-on"}}) {
		t.Errorf("task %+v", n)
	}
	if n := byID[string(f.child.ID)]; !slices.Equal(n.Via, []StarEdge{{From: string(f.home.ID), To: string(f.child.ID), Kind: StarParent}}) {
		t.Errorf("a child is reached by its parent only, not by the embed that goes the other way: %+v", n.Via)
	}
	cp := pictureNodeID(f.child.ID, f.cp)
	if n := byID[cp]; !slices.Equal(n.Via, []StarEdge{{From: cp, To: string(f.child.ID), Kind: StarOwner}, {From: cp, To: pictureNodeID(f.home.ID, f.hp2), Kind: StarCopy}}) {
		t.Errorf("a copy is reached by its item and its original: %+v", n.Via)
	}
	if len(byID[string(f.home.ID)].Via) != 0 {
		t.Errorf("the start has a way in: %+v", byID[string(f.home.ID)])
	}
	// The edges between the nodes, the embed against the walk among them.
	if !hasImpactEdge(m, StarEdge{From: string(f.home.ID), To: string(f.child.ID), Kind: RelEmbeds}) ||
		!hasImpactEdge(m, StarEdge{From: string(f.child.ID), To: string(f.task.ID), Kind: RelMentions}) || len(m.Edges) != 8 {
		t.Errorf("edges %+v", m.Edges)
	}
}

func hasImpactEdge(m Impact, e StarEdge) bool { return slices.Contains(m.Edges, e) }

// What Home depends on: the item it embeds, and through it what that one
// relies on, across spaces; labels are no dependency.
func TestImpactWalksWhatAnItemDependsOn(t *testing.T) {
	f := newStarFixture(t)
	m, err := f.s.As("ben").Impact(string(f.home.ID), ImpactDependsOn, 0, 1000)
	noErr(t, err)
	want := []string{hop(f.home.ID, 0), hop(f.child.ID, 1), hop(f.task.ID, 2), hop(f.x1.ID, 2)}
	if got := impactIDs(m); !slices.Equal(got, want) {
		t.Fatalf("nodes\n got %v\nwant %v", got, want)
	}
	if n := m.Nodes[3]; !n.Outside || n.Space != "OPS" {
		t.Errorf("an item of another space is outside: %+v", n)
	}
	if !m.Complete {
		t.Error("not complete")
	}
	// From OPS, its own pictures are inside and the copy in PLAT outside.
	m, err = f.s.As("ben").Impact(string(f.x3.ID), ImpactAffected, 0, 1000)
	noErr(t, err)
	if got := impactIDs(m); !slices.Equal(got, []string{hop(f.x3.ID, 0), picAt(f.x3, f.xp, 1), picAt(f.task, f.tp, 2)}) ||
		m.Nodes[1].Outside || !m.Nodes[2].Outside || m.Space != "OPS" {
		t.Errorf("across spaces %v %+v", got, m.Nodes)
	}
}

// Shared labels make no node and no edge, though Home and Child share
// one.
func TestImpactFollowsNoLabels(t *testing.T) {
	f := newStarFixture(t)
	for _, dir := range []string{ImpactAffected, ImpactDependsOn} {
		for _, start := range []ID{f.home.ID, f.child.ID, f.task.ID} {
			m, err := f.s.As("anna").Impact(string(start), dir, 0, 1000)
			noErr(t, err)
			for _, n := range m.Nodes {
				if n.Kind == StarLabel {
					t.Errorf("%s %s: label node %+v", start, dir, n)
				}
			}
			for _, e := range m.Edges {
				if e.Kind == StarLabelled {
					t.Errorf("%s %s: label edge %+v", start, dir, e)
				}
			}
		}
	}
}

// The depth bounds the hops; a walk that reaches its depth with more
// beyond is not complete, one that ends within it is.
func TestImpactStopsAtItsDepth(t *testing.T) {
	f := newStarFixture(t)
	ben := f.s.As("ben")
	for depth, want := range map[int]struct {
		nodes    int
		complete bool
	}{1: {4, false}, 2: {6, true}, 3: {6, true}, 5: {6, true}, 0: {6, true}} {
		m, err := ben.Impact(string(f.home.ID), ImpactAffected, depth, 1000)
		noErr(t, err)
		if len(m.Nodes) != want.nodes || m.Complete != want.complete || m.Truncated || m.Depth != depth {
			t.Errorf("depth %d: %v complete %v", depth, impactIDs(m), m.Complete)
		}
		for _, n := range m.Nodes {
			if depth > 0 && n.Distance > depth {
				t.Errorf("depth %d: %s at %d", depth, n.ID, n.Distance)
			}
		}
	}
	if m, _ := ben.Impact(string(f.home.ID), ImpactAffected, -3, 1000); len(m.Nodes) != 6 {
		t.Errorf("a negative depth is no limit: %v", impactIDs(m))
	}
}

// The budget keeps the nearest nodes, in their order, and says the walk
// was cut.
func TestImpactIsTruncatedAtTheBudget(t *testing.T) {
	f := newStarFixture(t)
	ben := f.s.As("ben")
	for max, want := range map[int][]string{
		0: {hop(f.home.ID, 0)},
		1: {hop(f.home.ID, 0)},
		3: {hop(f.home.ID, 0), hop(f.child.ID, 1), hop(f.task.ID, 1)},
		5: {hop(f.home.ID, 0), hop(f.child.ID, 1), hop(f.task.ID, 1), picAt(f.home, f.hp2, 1), picAt(f.task, f.tp, 2)},
	} {
		m, err := ben.Impact(string(f.home.ID), ImpactAffected, 0, max)
		noErr(t, err)
		if got := impactIDs(m); !slices.Equal(got, want) || !m.Truncated || m.Complete {
			t.Errorf("max %d: %v truncated %v complete %v", max, got, m.Truncated, m.Complete)
		}
		ids := map[string]bool{}
		for _, n := range m.Nodes {
			ids[n.ID] = true
		}
		for _, e := range m.Edges {
			if !ids[e.From] || !ids[e.To] {
				t.Errorf("max %d: edge to a node left out %+v", max, e)
			}
		}
	}
	if m, _ := ben.Impact(string(f.home.ID), ImpactAffected, 0, 6); m.Truncated || !m.Complete {
		t.Error("truncated at its own size")
	}
}

// D10: an unreadable item is no node, no edge's end, and no bridge: what
// is reachable only through it does not appear, though its reader may
// read it.
func TestImpactRevealsNothingUnreadable(t *testing.T) {
	f := newStarFixture(t)
	anna, ben := f.s.As("anna"), f.s.As("ben")
	down := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Downstream of the plans"}))
	noErr(t, anna.AddRelation("depends-on", down.ID, f.sec.ID))

	m, err := anna.Impact(string(f.x2.ID), ImpactAffected, 0, 1000)
	noErr(t, err)
	if got := impactIDs(m); !slices.Equal(got, []string{hop(f.x2.ID, 0), hop(f.sec.ID, 1), hop(down.ID, 2)}) {
		t.Fatalf("anna %v", got)
	}
	m, err = ben.Impact(string(f.x2.ID), ImpactAffected, 0, 1000)
	noErr(t, err)
	if got := impactIDs(m); !slices.Equal(got, []string{hop(f.x2.ID, 0)}) || !m.Complete || len(m.Edges) != 0 {
		t.Errorf("ben passes through what he may not read: %v %+v", got, m.Edges)
	}
	for _, dir := range []string{ImpactAffected, ImpactDependsOn} {
		for _, start := range []ID{f.home.ID, f.x2.ID, down.ID} {
			m, err := ben.Impact(string(start), dir, 0, 1000)
			noErr(t, err)
			data, err := json.Marshal(m)
			noErr(t, err)
			for _, secret := range []string{string(f.sec.ID), f.sec.Key, f.sec.Title} {
				if strings.Contains(string(data), secret) {
					t.Errorf("%s %s names %q: %s", start, dir, secret, data)
				}
			}
			if start == down.ID && len(m.Nodes) != 1 {
				t.Errorf("what Downstream depends on, for ben: %v", impactIDs(m))
			}
		}
	}
}

// An unknown, unreadable or trashed start is not found, as every read of
// an item; a direction that is neither is refused.
func TestImpactOfWhatOneMayNotReadIsNotFound(t *testing.T) {
	f := newStarFixture(t)
	ben := f.s.As("ben")
	for _, start := range []string{"nope", string(f.sec.ID), "picture:nope/x", pictureNodeID(f.home.ID, f.hp), pictureNodeID(f.home.ID, f.pdf), "label:runbook"} {
		if _, err := ben.Impact(start, ImpactAffected, 1, 1000); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: %v", start, err)
		}
	}
	if _, err := f.s.As("carl").Impact(string(f.home.ID), ImpactAffected, 1, 1000); !errors.Is(err, ErrNotFound) {
		t.Errorf("without a role: %v", err)
	}
	noErr(t, f.s.As("anna").Trash(f.x3.ID))
	if _, err := ben.Impact(string(f.x3.ID), ImpactAffected, 1, 1000); !errors.Is(err, ErrNotFound) {
		t.Errorf("trashed: %v", err)
	}
	if _, err := ben.Impact(string(f.home.ID), "sideways", 1, 1000); !errors.Is(err, ErrValidation) {
		t.Errorf("direction: %v", err)
	}
}

// A picture is a start too: what depends on it are its copies; it
// depends on its item and its original, and through them on theirs.
func TestImpactOfAPicture(t *testing.T) {
	f := newStarFixture(t)
	ben := f.s.As("ben")
	m, err := ben.Impact(pictureNodeID(f.home.ID, f.hp2), ImpactAffected, 0, 1000)
	noErr(t, err)
	if got := impactIDs(m); !slices.Equal(got, []string{picAt(f.home, f.hp2, 0), picAt(f.child, f.cp, 1)}) || m.Space != "PLAT" {
		t.Errorf("affected %v", got)
	}
	m, err = ben.Impact(pictureNodeID(f.child.ID, f.cp), ImpactDependsOn, 0, 1000)
	noErr(t, err)
	want := []string{picAt(f.child, f.cp, 0), hop(f.child.ID, 1), picAt(f.home, f.hp2, 1), hop(f.home.ID, 2), hop(f.task.ID, 2), hop(f.x1.ID, 2)}
	if got := impactIDs(m); !slices.Equal(got, want) {
		t.Errorf("depends on\n got %v\nwant %v", got, want)
	}
}

func TestImpactIsDeterministic(t *testing.T) {
	f := newStarFixture(t)
	for _, dir := range []string{ImpactAffected, ImpactDependsOn} {
		first, err := f.s.As("anna").Impact(string(f.home.ID), dir, 0, 1000)
		noErr(t, err)
		for range 20 {
			again, err := f.s.As("anna").Impact(string(f.home.ID), dir, 0, 1000)
			noErr(t, err)
			if !reflect.DeepEqual(first, again) {
				t.Fatalf("%s: the walk changed\n%+v\n%+v", dir, first, again)
			}
		}
	}
}
