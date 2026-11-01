package item

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// starFixture is a space PLAT whose items have every kind of edge, an item
// restricted to anna, and items of OPS connected to it (ADR-0157).
type starFixture struct {
	s                        *Store
	home, child, task, sec   Item
	x1, x2, x3               Item
	hp, hp2, pdf, cp, xp, tp ID
}

func newStarFixture(t *testing.T) starFixture {
	t.Helper()
	s := permFixture(t)
	noErr(t, s.GrantRole("root", "OPS", "anna", Editor))
	noErr(t, s.GrantRole("root", "OPS", "ben", Viewer))
	anna := s.As("anna")
	f := starFixture{s: s, hp: "00000000000000000000000000000001", hp2: "00000000000000000000000000000002", pdf: "00000000000000000000000000000003",
		cp: "00000000000000000000000000000004", xp: "00000000000000000000000000000005", tp: "00000000000000000000000000000006"}
	f.home = mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Home"}))
	f.child = mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Child", Parent: f.home.ID}))
	f.sec = mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Hidden plans", Parent: f.home.ID}))
	f.task = mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "task", Title: "Patch", Properties: Props{"priority": "low"}}))
	f.x1 = mustItem(t)(anna.Create(NewItem{Space: "OPS", Type: "page", Title: "Ops guide"}))
	f.x2 = mustItem(t)(anna.Create(NewItem{Space: "OPS", Type: "page", Title: "Ops secret link"}))
	f.x3 = mustItem(t)(anna.Create(NewItem{Space: "OPS", Type: "page", Title: "Ops pictures"}))
	noErr(t, s.Restrict("root", f.sec.ID, Restriction{Read: []string{"anna"}}))

	noErr(t, anna.SetLabels(f.home.ID, []string{"runbook", "solo"}))
	noErr(t, anna.SetLabels(f.child.ID, []string{"runbook"}))
	noErr(t, anna.SetLabels(f.sec.ID, []string{"runbook", "shared-secret"}))
	noErr(t, anna.SetLabels(f.task.ID, []string{"shared-secret"}))

	noErr(t, anna.AddRelation("depends-on", f.task.ID, f.home.ID))
	noErr(t, anna.AddRelation("implements", f.child.ID, f.x1.ID))
	noErr(t, anna.AddRelation("blocks", f.sec.ID, f.home.ID))
	noErr(t, anna.AddRelation("blocks", f.sec.ID, f.x2.ID))
	noErr(t, anna.CommitBody(f.child.ID, Doc{Blocks: []Block{{Kind: Paragraph, Text: "see @Patch", Mentions: []ID{f.task.ID}}}}))
	noErr(t, anna.CommitBody(f.home.ID, Doc{Blocks: []Block{{Kind: Embed, Ref: f.child.ID}}}))

	sum := strings.Repeat("ab", 32)
	file := func(on ID, id ID, name, typ string, src *Source) {
		_, err := anna.AddAttachment(on, Attachment{ID: id, Name: name, Type: typ, Size: 3, SHA256: sum, Source: src})
		noErr(t, err)
	}
	file(f.home.ID, f.hp, "logo.png", "image/png", nil)
	file(f.home.ID, f.hp2, "logo.png", "image/png", &Source{Item: f.home.ID, Attachment: f.hp}) // a new version
	file(f.home.ID, f.pdf, "notes.pdf", "application/pdf", nil)
	file(f.child.ID, f.cp, "logo.png", "image/png", &Source{Item: f.home.ID, Attachment: f.hp2}) // a copy in the space
	file(f.x3.ID, f.xp, "rack.jpg", "image/jpeg", nil)
	file(f.task.ID, f.tp, "rack.jpg", "image/jpeg", &Source{Item: f.x3.ID, Attachment: f.xp}) // a copy from OPS
	return f
}

func starIDs(m Starmap) []string {
	var out []string
	for _, n := range m.Nodes {
		out = append(out, n.ID)
	}
	return out
}

func hasEdge(m Starmap, want StarEdge) bool { return slices.Contains(m.Edges, want) }

