package auth

import (
	"errors"
	"slices"
	"testing"
)

// TestFavouritesAreKeptPerUserAndSurviveReload: marking and unmarking a
// space changes the list once, in the order marked, and a reload of the
// journal gives it back (ADR-0145).
func TestFavouritesAreKeptPerUserAndSurviveReload(t *testing.T) {
	a, j, _ := newAccounts(t)
	u, err := a.CreateUser("anna", "anna's password")
	noErr(t, err)
	if got := a.Favourites(u.ID); len(got) != 0 {
		t.Fatalf("a new user has favourites %v", got)
	}
	noErr(t, a.SetFavourite(u.ID, "PLAT", true))
	noErr(t, a.SetFavourite(u.ID, "OPS", true))
	n := len(j.records)
	noErr(t, a.SetFavourite(u.ID, "PLAT", true)) // already: nothing recorded
	noErr(t, a.SetFavourite(u.ID, "HR", false))  // not one: nothing recorded
	if len(j.records) != n {
		t.Error("an unchanged list was recorded again")
	}
	if got := a.Favourites(u.ID); !slices.Equal(got, []string{"PLAT", "OPS"}) {
		t.Fatalf("favourites %v", got)
	}
	for _, bad := range []string{"", "plat", "1A", "A-B", "A B"} {
		if err := a.SetFavourite(u.ID, bad, true); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q: %v", bad, err)
		}
	}
	if err := a.SetFavourite("nobody", "PLAT", true); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown user: %v", err)
	}
	if got := a.Favourites("nobody"); got != nil {
		t.Errorf("unknown user's favourites: %v", got)
	}

	b, err := New(j.records, j.append, WithIterations(1000))
	noErr(t, err)
	if got := b.Favourites(u.ID); !slices.Equal(got, []string{"PLAT", "OPS"}) {
		t.Fatalf("after reload: %v", got)
	}
	noErr(t, b.SetFavourite(u.ID, "PLAT", false))
	noErr(t, b.SetFavourite(u.ID, "OPS", false))
	if got := b.Favourites(u.ID); len(got) != 0 {
		t.Fatalf("all unmarked: %v", got)
	}
	c, err := New(j.records, j.append, WithIterations(1000))
	noErr(t, err)
	if got := c.Favourites(u.ID); len(got) != 0 {
		t.Fatalf("all unmarked, after reload: %v", got)
	}
}

// TestFavouritesAreBounded: a user marks at most maxFavourites spaces.
func TestFavouritesAreBounded(t *testing.T) {
	a, _, _ := newAccounts(t)
	u, err := a.CreateUser("anna", "anna's password")
	noErr(t, err)
	for i := range maxFavourites {
		noErr(t, a.SetFavourite(u.ID, "S"+string(rune('A'+i/26%26))+string(rune('A'+i%26))+string(rune('A'+i/676)), true))
	}
	if err := a.SetFavourite(u.ID, "ONEMORE", true); !errors.Is(err, ErrInvalid) {
		t.Errorf("past the bound: %v", err)
	}
}

// TestFavouritesOfAnUnknownUserAreRefusedOnLoad: a journal that names no
// such user does not load.
func TestFavouritesOfAnUnknownUserAreRefusedOnLoad(t *testing.T) {
	_, err := New([][]byte{[]byte(`{"v":1,"kind":"FavouritesSet","user":"ghost","spaces":["PLAT"]}`)}, func([]byte) error { return nil }, WithIterations(1000))
	if err == nil {
		t.Fatal("favourites of an unknown user were loaded")
	}
}
