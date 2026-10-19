package server

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestAccessIsListedAndTakenBackThroughTheAPI(t *testing.T) {
	w := newWorld(t)
	roles := fmt.Sprint(w.admin.expect("GET", "/api/spaces/PLAT/roles", "", http.StatusOK)["roles"])
	if !strings.Contains(roles, w.ids["vera"]) || !strings.Contains(roles, "viewer") {
		t.Fatalf("roles %s", roles)
	}
	w.bert.expect("GET", "/api/spaces/PLAT/roles", "", http.StatusForbidden)
	w.carla.expect("GET", "/api/spaces/PLAT/roles", "", http.StatusNotFound)

	w.bert.expect("DELETE", "/api/spaces/PLAT/roles/"+w.ids["vera"], "", http.StatusForbidden)
	w.admin.expect("DELETE", "/api/spaces/PLAT/roles/"+w.ids["vera"], "", http.StatusNoContent)
	w.vera.expect("GET", "/api/spaces/PLAT/roles", "", http.StatusNotFound)

	w.admin.expect("PUT", "/api/groups/ops/members/"+w.ids["carla"], "", http.StatusNoContent)
	if g := fmt.Sprint(w.admin.expect("GET", "/api/groups", "", http.StatusOK)["groups"]); !strings.Contains(g, "ops") || !strings.Contains(g, w.ids["carla"]) {
		t.Fatalf("groups %s", g)
	}
	w.bert.expect("GET", "/api/groups", "", http.StatusForbidden)
	w.admin.expect("DELETE", "/api/groups/ops/members/"+w.ids["carla"], "", http.StatusNoContent)

	w.admin.expect("PUT", "/api/system-roles/erasure/"+w.ids["bert"], "", http.StatusNoContent)
	if h := fmt.Sprint(w.admin.expect("GET", "/api/system-roles", "", http.StatusOK)["holders"]); !strings.Contains(h, w.ids["bert"]) || !strings.Contains(h, "erasure") {
		t.Fatalf("holders %s", h)
	}
	w.admin.expect("DELETE", "/api/system-roles/erasure/"+w.ids["bert"], "", http.StatusNoContent)
	adminID := w.admin.expect("GET", "/api/me", "", http.StatusOK)["user"].(map[string]any)["id"].(string)
	w.admin.expect("DELETE", "/api/system-roles/admin/"+adminID, "", http.StatusBadRequest) // the last administrator

	audit := fmt.Sprint(w.admin.expect("GET", "/api/audit", "", http.StatusOK))
	for _, e := range []string{"role.revoke", "group.remove", "system-role.revoke"} {
		if !strings.Contains(audit, e) {
			t.Errorf("the audit trail has no %s", e)
		}
	}
}
