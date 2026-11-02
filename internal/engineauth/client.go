package engineauth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/dbuchs7/orbithek/internal/budget"
	"github.com/dbuchs7/orbithek/internal/engine"
	"github.com/dbuchs7/orbithek/internal/engineconn"
	"github.com/dbuchs7/orbithek/internal/site"
)

// ClientMetadataPath is where this server publishes its client metadata
// document (ADR-0152): its address is the client ID it names itself by at
// an engine, and the document gives the engine what it needs to know of
// it — its name, where people are sent back to, and the public key its
// assertions are checked with. No client is registered by hand and no
// secret is shared.
const ClientMetadataPath = "/api/engines/client-metadata"

// assertionType is RFC 7523's client assertion of a signed JWT.
const assertionType = "urn:ietf:params:oauth:client-assertion-type:jwt-bearer"

// assertionLifetime bounds how long an assertion may be used.
const assertionLifetime = 2 * time.Minute

// client is how this server names itself to an engine: a client an
// administrator registered there, its ID and secret from the environment,
// or the client metadata document, authenticated by a signed assertion.
type client struct {
	id, secret string
	metadata   bool
}

// MetadataClientID is the client ID this server names itself by through
// its client metadata document, empty without a public address.
func (b *Broker) MetadataClientID() string {
	if b.public == "" {
		return ""
	}
	return b.public + ClientMetadataPath
}

// envClient is the client an administrator registered at the engine, if
// the environment names it.
func (b *Broker) envClient(engineID string) (client, bool) {
	id, secret := b.getenv(engine.ClientIDVar(engineID)), b.getenv(engine.ClientSecretVar(engineID))
	return client{id: id, secret: secret}, id != "" && secret != ""
}

// EnvClient reports whether the environment names a client registered at
// the engine; people connect through it rather than the document.
func (b *Broker) EnvClient(engineID string) (id string, ok bool) {
	c, ok := b.envClient(engineID)
	return c.id, ok
}

// personClient is the client a person connects through: the registered
// one where the environment names it, otherwise the metadata document once
// an administrator connected the engine — which shows the engine knows it.
func (b *Broker) personClient(engineID string) (client, bool) {
	if b.public == "" {
		return client{}, false
	}
	if c, ok := b.envClient(engineID); ok {
		return c, true
	}
	if _, ok := b.System(engineID); ok {
		return client{id: b.MetadataClientID(), metadata: true}, true
	}
	return client{}, false
}

// clientOf is the client a connection's grant was issued to: a grant is
// renewed and revoked by the client it belongs to.
func (b *Broker) clientOf(engineID string, p engineconn.Profile) (client, bool) {
	if p.Client != "" {
		return client{id: p.Client, metadata: true}, true
	}
	return b.envClient(engineID)
}

// signingKey is this server's key for its assertions, made on first use and
// kept sealed with the connections, never backed up: a restored server
// makes a new one, which its document tells every engine.
func (b *Broker) signingKey() (ed25519.PrivateKey, error) {
	b.keyMu.Lock()
	defer b.keyMu.Unlock()
	if b.key != nil {
		return b.key, nil
	}
	seed, ok, err := b.conns.ClientKey()
	if err != nil {
		return nil, err
	}
	if !ok {
		seed = make([]byte, ed25519.SeedSize)
		if _, err := rand.Read(seed); err != nil {
			return nil, err
		}
		if err := b.conns.SetClientKey(seed); err != nil {
			return nil, err
		}
	}
	b.key = ed25519.NewKeyFromSeed(seed)
	return b.key, nil
}

// ReplaceKey makes a new signing key; the old one is destroyed, and an
// engine learns the new one when it next reads the document.
func (b *Broker) ReplaceKey() error {
	seed := make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		return err
	}
	b.keyMu.Lock()
	defer b.keyMu.Unlock()
	if err := b.conns.SetClientKey(seed); err != nil {
		return err
	}
	b.key = ed25519.NewKeyFromSeed(seed)
	return nil
}

