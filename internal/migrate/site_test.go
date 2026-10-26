package migrate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

func TestAccountsAndGrantsAreResolved(t *testing.T) {
	idOf := func(name string) (string, bool) {
		id, ok := map[string]string{"anna": "u-anna"}[name]
		return id, ok
	}
	if got, err := LocalAccounts(map[string]string{"acc-1": "anna"}, idOf); err != nil || got["acc-1"] != "u-anna" {
		t.Errorf("accounts %v %v", got, err)
	}
	if _, err := LocalAccounts(map[string]string{"acc-1": "nobody"}, idOf); err == nil {
		t.Error("an unknown username")
	}
	grants, err := LocalGrants([]string{"anna=space-admin", "@eng = editor"}, idOf)
	if err != nil || len(grants) != 2 || grants[0] != (Grant{Principal: "u-anna", Role: item.SpaceAdmin}) || grants[1] != (Grant{Principal: "@eng", Role: item.Editor}) {
		t.Errorf("grants %+v %v", grants, err)
	}
	for _, bad := range [][]string{{"nobody=viewer"}, {"anna=owner"}, {"anna"}, {"@=viewer"}, {"=viewer"}} {
		if _, err := LocalGrants(bad, idOf); err == nil {
			t.Errorf("%v resolved", bad)
		}
	}
}

func TestReadCloudReadsNothingUnasked(t *testing.T) {
	ctx := context.Background()
	if snap, problems, err := ReadCloud(ctx, "https://acme.example", "a", "t", nil, nil, nil); err != nil || len(problems) != 0 || len(snap.Pages)+len(snap.Issues) != 0 {
		t.Errorf("%+v %v %v", snap, problems, err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", http.StatusUnauthorized) }))
	defer srv.Close()
	if _, _, err := ReadCloud(ctx, srv.URL, "a", "t", srv.Client(), []string{"ENG"}, nil); err == nil {
		t.Error("a refused space")
	}
	if _, _, err := ReadCloud(ctx, srv.URL, "a", "t", srv.Client(), nil, []string{"PLAT"}); err == nil {
		t.Error("a refused project")
	}
}
