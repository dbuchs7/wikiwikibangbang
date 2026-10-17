// Package ssotest is a fake OpenID Connect provider for tests: discovery,
// keys, an authorization step the test drives by hand, and a token endpoint
// that checks the code and its PKCE verifier.
package ssotest

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
)

// IdP is a running fake provider.
type IdP struct {
	*httptest.Server
	ClientID string
	key      *rsa.PrivateKey

	mu    sync.Mutex
	codes map[string]grant
}

// Claims is what a login asserts; zero fields get defaults.
type Claims struct {
	Subject, PreferredUsername, Email string
	Audience, Issuer, Nonce           string // override to forge
	Expires                           time.Time
}

type grant struct {
	claims    Claims
	challenge string
}

// New starts a fake provider for the client ID.
func New(t *testing.T, clientID string) *IdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	idp := &IdP{ClientID: clientID, key: key, codes: map[string]grant{}}
	mux := http.NewServeMux()
	idp.Server = httptest.NewServer(mux)
	t.Cleanup(idp.Close)
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"issuer": idp.URL, "authorization_endpoint": idp.URL + "/authorize", "token_endpoint": idp.URL + "/token",
			"jwks_uri": idp.URL + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"},
			"response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"},
		})
	})
	mux.HandleFunc("GET /keys", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
	})
	mux.HandleFunc("POST /token", idp.token)
	return idp
}

// Authorize plays the user approving the login at authURL and returns the
// callback query the provider would redirect the browser with.
func (idp *IdP) Authorize(t *testing.T, authURL string, c Claims) url.Values {
	t.Helper()
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("client_id") != idp.ClientID || q.Get("code_challenge_method") != "S256" || q.Get("response_type") != "code" {
		t.Fatalf("authorization request %s", u)
	}
	if c.Nonce == "" {
		c.Nonce = q.Get("nonce")
	}
	b := make([]byte, 16)
	rand.Read(b)
	code := hex.EncodeToString(b)
	idp.mu.Lock()
	idp.codes[code] = grant{claims: c, challenge: q.Get("code_challenge")}
	idp.mu.Unlock()
	return url.Values{"code": {code}, "state": {q.Get("state")}}
}

func (idp *IdP) token(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	idp.mu.Lock()
	g, ok := idp.codes[r.Form.Get("code")]
	delete(idp.codes, r.Form.Get("code"))
	idp.mu.Unlock()
	sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
	if !ok || base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid_grant"}`))
		return
	}
	c := g.claims
	if c.Audience == "" {
		c.Audience = idp.ClientID
	}
	if c.Issuer == "" {
		c.Issuer = idp.URL
	}
	if c.Expires.IsZero() {
		c.Expires = time.Now().Add(time.Hour)
	}
	payload, _ := json.Marshal(map[string]any{
		"iss": c.Issuer, "aud": c.Audience, "sub": c.Subject, "nonce": c.Nonce,
		"iat": time.Now().Unix(), "exp": c.Expires.Unix(),
		"preferred_username": c.PreferredUsername, "email": c.Email, "email_verified": c.Email != "",
	})
	signer, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: idp.key}, (&jose.SignerOptions{}).WithHeader("kid", "k1"))
	jws, _ := signer.Sign(payload)
	idToken, _ := jws.CompactSerialize()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "token_type": "Bearer", "expires_in": 3600, "id_token": idToken})
}
