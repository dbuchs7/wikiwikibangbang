package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
	"github.com/dbuchs7/wikiwikibangbang/internal/gitrepo"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/site"
)

// png is the smallest picture a page keeps.
var png = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x02\x00\x00\x00\x90wS\xde")

// forge is a Gitea instance holding one repository, o/r, whose branch
// main moves with every change of its files.
type forge struct {
	mu      sync.Mutex
	files   map[string]string
	history map[string]map[string]string // the files at each commit
	commit  string
	n       int
	reads   []string // the files read
	trees   int      // the listings asked for
	commits int      // the commits asked for
}

func newForge() *forge {
	f := &forge{files: map[string]string{
		"README.md":     "# Outside the folder",
		"docs/index.md": "---\ntitle: Handbook\n---\nWelcome. See [the guide](guide/) and [A](a.md#top).",
		"docs/a.md": "# Page A\n\nRead [B](guide/b.md), [the readme](../README.md) and [itself](a.md).\n\n![Diagram](img/p.png)\n\n" +
			"![Badge](BADGE)",
		"docs/guide/b.md":   "# Page B\n\nBack to [A](../a.md) and [the handbook](../index.md).",
		"docs/img/p.png":    string(png),
		"docs/notes.txt":    "not Markdown",
		"docs/big.md":       strings.Repeat("x", maxMarkdown+1),
		"docs/empty/x.json": "{}",
	}}
	f.bump()
	return f
}

// bump makes a new commit.
func (f *forge) bump() {
	f.n++
	f.commit = fmt.Sprintf("%040d", f.n)
	if f.history == nil {
		f.history = map[string]map[string]string{}
	}
	f.history[f.commit] = maps.Clone(f.files)
}

// at is the files at a ref: main, or a commit.
func (f *forge) at(ref string) map[string]string {
	if ref == "main" {
		return f.files
	}
	return f.history[ref]
}

func (f *forge) set(p, content string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if content == "" {
		delete(f.files, p)
	} else {
		f.files[p] = content
	}
	f.bump()
}

func (f *forge) counts() (int, int, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	reads := f.reads
	f.reads = nil
	t, c := f.trees, f.commits
	f.trees, f.commits = 0, 0
	return t, c, reads
}

