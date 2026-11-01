package item

import "testing"

// The additional data of a sealed event is part of the stored data: a
// change makes every event sealed before unreadable (ADR-0155). It keeps
// the product's former name.
func TestEventAADIsStable(t *testing.T) {
	got := string(aad(&wireEvent{Seq: 7, Kind: "Created", Item: "PLAT-1"}))
	if want := "wikiwikibangbang event 7 Created PLAT-1"; got != want {
		t.Fatalf("aad %q, want %q", got, want)
	}
	if got := string(secretAAD(&wireEvent{Seq: 7, Kind: "Created", Item: "PLAT-1"}, "p1")); got != "wikiwikibangbang event 7 Created PLAT-1 passage p1" {
		t.Fatalf("secret aad %q", got)
	}
}
