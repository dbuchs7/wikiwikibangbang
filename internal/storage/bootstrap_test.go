package storage

import (
	"strings"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/auth"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

func openFast(t *testing.T, dir string) *DB {
	t.Helper()
	db, _, err := OpenWith(dir, Options{Auth: []auth.Option{auth.WithIterations(1000)}})
	noErr(t, err)
	return db
}

func isAdmin(db *DB, id auth.UserID) bool {
	admin := false
	db.Read(func(s *item.Store) { admin = s.HasSystemRole(string(id), item.RoleAdmin) })
	return admin
}

func TestEnsureAdmin(t *testing.T) {
	dir := t.TempDir()
	db := openFast(t, dir)
	b, err := db.EnsureAdmin("admin", "")
	noErr(t, err)
	if !b.Created || len(b.Password) < 20 || !isAdmin(db, b.User.ID) {
		t.Fatalf("first start: %+v", b)
	}
	if _, _, err := db.Accounts.Login("admin", b.Password, "x"); err != nil {
		t.Fatalf("generated password does not log in: %v", err)
	}
	again, err := db.EnsureAdmin("admin", "")
	noErr(t, err)
	if again.Created || again.Password != "" {
		t.Fatalf("second call: %+v", again)
	}
	noErr(t, db.Close())

	// Accounts, the role and the audit trail survive a restart.
	db = openFast(t, dir)
	defer db.Close()
	if got := db.Audit.List(0, 10); len(got) != 2 || got[1].Event != "user.create" || got[0].Event != "system-role.grant" || got[0].Actor != SystemActor {
		t.Fatalf("audit after restart: %+v", got)
	}
	if b2, err := db.EnsureAdmin("admin", ""); err != nil || b2.Created || !isAdmin(db, b.User.ID) {
		t.Fatalf("after restart: %+v, %v", b2, err)
	}
}

func TestEnsureAdminWithAGivenPassword(t *testing.T) {
	db := openFast(t, t.TempDir())
	defer db.Close()
	b, err := db.EnsureAdmin("root", "a given password!")
	noErr(t, err)
	if b.Password != "" {
		t.Fatal("a given password was echoed back")
	}
	if _, err := db.EnsureAdmin("x", "short"); err != nil {
		t.Fatal("an existing administrator must make the call a no-op")
	}
	db2 := openFast(t, t.TempDir())
	defer db2.Close()
	if _, err := db2.EnsureAdmin("root", "short"); err == nil {
		t.Fatal("a too short password was accepted")
	}
}

// TestEnsureAdminHealsAMissingRole covers a crash between creating the
// account and granting its role.
func TestEnsureAdminHealsAMissingRole(t *testing.T) {
	db := openFast(t, t.TempDir())
	defer db.Close()
	u, err := db.Accounts.CreateUser("admin", "a given password!")
	noErr(t, err)
	b, err := db.EnsureAdmin("admin", "")
	noErr(t, err)
	if b.Created || b.User.ID != u.ID || !isAdmin(db, u.ID) {
		t.Fatalf("healing: %+v", b)
	}
	other := openFast(t, t.TempDir())
	defer other.Close()
	_, err = other.Accounts.CreateUser("someone", "a given password!")
	noErr(t, err)
	if _, err := other.EnsureAdmin("admin", ""); err == nil || !strings.Contains(err.Error(), "no administrator") {
		t.Fatalf("no admin and no matching account: %v", err)
	}
}