func (f *forge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	const api = "/api/v1/repos/o/r"
	p := r.URL.Path
	switch {
	case p == api+"/commits":
		f.commits++
		switch sha := r.URL.Query().Get("sha"); {
		case sha == "main":
			fmt.Fprintf(w, `[{"sha":%q}]`, f.commit)
		case f.history[sha] != nil:
			fmt.Fprintf(w, `[{"sha":%q}]`, sha)
		default:
			fmt.Fprint(w, "[]")
		}
	case p == api+"/git/trees/"+f.commit:
		f.trees++
		var tree []map[string]string
		for _, name := range slices.Sorted(maps.Keys(f.files)) {
			tree = append(tree, map[string]string{"path": name, "type": "blob", "sha": gitrepo.BlobID([]byte(served(f.files[name], r)), f.commit)})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"tree": tree, "total_count": len(tree)})
	case strings.HasPrefix(p, api+"/raw/") && f.at(r.URL.Query().Get("ref")) != nil:
		name := strings.TrimPrefix(p, api+"/raw/")
		f.reads = append(f.reads, name)
		content, ok := f.at(r.URL.Query().Get("ref"))[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, served(content, r))
	case strings.HasPrefix(p, "/o/r/raw/commit/"):
		commit, name, _ := strings.Cut(strings.TrimPrefix(p, "/o/r/raw/commit/"), "/")
		content, ok := f.at(commit)[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, content)
	case p == "/badge.svg":
		w.Header().Set("Content-Type", "image/svg+xml")
		fmt.Fprint(w, `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	default:
		http.NotFound(w, r)
	}
}

// served is a file as the forge serves it: a picture on another site
// named by the forge's own address.
func served(content string, r *http.Request) string {
	return strings.ReplaceAll(content, "BADGE", "http://"+r.Host+"/badge.svg")
}

func gitSetup(t *testing.T, path string) (*fixture, *forge, string) {
	t.Helper()
	f := setup(t)
	src := newForge()
	srv := httptest.NewServer(src)
	t.Cleanup(srv.Close)
	var c site.Connector
	for _, tpl := range site.ConnectorTemplates() {
		if tpl.Kind == site.KindGit {
			c = tpl
		}
	}
	c.ID, c.Name, c.Enabled, c.Space = "docs", "Docs", true, "API"
	c.Git.URL = srv.URL + "/o/r/src/branch/main/" + path
	if _, err := f.db.Site.SetConnector("admin", c); err != nil {
		t.Fatal(err)
	}
	return f, src, srv.URL
}

// pages are the connector's pages by their path in the repository.
func (f *fixture) pages(t *testing.T) map[string]item.Item {
	t.Helper()
	out := map[string]item.Item{}
	f.db.Read(func(s *item.Store) {
		for _, it := range s.Find(item.Query{Space: "API", Type: PageType}) {
			if it.Origin != nil && it.Origin.System == "docs" {
				out[strings.TrimPrefix(it.Origin.ExternalID, "entry/")] = it
			}
		}
	})
	return out
}

func marks(b item.Block) map[string]item.Mark {
	out := map[string]item.Mark{}
	runes := []rune(b.Text)
	for _, m := range b.Marks {
		out[string(runes[m.Start:m.End])] = m
	}
	return out
}

// A folder becomes a tree of pages: folders with Markdown files and the
// files, the folder's index file as its text; links between the files
// become mentions, pictures attachments (ADR-0135).
func TestAFolderBecomesAPageTree(t *testing.T) {
	f, src, base := gitSetup(t, "docs")
	ctx := context.Background()
	dry, err := f.r.Run(ctx, "docs", true, "admin")
	if err != nil || dry.New != 4 || len(f.pages(t)) != 0 {
		t.Fatalf("dry run %+v %v", dry, err)
	}
	res, err := f.r.Run(ctx, "docs", false, "")
	if err != nil || res.New != 4 || res.Datasets != 4 || res.Version != src.commit || len(res.Problems) != 1 || !strings.Contains(res.Problems[0], "big.md is larger than 2 MB") {
		t.Fatalf("%+v %v", res, err)
	}
	pages := f.pages(t)
	docs, guide, a, b := pages["docs/"], pages["docs/guide/"], pages["docs/a.md"], pages["docs/guide/b.md"]
	if docs.Title != "Handbook" || guide.Title != "guide" || a.Title != "Page A" || b.Title != "Page B" {
		t.Fatalf("titles %q %q %q %q", docs.Title, guide.Title, a.Title, b.Title)
	}
	if docs.Parent != "" || a.Parent != docs.ID || guide.Parent != docs.ID || b.Parent != guide.ID {
		t.Errorf("the tree: %s %s %s %s", docs.Parent, a.Parent, guide.Parent, b.Parent)
	}
	if a.Origin.URL != base+"/o/r/src/branch/main/docs/a.md" || a.Origin.Version != src.commit || a.Origin.Raw != strings.ReplaceAll(src.files["docs/a.md"], "BADGE", base+"/badge.svg") {
		t.Errorf("origin %+v", a.Origin)
	}
	if len(guide.Body.Blocks) != 2 || guide.Body.Blocks[1].Kind != item.Children {
		t.Errorf("a folder without an index lists its pages: %+v", guide.Body.Blocks)
	}
	// a.md: the panel, the paragraph with its links, the picture, the badge
	// as a link.
	head := a.Body.Blocks[0]
	if head.Kind != item.Panel || !strings.Contains(head.Text, "o/r: docs/a.md") || marks(head)["o/r: docs/a.md"].Href != a.Origin.URL {
		t.Errorf("the heading %+v", head)
	}
	links := marks(a.Body.Blocks[1])
	if m := links["B"]; m.Type != item.MarkMention || m.Ref != b.ID || !slices.Contains(a.Body.Blocks[1].Mentions, b.ID) {
		t.Errorf("a link to a page made later is a mention: %+v", m)
	}
	if m := links["the readme"]; m.Type != item.MarkLink || m.Href != base+"/o/r/src/branch/main/README.md" {
		t.Errorf("a link outside the folder: %+v", m)
	}
	if m := links["itself"]; m.Type != item.MarkLink {
		t.Errorf("a link to the page itself stays a link: %+v", m)
	}
	pic := a.Body.Blocks[2]
	if pic.Kind != item.Image || pic.Text != "Diagram" || len(a.Attachments) != 1 || a.Attachments[0].Name != "p.png" || pic.Ref != a.Attachments[0].ID {
		t.Errorf("the picture %+v %+v", pic, a.Attachments)
	}
	if badge := a.Body.Blocks[3]; badge.Kind != item.Paragraph || badge.Text != "Badge" || marks(badge)["Badge"].Href == "" {
		t.Errorf("an SVG stays a link: %+v", badge)
	}
	bl := marks(b.Body.Blocks[1])
	if bl["A"].Ref != a.ID || bl["the handbook"].Ref != docs.ID {
		t.Errorf("links back: %+v", bl)
	}
	if dl := marks(docs.Body.Blocks[1]); dl["the guide"].Ref != guide.ID || dl["A"].Ref != a.ID {
		t.Errorf("the index's links: %+v", dl)
	}

	// The schedule finds the same commit and reads nothing more.
	src.counts()
	same, err := f.r.Run(ctx, "docs", false, "")
	if trees, _, reads := src.counts(); err != nil || !same.Same || trees != 0 || len(reads) != 0 {
		t.Fatalf("an unchanged source %+v %v: %d listings, %v", same, err, trees, reads)
	}
	// A person's run reads the listing; unchanged files are not read.
	forced, err := f.r.Run(ctx, "docs", false, "admin")
	if trees, _, reads := src.counts(); err != nil || forced.Same || forced.Unchanged != 4 || trees != 1 || !slices.Equal(reads, []string{"docs/big.md"}) {
		t.Fatalf("a run by hand %+v %v: %d listings, %v", forced, err, trees, reads)
	}

	// Text edited here stays until the source changes.
	edited := append(slices.Clone(a.Body.Blocks), item.Block{Kind: item.Paragraph, Text: "Added here."})
	if err := f.db.Write(func(s *item.Store) error { return s.CommitBody("anna", a.ID, item.Doc{Blocks: edited}) }); err != nil {
		t.Fatal(err)
	}
	src.set("docs/guide/b.md", "# Page B\n\nNew text.")
	changed, err := f.r.Run(ctx, "docs", false, "")
	_, _, reads := src.counts()
	slices.Sort(reads)
	if err != nil || changed.Changed != 1 || !slices.Equal(reads, []string{"docs/big.md", "docs/guide/b.md"}) {
		t.Fatalf("a changed file %+v %v %v", changed, err, reads)
	}
	pages = f.pages(t)
	if got := pages["docs/a.md"].Body.Blocks; len(got) != len(edited) {
		t.Errorf("the edit was replaced though the source did not change")
	}
	if pages["docs/guide/b.md"].Body.Blocks[1].Text != "New text." || pages["docs/guide/b.md"].Origin.Version != src.commit {
		t.Errorf("the changed page %+v", pages["docs/guide/b.md"])
	}
	// A changed source replaces the edit.
	src.set("docs/a.md", "# Page A\n\nShorter.")
	if _, err := f.r.Run(ctx, "docs", false, ""); err != nil {
		t.Fatal(err)
	}
	if got := f.pages(t)["docs/a.md"].Body.Blocks; len(got) != 2 || got[1].Text != "Shorter." {
		t.Errorf("the source's change %+v", got)
	}
	// A file that is gone is withdrawn, and so is its folder that holds
	// no Markdown file any more; their pages stay.
	src.set("docs/guide/b.md", "")
	gone, err := f.r.Run(ctx, "docs", false, "")
	if err != nil || gone.Withdrawn != 2 || !slices.Contains(f.pages(t)["docs/guide/b.md"].Labels, WithdrawnLabel) {
		t.Fatalf("withdrawn %+v %v", gone, err)
	}
	// It comes back, and is written again.
	src.set("docs/guide/b.md", "# Page B\n\nBack.")
	back, err := f.r.Run(ctx, "docs", false, "")
	if b := f.pages(t)["docs/guide/b.md"]; err != nil || back.Changed != 2 || slices.Contains(b.Labels, WithdrawnLabel) || b.Body.Blocks[0].Kind != item.Panel || b.Body.Blocks[0].Tone != "info" {
		t.Fatalf("back %+v %v %+v", back, err, b.Body.Blocks)
	}
}

// A file's address mirrors that file alone, at the top of the space.
func TestAFileIsOnePage(t *testing.T) {
	f, _, base := gitSetup(t, "")
	c, _ := f.db.Site.Connector("docs")
	c.Git.URL = base + "/o/r/src/branch/main/docs/guide/b.md"
	if _, err := f.db.Site.SetConnector("admin", c); err != nil {
		t.Fatal(err)
	}
	res, err := f.r.Run(context.Background(), "docs", false, "")
	if err != nil || res.New != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	b := f.pages(t)["docs/guide/b.md"]
	if b.Title != "Page B" || b.Parent != "" {
		t.Errorf("%+v", b)
	}
	// The link to a file outside what is mirrored stays a link.
	if m := marks(b.Body.Blocks[1])["A"]; m.Type != item.MarkLink {
		t.Errorf("%+v", m)
	}
}

// The repository's root mirrors every Markdown file, its README as the
// root's text.
func TestTheRootOfARepository(t *testing.T) {
	f, src, _ := gitSetup(t, "")
	src.set("docs/big.md", "")
	res, err := f.r.Run(context.Background(), "docs", false, "")
	if err != nil || res.New != 5 || len(res.Problems) != 0 {
		t.Fatalf("%+v %v", res, err)
	}
	pages := f.pages(t)
	if root := pages["/"]; root.Title != "Outside the folder" || root.Parent != "" || pages["docs/"].Parent != root.ID {
		t.Errorf("the root %+v", root)
	}
}

func TestGitSourceErrors(t *testing.T) {
	f, src, base := gitSetup(t, "docs")
	c, _ := f.db.Site.Connector("docs")
	c.Git.URL = base + "/o/r/src/branch/gone/docs"
	if _, err := f.db.Site.SetConnector("admin", c); err != nil {
		t.Fatal(err)
	}
	if res, err := f.r.Run(context.Background(), "docs", false, ""); err == nil || !strings.Contains(res.Error, "not found") {
		t.Errorf("an unknown branch: %+v %v", res, err)
	}
	// A server in the own network is not reached unless allowed.
	f.r.getenv = func(string) string { return "" }
	c.Git.URL = base + "/o/r/src/branch/main/docs"
	if _, err := f.db.Site.SetConnector("admin", c); err != nil {
		t.Fatal(err)
	}
	if _, err := f.r.Run(context.Background(), "docs", false, ""); err == nil || !strings.Contains(err.Error(), "not public") {
		t.Errorf("a private address: %v", err)
	}
	_ = src
	if fileName("https://x.example/") != "picture" || fileName("https://x.example/a%2Fb.png") != "a_b.png" || fileName("::") != "picture" {
		t.Error("file names")
	}
	if _, ok := within("docs/a.md", "../../x.md"); ok {
		t.Error("a link above the repository")
	}
	if p, ok := within("docs/a.md", "/README.md"); !ok || p != "README.md" {
		t.Error("a link from the root")
	}
}

// The pictures of a page are read up to their budget together; the ones
// beyond it stay links (ADR-0135).
func TestPicturesKeepToTheirBudget(t *testing.T) {
	b := budget.Default()
	b.MarkdownPictureBytes = int64(len(png)) - 1
	budget.Set(b)
	defer budget.Set(budget.Default())
	f, _, _ := gitSetup(t, "docs")
	if _, err := f.r.Run(context.Background(), "docs", false, ""); err != nil {
		t.Fatal(err)
	}
	a := f.pages(t)["docs/a.md"]
	if len(a.Attachments) != 0 || a.Body.Blocks[2].Kind != item.Paragraph || marks(a.Body.Blocks[2])["Diagram"].Href == "" {
		t.Errorf("a picture beyond the budget: %+v", a.Body.Blocks[2])
	}
}
