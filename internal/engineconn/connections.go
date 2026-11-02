// Package engineconn keeps the connections of people to process engines
// (ADR-0151): a person approves this server at an engine once, by OAuth,
// and the server then reads and works that person's tasks there with the
// person's own grant, so the engine decides what each person sees and may
// do. A connection names who the person is at the engine and holds the
// grant's refresh token, sealed with a key of its own that is destroyed
// when the connection ends. The OAuth flow, and the access tokens, which
// live in memory only, are internal/engineauth's.
package engineconn

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
)

// ErrNotConnected is a person without a connection to the engine.
var ErrNotConnected = errors.New("engineconn: not connected to the engine")

// Profile is who a person is at an engine, as the engine answers it with
// the person's grant.
type Profile struct {
	// Subject is the engine's ID of the person, what the connection keeps.
	Subject  string   `json:"subject"`
	Username string   `json:"username"`
	Name     string   `json:"name,omitempty"`
	Email    string   `json:"email,omitempty"`
	Groups   []string `json:"groups,omitempty"`
	// Client is the client the grant was issued to, when it is this
	// server's client metadata document rather than a client registered
	// by an administrator (ADR-0152): renewing and revoking it must name
	// the same client.
	Client string `json:"client,omitempty"`
	// System is what the engine approved for this server, on its system
	// grant only (ADR-0152).
	System *System `json:"system,omitempty"`
}

// System is what an engine's administrator approved for this server's
// system grant (ADR-0152).
type System struct {
	ApprovedBy   string       `json:"approvedBy"`
	ApprovedAt   time.Time    `json:"approvedAt"`
	Capabilities []Capability `json:"capabilities"`
	// Preconsent says people's grants are approved without a consent
	// screen of their own.
	Preconsent bool `json:"preconsent"`
}

// Capability is one thing a system grant reaches — diagrams, tasklist or
// events — and the projects it is confined to, none for all.
type Capability struct {
	Scope    string   `json:"scope"`
	Projects []string `json:"projects,omitempty"`
}

// Holds reports whether the grant reaches scope.
func (s *System) Holds(scope string) bool {
	return s != nil && slices.ContainsFunc(s.Capabilities, func(c Capability) bool { return c.Scope == scope })
}

// SystemUser is the subject an engine's system grant is kept under
// (ADR-0152); keyUser and keyEngine hold this server's signing key for
// client authentication. No account has an ID that starts with ":", so
// none can be them, and listings leave them out.
const (
	SystemUser = ":system"
	keyUser    = ":client"
	keyEngine  = ":key"
)

func reserved(user string) bool { return strings.HasPrefix(user, ":") }

// Connection is one person's connection to one engine.
type Connection struct {
	User   string `json:"-"`
	Engine string `json:"engine"`
	Profile
	Connected time.Time `json:"connected"`
}

// Sealer seals refresh tokens with a key per connection and destroys the
// key when the connection ends, so a token of an ended connection cannot
// be read again, from the journal or from a copy of it.
type Sealer interface {
	Seal(key string, pt, aad []byte) ([]byte, error)
	Open(key string, ct, aad []byte) ([]byte, error)
	Shred(keys ...string) error
}

// Kinds of the journal's records.
const (
	connected = "connected"
	renewed   = "renewed"
	ended     = "ended"
	// declined: a person ended their connection themselves, so they are
	// not connected again without asking (ADR-0152); connecting clears it.
	declined = "declined"
)

// record is one entry of the journal. Sealed holds the refresh token,
// sealed with the connection's key.
type record struct {
	V       int       `json:"v"`
	Kind    string    `json:"kind"`
	At      time.Time `json:"at"`
	User    string    `json:"user"`
	Engine  string    `json:"engine"`
	Key     string    `json:"key,omitempty"`
	Profile *Profile  `json:"profile,omitempty"`
	Sealed  []byte    `json:"sealed,omitempty"`
	Reason  string    `json:"reason,omitempty"`
}

// conn is a connection with its key and its sealed refresh token, never
// the token in plaintext.
type conn struct {
	Connection
	key     string
	sealed  []byte
	renewed time.Time
}

// Connections is safe for concurrent use.
type Connections struct {
	mu      sync.Mutex
	journal func([]byte) error
	sealer  Sealer
	newKey  func() string
	now     func() time.Time
	conns   map[string]*conn // by user and engine
	// declined are the people, by user and engine, who ended their
	// connection themselves.
	declined map[string]bool
	records  int
}

func id(user, engine string) string { return user + "\x00" + engine }

// The prefix binds the ciphertext to its purpose. It is part of the stored
// data and keeps the product's former name; changing it makes everything
// sealed before unreadable (ADR-0153, ADR-0155).
func aad(user, engine, key string) []byte {
	return []byte("wikiwikibangbang engine connection " + user + " " + engine + " " + key)
}