func TestAStarmapShowsTheSpaceInItsContext(t *testing.T) {
	f := newStarFixture(t)
	m, err := f.s.As("ben").Starmap("PLAT", 600)
	noErr(t, err)
	pic := func(it Item, a ID) string { return pictureNodeID(it.ID, a) }
	want := []string{string(f.home.ID), string(f.child.ID), string(f.task.ID), // the space in tree order
		string(f.x1.ID), string(f.x3.ID), // outside, as made
		pic(f.task, f.tp), pic(f.child, f.cp), pic(f.home, f.hp2), // pictures, newest first, the top of a stack only
		pic(f.x3, f.xp), // the original of a copy, outside
		"label:runbook"}
	if got := starIDs(m); !slices.Equal(got, want) {
		t.Fatalf("nodes\n got %v\nwant %v", got, want)
	}
	if m.Truncated {
		t.Error("truncated within the budget")
	}
	byID := map[string]StarNode{}
	for _, n := range m.Nodes {
		byID[n.ID] = n
	}
	if n := byID[string(f.task.ID)]; n.Kind != StarItem || n.Key != f.task.Key || n.Title != "Patch" || n.Type != "task" || n.Space != "PLAT" ||
		n.State != "open" || n.Outside || !slices.Equal(n.Labels, []string{"shared-secret"}) {
		t.Errorf("item node %+v", n)
	}
	if n := byID[string(f.x1.ID)]; !n.Outside || n.Space != "OPS" {
		t.Errorf("outside node %+v", n)
	}
	if n := byID[pic(f.x3, f.xp)]; n.Kind != StarPicture || !n.Outside || n.Item != f.x3.ID || n.Name != "rack.jpg" || n.Space != "OPS" {
		t.Errorf("outside picture %+v", n)
	}
	if n := byID[pic(f.home, f.hp2)]; n.Kind != StarPicture || n.Outside || n.Item != f.home.ID || n.Name != "logo.png" {
		t.Errorf("picture %+v", n)
	}
	if n := byID["label:runbook"]; n.Kind != StarLabel || n.Name != "runbook" {
		t.Errorf("label %+v", n)
	}
	for _, e := range []StarEdge{
		{From: string(f.home.ID), To: string(f.child.ID), Kind: StarParent},
		{From: string(f.task.ID), To: string(f.home.ID), Kind: StarRelation, Type: "depends-on"},
		{From: string(f.child.ID), To: string(f.x1.ID), Kind: StarRelation, Type: "implements"},
		{From: string(f.child.ID), To: string(f.task.ID), Kind: RelMentions},
		{From: string(f.home.ID), To: string(f.child.ID), Kind: RelEmbeds},
		{From: pic(f.home, f.hp2), To: string(f.home.ID), Kind: StarOwner},
		{From: pic(f.x3, f.xp), To: string(f.x3.ID), Kind: StarOwner},
		{From: pic(f.child, f.cp), To: pic(f.home, f.hp2), Kind: StarCopy},
		{From: pic(f.task, f.tp), To: pic(f.x3, f.xp), Kind: StarCopy},
		{From: "label:runbook", To: string(f.home.ID), Kind: StarLabelled},
		{From: "label:runbook", To: string(f.child.ID), Kind: StarLabelled},
	} {
		if !hasEdge(m, e) {
			t.Errorf("no edge %+v", e)
		}
	}
	if len(m.Edges) != 13 { // the eleven above and the pictures of Child and Patch
		t.Errorf("%d edges: %+v", len(m.Edges), m.Edges)
	}
	seen := map[StarEdge]bool{}
	for _, e := range m.Edges {
		if seen[e] {
			t.Errorf("edge twice: %+v", e)
		}
		seen[e] = true
	}
}

// D10: an item the reader may not read is on no node, no edge, no label
// hub and in no count; nor is what only it connects.
func TestAStarmapRevealsNothingUnreadable(t *testing.T) {
	f := newStarFixture(t)
	m, err := f.s.As("ben").Starmap("PLAT", 600)
	noErr(t, err)
	data, err := json.Marshal(m)
	noErr(t, err)
	for _, secret := range []string{string(f.sec.ID), f.sec.Key, f.sec.Title, string(f.x2.ID), f.x2.Key, f.x2.Title, "label:shared-secret", "label:solo", string(f.pdf)} {
		if strings.Contains(string(data), secret) {
			t.Errorf("the map names %q: %s", secret, data)
		}
	}
	// Whoever may read it sees it, its edges, what it connects and the
	// label it shares.
	m, err = f.s.As("anna").Starmap("PLAT", 600)
	noErr(t, err)
	ids := starIDs(m)
	for _, want := range []string{string(f.sec.ID), string(f.x2.ID), "label:shared-secret"} {
		if !slices.Contains(ids, want) {
			t.Errorf("anna misses %s: %v", want, ids)
		}
	}
	if !hasEdge(m, StarEdge{From: string(f.sec.ID), To: string(f.x2.ID), Kind: StarRelation, Type: "blocks"}) ||
		!hasEdge(m, StarEdge{From: string(f.home.ID), To: string(f.sec.ID), Kind: StarParent}) {
		t.Errorf("anna's edges %+v", m.Edges)
	}
	// One who reads the space only sees no item of another space.
	noErr(t, f.s.GrantRole("root", "PLAT", "vic", Viewer))
	m, err = f.s.As("vic").Starmap("PLAT", 600)
	noErr(t, err)
	for _, n := range m.Nodes {
		if n.Outside {
			t.Errorf("vic sees %+v", n)
		}
	}
}

