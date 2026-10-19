package item

import (
	"errors"
	"slices"
	"testing"
)

func titles(items []Item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Title
	}
	return out
}

func nodeTitles(nodes []TreeNode) []string {
	out := make([]string, len(nodes))
	for i, n := range nodes {
		out[i] = n.Item.Title
	}
	return out
}

func TestSiblingsKeepThePlacedOrder(t *testing.T) {
	s := setup(t)
	home := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "Home"}))
	a := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "A", Parent: home.ID}))
	b := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "B", Parent: home.ID}))
	c := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "C", Parent: home.ID}))
	if got := titles(s.Children("PLAT", home.ID)); !slices.Equal(got, []string{"A", "B", "C"}) {
		t.Fatalf("unplaced children %v, want oldest first", got)
	}
	noErr(t, s.Place("anna", c.ID, "PLAT", home.ID, a.ID))
	if got := titles(s.Children("PLAT", home.ID)); !slices.Equal(got, []string{"C", "A", "B"}) {
		t.Fatalf("after placing C before A: %v", got)
	}
	noErr(t, s.Place("anna", c.ID, "PLAT", home.ID, ""))
	if got := titles(s.Children("PLAT", home.ID)); !slices.Equal(got, []string{"A", "B", "C"}) {
		t.Fatalf("after placing C last: %v", got)
	}
	// A child created later follows the placed ones.
	mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "D", Parent: home.ID}))
	noErr(t, s.Place("anna", b.ID, "PLAT", "", home.ID)) // B to the top level, before Home
	if got := titles(s.Children("PLAT", home.ID)); !slices.Equal(got, []string{"A", "C", "D"}) {
		t.Fatalf("children after B left: %v", got)
	}
	if got := titles(s.Children("PLAT", "")); !slices.Equal(got, []string{"B", "Home"}) {
		t.Fatalf("top level %v", got)
	}
	// Trashed children are not listed; the order survives a replay.
	noErr(t, s.Trash("anna", a.ID))
	again, err := Replay(s.Log())
	noErr(t, err)
	for _, st := range []*Store{s, again} {
		if got := titles(st.Children("PLAT", home.ID)); !slices.Equal(got, []string{"C", "D"}) {
			t.Fatalf("children %v", got)
		}
	}
	// Placing before something that is not a sibling, or below itself, fails.
	if err := s.Place("anna", c.ID, "PLAT", home.ID, b.ID); !errors.Is(err, ErrValidation) {
		t.Errorf("before a non-sibling: %v", err)
	}
	if err := s.Place("anna", c.ID, "PLAT", home.ID, c.ID); !errors.Is(err, ErrValidation) {
		t.Errorf("before itself: %v", err)
	}
	if err := s.Place("anna", home.ID, "PLAT", c.ID, ""); !errors.Is(err, ErrValidation) {
		t.Errorf("below itself: %v", err)
	}
}

func TestTreeShowsOnlyWhatOneMayRead(t *testing.T) {
	s := permFixture(t)
	anna, ben, carl := s.As("anna"), s.As("ben"), s.As("carl")
	home := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Home"}))
	open := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Open", Parent: home.ID}))
	secret := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Secret", Parent: home.ID}))
	mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Below secret", Parent: secret.ID}))
	last := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Last", Parent: home.ID}))
	noErr(t, anna.Restrict(secret.ID, Restriction{Read: []string{"anna"}}))

	top, err := ben.Tree("PLAT", "")
	noErr(t, err)
	if len(top) != 1 || top[0].Item.ID != home.ID || !top[0].HasChildren {
		t.Fatalf("top %+v", top)
	}
	kids, err := ben.Tree("PLAT", home.ID)
	noErr(t, err)
	if got := nodeTitles(kids); !slices.Equal(got, []string{"Open", "Last"}) || kids[0].HasChildren {
		t.Fatalf("ben sees %v", got)
	}
	annas, err := anna.Tree("PLAT", home.ID)
	noErr(t, err)
	if got := nodeTitles(annas); !slices.Equal(got, []string{"Open", "Secret", "Last"}) || !annas[1].HasChildren {
		t.Fatalf("anna sees %+v", annas)
	}
	// Ben moves Last before Open; the hidden Secret keeps its place after Open.
	if err := ben.Place(last.ID, "PLAT", home.ID, open.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("a viewer places: %v", err)
	}
	noErr(t, anna.Place(last.ID, "PLAT", home.ID, open.ID))
	annas, _ = anna.Tree("PLAT", home.ID)
	if got := nodeTitles(annas); !slices.Equal(got, []string{"Last", "Open", "Secret"}) {
		t.Fatalf("after placing %v", got)
	}
	// An outsider learns nothing, and one cannot place before an unreadable item.
	if nodes, err := carl.Tree("PLAT", ""); err != nil || len(nodes) != 0 {
		t.Fatalf("outsider %+v, %v", nodes, err)
	}
	if _, err := carl.Tree("PLAT", home.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("outsider below home: %v", err)
	}
	if _, err := anna.Tree("NOPE", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown space: %v", err)
	}
	noErr(t, s.GrantRole("root", "PLAT", "ben", Editor))
	if err := ben.Place(last.ID, "PLAT", home.ID, secret.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("placing before an unreadable sibling: %v", err)
	}
}