// New rebuilds the connections from records and journals later changes.
// newKey makes the ID of a new connection's key.
func New(records [][]byte, journal func([]byte) error, sealer Sealer, newKey func() string) (*Connections, error) {
	c := &Connections{journal: journal, sealer: sealer, newKey: newKey, now: time.Now, conns: map[string]*conn{}, declined: map[string]bool{}}
	for i, data := range records {
		var r record
		if err := json.Unmarshal(data, &r); err != nil {
			return nil, fmt.Errorf("engineconn: record %d: %w", i+1, err)
		}
		if err := c.apply(r); err != nil {
			return nil, fmt.Errorf("engineconn: record %d: %w", i+1, err)
		}
	}
	c.records = len(records)
	return c, nil
}

func (c *Connections) apply(r record) error {
	if r.V != 1 {
		return fmt.Errorf("format version %d is not supported", r.V)
	}
	k := id(r.User, r.Engine)
	switch r.Kind {
	case connected:
		if r.Profile == nil || r.Key == "" || len(r.Sealed) == 0 {
			return errors.New("a connection without its profile, key or token")
		}
		c.conns[k] = &conn{Connection: Connection{User: r.User, Engine: r.Engine, Profile: *r.Profile, Connected: r.At}, key: r.Key, sealed: r.Sealed, renewed: r.At}
		delete(c.declined, k)
	case renewed:
		cn, ok := c.conns[k]
		if !ok || cn.key != r.Key {
			return nil // of a connection that ended since
		}
		cn.sealed, cn.renewed = r.Sealed, r.At
		if r.Profile != nil {
			cn.Profile = *r.Profile
		}
	case ended:
		if cn, ok := c.conns[k]; ok && (r.Key == "" || cn.key == r.Key) {
			delete(c.conns, k)
		}
	case declined:
		c.declined[k] = true
	default:
		return fmt.Errorf("unknown kind %q", r.Kind)
	}
	return nil
}

func (c *Connections) record(r record) error {
	r.V = 1
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if err := c.journal(data); err != nil {
		return err
	}
	c.records++
	return c.apply(r)
}

// Connect keeps a new connection of user to engine, replacing one the
// person had, whose key is destroyed.
func (c *Connections) Connect(user, engine string, p Profile, refresh string) (Connection, error) {
	if user == "" || engine == "" || p.Subject == "" || refresh == "" {
		return Connection{}, errors.New("engineconn: a connection needs a person, an engine, the engine's ID of the person and a refresh token")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	old, had := c.conns[id(user, engine)]
	key := c.newKey()
	sealed, err := c.sealer.Seal(key, []byte(refresh), aad(user, engine, key))
	if err != nil {
		return Connection{}, err
	}
	p.Groups = slices.Clone(p.Groups)
	if err := c.record(record{Kind: connected, At: c.now().UTC(), User: user, Engine: engine, Key: key, Profile: &p, Sealed: sealed}); err != nil {
		return Connection{}, err
	}
	if had {
		_ = c.sealer.Shred(old.key) // retried by the sealer; the record already replaced it
	}
	return c.conns[id(user, engine)].Connection, nil
}

// Renewed keeps the refresh token that replaced the one used, and the
// profile when the engine answered one. A connection that ended meanwhile
// stays ended.
func (c *Connections) Renewed(user, engine, refresh string, p *Profile) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	cn, ok := c.conns[id(user, engine)]
	if !ok {
		return ErrNotConnected
	}
	sealed, err := c.sealer.Seal(cn.key, []byte(refresh), aad(user, engine, cn.key))
	if err != nil {
		return err
	}
	if p != nil {
		q := *p
		q.Groups = slices.Clone(q.Groups)
		p = &q
	}
	return c.record(record{Kind: renewed, At: c.now().UTC(), User: user, Engine: engine, Key: cn.key, Profile: p, Sealed: sealed})
}

// End ends a connection, for reason, and destroys its key.
func (c *Connections) End(user, engine, reason string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	cn, ok := c.conns[id(user, engine)]
	if !ok {
		return ErrNotConnected
	}
	if err := c.record(record{Kind: ended, At: c.now().UTC(), User: user, Engine: engine, Key: cn.key, Reason: reason}); err != nil {
		return err
	}
	return c.sealer.Shred(cn.key)
}

// EndEngine ends every connection to engine, as when the engine is
// removed, and answers whose they were. Their keys are destroyed
// together.
func (c *Connections) EndEngine(engine, reason string) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var users, keys []string
	for _, cn := range c.conns {
		if cn.Engine != engine {
			continue
		}
		key := cn.key
		if err := c.record(record{Kind: ended, At: c.now().UTC(), User: cn.User, Engine: engine, Key: key, Reason: reason}); err != nil {
			return users, errors.Join(err, c.sealer.Shred(keys...))
		}
		users, keys = append(users, cn.User), append(keys, key)
	}
	slices.Sort(users)
	if len(keys) == 0 {
		return nil, nil
	}
	return users, c.sealer.Shred(keys...)
}

