package server

import (
	"net/http"
	"strings"
	"testing"
)

func TestDeletingASpaceTakesTwoSteps(t *testing.T) {
	w := newWorld(t)
	created := w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"Old work","properties":{"priority":"low"}}`, http.StatusCreated)
	key := field(created, "item", "key").(string)

	// Only administrators delete spaces; bert is an editor.
	w.bert.expect("DELETE", "/api/spaces/PLAT", "", http.StatusForbidden)
	w.bert.expect("GET", "/api/spaces/trash", "", http.StatusForbidden)

	w.admin.expect("DELETE", "/api/spaces/PLAT", "", http.StatusNoContent)
	if listed := mustJSON(t, w.bert.expect("GET", "/api/spaces", "", http.StatusOK)); strings.Contains(listed, "PLAT") {
		t.Fatalf("a space in the trash is still listed: %s", listed)
	}
	w.bert.expect("GET", "/api/items/"+key, "", http.StatusNotFound)
	w.admin.expect("GET", "/api/items/"+key, "", http.StatusNotFound)
	trash := w.admin.expect("GET", "/api/spaces/trash", "", http.StatusOK)["spaces"].([]any)
	if len(trash) != 1 || trash[0].(map[string]any)["key"] != "PLAT" || trash[0].(map[string]any)["items"] != float64(1) {
		t.Fatalf("trash: %v", trash)
	}

	// Restoring brings it back as it was.
	w.admin.expect("POST", "/api/spaces/PLAT/restore", "", http.StatusNoContent)
	w.bert.expect("GET", "/api/items/"+key, "", http.StatusOK)

	// Deleting for good needs the trash first and the key repeated.
	w.admin.expect("POST", "/api/spaces/PLAT/purge", `{"confirm":"PLAT"}`, http.StatusNotFound)
	w.admin.expect("DELETE", "/api/spaces/PLAT", "", http.StatusNoContent)
	w.admin.expect("POST", "/api/spaces/PLAT/purge", `{"confirm":"plat"}`, http.StatusBadRequest)
	w.bert.expect("POST", "/api/spaces/PLAT/purge", `{"confirm":"PLAT"}`, http.StatusForbidden)
	w.admin.expect("POST", "/api/spaces/PLAT/purge", `{"confirm":"PLAT"}`, http.StatusNoContent)
	if trash := w.admin.expect("GET", "/api/spaces/trash", "", http.StatusOK)["spaces"].([]any); len(trash) != 0 {
		t.Fatalf("trash after purge: %v", trash)
	}
	w.admin.expect("POST", "/api/spaces/PLAT/restore", "", http.StatusNotFound)
	// The key is not used again.
	w.admin.expect("POST", "/api/spaces", `{"key":"PLAT","name":"Again"}`, http.StatusBadRequest)

	audit := mustJSON(t, w.admin.expect("GET", "/api/audit?limit=20", "", http.StatusOK))
	for _, e := range []string{"space.trash", "space.restore", "space.purge"} {
		if !strings.Contains(audit, e) {
			t.Errorf("the audit trail lacks %s", e)
		}
	}
}
