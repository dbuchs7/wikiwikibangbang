package engineconn

import "testing"

// The additional data of a sealed refresh token is part of the stored data
// (ADR-0155); it keeps the product's former name.
func TestConnectionAADIsStable(t *testing.T) {
	if got := string(aad("u1", "atlas", "k1")); got != "wikiwikibangbang engine connection u1 atlas k1" {
		t.Fatalf("aad %q", got)
	}
}
