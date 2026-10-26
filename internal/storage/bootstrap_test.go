package storage

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/audit"
	"github.com/dbuchs7/wikiwikibangbang/internal/auth"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/wal"
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

// TestAuditEntriesExpireAfterTenYears covers ADR-0029's retention: at start
// and when ExpireAudit runs, entries older than ten years leave the journal.
func TestAuditEntriesExpireAfterTenYears(t *testing.T) {
	dir := t.TempDir()
	db := openFast(t, dir)
	old := time.Now().AddDate(-11, 0, 0).UTC()
	for i, at := range []time.Time{old, old.Add(time.Hour)} {
		data, err := json.Marshal(audit.Entry{Seq: i + 1, At: at, Event: audit.Login})
		noErr(t, err)
		noErr(t, db.audit.Append(data))
	}
	noErr(t, db.Close())

	db = openFast(t, dir)
	if got := db.Audit.List(0, 10); len(got) != 1 || got[0].Event != audit.Expired || got[0].Detail["removed"] != "2" {
		t.Fatalf("after the start: %+v", got)
	}
	db.Audit.Record(audit.Entry{Event: audit.Logout})
	if got := db.Audit.List(0, 1); got[0].Seq != 3 {
		t.Fatalf("numbering restarted at %d, want 3", got[0].Seq)
	}
	// Ten years later, the marker and the new entry expire too.
	n, err := db.ExpireAudit(time.Now().AddDate(10, 0, 1))
	noErr(t, err)
	if got := db.Audit.List(0, 10); n != 2 || len(got) != 1 || got[0].Event != audit.Expired || got[0].Seq != 3 {
		t.Fatalf("ExpireAudit dropped %d, memory %+v", n, got)
	}
	n, err = db.ExpireAudit(time.Now())
	noErr(t, err)
	if n != 0 {
		t.Fatalf("nothing to expire, dropped %d", n)
	}
	db.Audit.Record(audit.Entry{Event: audit.Login})
	noErr(t, db.Close())
	db = openFast(t, dir)
	defer db.Close()
	// The expiry ran with a clock ten years ahead, so its marker removed
	// entries the real clock calls young: the start says the chain is
	// broken there (ADR-0123).
	if got := db.Audit.List(0, 10); len(got) != 3 || got[1].Seq != 4 || got[2].Event != audit.Expired ||
		got[0].Event != audit.ChainBroken || got[0].Detail["reason"] != audit.BrokenTooYoung || got[0].Detail["at"] != "3" {
		t.Fatalf("after a rewrite and a restart: %+v", got)
	}
}

// TestAuditListingReachesTheJournal covers ADR-0122: entries older than
// the in-memory window are listed from audit.wal.
func TestAuditListingReachesTheJournal(t *testing.T) {
	dir := t.TempDir()
	at := time.Now().UTC().Add(-time.Hour)
	var recs [][]byte
	for i := range audit.Retained + 2 {
		e := audit.Entry{Seq: i + 1, At: at, Event: audit.LoginFailed}
		if i == 0 {
			e.Event = audit.DesignChanged
		}
		data, err := json.Marshal(e)
		noErr(t, err)
		recs = append(recs, data)
	}
	noErr(t, os.MkdirAll(dir, 0o700))
	noErr(t, wal.Rewrite(dir, AuditFile, recs))
	db := openFast(t, dir)
	defer db.Close()
	got, err := db.Audit.ListAll(0, 10, func(e audit.Entry) bool { return e.Event == audit.DesignChanged })
	if err != nil || len(got) != 1 || got[0].Seq != 1 {
		t.Fatalf("%+v, %v", got, err)
	}
}
