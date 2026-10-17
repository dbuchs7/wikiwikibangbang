package storage

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"github.com/dbuchs7/wikiwikibangbang/internal/auth"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

// SystemActor is the actor of changes the server makes on its own, such as
// the first administrator's role.
const SystemActor = "system"

// Bootstrap is what EnsureAdmin did.
type Bootstrap struct {
	Created bool // an account was created
	User    auth.User
	// Password is set when it was generated; it is shown once and never
	// stored in clear.
	Password string
}

// EnsureAdmin makes sure the installation has an administrator (ADR-0013):
// on the first start it creates the account username with password, or with
// a generated password when password is empty, and grants it the admin
// system role. If the accounts exist but no one is an administrator (a crash
// between the two steps), the role goes to username's account.
func (db *DB) EnsureAdmin(username, password string) (Bootstrap, error) {
	var b Bootstrap
	held := false
	db.Read(func(s *item.Store) { held = s.SystemRoleHeld(item.RoleAdmin) })
	if held {
		return b, nil
	}
	var found bool
	for _, u := range db.Accounts.Users() {
		if u.Username == username {
			b.User, found = u, true
		}
	}
	if !found {
		if db.Accounts.HasUsers() {
			return b, fmt.Errorf("storage: no administrator exists and there is no account %q to make one", username)
		}
		if password == "" {
			raw := make([]byte, 18)
			if _, err := rand.Read(raw); err != nil {
				return b, err
			}
			password = base64.RawURLEncoding.EncodeToString(raw)
			b.Password = password
		}
		u, err := db.Accounts.CreateUser(username, password)
		if err != nil {
			return Bootstrap{}, fmt.Errorf("storage: creating the first administrator: %w", err)
		}
		b.User, b.Created = u, true
	}
	err := db.Write(func(s *item.Store) error {
		return s.GrantSystemRole(SystemActor, string(b.User.ID), item.RoleAdmin)
	})
	if err != nil {
		return Bootstrap{}, fmt.Errorf("storage: granting the administrator role: %w", err)
	}
	return b, nil
}
