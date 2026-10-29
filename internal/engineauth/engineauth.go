// Package engineauth connects people to process engines by OAuth and
// holds the access tokens of their grants, in memory (ADR-0151): the
// authorization code flow with PKCE for the engine's tasks resource, the
// renewal of a grant with its refresh token — which the engine replaces
// on every use — and its end. The connections themselves are kept by
// internal/engineconn.
package engineauth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"

	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
	"github.com/dbuchs7/wikiwikibangbang/internal/engine"
	"github.com/dbuchs7/wikiwikibangbang/internal/engineconn"
	"github.com/dbuchs7/wikiwikibangbang/internal/site"
)

// Errors of connecting and of using a connection. The messages never
// carry a token or the client's secret.
var (
	// ErrNotConfigured: this server is not registered at the engine, or
	// has no public address the engine could send people back to.
	ErrNotConfigured = errors.New("engineauth: this server is not registered at the engine")
	// ErrUnknownState: the connection was not begun in this browser, or
	// took too long.
	ErrUnknownState = errors.New("engineauth: unknown or expired connection")
	// ErrEnded: the engine refused the person's grant, so the connection
	// ended — the engine's account was disabled or deleted, or the person
	// withdrew the grant there.
	ErrEnded = errors.New("engineauth: the engine refused the grant; the connection ended")
	// ErrUnavailable: the engine's authorization did not answer, or not as
	// expected; the connection stays.
	ErrUnavailable = errors.New("engineauth: the engine's authorization is not available")
)

// FlowLifetime bounds how long a begun connection may take.
const FlowLifetime = 10 * time.Minute

// metadataLifetime is how long the engine's authorization metadata is
// kept before it is read again.
const metadataLifetime = time.Hour

// tasksResource is the engine's resource a person's grant is confined to:
// the person's tasks, their forms and values, and who they are.
const tasksResource = "/tasks"

// Broker connects people to engines and holds the access tokens of their
// grants, in memory. It is safe for concurrent use.
type Broker struct {
	conns   *engineconn.Connections
	engines *engine.Reader
	client  *http.Client
	getenv  func(string) string
	public  string
	now     func() time.Time

	// ended is told of a connection the engine ended by refusing its
	// grant.
	ended func(user, engineID string)

	mu      sync.Mutex
	pending map[string]flow
	meta    map[string]metadata
	access  map[string]access
	locks   map[string]*sync.Mutex
}

type flow struct {
	user, engine, base, verifier string
	expires                      time.Time
}

// metadata is what the engine says of its tasks resource and of the
// authorization server that grants it (RFC 9728, RFC 8414).
type metadata struct {
	base, resource, authURL, tokenURL, revokeURL string
	read                                         time.Time
}

type access struct {
	token   string
	expires time.Time
}

// NewBroker connects people with conns, reads who they are with engines,
// calls the engines' authorization with client, takes the registration
// from getenv, and has the engines send people back to publicURL.
func NewBroker(conns *engineconn.Connections, engines *engine.Reader, client *http.Client, getenv func(string) string, publicURL string) *Broker {
	return &Broker{conns: conns, engines: engines, client: client, getenv: getenv, public: strings.TrimRight(publicURL, "/"), now: time.Now,
		pending: map[string]flow{}, meta: map[string]metadata{}, access: map[string]access{}, locks: map[string]*sync.Mutex{}}
}

// OnEnded has fn told of every connection that ends because the engine
// refused its grant, so it can be recorded.
func (b *Broker) OnEnded(fn func(user, engineID string)) { b.ended = fn }

// Connections are the connections the broker keeps.
func (b *Broker) Connections() *engineconn.Connections { return b.conns }

// Callback is the address the engine sends a person back to, which its
// administrator registers for this server.
func (b *Broker) Callback(engineID string) string {
	if b.public == "" {
		return ""
	}
	return b.public + "/api/engines/" + url.PathEscape(engineID) + "/connect/callback"
}

// Configured reports whether people may connect to the engine: this
// server is registered there and has a public address.
func (b *Broker) Configured(engineID string) bool {
	return b.public != "" && b.getenv(engine.ClientIDVar(engineID)) != "" && b.getenv(engine.ClientSecretVar(engineID)) != ""
}

func key(user, engineID string) string { return user + "\x00" + engineID }

func random() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		panic(err) // crypto/rand does not fail on supported platforms
	}
	return hex.EncodeToString(buf)
}

