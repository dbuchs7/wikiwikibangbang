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

// TestAlbumListsReadableImages: the album (ADR-0054) lists the images and
// drawings of the items the caller may read, filtered and paged; never
// other files, never a restricted item's to those it excludes.
func TestAlbumListsReadableImages(t *testing.T) {
	w := newWorld(t)
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{7}, 100)...)
	mk := func(title string) (string, string) {
		it := w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"`+title+`","properties":{"priority":"low"}}`, http.StatusCreated)
		return field(it, "item", "key").(string), field(it, "item", "id").(string)
	}
	openKey, _ := mk("Network plan")
	_, secretID := mk("Secret")
	for _, f := range []struct{ key, name string }{{openKey, "rack.png"}, {openKey, "flow" + DrawingSuffix}, {secretID, "secret.png"}} {
		if r := w.bert.upload("/api/items/"+f.key+"/attachments?name="+f.name, "application/octet-stream", png); r.Code != http.StatusCreated {
			t.Fatalf("upload %s: %d", f.name, r.Code)
		}
	}
	if r := w.bert.upload("/api/items/"+openKey+"/attachments?name=notes.txt", "application/octet-stream", []byte("plain text")); r.Code != http.StatusCreated {
		t.Fatalf("upload notes: %d", r.Code)
	}
	w.bert.expect("PUT", "/api/items/"+secretID+"/restriction", `{"read":["`+w.ids["bert"]+`"],"edit":[]}`, http.StatusOK)

	names := func(c *client, query string) []string {
		var out []string
		for _, p := range field(c.expect("GET", "/api/album"+query, "", http.StatusOK), "pictures").([]any) {
			out = append(out, p.(map[string]any)["attachment"].(map[string]any)["name"].(string))
		}
		return out
	}
	if got := names(w.bert, ""); len(got) != 3 {
		t.Errorf("bert sees %v", got)
	}
	if got := names(w.vera, ""); len(got) != 2 || strings.Contains(strings.Join(got, " "), "secret") {
		t.Errorf("vera sees %v", got)
	}
	if got := names(w.carla, ""); len(got) != 0 {
		t.Errorf("an outsider sees %v", got)
	}
	if got := names(w.vera, "?kind=drawing"); len(got) != 1 || got[0] != "flow"+DrawingSuffix {
		t.Errorf("drawings: %v", got)
	}
	if got := names(w.vera, "?kind=image&q=RACK"); len(got) != 1 || got[0] != "rack.png" {
		t.Errorf("search: %v", got)
	}
	if got := names(w.vera, "?q=network"); len(got) != 2 {
		t.Errorf("search by the item's title: %v", got)
	}
	page := w.vera.expect("GET", "/api/album?limit=1", "", http.StatusOK)
	if field(page, "total").(float64) != 2 || field(page, "next").(float64) != 1 {
		t.Errorf("paging: %v", page)
	}
	for _, bad := range []string{"?kind=video", "?limit=0", "?limit=201", "?offset=-1"} {
		w.vera.expect("GET", "/api/album"+bad, "", http.StatusBadRequest)
	}
}

// TestCopiesThroughTheAPI: an upload names its source; the files of an
// item say where copies came from and whether the original was changed
// since, as far as the reader may know (ADR-0055).
func TestCopiesThroughTheAPI(t *testing.T) {
	w := newWorld(t)
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{7}, 100)...)
	mk := func(title string) (string, string) {
		it := w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"`+title+`","properties":{"priority":"low"}}`, http.StatusCreated)
		return field(it, "item", "key").(string), field(it, "item", "id").(string)
	}
	planKey, planID := mk("Plans")
	_, runID := mk("Runbook")
	up := func(itemID, query string, content []byte, status int) map[string]any {
		r := w.bert.upload("/api/items/"+itemID+"/attachments?name=plan.drawing.png"+query, "application/octet-stream", content)
		if r.Code != status {
			t.Fatalf("upload %s: %d %s", query, r.Code, r.Body)
		}
		var out map[string]any
		_ = json.Unmarshal(r.Body.Bytes(), &out)
		return out
	}
	v1 := field(up(planID, "", png, http.StatusCreated), "attachment", "id").(string)
	from := "&sourceItem=" + planID + "&sourceAttachment="
	up(runID, from+v1, append(png, 1), http.StatusBadRequest) // other content is no copy
	c := up(runID, from+v1, png, http.StatusCreated)
	if field(c, "attachment", "origin", "item", "key") != planKey {
		t.Fatalf("copy %v", c)
	}
	origin := func(c *client) map[string]any {
		list := field(c.expect("GET", "/api/items/"+runID+"/attachments", "", http.StatusOK), "attachments").([]any)
		o, _ := list[0].(map[string]any)["origin"].(map[string]any)
		return o
	}
	if o := origin(w.vera); o["readable"] != true || o["newer"] != nil {
		t.Fatalf("a fresh copy: %v", o)
	}
	v2 := field(up(planID, from+v1, append(png, 2), http.StatusCreated), "attachment", "id").(string) // drawn again
	if o := origin(w.vera); o["newer"] == nil || o["newer"].(map[string]any)["id"] != v2 {
		t.Fatalf("a newer original: %v", o)
	}
	w.bert.expect("PUT", "/api/items/"+planID+"/restriction", `{"read":["`+w.ids["bert"]+`"],"edit":[]}`, http.StatusOK)
	if o := origin(w.vera); o["readable"] != false || o["item"] != nil || o["newer"] != nil {
		t.Fatalf("an unreadable source tells: %v", o)
	}
}

