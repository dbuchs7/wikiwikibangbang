package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func (c *client) put(path, contentType string, content []byte) *httptest.ResponseRecorder {
	c.t.Helper()
	req := httptest.NewRequest(http.MethodPut, path, bytes.NewReader(content))
	req.RemoteAddr = "192.0.2.1:4000"
	req.Header.Set("Content-Type", contentType)
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	return rec
}

func TestProfilePictures(t *testing.T) {
	w := newWorld(t)
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{7}, 100)...)
	if me := w.bert.expect("GET", "/api/me", "", http.StatusOK); me["avatar"] != "" {
		t.Fatalf("a picture before any: %v", me["avatar"])
	}
	w.vera.expect("GET", "/api/users/"+w.ids["bert"]+"/avatar", "", http.StatusNotFound)

	if r := w.bert.put("/api/me/avatar", "application/json", png); r.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("a JSON body: %d", r.Code)
	}
	if r := w.bert.put("/api/me/avatar", "application/octet-stream", []byte("<svg onload=alert(1)>")); r.Code != http.StatusBadRequest {
		t.Fatalf("not an image: %d", r.Code)
	}
	if r := w.bert.put("/api/me/avatar", "application/octet-stream", append(png, make([]byte, 300<<10)...)); r.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("too large: %d", r.Code)
	}
	if r := w.bert.put("/api/me/avatar", "application/octet-stream", png); r.Code != http.StatusOK {
		t.Fatalf("set: %d %s", r.Code, r.Body)
	}
	tag := w.bert.expect("GET", "/api/me", "", http.StatusOK)["avatar"].(string)
	if tag == "" {
		t.Fatal("/api/me names no picture")
	}

	names := w.vera.expect("GET", "/api/users/names?id="+w.ids["bert"]+"&id="+w.ids["vera"], "", http.StatusOK)
	if avatars := names["avatars"].(map[string]any); avatars[w.ids["bert"]] != tag || len(avatars) != 1 {
		t.Fatalf("names name the pictures %v", avatars)
	}

	got := w.vera.do("GET", "/api/users/"+w.ids["bert"]+"/avatar", "")
	if got.Code != http.StatusOK || !bytes.Equal(got.Body.Bytes(), png) || got.Header().Get("Content-Type") != "image/png" ||
		got.Header().Get("X-Content-Type-Options") != "nosniff" || got.Header().Get("Content-Security-Policy") == "" {
		t.Fatalf("download %d %v", got.Code, got.Header())
	}

	w.bert.expect("DELETE", "/api/me/avatar", "", http.StatusNoContent)
	w.vera.expect("GET", "/api/users/"+w.ids["bert"]+"/avatar", "", http.StatusNotFound)
}