// kidOf names a public key in the document and in the assertions.
func kidOf(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return base64.RawURLEncoding.EncodeToString(sum[:12])
}

// assertion is a signed JWT that authenticates this server to an engine's
// token or revocation endpoint (RFC 7523), for aud, the token endpoint.
func (b *Broker) assertion(aud string) (string, error) {
	key, err := b.signingKey()
	if err != nil {
		return "", err
	}
	pub := key.Public().(ed25519.PublicKey)
	now := b.now()
	id := b.MetadataClientID()
	header, _ := json.Marshal(map[string]string{"alg": "EdDSA", "typ": "JWT", "kid": kidOf(pub)})
	claims, _ := json.Marshal(map[string]any{"iss": id, "sub": id, "aud": aud, "jti": random(),
		"iat": now.Unix(), "exp": now.Add(assertionLifetime).Unix()})
	signing := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claims)
	return signing + "." + base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, []byte(signing))), nil
}

// ClientMetadata is this server's client metadata document: name is the
// installation's, engines those whose callback people are sent back to.
func (b *Broker) ClientMetadata(name string, engines []site.Engine) (map[string]any, error) {
	if b.public == "" {
		return nil, ErrNotConfigured
	}
	key, err := b.signingKey()
	if err != nil {
		return nil, err
	}
	pub := key.Public().(ed25519.PublicKey)
	redirects := []string{}
	for _, e := range engines {
		if e.Kind == site.EngineAtlas {
			redirects = append(redirects, b.Callback(e.ID))
		}
	}
	if strings.TrimSpace(name) == "" {
		if u, err := url.Parse(b.public); err == nil {
			name = u.Host
		}
	}
	return map[string]any{
		"client_id": b.MetadataClientID(), "client_name": name, "client_uri": b.public,
		"redirect_uris": redirects, "grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"},
		"token_endpoint_auth_method": "private_key_jwt", "token_endpoint_auth_signing_alg": "EdDSA",
		"jwks": map[string]any{"keys": []map[string]string{{"kty": "OKP", "crv": "Ed25519", "x": base64.RawURLEncoding.EncodeToString(pub),
			"kid": kidOf(pub), "use": "sig", "alg": "EdDSA"}}},
	}, nil
}

// authenticate adds the client's credentials to a request of the engine's
// token or revocation endpoint: its secret, or an assertion for aud.
func (b *Broker) authenticate(v url.Values, c client, aud string) error {
	v.Set("client_id", c.id)
	if !c.metadata {
		v.Set("client_secret", c.secret)
		return nil
	}
	a, err := b.assertion(aud)
	if err != nil {
		return fmt.Errorf("%w: signing the assertion: %v", ErrUnavailable, err)
	}
	v.Set("client_assertion_type", assertionType)
	v.Set("client_assertion", a)
	return nil
}

// retrieve asks the engine's token endpoint for a token. An answer other
// than a token is an *oauth2.RetrieveError carrying the engine's error
// code, as golang.org/x/oauth2 answers it.
func (b *Broker) retrieve(ctx context.Context, m metadata, c client, v url.Values) (*oauth2.Token, error) {
	if err := b.authenticate(v, c, m.tokenURL); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.tokenURL, strings.NewReader(v.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	res, err := b.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, budget.Get().EngineResponseBytes))
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(body, &e)
		return nil, &oauth2.RetrieveError{Response: res, Body: body, ErrorCode: e.Error}
	}
	var got struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &got); err != nil || got.AccessToken == "" {
		return nil, &oauth2.RetrieveError{Response: res, Body: body}
	}
	tok := &oauth2.Token{AccessToken: got.AccessToken, TokenType: got.TokenType, RefreshToken: got.RefreshToken}
	if got.ExpiresIn > 0 {
		tok.Expiry = b.now().Add(time.Duration(got.ExpiresIn) * time.Second)
	}
	return tok, nil
}