// TestAlbumFoldersAndLabelsThroughTheAPI: folders of the album hold
// pictures uploaded into them; pictures carry labels of their own, which
// narrow the album and never count as labels of items (ADR-0090).
func TestAlbumFoldersAndLabelsThroughTheAPI(t *testing.T) {
	w := newWorld(t)
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{7}, 100)...)
	w.vera.expect("POST", "/api/album/folders", `{"space":"PLAT","name":"Logos"}`, http.StatusForbidden)
	logos := field(w.bert.expect("POST", "/api/album/folders", `{"space":"PLAT","name":"Logos"}`, http.StatusCreated), "item").(map[string]any)
	id := logos["id"].(string)
	brand := field(w.bert.expect("POST", "/api/album/folders", `{"parent":"`+id+`","name":"Brand"}`, http.StatusCreated), "item").(map[string]any)
	folders := field(w.vera.expect("GET", "/api/album/folders?space=PLAT", "", http.StatusOK), "folders").([]any)
	if len(folders) != 1 || folders[0].(map[string]any)["name"] != "Logos" {
		t.Fatalf("top folders %v", folders)
	}
	if sub := field(w.vera.expect("GET", "/api/album/folders?parent="+id, "", http.StatusOK), "folders").([]any); len(sub) != 1 || sub[0].(map[string]any)["id"] != brand["id"] {
		t.Fatalf("sub folders %v", sub)
	}

	rec := w.bert.upload("/api/items/"+id+"/attachments?name=logo.png", "application/octet-stream", png)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body)
	}
	var up struct {
		Attachment struct {
			ID string `json:"id"`
		} `json:"attachment"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &up); err != nil || up.Attachment.ID == "" {
		t.Fatalf("upload answer %s", rec.Body)
	}
	task := w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"Patch","properties":{"priority":"low"}}`, http.StatusCreated)
	if r := w.bert.upload("/api/items/"+field(task, "item", "key").(string)+"/attachments?name=other.png", "application/octet-stream", png); r.Code != http.StatusCreated {
		t.Fatalf("upload other: %d", r.Code)
	}

	labels := "/api/items/" + id + "/attachments/" + up.Attachment.ID + "/labels"
	w.vera.expect("PUT", labels, `{"labels":["brand"]}`, http.StatusForbidden)
	w.bert.expect("PUT", labels, `{"labels":["bad label!"]}`, http.StatusBadRequest)
	w.bert.expect("PUT", labels, `{"labels":["Brand","print"]}`, http.StatusOK)

	pictures := func(query string) []map[string]any {
		var out []map[string]any
		for _, p := range field(w.vera.expect("GET", "/api/album"+query, "", http.StatusOK), "pictures").([]any) {
			out = append(out, p.(map[string]any))
		}
		return out
	}
	if got := pictures("?folder=" + id); len(got) != 1 || got[0]["used"] != false || got[0]["editable"] != false {
		t.Fatalf("the folder's pictures %v", got)
	}
	if got := pictures("?label=Brand"); len(got) != 1 || got[0]["attachment"].(map[string]any)["labels"].([]any)[1] != "print" {
		t.Fatalf("by label %v", got)
	}
	if got := pictures("?q=print"); len(got) != 1 {
		t.Fatalf("search by label %v", got)
	}
	if got := len(pictures("")); got != 2 {
		t.Fatalf("the whole album has %d pictures", got)
	}
	counted := field(w.vera.expect("GET", "/api/album/labels?q=pr", "", http.StatusOK), "labels").([]any)
	if len(counted) != 1 || counted[0].(map[string]any)["label"] != "print" {
		t.Fatalf("picture labels %v", counted)
	}
	if items := field(w.vera.expect("GET", "/api/labels", "", http.StatusOK), "labels").([]any); len(items) != 0 {
		t.Fatalf("a picture's labels count as an item's: %v", items)
	}
	if got := field(w.carla.expect("GET", "/api/album/labels", "", http.StatusOK), "labels").([]any); len(got) != 0 {
		t.Fatalf("an outsider sees %v", got)
	}
}
