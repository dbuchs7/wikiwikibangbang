package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/autobackup"
	"github.com/dbuchs7/wikiwikibangbang/internal/backup"
)

func TestAutomaticBackupsThroughTheAPI(t *testing.T) {
	backup.Iterations = 1000
	t.Setenv(autobackup.PassphraseEnv, "a long passphrase")
	w := newWorld(t)
	dir := t.TempDir()

	w.bert.expect("GET", "/api/backups", "", http.StatusForbidden)
	w.bert.expect("PUT", "/api/backups/settings", `{"fullDays":14,"keep":3}`, http.StatusForbidden)
	w.bert.expect("POST", "/api/backups/run", `{"kind":"full"}`, http.StatusForbidden)
	w.bert.expect("POST", "/api/backups/check", `{"destination":"/"}`, http.StatusForbidden)

	got := w.admin.expect("GET", "/api/backups", "", http.StatusOK)
	if field(got, "settings", "fullDays").(float64) != 14 || field(got, "status", "passphrase") != true || field(got, "maxVersions").(float64) != 10 {
		t.Fatalf("defaults %v", got)
	}
	w.admin.expect("POST", "/api/backups/run", `{"kind":"full"}`, http.StatusConflict)
	w.admin.expect("PUT", "/api/backups/settings", `{"enabled":true,"destination":"relative","fullDays":14,"incrementalDays":1,"keep":3}`, http.StatusBadRequest)
	w.admin.expect("PUT", "/api/backups/settings", `{"enabled":true,"destination":"https://u:p@host/dav","fullDays":14,"incrementalDays":1,"keep":3}`, http.StatusBadRequest)
	w.admin.expect("PUT", "/api/backups/settings", `{"enabled":true,"destination":"`+dir+`","fullDays":14,"incrementalDays":1,"keep":11}`, http.StatusBadRequest)
	w.admin.expect("PUT", "/api/backups/settings", `{`, http.StatusBadRequest)
	w.admin.expect("PUT", "/api/backups/settings", `{"enabled":false,"destination":"`+dir+`","fullDays":7,"incrementalDays":1,"keep":2}`, http.StatusOK)

	if c := w.admin.expect("POST", "/api/backups/check", `{"destination":"`+dir+`"}`, http.StatusOK); c["ok"] != true {
		t.Fatalf("check %v", c)
	}
	if c := w.admin.expect("POST", "/api/backups/check", `{"destination":"`+filepath.Join(dir, "missing")+`"}`, http.StatusOK); c["ok"] != false || c["error"] == "" {
		t.Fatalf("check of a missing directory %v", c)
	}
	w.admin.expect("POST", "/api/backups/check", `[`, http.StatusBadRequest)

	w.admin.expect("POST", "/api/backups/run", `{"kind":"weekly"}`, http.StatusBadRequest)
	w.admin.expect("POST", "/api/backups/run", `{`, http.StatusBadRequest)
	w.admin.expect("POST", "/api/backups/run", `{"kind":"full"}`, http.StatusAccepted)
	// The backup runs in the background; the status shows it once done.
	deadline := time.Now().Add(20 * time.Second)
	var history []any
	for time.Now().Before(deadline) {
		got = w.admin.expect("GET", "/api/backups", "", http.StatusOK)
		if history = field(got, "status", "history").([]any); len(history) > 0 && field(got, "status", "running") == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(history) != 1 || history[0].(map[string]any)["error"] != nil {
		t.Fatalf("history %v", history)
	}
	versions := field(got, "status", "versions").([]any)
	if len(versions) != 1 || versions[0].(map[string]any)["complete"] != true {
		t.Fatalf("versions %v", versions)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || !strings.HasSuffix(entries[0].Name(), "-000-full.wwbb") {
		t.Fatalf("stored %v", entries)
	}
	audit := w.admin.expect("GET", "/api/audit", "", http.StatusOK)
	data, _ := json.Marshal(audit)
	if !strings.Contains(string(data), "backup.change") || !strings.Contains(string(data), entries[0].Name()) {
		t.Fatalf("not audited: %s", data)
	}
}
