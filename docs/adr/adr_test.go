package adr

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

var (
	fileNameRE    = regexp.MustCompile(`^(\d{4})-[a-z0-9]+(?:[a-z0-9.-]*[a-z0-9])?\.md$`)
	headingRE     = regexp.MustCompile(`^# ADR-(\d{4}): (\S.*)$`)
	frontMatterRE = regexp.MustCompile(`^- \*\*([A-Za-z ]+):\*\* ?(.*)$`)
	supersededRE  = regexp.MustCompile(`^Superseded by ADR-(\d{4})$`)
	indexRowRE    = regexp.MustCompile(`^\| \[(\d{4})\]\(([^)]+)\) \| (.+?) \| (.+?) \| (.+?) \|$`)
	monthRE       = regexp.MustCompile(`^\d{4}-\d{2}$`)
)

var validImplementations = map[string]bool{
	"Not started": true,
	"Partial":     true,
	"Landed":      true,
	"Superseded":  true,
}

// questionMaxAge is how long an open question may go unchecked. A record
// that rests on an unanswered question must be looked at again at least
// once a year, so the gap cannot quietly become settled truth.
const questionMaxAge = 12 * 30 * 24 * time.Hour

// record is one parsed ADR file.
type record struct {
	file        string
	number      string
	title       string
	frontMatter map[string]string
}

func loadRecords(t *testing.T) map[string]record {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading ADR directory: %v", err)
	}
	records := make(map[string]record)
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".md") || name == "README.md" || name == "template.md" {
			continue
		}
		m := fileNameRE.FindStringSubmatch(name)
		if m == nil {
			t.Errorf("%s: file name must match NNNN-lowercase-slug.md", name)
			continue
		}
		r, err := parseRecord(name)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if r.number != m[1] {
			t.Errorf("%s: heading says ADR-%s, file name says %s", name, r.number, m[1])
		}
		if prev, ok := records[m[1]]; ok {
			t.Errorf("ADR number %s used twice: %s and %s", m[1], prev.file, name)
			continue
		}
		records[m[1]] = r
	}
	return records
}

func parseRecord(name string) (record, error) {
	f, err := os.Open(name)
	if err != nil {
		return record{}, err
	}
	defer f.Close()

	r := record{file: name, frontMatter: make(map[string]string)}
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return record{}, fmt.Errorf("empty file")
	}
	h := headingRE.FindStringSubmatch(sc.Text())
	if h == nil {
		return record{}, fmt.Errorf("first line must be '# ADR-NNNN: Title', got %q", sc.Text())
	}
	r.number, r.title = h[1], h[2]

	// The front matter is the bullet list directly below the heading.
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			if len(r.frontMatter) > 0 {
				break
			}
			continue
		}
		m := frontMatterRE.FindStringSubmatch(line)
		if m == nil {
			break
		}
		if _, dup := r.frontMatter[m[1]]; dup {
			return record{}, fmt.Errorf("front matter key %q appears twice", m[1])
		}
		r.frontMatter[m[1]] = strings.TrimSpace(m[2])
	}
	return r, sc.Err()
}

func TestRecordsAreNumberedWithoutGaps(t *testing.T) {
	records := loadRecords(t)
	if len(records) == 0 {
		t.Fatal("no ADRs found")
	}
	numbers := make([]string, 0, len(records))
	for n := range records {
		numbers = append(numbers, n)
	}
	sort.Strings(numbers)
	for i, n := range numbers {
		if want := fmt.Sprintf("%04d", i+1); n != want {
			t.Errorf("ADR numbers must run 0001, 0002, … without gaps: expected %s, found %s", want, n)
			break
		}
	}
}

func TestFrontMatter(t *testing.T) {
	records := loadRecords(t)
	now := time.Now()
	for _, r := range records {
		fm := r.frontMatter
		for _, key := range []string{"Status", "Implementation", "Date", "Deciders"} {
			if fm[key] == "" {
				t.Errorf("%s: front matter needs a non-empty %q line", r.file, key)
			}
		}

		status := fm["Status"]
		switch status {
		case "Proposed", "Accepted", "Deprecated", "":
		default:
			m := supersededRE.FindStringSubmatch(status)
			if m == nil {
				t.Errorf("%s: Status %q is not one of Proposed, Accepted, Deprecated, Superseded by ADR-NNNN", r.file, status)
			} else if _, ok := records[m[1]]; !ok {
				t.Errorf("%s: superseded by ADR-%s, which does not exist", r.file, m[1])
			}
		}

		impl := fm["Implementation"]
		if impl != "" && !validImplementations[impl] {
			t.Errorf("%s: Implementation %q is not one of Not started, Partial, Landed, Superseded", r.file, impl)
		}
		if impl == "Landed" && status != "Accepted" {
			t.Errorf("%s: Implementation Landed requires Status Accepted, got %q", r.file, status)
		}

		if d := fm["Date"]; d != "" {
			if _, err := time.Parse("2006-01-02", d); err != nil {
				t.Errorf("%s: Date %q is not YYYY-MM-DD", r.file, d)
			}
		}

		q, hasQuestion := fm["Open question"]
		checked, hasChecked := fm["Question checked"]
		switch {
		case hasQuestion && q == "":
			t.Errorf("%s: Open question is empty; delete the line when nothing is open", r.file)
		case hasQuestion && !hasChecked:
			t.Errorf("%s: an Open question needs a Question checked month (YYYY-MM)", r.file)
		case !hasQuestion && hasChecked:
			t.Errorf("%s: Question checked without an Open question", r.file)
		case hasChecked:
			if !monthRE.MatchString(checked) {
				t.Errorf("%s: Question checked %q is not YYYY-MM", r.file, checked)
				break
			}
			when, err := time.Parse("2006-01", checked)
			if err != nil {
				t.Errorf("%s: Question checked %q: %v", r.file, checked, err)
				break
			}
			if now.Sub(when) > questionMaxAge {
				t.Errorf("%s: the open question was last checked %s; look at it again and update Question checked", r.file, checked)
			}
		}
	}
}

func TestIndexMatchesRecords(t *testing.T) {
	records := loadRecords(t)
	f, err := os.Open("README.md")
	if err != nil {
		t.Fatalf("reading index: %v", err)
	}
	defer f.Close()

	seen := make(map[string]bool)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		m := indexRowRE.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		number, link, title, status, impl := m[1], m[2], m[3], m[4], m[5]
		if seen[number] {
			t.Errorf("README.md: ADR-%s listed twice", number)
			continue
		}
		seen[number] = true
		r, ok := records[number]
		if !ok {
			t.Errorf("README.md: row for ADR-%s, but no such record exists", number)
			continue
		}
		if filepath.Clean(link) != r.file {
			t.Errorf("README.md: ADR-%s links to %q, want %q", number, link, r.file)
		}
		if title != r.title {
			t.Errorf("README.md: ADR-%s title %q, record says %q", number, title, r.title)
		}
		if status != r.frontMatter["Status"] {
			t.Errorf("README.md: ADR-%s status %q, record says %q", number, status, r.frontMatter["Status"])
		}
		if impl != r.frontMatter["Implementation"] {
			t.Errorf("README.md: ADR-%s implementation %q, record says %q", number, impl, r.frontMatter["Implementation"])
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("reading index: %v", err)
	}
	for n, r := range records {
		if !seen[n] {
			t.Errorf("README.md: no index row for %s", r.file)
		}
	}
}
