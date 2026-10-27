package server

import (
	"net/http"
	"testing"
)

// TestFavouriteSpacesThroughTheAPI: a person marks a space they hold a
// role in as a favourite; the space list says so for them alone, and
// unmarking takes it back. A space one holds no role in, or that does not
// exist, cannot be marked (ADR-0145).
func TestFavouriteSpacesThroughTheAPI(t *testing.T) {
	w := newWorld(t)
	w.admin.expect("POST", "/api/spaces", `{"key":"OPS","name":"Operations"}`, http.StatusCreated)
	favourite := func(c *client, key string) bool {
		for _, s := range field(c.expect("GET", "/api/spaces", "", http.StatusOK), "spaces").([]any) {
			if sp := s.(map[string]any); sp["key"] == key {
				return sp["favourite"] == true
			}
		}
		t.Fatalf("%s is not listed", key)
		return false
	}
	if favourite(w.vera, "PLAT") {
		t.Fatal("a favourite before marking one")
	}
	w.vera.expect("PUT", "/api/spaces/PLAT/favourite", "", http.StatusNoContent)
	w.vera.expect("PUT", "/api/spaces/PLAT/favourite", "", http.StatusNoContent) // again: unchanged
	if !favourite(w.vera, "PLAT") {
		t.Fatal("vera's favourite is not shown")
	}
	if favourite(w.bert, "PLAT") || favourite(w.admin, "PLAT") {
		t.Fatal("vera's favourite shows for someone else")
	}
	if favourite(w.admin, "OPS") {
		t.Fatal("an unmarked space is a favourite")
	}

	w.vera.expect("PUT", "/api/spaces/OPS/favourite", "", http.StatusNotFound)    // no role there
	w.carla.expect("PUT", "/api/spaces/PLAT/favourite", "", http.StatusNotFound)  // no role at all
	w.vera.expect("PUT", "/api/spaces/NOSUCH/favourite", "", http.StatusNotFound) // no such space
	w.vera.expect("DELETE", "/api/spaces/bad-key/favourite", "", http.StatusBadRequest)

	w.vera.expect("DELETE", "/api/spaces/PLAT/favourite", "", http.StatusNoContent)
	w.vera.expect("DELETE", "/api/spaces/PLAT/favourite", "", http.StatusNoContent) // again: unchanged
	if favourite(w.vera, "PLAT") {
		t.Fatal("an unmarked favourite is still shown")
	}
}
