package web

import (
	"encoding/json"
	"io/fs"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// catalogue reads static/assets/locales/<lang>.js, whose default export is
// strict JSON (ADR-0017).
func catalogue(t *testing.T, lang string) map[string]any {
	t.Helper()
	data, err := fs.ReadFile(FS(), "assets/locales/"+lang+".js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	start := strings.Index(src, "export default ")
	end := strings.LastIndex(src, "};")
	if start < 0 || end < start {
		t.Fatalf("%s.js: no `export default {…};`", lang)
	}
	var c map[string]any
	if err := json.Unmarshal([]byte(src[start+len("export default "):end+1]), &c); err != nil {
		t.Fatalf("%s.js is not strict JSON: %v", lang, err)
	}
	return c
}

var placeholderRE = regexp.MustCompile(`\{(\w+)\}`)

func placeholders(v any) []string {
	var texts []string
	switch m := v.(type) {
	case string:
		texts = []string{m}
	case map[string]any:
		for _, form := range m {
			if s, ok := form.(string); ok {
				texts = append(texts, s)
			}
		}
	}
	var out []string
	for _, s := range texts {
		for _, p := range placeholderRE.FindAllStringSubmatch(s, -1) {
			if !slices.Contains(out, p[1]) {
				out = append(out, p[1])
			}
		}
	}
	slices.Sort(out)
	return out
}

var pluralForms = []string{"zero", "one", "two", "few", "many", "other"}

// TestCataloguesAreConsistent: every language has a catalogue, a
// translation has no key the source lacks and uses the same placeholders,
// plural messages have an "other" form. A key the source has and a
// translation lacks is a gap: it is reported, not refused, because the
// UI falls back to English (ADR-0017).
func TestCataloguesAreConsistent(t *testing.T) {
	src := catalogue(t, Languages[0])
	for _, lang := range Languages {
		c := catalogue(t, lang)
		if name, _ := c["$name"].(string); name == "" {
			t.Errorf("%s: no $name", lang)
		}
		var gaps []string
		for key, v := range c {
			want, ok := src[key]
			if !ok {
				t.Errorf("%s: %q is not in the source catalogue", lang, key)
				continue
			}
			if got, exp := placeholders(v), placeholders(want); key != "$name" && !slices.Equal(got, exp) {
				t.Errorf("%s: %q uses placeholders %v, the source %v", lang, key, got, exp)
			}
			if m, ok := v.(map[string]any); ok {
				if _, ok := m["other"]; !ok {
					t.Errorf("%s: plural %q has no \"other\" form", lang, key)
				}
				for form := range m {
					if !slices.Contains(pluralForms, form) {
						t.Errorf("%s: plural %q has an unknown form %q", lang, key, form)
					}
				}
			}
		}
		for key := range src {
			if _, ok := c[key]; !ok {
				gaps = append(gaps, key)
			}
		}
		if len(gaps) > 0 {
			slices.Sort(gaps)
			t.Logf("%s: %d untranslated keys fall back to %s: %v", lang, len(gaps), Languages[0], gaps)
		}
	}
}

var tCallRE = regexp.MustCompile(`\bt\(\s*"([^"]+)"`)

// TestEveryUsedKeyIsInTheSource: a key the UI uses that the source
// catalogue lacks would render as the bare key.
func TestEveryUsedKeyIsInTheSource(t *testing.T) {
	src := catalogue(t, Languages[0])
	used := 0
	err := fs.WalkDir(FS(), "assets", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".js") || strings.HasPrefix(path, "assets/locales/") {
			return err
		}
		data, err := fs.ReadFile(FS(), path)
		if err != nil {
			return err
		}
		for _, m := range tCallRE.FindAllStringSubmatch(string(data), -1) {
			used++
			if _, ok := src[m[1]]; !ok {
				t.Errorf("%s uses %q, which %s.js lacks", path, m[1], Languages[0])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if used == 0 {
		t.Fatal("no t(\"…\") calls found")
	}
}
