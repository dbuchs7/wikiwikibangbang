package storage

import "testing"

// The additional data of an attachment's chunks is part of the stored
// data (ADR-0155); it keeps the product's former name.
func TestChunkAADIsStable(t *testing.T) {
	if got := string(chunkAAD("PLAT-1", "a1", 3, true)); got != "wikiwikibangbang attachment PLAT-1 a1 3 1" {
		t.Fatalf("aad %q", got)
	}
}
