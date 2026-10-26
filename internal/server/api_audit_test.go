package server

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestTheAuditTrailRecordsSecurityEvents(t *testing.T) {
	w := newWorld(t)
	w.bert.expect("POST", "/api/login", `{"username":"bert","password":"wrong password!"}`, http.StatusUnauthorized)
	tok := w.bert.expect("POST", "/api/tokens", `{"name":"ci","scope":"read","days":1}`, http.StatusCreated)
	it := w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"x","properties":{"priority":"low"}}`, http.StatusCreated)
	id := field(it, "item", "id").(string)
	w.bert.expect("POST", "/api/items/"+id+"/trash", "", http.StatusOK)
	w.admin.expect("POST", "/api/items/"+id+"/purge", "", http.StatusOK)
	w.admin.expect("PUT", "/api/groups/ops/members/"+w.ids["carla"], "", http.StatusNoContent)

	w.bert.expect("GET", "/api/audit", "", http.StatusForbidden)
	got := w.admin.expect("GET", "/api/audit?limit=500", "", http.StatusOK)
	var trail []string
	for _, e := range got["entries"].([]any) {
		m := e.(map[string]any)
		trail = append(trail, fmt.Sprint(m["event"]))
		if strings.Contains(fmt.Sprint(m), tok["secret"].(string)) || strings.Contains(fmt.Sprint(m), "password!") {
			t.Fatalf("an audit entry holds a secret: %v", m)
		}
	}
	all := strings.Join(trail, " ")
	for _, want := range []string{"user.create", "system-role.grant", "role.grant", "login", "login.failed", "token.create", "item.purge", "group.add"} {
		if !strings.Contains(" "+all+" ", " "+want+" ") {
			t.Errorf("no %s entry in %s", want, all)
		}
	}

	page := w.admin.expect("GET", "/api/audit?limit=2", "", http.StatusOK)
	next := page["next"].(float64)
	older := w.admin.expect("GET", fmt.Sprintf("/api/audit?limit=2&before=%d", int(next)), "", http.StatusOK)
	if first := older["entries"].([]any)[0].(map[string]any)["seq"].(float64); first != next-1 {
		t.Fatalf("second page starts at %v, want %v", first, next-1)
	}
	w.admin.expect("GET", "/api/audit?limit=0", "", http.StatusBadRequest)
	w.admin.expect("GET", "/api/audit?before=x", "", http.StatusBadRequest)
}

// TestTheAuditChainIsChecked covers GET /api/audit/verify (ADR-0123).
func TestTheAuditChainIsChecked(t *testing.T) {
	w := newWorld(t)
	w.bert.expect("GET", "/api/audit/verify", "", http.StatusForbidden)
	c := w.admin.expect("GET", "/api/audit/verify", "", http.StatusOK)
	if c["broken"] != nil || c["entries"].(float64) < 1 || len(c["head"].(string)) != 64 {
		t.Fatalf("%v", c)
	}
	newest := w.admin.expect("GET", "/api/audit?limit=1", "", http.StatusOK)["entries"].([]any)[0].(map[string]any)
	if c["headSeq"] != newest["seq"] || len(newest["prev"].(string)) != 64 {
		t.Fatalf("head %v, newest %v", c["headSeq"], newest)
	}
}
