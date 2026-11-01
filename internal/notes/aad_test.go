package notes

import "testing"

// The additional data of a sealed note is part of the stored data
// (ADR-0155); it keeps the product's former name.
func TestNoteAADIsStable(t *testing.T) {
	if got := string(aad(record{Owner: "u1", Item: "PLAT-1", Note: "n1"})); got != "wikiwikibangbang note u1 PLAT-1 n1" {
		t.Fatalf("aad %q", got)
	}
}
