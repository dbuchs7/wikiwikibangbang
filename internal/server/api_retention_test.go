package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

// TestAlbumRetentionThroughTheAPI: administrators set the album's default
// rule and preview and run the clean-up; whoever may edit a picture
// previews, sets its own rule, which takes precedence, and cleans it up;
// the current version and versions a text shows stay (ADR-0104).
func TestAlbumRetentionThroughTheAPI(t *testing.T) {
	w := newWorld(t)
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{7}, 100)...)
	mk := func(title string) string {
		it := w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"`+title+`","properties":{"priority":"low"}}`, http.StatusCreated)
		return field(it, "item", "id").(string)
	}
	up := func(itemID, query string, n byte) string {
		r := w.bert.upload("/api/items/"+itemID+"/attachments?name=plan"+DrawingSuffix+query, "application/octet-stream", append(png, n))
		if r.Code != http.StatusCreated {
			t.Fatalf("upload: %d %s", r.Code, r.Body)
		}
		var out map[string]any
		_ = json.Unmarshal(r.Body.Bytes(), &out)
		return field(out, "attachment", "id").(string)
	}
	drawn := func(itemID string, times int) []string {
		var ids []string
		for i := 0; i < times; i++ {
			q := ""
			if i > 0 {
				q = "&sourceItem=" + itemID + "&sourceAttachment=" + ids[i-1]
			}
			ids = append(ids, up(itemID, q, byte(i)))
		}
		return ids
	}
	plans, other := mk("Plans"), mk("Other")
	v := drawn(plans, 4)
	o := drawn(other, 3)
	ids := func(list any) (out []string) {
		for _, a := range list.([]any) {
			out = append(out, a.(map[string]any)["id"].(string))
		}
		return out
	}

	// The default keeps everything, and nothing runs on its own.
	if got := field(w.vera.expect("GET", "/api/album/retention", "", http.StatusOK), "settings").(map[string]any); got["automatic"] != false || got["keep"].(float64) != 0 {
		t.Fatalf("default %v", got)
	}
	w.bert.expect("PUT", "/api/album/retention", `{"automatic":true,"keep":2}`, http.StatusForbidden)
	w.admin.expect("PUT", "/api/album/retention", `{"keep":-1}`, http.StatusBadRequest)
	w.admin.expect("PUT", "/api/album/retention", `{"automatic":true,"keep":2}`, http.StatusOK)

	// A picture's plan follows the default until it has its own rule.
	base := "/api/items/" + plans + "/attachments/" + v[3] + "/retention"
	p := w.vera.expect("GET", base, "", http.StatusOK)
	if p["own"] != false || strings.Join(ids(p["remove"]), ",") != v[1]+","+v[0] {
		t.Fatalf("plan by the default: %v", p)
	}
	if p := w.vera.expect("GET", base+"?keep=3", "", http.StatusOK); strings.Join(ids(p["remove"]), ",") != v[0] {
		t.Errorf("a rule tried: %v", p)
	}
	w.vera.expect("GET", base+"?keep=x", "", http.StatusBadRequest)
	w.vera.expect("PUT", base, `{"keep":3}`, http.StatusForbidden)
	if p := w.bert.expect("PUT", base, `{"keep":3}`, http.StatusOK); p["own"] != true || strings.Join(ids(p["remove"]), ",") != v[0] {
		t.Fatalf("own rule: %v", p)
	}
	album := field(w.vera.expect("GET", "/api/album?q=Plans", "", http.StatusOK), "pictures").([]any)
	if r, _ := album[0].(map[string]any)["retention"].(map[string]any); r == nil || r["keep"].(float64) != 3 {
		t.Errorf("the album tells the own rule: %v", album)
	}

	// The administrators' preview counts both pictures, each by its rule.
	pre := w.admin.expect("GET", "/api/album/retention/preview", "", http.StatusOK)
	if pre["stacks"].(float64) != 2 || pre["versions"].(float64) != 2 {
		t.Fatalf("preview %v", pre)
	}
	if pre := w.admin.expect("GET", "/api/album/retention/preview?keep=1", "", http.StatusOK); pre["versions"].(float64) != 3 {
		t.Errorf("preview with another default %v", pre)
	}
	w.bert.expect("GET", "/api/album/retention/preview", "", http.StatusForbidden)

	// Cleaning one picture up uses its own rule, or values given this once.
	w.vera.expect("POST", base+"/clean", "", http.StatusForbidden)
	if c := w.bert.expect("POST", base+"/clean", "", http.StatusOK); strings.Join(ids(c["removed"]), ",") != v[0] || len(c["remove"].([]any)) != 0 || len(c["versions"].([]any)) != 3 {
		t.Fatalf("cleaned %v", c)
	}
	w.bert.expect("PUT", "/api/items/"+plans+"/body", `{"blocks":[{"kind":"image","ref":"`+v[1]+`"}]}`, http.StatusOK)
	if c := w.bert.expect("POST", base+"/clean", `{"keep":1}`, http.StatusOK); strings.Join(ids(c["removed"]), ",") != v[2] || strings.Join(toStrings(c["used"]), ",") != v[1] {
		t.Fatalf("the text keeps its version: %v", c)
	}
	w.bert.expect("PUT", base, `{"default":true}`, http.StatusOK)

	// The administrators' run cleans the other picture by the default.
	run := w.admin.expect("POST", "/api/album/retention/run", "", http.StatusOK)
	if run["stacks"].(float64) != 1 || run["versions"].(float64) != 1 {
		t.Fatalf("run %v", run)
	}
	left := field(w.vera.expect("GET", "/api/items/"+other+"/attachments", "", http.StatusOK), "attachments").([]any)
	if len(left) != 2 || strings.Contains(jsonOf(left), o[0]) {
		t.Errorf("left %v", left)
	}
	if done, err := CleanAlbum(w.db, item.RetentionActor, time.Now()); err != nil || len(done) != 0 {
		t.Errorf("a second run: %v %v", done, err)
	}
	if a := jsonOf(w.admin.expect("GET", "/api/audit", "", http.StatusOK)); !strings.Contains(a, "album.retention.change") || !strings.Contains(a, `"album.clean"`) {
		t.Errorf("audit %s", a)
	}
}
