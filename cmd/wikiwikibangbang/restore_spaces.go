package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dbuchs7/wikiwikibangbang/internal/audit"
	"github.com/dbuchs7/wikiwikibangbang/internal/backup"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	partial "github.com/dbuchs7/wikiwikibangbang/internal/restore"
	"github.com/dbuchs7/wikiwikibangbang/internal/storage"
)

// restoreActor is recorded as the author of everything a restore writes.
const restoreActor = "restore"

// restoreSpaces restores spaces from a backup into a data directory in
// use (ADR-0124): the backup is unpacked beside it, read, and removed
// again; what the data directory still knows is skipped, what it deleted
// for good never comes back, and both are reported.
func restoreSpaces(from fileFlags, at, passFile string, spaces []string, dataDir string, grants grantFlags, dryRun bool, stdout, stderr io.Writer) int {
	if len(from) == 0 || len(spaces) == 0 {
		fmt.Fprintln(stderr, "import: -from backup needs -backup and -spaces")
		return 2
	}
	pass, err := backupPassphrase(passFile)
	if err != nil {
		fmt.Fprintln(stderr, "import:", err)
		return 1
	}
	if pass == "" {
		fmt.Fprintln(stderr, "import: set WWBB_BACKUP_PASSPHRASE or -passphrase-file")
		return 2
	}
	chain, _, err := backupChain(from, at)
	if err != nil {
		fmt.Fprintln(stderr, "import:", err)
		return 1
	}
	db, _, err := storage.Open(dataDir)
	if err != nil {
		fmt.Fprintln(stderr, "import: cannot open the data directory (is the server still running?):", err)
		return 1
	}
	defer db.Close()
	roles, err := grants.resolve(db.Accounts)
	if err != nil {
		fmt.Fprintln(stderr, "import:", err)
		return 1
	}
	var given []item.Grant
	for _, g := range roles {
		given = append(given, item.Grant{Principal: g.Principal, Role: g.Role})
	}
	users := map[string]bool{}
	for _, u := range db.Accounts.Users() {
		users[string(u.ID)] = true
	}

	// The backup is unpacked next to the data directory, on the same disk
	// and with the same protection, and removed when done.
	abs, err := filepath.Abs(dataDir)
	if err != nil {
		fmt.Fprintln(stderr, "import:", err)
		return 1
	}
	stage, err := os.MkdirTemp(filepath.Dir(abs), ".wikiwikibangbang-restore-*")
	if err != nil {
		fmt.Fprintln(stderr, "import:", err)
		return 1
	}
	defer os.RemoveAll(stage)
	m, err := backup.RestoreChain(chain, pass, stage)
	if err != nil {
		fmt.Fprintln(stderr, "import:", err)
		return 1
	}
	src, _, err := storage.Open(stage)
	if err != nil {
		fmt.Fprintln(stderr, "import: reading the backup:", err)
		return 1
	}
	defer src.Close()
	var srcStore *item.Store
	src.Read(func(s *item.Store) { srcStore = s })
	source := partial.Source{Store: srcStore, Instance: m.ID,
		Open: func(id, a item.ID) (io.ReadCloser, error) { return src.OpenAttachment(id, a) }}
	if source.Instance == "" {
		source.Instance = m.Created.UTC().Format("20060102T150405Z")
	}

	var target partial.Target = dataTarget{db}
	if dryRun {
		db.Read(func(s *item.Store) { target, err = partial.Dry(s) })
		if err != nil {
			fmt.Fprintln(stderr, "import:", err)
			return 1
		}
	}
	rep, err := partial.Run(target, source, partial.Options{Spaces: spaces, Actor: restoreActor, Users: users, Grants: given})
	if err != nil {
		fmt.Fprintln(stderr, "import:", err)
		return 1
	}
	if !dryRun {
		// The restore is on record, with what it left out because it was
		// deleted for good: the proof that an erasure held (ADR-0124).
		db.Audit.Record(audit.Entry{Event: audit.PartialRestore, Actor: storage.SystemActor, Detail: map[string]string{
			"spaces": strings.Join(spaces, ","), "backup": source.Instance, "created": m.Created.UTC().Format("2006-01-02T15:04:05Z"),
			"restored": strconv.Itoa(len(rep.Restored)), "skipped": strconv.Itoa(len(rep.Skipped)), "erased": strconv.Itoa(len(rep.Erased())),
		}})
	}
	printRestore(stdout, rep, m, dryRun)
	return 0
}

// dataTarget restores into an open data directory.
type dataTarget struct{ db *storage.DB }

func (t dataTarget) Write(fn func(*item.Store) error) error { return t.db.Write(fn) }

func (t dataTarget) CopyFile(id item.ID, content io.Reader, size int64) (item.Attachment, error) {
	return t.db.StoreAttachment(id, content, size)
}

func (t dataTarget) Discard(id, attachment item.ID) { t.db.DiscardAttachment(id, attachment) }

func printRestore(w io.Writer, rep partial.Report, m backup.Manifest, dryRun bool) {
	what := "restored"
	if dryRun {
		what = "dry run, nothing written"
	}
	fmt.Fprintf(w, "%s from the backup of %s: %d items restored, %d skipped (%d deleted for good); %d spaces created, %d roles granted\n",
		what, m.Created.UTC().Format("2006-01-02T15:04:05Z"), len(rep.Restored), len(rep.Skipped), len(rep.Erased()), len(rep.Spaces), rep.Granted)
	if len(rep.Spaces) > 0 {
		fmt.Fprintf(w, "spaces created: %s\n", strings.Join(rep.Spaces, ", "))
	}
	if len(rep.Restored) > 0 {
		fmt.Fprintln(w, "restored:")
		for _, o := range rep.Restored {
			line := fmt.Sprintf("  %s %s", o.Key, o.Title)
			if o.Reason != "" {
				line += " (" + o.Reason + ")"
			}
			fmt.Fprintln(w, line)
		}
	}
	if len(rep.Skipped) > 0 {
		fmt.Fprintln(w, "skipped:")
		for _, o := range rep.Skipped {
			// An item deleted for good is named by key and ID only.
			name := o.Key + " " + o.Title
			if o.Title == "" {
				name = fmt.Sprintf("%s (%s)", o.Key, o.ID)
			}
			fmt.Fprintf(w, "  %s: %s\n", name, o.Reason)
		}
	}
	if len(rep.Notes) > 0 {
		fmt.Fprintf(w, "%d notes:\n", len(rep.Notes))
		for _, n := range rep.Notes {
			fmt.Fprintf(w, "  %s\n", n)
		}
	}
}