func TestAStarmapOfAnUnknownOrUnreadableSpaceIsNotFound(t *testing.T) {
	f := newStarFixture(t)
	if _, err := f.s.As("ben").Starmap("NOPE", 600); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown space: %v", err)
	}
	if _, err := f.s.As("carl").Starmap("PLAT", 600); !errors.Is(err, ErrNotFound) {
		t.Errorf("a space without a role: %v", err)
	}
}

// Beyond the budget the space's items come first, then the outside items,
// the pictures and the labels; an outside node whose anchor was left out
// goes too, and no edge leads to a node left out.
func TestAStarmapIsTruncatedInItsOrder(t *testing.T) {
	f := newStarFixture(t)
	ben := f.s.As("ben")
	full, err := ben.Starmap("PLAT", 600)
	noErr(t, err)
	for max, want := range map[int][]string{
		2: {string(f.home.ID), string(f.child.ID)},
		4: {string(f.home.ID), string(f.child.ID), string(f.task.ID), string(f.x1.ID)},
		// X3 is there only for the copy of its picture, which does not
		// fit; the room it leaves takes a label.
		5: {string(f.home.ID), string(f.child.ID), string(f.task.ID), string(f.x1.ID), "label:runbook"},
		9: starIDs(full)[:9],
	} {
		m, err := ben.Starmap("PLAT", max)
		noErr(t, err)
		if got := starIDs(m); !slices.Equal(got, want) || !m.Truncated {
			t.Errorf("max %d: %v (truncated %v), want %v", max, got, m.Truncated, want)
		}
		for _, e := range m.Edges {
			if !slices.Contains(starIDs(m), e.From) || !slices.Contains(starIDs(m), e.To) {
				t.Errorf("max %d: edge to a node left out %+v", max, e)
			}
		}
	}
	if m, _ := ben.Starmap("PLAT", len(full.Nodes)); m.Truncated {
		t.Error("truncated at its own size")
	}
}

// The same state gives the same map, so the layout drawn from it is the
// same on every load.
func TestAStarmapIsDeterministic(t *testing.T) {
	f := newStarFixture(t)
	first, err := f.s.As("anna").Starmap("PLAT", 600)
	noErr(t, err)
	for range 20 {
		again, err := f.s.As("anna").Starmap("PLAT", 600)
		noErr(t, err)
		if !reflect.DeepEqual(first, again) {
			t.Fatalf("the map changed\n%+v\n%+v", first, again)
		}
	}
}

// A copy whose original was deleted, or is no picture, leads nowhere;
// one whose original is not readable is a picture without origin.
func TestACopyWithoutAReadableOriginalHasNoCopyEdge(t *testing.T) {
	f := newStarFixture(t)
	anna := f.s.As("anna")
	noErr(t, anna.DeleteAttachment(f.x3.ID, f.xp))
	m, err := f.s.As("ben").Starmap("PLAT", 600)
	noErr(t, err)
	if slices.Contains(starIDs(m), string(f.x3.ID)) {
		t.Errorf("the deleted original's item is on the map: %v", starIDs(m))
	}
	// An original's item restricted from ben.
	noErr(t, f.s.Restrict("root", f.home.ID, Restriction{Read: []string{"anna"}}))
	m, err = f.s.As("ben").Starmap("PLAT", 600)
	noErr(t, err)
	for _, e := range m.Edges {
		if e.Kind == StarCopy {
			t.Errorf("a copy edge to what ben may not read: %+v", e)
		}
	}
}

func TestPicturesAreTheBrowsersImages(t *testing.T) {
	for typ, want := range map[string]bool{"image/png": true, "image/webp": true, "image/svg+xml": false, "application/pdf": false} {
		if got := IsPicture(Attachment{Type: typ}); got != want {
			t.Errorf("%s: %v", typ, got)
		}
	}
}
