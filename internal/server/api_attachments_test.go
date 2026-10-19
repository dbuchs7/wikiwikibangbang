package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
)

// upload sends content as a file of an item.
func (c *client) upload(path, contentType string, content []byte) *httptest.ResponseRecorder {
	c.t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(content))
	req.RemoteAddr = "192.0.2.1:4000"
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	return rec
}

func TestAttachmentsThroughTheAPI(t *testing.T) {
	w := newWorld(t)
	it := w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"Patch","properties":{"priority":"low"}}`, http.StatusCreated)
	key := field(it, "item", "key").(string)
	base := "/api/items/" + key + "/attachments"
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{7}, 100)...)

	rec := w.bert.upload(base+"?name=Flow%20chart.png", "application/octet-stream", png)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body)
	}
	var created struct{ Attachment attachmentJSON }
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	a := created.Attachment
	if a.Name != "Flow chart.png" || a.Type != "image/png" || !a.Inline || a.Size != int64(len(png)) || a.Author != w.ids["bert"] {
		t.Fatalf("attachment %+v", a)
	}
	html := w.bert.upload(base+"?name=page.html", "application/octet-stream", []byte("<html><script>alert(1)</script></html>"))
	if html.Code != http.StatusCreated {
		t.Fatalf("html upload: %d %s", html.Code, html.Body)
	}
	var page struct{ Attachment attachmentJSON }
	_ = json.Unmarshal(html.Body.Bytes(), &page)

	// Refused: a form's content type, a viewer, an outsider, a bad name,
	// a file over the budget.
	if r := w.bert.upload(base+"?name=x", "text/plain", []byte("x")); r.Code != http.StatusUnsupportedMediaType {
		t.Errorf("text/plain upload: %d", r.Code)
	}
	if r := w.vera.upload(base+"?name=x", "application/octet-stream", []byte("x")); r.Code != http.StatusForbidden {
		t.Errorf("viewer upload: %d", r.Code)
	}
	if r := w.carla.upload(base+"?name=x", "application/octet-stream", []byte("x")); r.Code != http.StatusNotFound {
		t.Errorf("outsider upload: %d", r.Code)
	}
	if r := w.bert.upload(base+"?name=a/b", "application/octet-stream", []byte("x")); r.Code != http.StatusBadRequest {
		t.Errorf("bad name: %d", r.Code)
	}
	small := budget.Get()
	small.AttachmentBytes = 10
	budget.Set(small)
	defer budget.Set(budget.Default())
	if r := w.bert.upload(base+"?name=big.bin", "application/octet-stream", make([]byte, 11)); r.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("too large: %d %s", r.Code, r.Body)
	}
	budget.Set(budget.Default())

	list := w.vera.expect("GET", base, "", http.StatusOK)["attachments"].([]any)
	if len(list) != 2 {
		t.Fatalf("list %v", list)
	}
	w.carla.expect("GET", base, "", http.StatusNotFound)

	// An image shows inline; anything else only downloads, sandboxed.
	img := w.vera.do("GET", base+"/"+string(a.ID), "")
	if img.Code != http.StatusOK || !bytes.Equal(img.Body.Bytes(), png) || img.Header().Get("Content-Type") != "image/png" ||
		!strings.HasPrefix(img.Header().Get("Content-Disposition"), "inline") {
		t.Fatalf("image download: %d %v", img.Code, img.Header())
	}
	dl := w.vera.do("GET", base+"/"+string(page.Attachment.ID), "")
	if dl.Header().Get("Content-Type") != "application/octet-stream" || !strings.HasPrefix(dl.Header().Get("Content-Disposition"), "attachment") ||
		dl.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(dl.Header().Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("html download headers %v", dl.Header())
	}
	w.carla.expect("GET", base+"/"+string(a.ID), "", http.StatusNotFound)

	// An image block shows the file; deleting it hides it.
	w.bert.expect("PUT", "/api/items/"+key+"/body", `{"blocks":[{"kind":"image","ref":"`+string(a.ID)+`","text":"The flow"}]}`, http.StatusOK)
	w.vera.expect("DELETE", base+"/"+string(a.ID), "", http.StatusForbidden)
	w.bert.expect("DELETE", base+"/"+string(a.ID), "", http.StatusOK)
	w.vera.expect("GET", base+"/"+string(a.ID), "", http.StatusNotFound)
}
