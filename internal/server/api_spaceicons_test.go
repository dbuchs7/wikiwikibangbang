package server

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
)

func TestSpaceIcons(t *testing.T) {
	w := newWorld(t)
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{2}, 40)...)
	// Bert edits PLAT but does not administer it; Carla has no role.
	if r := w.bert.put("/api/spaces/PLAT/icon", "application/octet-stream", png); r.Code != http.StatusForbidden {
		t.Fatalf("an editor sets the icon: %d", r.Code)
	}
	if r := w.admin.put("/api/spaces/PLAT/icon", "application/octet-stream", []byte("plain text")); r.Code != http.StatusBadRequest {
		t.Fatalf("text as icon: %d", r.Code)
	}
	if r := w.admin.put("/api/spaces/NOPE/icon", "application/octet-stream", png); r.Code != http.StatusForbidden {
		t.Fatalf("an icon for a space that does not exist: %d", r.Code)
	}
	if r := w.admin.put("/api/spaces/PLAT/icon", "application/octet-stream", png); r.Code != http.StatusOK {
		t.Fatalf("set: %d %s", r.Code, r.Body)
	}
	spaces := w.bert.expect("GET", "/api/spaces", "", http.StatusOK)["spaces"].([]any)
	if icon, _ := spaces[0].(map[string]any)["icon"].(string); icon == "" {
		t.Fatalf("the listing names the icon: %v", spaces)
	}
	got := w.bert.do("GET", "/api/spaces/PLAT/icon", "")
	if got.Code != http.StatusOK || got.Header().Get("Content-Type") != "image/png" || !strings.Contains(got.Header().Get("Content-Security-Policy"), "sandbox") || !bytes.Equal(got.Body.Bytes(), png) {
		t.Fatalf("icon %d %v", got.Code, got.Header())
	}
	w.carla.expect("GET", "/api/spaces/PLAT/icon", "", http.StatusNotFound)
	w.bert.expect("DELETE", "/api/spaces/PLAT/icon", "", http.StatusForbidden)
	w.admin.expect("DELETE", "/api/spaces/PLAT/icon", "", http.StatusNoContent)
	w.bert.expect("GET", "/api/spaces/PLAT/icon", "", http.StatusNotFound)
}

func TestSpaceIconUploadsAreChecked(t *testing.T) {
	w := newWorld(t)
	if r := w.admin.put("/api/spaces/PLAT/icon", "image/png", []byte("x")); r.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("content type: %d", r.Code)
	}
	big := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{1}, 3<<20)...)
	if r := w.admin.put("/api/spaces/PLAT/icon", "application/octet-stream", big); r.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("too large: %d", r.Code)
	}
	w.admin.expect("DELETE", "/api/spaces/PLAT/icon", "", http.StatusNoContent) // none set: nothing to do
}
