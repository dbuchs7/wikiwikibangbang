package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/auth"
	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
)

func TestAccountRequestsThroughTheAPI(t *testing.T) {
	w := newWorld(t)
	anon := &client{t: t, h: w.admin.h}
	ask := `{"username":"dana","password":"dana's password","email":"dana@example.org","reason":"Team Finance"}`
	// Closed by default.
	if got := anon.expect("GET", "/api/signup", "", http.StatusOK); got["open"] != false {
		t.Fatalf("signup %v", got)
	}
	anon.expect("POST", "/api/signup", ask, http.StatusForbidden)
	w.bert.expect("PUT", "/api/settings/access", `{"accountRequests":true}`, http.StatusForbidden)
	w.admin.expect("PUT", "/api/settings/access", `{"accountRequests":true}`, http.StatusOK)
	if got := anon.expect("GET", "/api/signup", "", http.StatusOK); got["open"] != true {
		t.Fatalf("signup %v", got)
	}

	anon.expect("POST", "/api/signup", `{"username":"dana","password":"dana's password","email":"nope"}`, http.StatusBadRequest)
	anon.expect("POST", "/api/signup", `{"username":"bert","password":"bert's password","email":"b@example.org"}`, http.StatusConflict)
	anon.expect("POST", "/api/signup", ask, http.StatusAccepted)
	anon.expect("POST", "/api/signup", ask, http.StatusConflict)

	w.bert.expect("GET", "/api/account-requests", "", http.StatusForbidden)
	if me := w.admin.expect("GET", "/api/me", "", http.StatusOK); me["requests"] != float64(1) {
		t.Fatalf("an administrator sees the waiting request: %v", me["requests"])
	}
	if me := w.bert.expect("GET", "/api/me", "", http.StatusOK); me["requests"] != float64(0) {
		t.Fatalf("others do not: %v", me["requests"])
	}
	list := w.admin.expect("GET", "/api/account-requests", "", http.StatusOK)["requests"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["email"] != "dana@example.org" || list[0].(map[string]any)["reason"] != "Team Finance" {
		t.Fatalf("requests %v", list)
	}
	id := list[0].(map[string]any)["id"].(string)
	w.bert.expect("POST", "/api/account-requests/"+id+"/approve", "", http.StatusForbidden)
	u := w.admin.expect("POST", "/api/account-requests/"+id+"/approve", "", http.StatusCreated)["user"].(map[string]any)
	w.admin.expect("POST", "/api/account-requests/"+id+"/approve", "", http.StatusNotFound)

	// Dana signs in with the password chosen when asking.
	dana := &client{t: t, h: w.admin.h}
	dana.login("dana", "dana's password")
	if got := dana.expect("GET", "/api/me/contacts", "", http.StatusOK); got["email"] != "dana@example.org" {
		t.Fatalf("contacts %v", got)
	}
	users := w.admin.expect("GET", "/api/users", "", http.StatusOK)["users"].([]any)
	found := false
	for _, x := range users {
		if m := x.(map[string]any); m["id"] == u["id"] {
			found = m["email"] == "dana@example.org" && len(m["channels"].([]any)) == 1
		}
	}
	if !found {
		t.Fatalf("administrators see the address: %v", users)
	}

	// A rejected request leaves no account.
	anon.expect("POST", "/api/signup", `{"username":"egon","password":"egon's password","email":"egon@example.org"}`, http.StatusAccepted)
	id = w.admin.expect("GET", "/api/account-requests", "", http.StatusOK)["requests"].([]any)[0].(map[string]any)["id"].(string)
	w.admin.expect("DELETE", "/api/account-requests/"+id, "", http.StatusNoContent)
	anon.expect("POST", "/api/login", `{"username":"egon","password":"egon's password"}`, http.StatusUnauthorized)

	trail := strings.Join(eventsOf(w.admin.expect("GET", "/api/audit", "", http.StatusOK)), ",")
	for _, e := range []string{"access.change", "account.request", "account.approve", "account.reject"} {
		if !strings.Contains(trail, e) {
			t.Errorf("the audit trail has no %s", e)
		}
	}
}

func TestContactsThroughTheAPI(t *testing.T) {
	w := newWorld(t)
	if got := w.bert.expect("GET", "/api/me/contacts", "", http.StatusOK); len(got) != 1 || got["mail"] != false {
		t.Fatalf("no channels at first: %v", got)
	}
	w.bert.expect("PUT", "/api/me/contacts", `{"slack":"https://example.org/x"}`, http.StatusBadRequest)
	got := w.bert.expect("PUT", "/api/me/contacts", `{"email":"bert@example.org","teams":"https://example.webhook.office.com/webhookb2/x"}`, http.StatusOK)
	if got["email"] != "bert@example.org" || got["slack"] != nil {
		t.Fatalf("contacts %v", got)
	}
	w.bert.expect("PUT", "/api/users/"+w.ids["vera"]+"/email", `{"email":"v@example.org"}`, http.StatusForbidden)
	w.admin.expect("PUT", "/api/users/"+w.ids["bert"]+"/email", `{"email":"b@example.org"}`, http.StatusNoContent)
	w.admin.expect("PUT", "/api/users/nobody/email", `{"email":"b@example.org"}`, http.StatusNotFound)
	if got := w.bert.expect("GET", "/api/me/contacts", "", http.StatusOK); got["email"] != "b@example.org" || got["teams"] == nil {
		t.Fatalf("the administrator changes the address only: %v", got)
	}
	trail := w.admin.expect("GET", "/api/audit", "", http.StatusOK)
	if s := strings.Join(eventsOf(trail), ","); !strings.Contains(s, "contacts.change") {
		t.Fatal("the audit trail has no contacts change")
	}
	if b, _ := w.admin.do("GET", "/api/audit", "").Body.ReadString(0); strings.Contains(b, "webhook.office.com") {
		t.Fatal("a webhook address is a credential and stays out of the audit trail")
	}
}

func TestUserSearch(t *testing.T) {
	w := newWorld(t)
	got := w.bert.expect("GET", "/api/users/search?q=ER", "", http.StatusOK)["users"].([]any)
	// "vera" contains "er"; "bert" too; prefixes come first.
	names := []string{}
	for _, u := range got {
		names = append(names, u.(map[string]any)["username"].(string))
	}
	if strings.Join(names, ",") != "bert,vera" {
		t.Fatalf("search: %v", names)
	}
	if got := w.bert.expect("GET", "/api/users/search?q=v", "", http.StatusOK)["users"].([]any); len(got) != 1 || got[0].(map[string]any)["username"] != "vera" {
		t.Fatalf("prefix: %v", got)
	}
	// Vera only reads, Carla has no role: neither looks people up.
	w.vera.expect("GET", "/api/users/search?q=b", "", http.StatusForbidden)
	w.carla.expect("GET", "/api/users/search?q=b", "", http.StatusForbidden)
}

func TestAccountRequestErrors(t *testing.T) {
	w := newWorld(t)
	anon := &client{t: t, h: w.admin.h}
	w.admin.expect("PUT", "/api/settings/access", `{"accountRequests":true}`, http.StatusOK)
	w.admin.expect("PUT", "/api/settings/access", `not json`, http.StatusBadRequest)
	anon.expect("POST", "/api/signup", `not json`, http.StatusBadRequest)
	b := budget.Default()
	b.AccountRequests = 1
	budget.Set(b)
	defer budget.Set(budget.Default())
	anon.expect("POST", "/api/signup", `{"username":"gina","password":"gina's password","email":"g@example.org"}`, http.StatusAccepted)
	anon.expect("POST", "/api/signup", `{"username":"hugo","password":"hugo's password","email":"h@example.org"}`, http.StatusServiceUnavailable)
	for range auth.MaxFailures {
		anon.do("POST", "/api/signup", `{"username":"x","password":"a long password","email":"x@example.org"}`)
	}
	if r := anon.do("POST", "/api/signup", `{"username":"ivan","password":"ivan's password","email":"i@example.org"}`); r.Code != http.StatusTooManyRequests {
		t.Fatalf("throttle %d", r.Code)
	}
	w.bert.expect("PUT", "/api/me/contacts", `not json`, http.StatusBadRequest)
	w.admin.expect("PUT", "/api/users/"+w.ids["bert"]+"/email", `{"email":"nope"}`, http.StatusBadRequest)
	w.admin.expect("PUT", "/api/users/"+w.ids["bert"]+"/email", `not json`, http.StatusBadRequest)
	w.bert.expect("GET", "/api/users/search?q=a", "", http.StatusOK)
}
