package server

import (
	"bytes"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/backup"
	"github.com/dbuchs7/wikiwikibangbang/internal/storage"
)

func TestBackupThroughTheAPI(t *testing.T) {
	backup.Iterations = 1000
	w := newWorld(t)
	w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"Back me up","properties":{"priority":"low"}}`, http.StatusCreated)

	w.bert.expect("POST", "/api/backup", `{"passphrase":"a long passphrase"}`, http.StatusForbidden)
	w.admin.expect("POST", "/api/backup", `{"passphrase":"short"}`, http.StatusBadRequest)
	rec := w.admin.do("POST", "/api/backup", `{"passphrase":"a long passphrase"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, `attachment; filename="wikiwikibangbang-`) {
		t.Fatalf("content disposition %q", cd)
	}
	dir := filepath.Join(t.TempDir(), "restored")
	if _, err := backup.Restore(bytes.NewReader(rec.Body.Bytes()), "a long passphrase", dir); err != nil {
		t.Fatal(err)
	}
	db, _, err := storage.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if it, err := db.Store.ByKey("PLAT-1"); err != nil || it.Title != "Back me up" {
		t.Fatalf("restored item %+v, %v", it, err)
	}
	audit := w.admin.expect("GET", "/api/audit", "", http.StatusOK)
	if !strings.Contains(field(audit, "entries").([]any)[0].(map[string]any)["event"].(string), "backup.create") {
		t.Fatalf("not audited: %v", audit["entries"])
	}
}