// Refresh is the refresh token of a connection, unsealed.
func (c *Connections) Refresh(user, engine string) (string, error) {
	c.mu.Lock()
	cn, ok := c.conns[id(user, engine)]
	var key string
	var sealed []byte
	if ok {
		key, sealed = cn.key, cn.sealed
	}
	c.mu.Unlock()
	if !ok {
		return "", ErrNotConnected
	}
	pt, err := c.sealer.Open(key, sealed, aad(user, engine, key))
	if err != nil {
		return "", fmt.Errorf("engineconn: the refresh token cannot be read: %w", err)
	}
	return string(pt), nil
}

// Get is a person's connection to an engine.
func (c *Connections) Get(user, engine string) (Connection, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cn, ok := c.conns[id(user, engine)]
	if !ok {
		return Connection{}, false
	}
	out := cn.Connection
	out.Groups = slices.Clone(out.Groups)
	return out, true
}

// Of lists connections, of user and of engine where they are not empty,
// by engine, then person.
func (c *Connections) Of(user, engine string) []Connection {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []Connection
	for _, cn := range c.conns {
		if reserved(cn.User) {
			continue
		}
		if (user == "" || cn.User == user) && (engine == "" || cn.Engine == engine) {
			x := cn.Connection
			x.Groups = slices.Clone(x.Groups)
			out = append(out, x)
		}
	}
	slices.SortFunc(out, func(a, b Connection) int {
		return strings.Compare(a.Engine+"\x00"+a.User, b.Engine+"\x00"+b.User)
	})
	return out
}

// Keys are the IDs of the keys of the connections there are; every other
// key of a connection is one to destroy.
func (c *Connections) Keys() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, 0, len(c.conns))
	for _, cn := range c.conns {
		out = append(out, cn.key)
	}
	return out
}

// Count is the number of people's connections.
func (c *Connections) Count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, cn := range c.conns {
		if !reserved(cn.User) {
			n++
		}
	}
	return n
}

// Decline records that a person ended their connection to engine
// themselves, so it is not made again without asking (ADR-0152).
func (c *Connections) Decline(user, engine string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.declined[id(user, engine)] {
		return nil
	}
	return c.record(record{Kind: declined, At: c.now().UTC(), User: user, Engine: engine})
}

// Declined reports whether the person ended their connection to engine
// themselves and has not connected since.
func (c *Connections) Declined(user, engine string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.declined[id(user, engine)]
}

// ClientKey is this server's signing key for client authentication at
// engines, as SetClientKey kept it, sealed like a refresh token
// (ADR-0152); ok is false before the first.
func (c *Connections) ClientKey() (seed []byte, ok bool, err error) {
	pt, err := c.Refresh(keyUser, keyEngine)
	if errors.Is(err, ErrNotConnected) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	seed, err = base64.RawStdEncoding.DecodeString(pt)
	return seed, err == nil, err
}

// SetClientKey keeps a new signing key, replacing the one before, whose
// key is destroyed.
func (c *Connections) SetClientKey(seed []byte) error {
	_, err := c.Connect(keyUser, keyEngine, Profile{Subject: "client-key", Username: "client-key"}, base64.RawStdEncoding.EncodeToString(seed))
	return err
}

// Compacted answers the records that hold the connections as they are,
// when the journal holds many more than that — a renewal every few hours
// per person adds up —, for the journal to be rewritten with; nil when it
// is not worth it. The tokens stay sealed.
func (c *Connections) Compacted() [][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.records < 2*(len(c.conns)+len(c.declined))+100 {
		return nil
	}
	out := [][]byte{}
	for _, cn := range c.conns {
		p := cn.Profile
		data, err := json.Marshal(record{V: 1, Kind: connected, At: cn.Connected, User: cn.User, Engine: cn.Engine, Key: cn.key, Profile: &p, Sealed: cn.sealed})
		if err != nil {
			return nil
		}
		out = append(out, data)
	}
	for k := range c.declined {
		user, engine, _ := strings.Cut(k, "\x00")
		data, err := json.Marshal(record{V: 1, Kind: declined, User: user, Engine: engine})
		if err != nil {
			return nil
		}
		out = append(out, data)
	}
	slices.SortFunc(out, func(a, b []byte) int { return strings.Compare(string(a), string(b)) })
	return out
}

// Rewritten tells that the journal was rewritten with n records.
func (c *Connections) Rewritten(n int) {
	c.mu.Lock()
	c.records = n
	c.mu.Unlock()
}