func (b *Broker) config(e site.Engine, m metadata) *oauth2.Config {
	return &oauth2.Config{
		ClientID: b.getenv(engine.ClientIDVar(e.ID)), ClientSecret: b.getenv(engine.ClientSecretVar(e.ID)),
		// The engine takes the client's credentials in the form only.
		Endpoint:    oauth2.Endpoint{AuthURL: m.authURL, TokenURL: m.tokenURL, AuthStyle: oauth2.AuthStyleInParams},
		RedirectURL: b.Callback(e.ID),
	}
}

func (b *Broker) ctx(ctx context.Context) context.Context {
	return context.WithValue(ctx, oauth2.HTTPClient, b.client)
}

// Begin starts a person's connection to an engine and answers the address
// of the engine's consent to send the browser to and the state, which the
// caller binds to the browser.
func (b *Broker) Begin(ctx context.Context, e site.Engine, user string) (authURL, state string, err error) {
	if !b.Configured(e.ID) {
		return "", "", ErrNotConfigured
	}
	m, err := b.metadata(ctx, e)
	if err != nil {
		return "", "", err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	for k, f := range b.pending {
		if !now.Before(f.expires) {
			delete(b.pending, k)
		}
	}
	if len(b.pending) >= budget.Get().EngineConnectsPending {
		return "", "", fmt.Errorf("%w: too many connections in progress", ErrUnavailable)
	}
	state, verifier := random(), oauth2.GenerateVerifier()
	b.pending[state] = flow{user: user, engine: e.ID, base: e.BaseURL, verifier: verifier, expires: now.Add(FlowLifetime)}
	return b.config(e, m).AuthCodeURL(state, oauth2.S256ChallengeOption(verifier), oauth2.SetAuthURLParam("resource", m.resource)), state, nil
}

// Pending answers whose connection to which engine a state begun, without
// using it up.
func (b *Broker) Pending(state string) (user, engineID string, ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	f, ok := b.pending[state]
	if !ok || !b.now().Before(f.expires) {
		return "", "", false
	}
	return f.user, f.engine, true
}

// Cancel forgets a begun connection the person refused at the engine.
func (b *Broker) Cancel(state string) {
	b.mu.Lock()
	delete(b.pending, state)
	b.mu.Unlock()
}

// Finish completes a connection: the state must be one Begin issued for
// the engine and not yet used; the code is exchanged with the PKCE
// verifier, and who the grant belongs to is read with it. A connection the
// person had to the engine is replaced.
func (b *Broker) Finish(ctx context.Context, e site.Engine, state, code string) (engineconn.Connection, error) {
	b.mu.Lock()
	f, ok := b.pending[state]
	delete(b.pending, state) // one use only
	b.mu.Unlock()
	if !ok || !b.now().Before(f.expires) || f.engine != e.ID || f.base != e.BaseURL {
		return engineconn.Connection{}, ErrUnknownState
	}
	m, err := b.metadata(ctx, e)
	if err != nil {
		return engineconn.Connection{}, err
	}
	tok, err := b.config(e, m).Exchange(b.ctx(ctx), code, oauth2.VerifierOption(f.verifier), oauth2.SetAuthURLParam("resource", m.resource))
	if err != nil {
		return engineconn.Connection{}, fmt.Errorf("%w: exchanging the code: %s", ErrUnavailable, tokenError(err))
	}
	if tok.AccessToken == "" || tok.RefreshToken == "" {
		return engineconn.Connection{}, fmt.Errorf("%w: the engine granted no refresh token", ErrUnavailable)
	}
	p, err := b.engines.Me(ctx, e, tok.AccessToken)
	if err != nil {
		return engineconn.Connection{}, err
	}
	// Not while a renewal of the connection it replaces runs, which would
	// keep that grant's token for this one.
	l := b.lock(f.user, e.ID)
	l.Lock()
	defer l.Unlock()
	replaced, rerr := b.conns.Refresh(f.user, e.ID)
	c, err := b.conns.Connect(f.user, e.ID, profileOf(p), tok.RefreshToken)
	if err != nil {
		return engineconn.Connection{}, err
	}
	b.keep(f.user, e.ID, tok)
	if rerr == nil && m.revokeURL != "" {
		b.revoke(ctx, e, m, replaced) // the grant it replaces is of no use now
	}
	return c, nil
}

func profileOf(p engine.Person) engineconn.Profile {
	return engineconn.Profile{Subject: p.ID, Username: p.Username, Name: p.Name, Email: p.Email, Groups: slices.Clone(p.Groups)}
}

func (b *Broker) keep(user, engineID string, tok *oauth2.Token) {
	expires := tok.Expiry
	if expires.IsZero() {
		expires = b.now().Add(5 * time.Minute)
	}
	b.mu.Lock()
	b.access[key(user, engineID)] = access{token: tok.AccessToken, expires: expires.Add(-time.Minute)}
	b.mu.Unlock()
}

func (b *Broker) lock(user, engineID string) *sync.Mutex {
	b.mu.Lock()
	defer b.mu.Unlock()
	l, ok := b.locks[key(user, engineID)]
	if !ok {
		l = &sync.Mutex{}
		b.locks[key(user, engineID)] = l
	}
	return l
}

// Token is an access token of the person's grant at the engine, renewed
// with the refresh token when the one held has run out. The engine
// replaces the refresh token on every use, so renewals of one connection
// run one after another: a second one with the token the first used would
// be refused. Only the engine refusing the grant ends the connection
// (ErrEnded); an engine that is busy or away leaves it as it is.
func (b *Broker) Token(ctx context.Context, e site.Engine, user string) (string, error) {
	l := b.lock(user, e.ID)
	l.Lock()
	defer l.Unlock()
	b.mu.Lock()
	a, ok := b.access[key(user, e.ID)]
	b.mu.Unlock()
	if ok && b.now().Before(a.expires) {
		return a.token, nil
	}
	refresh, err := b.conns.Refresh(user, e.ID)
	if err != nil {
		return "", err
	}
	if !b.Configured(e.ID) {
		return "", ErrNotConfigured
	}
	m, err := b.metadata(ctx, e)
	if err != nil {
		return "", err
	}
	tok, err := b.config(e, m).TokenSource(b.ctx(ctx), &oauth2.Token{RefreshToken: refresh}).Token()
	if err != nil {
		var re *oauth2.RetrieveError
		if errors.As(err, &re) && re.ErrorCode == "invalid_grant" {
			b.forget(user, e.ID)
			if err := b.conns.End(user, e.ID, "refused by the engine"); err != nil && !errors.Is(err, engineconn.ErrNotConnected) {
				return "", err
			}
			if b.ended != nil {
				b.ended(user, e.ID)
			}
			return "", ErrEnded
		}
		return "", fmt.Errorf("%w: renewing the grant: %s", ErrUnavailable, tokenError(err))
	}
	if tok.RefreshToken != "" && tok.RefreshToken != refresh {
		if err := b.conns.Renewed(user, e.ID, tok.RefreshToken, nil); err != nil {
			return "", err
		}
	}
	b.keep(user, e.ID, tok)
	return tok.AccessToken, nil
}

// Refused tells that the engine refused an access token the broker gave
// out, as it does once the grant is withdrawn there: the next Token
// renews, and so learns whether the grant still stands.
func (b *Broker) Refused(user, engineID string) { b.forget(user, engineID) }

func (b *Broker) forget(user, engineID string) {
	b.mu.Lock()
	delete(b.access, key(user, engineID))
	b.mu.Unlock()
}

// Disconnect ends a person's connection to an engine. Where the engine
// offers to revoke a grant (RFC 7009), the refresh token is revoked
// there first, as far as the engine answers; the connection ends here in
// any case.
func (b *Broker) Disconnect(ctx context.Context, e site.Engine, user string) error {
	l := b.lock(user, e.ID)
	l.Lock()
	defer l.Unlock()
	refresh, err := b.conns.Refresh(user, e.ID)
	if errors.Is(err, engineconn.ErrNotConnected) {
		return err
	}
	if err == nil && b.Configured(e.ID) {
		if m, merr := b.metadata(ctx, e); merr == nil && m.revokeURL != "" {
			b.revoke(ctx, e, m, refresh)
		}
	}
	b.forget(user, e.ID)
	return b.conns.End(user, e.ID, "disconnected")
}

// revoke asks the engine to revoke a refresh token, and with it the
// grant; what it answers does not change that the connection ends.
func (b *Broker) revoke(ctx context.Context, e site.Engine, m metadata, refresh string) {
	form := url.Values{"token": {refresh}, "token_type_hint": {"refresh_token"},
		"client_id": {b.getenv(engine.ClientIDVar(e.ID))}, "client_secret": {b.getenv(engine.ClientSecretVar(e.ID))}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.revokeURL, strings.NewReader(form.Encode()))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if res, err := b.client.Do(req); err == nil {
		res.Body.Close()
	}
}

// EndEngine ends every connection to an engine, as when it is removed or
// its address changes: the grants belong to the engine at the old
// address. It answers whose connections ended.
func (b *Broker) EndEngine(engineID, reason string) ([]string, error) {
	users, err := b.conns.EndEngine(engineID, reason)
	b.mu.Lock()
	for _, u := range users {
		delete(b.access, key(u, engineID))
	}
	delete(b.meta, engineID)
	b.mu.Unlock()
	return users, err
}

// metadata reads what the engine says of its tasks resource and of the
// authorization server that grants it, kept for metadataLifetime. The
// resource is the engine's own spelling of it, which it compares exactly.
func (b *Broker) metadata(ctx context.Context, e site.Engine) (metadata, error) {
	b.mu.Lock()
	m, ok := b.meta[e.ID]
	b.mu.Unlock()
	if ok && m.base == e.BaseURL && b.now().Sub(m.read) < metadataLifetime {
		return m, nil
	}
	if e.Kind != site.EngineAtlas {
		return metadata{}, fmt.Errorf("%w: kind %q has no grants for its tasks", ErrNotConfigured, e.Kind)
	}
	var pr struct {
		Resource             string   `json:"resource"`
		AuthorizationServers []string `json:"authorization_servers"`
	}
	if err := b.getJSON(ctx, e.BaseURL+"/.well-known/oauth-protected-resource"+tasksResource, &pr); err != nil {
		return metadata{}, err
	}
	if pr.Resource == "" || len(pr.AuthorizationServers) == 0 {
		return metadata{}, fmt.Errorf("%w: the engine names no resource for tasks or no authorization server", ErrUnavailable)
	}
	issuer := strings.TrimRight(pr.AuthorizationServers[0], "/")
	var as struct {
		Issuer         string   `json:"issuer"`
		Authorize      string   `json:"authorization_endpoint"`
		Token          string   `json:"token_endpoint"`
		Revoke         string   `json:"revocation_endpoint"`
		ChallengeKinds []string `json:"code_challenge_methods_supported"`
	}
	if err := b.getJSON(ctx, issuer+"/.well-known/oauth-authorization-server", &as); err != nil {
		return metadata{}, err
	}
	if as.Authorize == "" || as.Token == "" || !slices.Contains(as.ChallengeKinds, "S256") {
		return metadata{}, fmt.Errorf("%w: the engine's authorization offers no code flow with PKCE", ErrUnavailable)
	}
	for _, u := range []string{as.Authorize, as.Token, as.Revoke} {
		if u != "" && !sameOrigin(u, issuer) {
			return metadata{}, fmt.Errorf("%w: the engine's authorization names an address of another server", ErrUnavailable)
		}
	}
	m = metadata{base: e.BaseURL, resource: pr.Resource, authURL: as.Authorize, tokenURL: as.Token, revokeURL: as.Revoke, read: b.now()}
	b.mu.Lock()
	b.meta[e.ID] = m
	b.mu.Unlock()
	return m, nil
}

// sameOrigin reports whether u is on the server of base: the person's
// refresh token and the client's secret go nowhere else.
func sameOrigin(u, base string) bool {
	a, err1 := url.Parse(u)
	c, err2 := url.Parse(base)
	return err1 == nil && err2 == nil && a.Scheme == c.Scheme && a.Host == c.Host
}

func (b *Broker) getJSON(ctx context.Context, u string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	req.Header.Set("Accept", "application/json")
	res, err := b.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: %s answered %d", ErrUnavailable, u, res.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, budget.Get().EngineResponseBytes)).Decode(into); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrUnavailable, u, err)
	}
	return nil
}

// tokenError is what the engine's token endpoint said, without anything
// that was sent to it.
func tokenError(err error) string {
	var re *oauth2.RetrieveError
	if errors.As(err, &re) {
		if re.ErrorCode != "" {
			return re.ErrorCode
		}
		if re.Response != nil {
			return fmt.Sprintf("it answered %d", re.Response.StatusCode)
		}
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err.Error()
	}
	return "no answer"
}
