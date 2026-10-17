package ssotest

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestTheFakeProviderRefusesBadGrants(t *testing.T) {
	idp := New(t, "wiki")
	for _, path := range []string{"/.well-known/openid-configuration", "/keys"} {
		res, err := idp.Client().Get(idp.URL + path)
		if err != nil || res.StatusCode != http.StatusOK {
			t.Fatalf("%s: %v %v", path, err, res)
		}
		res.Body.Close()
	}
	res, err := idp.Client().PostForm(idp.URL+"/token", url.Values{"code": {"nope"}, "code_verifier": {"x"}})
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown code: %d", res.StatusCode)
	}
	cb := idp.Authorize(t, idp.URL+"/authorize?client_id=wiki&code_challenge_method=S256&response_type=code&state=s&nonce=n&code_challenge=c", Claims{Subject: "x"})
	if cb.Get("state") != "s" || !strings.HasPrefix(cb.Encode(), "code=") {
		t.Fatalf("callback %v", cb)
	}
}
