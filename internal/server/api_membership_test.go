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

// An editor protects an item for a group and for people by name; only
// groups and people that exist are taken, and the editor finds groups by
// name without seeing their members (ADR-0078).
func TestProtectingAnItemForAGroup(t *testing.T) {
	w := newWorld(t)
	w.admin.expect("PUT", "/api/groups/redaktion/members/"+w.ids["vera"], "", http.StatusNoContent)
	w.admin.expect("PUT", "/api/groups/reviewers/members/"+w.ids["bert"], "", http.StatusNoContent)
	got := fmt.Sprint(w.bert.expect("GET", "/api/groups/search?q=RE", "", http.StatusOK)["groups"])
	if got != "[redaktion reviewers]" {
		t.Fatalf("groups found %s", got)
	}
	if got := fmt.Sprint(w.bert.expect("GET", "/api/groups/search?q=view", "", http.StatusOK)["groups"]); got != "[reviewers]" {
		t.Fatalf("inner match %s", got)
	}
	if got := fmt.Sprint(w.bert.expect("GET", "/api/groups/search?q=zzz", "", http.StatusOK)["groups"]); got != "[]" {
		t.Fatalf("nothing found %s", got)
	}
	w.vera.expect("GET", "/api/groups/search?q=r", "", http.StatusForbidden)
	w.carla.expect("GET", "/api/groups/search?q=r", "", http.StatusForbidden)

	created := w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"Protected","properties":{"priority":"low"}}`, http.StatusCreated)
	id := created["item"].(map[string]any)["id"].(string)
	path := "/api/items/" + id + "/restriction"
	w.bert.expect("PUT", path, `{"read":["@nobody-here"],"edit":[]}`, http.StatusBadRequest)
	w.bert.expect("PUT", path, `{"read":["`+w.ids["bert"]+`","no-such-user"],"edit":[]}`, http.StatusBadRequest)
	// Bert would lock himself out with the group alone.
	w.bert.expect("PUT", path, `{"read":["@redaktion"],"edit":[]}`, http.StatusBadRequest)
	w.bert.expect("PUT", path, `{"read":["@redaktion","`+w.ids["bert"]+`"],"edit":["`+w.ids["bert"]+`"]}`, http.StatusOK)
	w.vera.expect("GET", "/api/items/"+id, "", http.StatusOK)
	w.admin.expect("DELETE", "/api/groups/redaktion/members/"+w.ids["vera"], "", http.StatusNoContent)
	w.vera.expect("GET", "/api/items/"+id, "", http.StatusNotFound)
}
