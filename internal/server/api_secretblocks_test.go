package server

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/dbuchs7/orbithek/internal/item"
)

// zipNames lists the files of a ZIP answer.
func zipNames(t *testing.T, body []byte) []string {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("not a ZIP archive: %v", err)
	}
	var out []string
	for _, f := range z.File {
		out = append(out, f.Name)
	}
	return out
}

// Protected blocks and their pictures through the API (ADR-0160): bert
// writes a page whose first section — a heading, a paragraph that
// mentions a task and a picture — he protects for himself; vera views
// PLAT and may read the page but not the section; admin administers every
// space and so decides, without reading it. Every way the page, its
// relations and its picture leave the server is checked for vera.
func TestProtectedBlocksAndPicturesThroughTheAPI(t *testing.T) {
	w := newWorld(t)
	task := field(w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"Pay Paul","properties":{"priority":"low"}}`, http.StatusCreated), "item", "id").(string)
	it := w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"Budget","properties":{"priority":"low"}}`, http.StatusCreated)
	key := field(it, "item", "key").(string)
	path := "/api/items/" + key
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte("SALARYPLAN"), 20)...)
	rec := w.bert.upload(path+"/attachments?name=salaries.png", "application/octet-stream", png)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body)
	}
	var up struct{ Attachment attachmentJSON }
	_ = json.Unmarshal(rec.Body.Bytes(), &up)
	pic := string(up.Attachment.ID)
	w.bert.expect("PUT", path+"/body", fmt.Sprintf(`{"blocks":[
		{"kind":"heading","level":1,"text":"Salaries"},
		{"kind":"paragraph","text":"Paul Muster earns 9000: Pay Paul","marks":[{"type":"mention","start":24,"end":32,"ref":%q}]},
		{"kind":"image","ref":%q,"text":"The salary plan"},
		{"kind":"heading","level":1,"text":"Office"},
		{"kind":"paragraph","text":"Open to all."}]}`, task, pic), http.StatusOK)

	// Only who reads every protection protects; ranges are checked.
	w.vera.expect("POST", path+"/secrets", `{"fromBlock":0,"toBlock":2}`, http.StatusForbidden)
	w.bert.expect("POST", path+"/secrets", `{"fromBlock":0}`, http.StatusBadRequest)
	w.bert.expect("POST", path+"/secrets", `{"fromBlock":2,"toBlock":9}`, http.StatusBadRequest)
	got := w.bert.expect("POST", path+"/secrets", `{"fromBlock":0,"toBlock":2}`, http.StatusOK)
	sec := field(got, "item", "secrets").([]any)[0].(map[string]any)
	id := sec["id"].(string)
	if sec["blocks"] != true || sec["readable"] != true {
		t.Fatalf("bert sees %v", sec)
	}
	if b := field(got, "item", "body").([]any)[2].(map[string]any); b["secret"] != id || b["ref"] != pic {
		t.Fatalf("bert reads the picture block %v", b)
	}
	// The same file shown in an open block is refused.
	w.bert.expect("PUT", path+"/body", fmt.Sprintf(`{"blocks":[{"kind":"heading","level":1,"text":"Salaries","secret":%q},{"kind":"image","ref":%q}]}`, id, pic), http.StatusBadRequest)
	w.bert.expect("GET", path+"/attachments/"+pic, "", http.StatusOK)

	secrets := []string{"Muster", "Salaries", "salary plan", "salaries.png", pic, "SALARYPLAN"}
	leaks := func(who *client, where, body string) {
		t.Helper()
		for _, s := range secrets {
			if strings.Contains(body, s) {
				t.Errorf("%s gives %q away: %s", where, s, body)
			}
		}
	}
	// The page as vera reads it: one placeholder, what follows in clear.
	read := w.vera.expect("GET", path, "", http.StatusOK)
	body := field(read, "item", "body").([]any)
	first := body[0].(map[string]any)
	if len(body) != 3 || first["text"] != "▒▒▒▒▒▒▒▒" || first["kind"] != "paragraph" || first["secret"] != id || body[1].(map[string]any)["text"] != "Office" {
		t.Fatalf("vera reads %v", body)
	}
	if field(read, "item", "fileCount") != nil || field(read, "item", "readsAllSecrets") != false {
		t.Errorf("vera counts the files or may change the text: %v", read)
	}
	leaks(w.vera, "the page", mustJSON(t, read))
	// The file: not by its route, not listed, not in the album.
	w.vera.expect("GET", path+"/attachments/"+pic, "", http.StatusNotFound)
	w.admin.expect("GET", path+"/attachments/"+pic, "", http.StatusNotFound)
	w.vera.expect("DELETE", path+"/attachments/"+pic, "", http.StatusForbidden)
	for _, p := range []string{path + "/attachments", "/api/album", "/api/album?space=PLAT", "/api/album/labels",
		"/api/spaces/PLAT/starmap", "/api/items/" + task + "/impact?direction=affected&depth=all", "/api/items/" + task + "/relations?direction=incoming",
		path + "/relations", path + "/embeds", path + "/history", path + "/comments",
		"/api/search?q=Muster", "/api/search?q=salary", "/api/search.csv?q=Muster", "/api/items?space=PLAT",
		path + "/export/html", path + "/export/md"} {
		rec := w.vera.do("GET", p, "")
		if rec.Code != http.StatusOK {
			t.Errorf("%s: %d %s", p, rec.Code, rec.Body)
		}
		leaks(w.vera, p, rec.Body.String())
	}
	if strings.Contains(w.vera.do("GET", "/api/items/"+task+"/impact?direction=affected&depth=all", "").Body.String(), key) {
		t.Errorf("the impact of the task reaches the page through the protected mention")
	}
	w.vera.expect("GET", path+"/impact?direction=dependson&depth=1&picture="+pic, "", http.StatusNotFound)
	for _, p := range []string{path + "/export/md.zip", path + "/export/docx"} {
		rec := w.vera.do("GET", p, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d", p, rec.Code)
		}
		for _, n := range zipNames(t, rec.Body.Bytes()) {
			if strings.Contains(n, "salaries") || strings.HasPrefix(n, "word/media/") || strings.HasSuffix(n, ".png") {
				t.Errorf("%s holds %s", p, n)
			}
		}
	}
	// Earlier versions, copies and the live session are for those who
	// read every protection.
	var version string
	for _, e := range field(w.vera.expect("GET", path+"/history", "", http.StatusOK), "events").([]any) {
		if m := e.(map[string]any); m["kind"] == "BodyCommitted" {
			version = fmt.Sprint(int(m["seq"].(float64)))
		}
	}
	w.vera.expect("GET", path+"/versions/"+version, "", http.StatusForbidden)
	w.admin.expect("POST", path+"/copy", `{"space":"PLAT","title":"Copy"}`, http.StatusForbidden)
	// A copy of the picture into another page leaves its protection: not
	// even bert may; vera may not read its source at all.
	other := field(w.vera.expect("GET", "/api/items/"+task, "", http.StatusOK), "item", "key").(string)
	if r := w.bert.upload("/api/items/"+other+"/attachments?name=c.png&sourceItem="+field(it, "item", "id").(string)+"&sourceAttachment="+pic, "application/octet-stream", png); r.Code != http.StatusForbidden {
		t.Errorf("bert copies the protected picture out: %d %s", r.Code, r.Body)
	}
	// MCP reads through the API, as vera.
	for _, c := range []struct{ tool, args string }{
		{"get_item", `{"ref":"` + key + `"}`}, {"get_item_markdown", `{"ref":"` + key + `"}`},
		{"search_items", `{"q":"Muster"}`}, {"get_starmap", `{"space":"PLAT"}`},
		{"get_impact", `{"ref":"` + task + `","direction":"affected","depth":"all"}`},
		{"get_relations", `{"ref":"` + task + `","direction":"incoming"}`}, {"get_embeds", `{"ref":"` + key + `"}`},
	} {
		text, isErr := w.vera.tool(c.tool, c.args)
		if isErr {
			t.Errorf("%s: %s", c.tool, text)
		}
		leaks(w.vera, "MCP "+c.tool, text)
	}
	// The change feed names the item only, never what changed.
	w.vera.expect("PUT", path+"/watch", "", http.StatusOK)
	w.bert.expect("PUT", path+"/title", `{"title":"Budget 2027"}`, http.StatusOK)
	leaks(w.vera, "the change feed", mustJSON(t, w.vera.expect("GET", "/api/changes?after=0", "", http.StatusOK)))

	// A copy of the whole page, by bert who reads it, keeps the
	// protection, its picture's copy included.
	copied := field(w.bert.expect("POST", path+"/copy", `{"space":"PLAT","title":"Budget copy"}`, http.StatusCreated), "item", "key").(string)
	leaks(w.vera, "the copy", mustJSON(t, w.vera.expect("GET", "/api/items/"+copied, "", http.StatusOK)))
	files := w.bert.expect("GET", "/api/items/"+copied+"/attachments", "", http.StatusOK)["attachments"].([]any)
	if len(files) != 1 {
		t.Fatalf("the copy has the files %v", files)
	}
	copyPic := files[0].(map[string]any)["id"].(string)
	w.vera.expect("GET", "/api/items/"+copied+"/attachments/"+copyPic, "", http.StatusNotFound)
	w.bert.expect("GET", "/api/items/"+copied+"/attachments/"+copyPic, "", http.StatusOK)

	// Vera asks, bert grants; she reads the section and gets the file.
	w.vera.expect("POST", path+"/secrets/"+id+"/request", "", http.StatusOK)
	w.bert.expect("POST", path+"/secrets/"+id+"/requests/"+w.ids["vera"], `{"grant":true}`, http.StatusOK)
	read = w.vera.expect("GET", path, "", http.StatusOK)
	if b := field(read, "item", "body").([]any); len(b) != 5 || b[1].(map[string]any)["text"] != "Paul Muster earns 9000: Pay Paul" {
		t.Fatalf("granted, vera reads %v", b)
	}
	w.vera.expect("GET", path+"/attachments/"+pic, "", http.StatusOK)

	// Erased for good: the blocks leave the text, the file the data
	// directory.
	got = w.admin.expect("POST", path+"/secrets/"+id+"/erase", "", http.StatusOK)
	if b := field(got, "item", "body").([]any); len(b) != 2 || field(got, "item", "secrets") != nil {
		t.Fatalf("after erasure: %v", b)
	}
	w.bert.expect("GET", path+"/attachments/"+pic, "", http.StatusNotFound)
	if _, err := w.db.OpenAttachment(item.ID(field(it, "item", "id").(string)), item.ID(pic)); err == nil {
		t.Errorf("the file of an erased protection is still in the data directory")
	}
}
