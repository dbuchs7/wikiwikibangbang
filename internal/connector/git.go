package connector

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
	"github.com/dbuchs7/wikiwikibangbang/internal/gitrepo"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/markdown"
	"github.com/dbuchs7/wikiwikibangbang/internal/site"
)

// maxMarkdown bounds a Markdown file a page is written from (ADR-0135).
const maxMarkdown = 2 << 20

// maxGitFiles bounds how many Markdown files one run reads (ADR-0135).
const maxGitFiles = 2000

// indexNames are the files whose text is their folder's page, the first
// found of them: generators of documentation sites read index.md and
// _index.md, the hosts show README.md.
var indexNames = []string{"index.md", "_index.md", "readme.md"}

// gitReader reads a folder (or a file) of a Git repository: the folder
// and every folder below it that holds Markdown files become pages, each
// Markdown file a page below its folder's (ADR-0135).
type gitReader struct{}

func (gitReader) Read(ctx context.Context, c site.Connector, env Env) (reading, error) {
	var rd reading
	a, err := gitrepo.Parse(c.Git.URL)
	if err != nil {
		return rd, err
	}
	client := gitrepo.Client{HTTP: env.HTTP, Token: env.Token}
	a, commit, err := client.Locate(ctx, a)
	if err != nil {
		return rd, fmt.Errorf("connector: %s: %w", c.Git.URL, err)
	}
	rd.version, rd.seen = commit, settingsKey(c)+"@"+commit
	if !env.Force && env.Seen == rd.seen {
		rd.same = true
		return rd, nil
	}
	files, truncated, err := client.Files(ctx, a, commit)
	if err != nil {
		return rd, fmt.Errorf("connector: %s: %w", c.Git.URL, err)
	}
	if truncated {
		rd.problem("the host listed only part of the folder; name a folder further down")
	}
	slices.SortFunc(files, func(x, y gitrepo.File) int { return strings.Compare(x.Path, y.Path) })
	if len(files) > maxGitFiles {
		rd.problem("the folder holds %d Markdown files; the first %d are read", len(files), maxGitFiles)
		files = files[:maxGitFiles]
	}
	t := gitTree{ctx: ctx, client: client, a: a, commit: commit, words: wordsIn(language(c)), files: files}
	rd.entries = t.entries()
	rd.datasets, rd.apply = len(files), true
	return rd, nil
}

// settingsKey names a connector's settings, so a run after a change of
// them reads the source again though its commit is the same.
func settingsKey(c site.Connector) string {
	data, _ := json.Marshal(c)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:8])
}

// gitTree makes the entries of the Markdown files below a folder.
type gitTree struct {
	ctx    context.Context
	client gitrepo.Client
	a      gitrepo.Address
	commit string
	words  labels
	files  []gitrepo.File
	// links map the addresses of the entries' files and folders to their
	// SourceIDs, for the links between pages.
	links map[string]string
}

// folderID is a folder's SourceID: its path with a slash, "/" for the
// repository's root, so it never names a file.
func folderID(dir string) string {
	if dir == "" || dir == "." {
		return "/"
	}
	return dir + "/"
}

// dirOf is the folder a file is in, "" for the repository's root.
func dirOf(p string) string {
	if d := path.Dir(p); d != "." {
		return d
	}
	return ""
}

func (t *gitTree) entries() []Entry {
	if !t.a.Folder {
		if len(t.files) == 0 {
			return nil
		}
		f := t.files[0]
		t.links = map[string]string{t.a.WebURL(t.a.Ref, f.Path, false): f.Path}
		return []Entry{t.fileEntry(f, "")}
	}
	root := t.a.Path
	// The folders that hold Markdown files, with the folders above them up
	// to the root, and each one's files and index file.
	children := map[string][]string{}
	filesIn := map[string][]gitrepo.File{}
	index := map[string]gitrepo.File{}
	known := map[string]bool{root: true}
	for _, f := range t.files {
		d := dirOf(f.Path)
		filesIn[d] = append(filesIn[d], f)
		for d != root && !known[d] {
			known[d] = true
			up := dirOf(d)
			children[up] = append(children[up], d)
			d = up
		}
	}
	for d, fs := range filesIn {
		for _, name := range indexNames {
			if i := slices.IndexFunc(fs, func(f gitrepo.File) bool { return strings.EqualFold(path.Base(f.Path), name) }); i >= 0 {
				index[d] = fs[i]
				break
			}
		}
	}
	t.links = map[string]string{}
	for d := range known {
		t.links[t.a.WebURL(t.a.Ref, d, true)] = folderID(d)
		if f, ok := index[d]; ok {
			t.links[t.a.WebURL(t.a.Ref, f.Path, false)] = folderID(d)
		}
	}
	for _, f := range t.files {
		if index[dirOf(f.Path)].Path != f.Path {
			t.links[t.a.WebURL(t.a.Ref, f.Path, false)] = f.Path
		}
	}
	var out []Entry
	var walk func(d, parent string)
	walk = func(d, parent string) {
		out = append(out, t.folderEntry(d, parent, index[d]))
		sub := children[d]
		slices.Sort(sub)
		for _, s := range sub {
			walk(s, folderID(d))
		}
		for _, f := range filesIn[d] {
			if f.Path != index[d].Path {
				out = append(out, t.fileEntry(f, folderID(d)))
			}
		}
	}
	walk(root, "")
	return out
}

// folderName is how a folder's page is called without an index file.
func (t *gitTree) folderName(d string) string {
	if d == "" {
		return path.Base(t.a.Repo)
	}
	return path.Base(d)
}

// values are what a Git source gives of a page (site.GitValues).
func (t *gitTree) values(p, addr string) map[string]string {
	return map[string]string{"path": p, "url": addr, "commit": t.commit}
}

func (t *gitTree) folderEntry(d, parent string, index gitrepo.File) Entry {
	addr := t.a.WebURL(t.a.Ref, d, true)
	e := Entry{SourceID: folderID(d), Dataset: folderID(d), Title: t.folderName(d), DatasetURL: addr, Parent: parent, Version: t.commit,
		Values: t.values(d, addr)}
	if index.Path == "" {
		// A folder without an index file lists its pages.
		e.Body = []item.Block{t.heading(addr, t.repoPath(d)), {Kind: item.Children}}
		return e
	}
	e.Blob = index.Blob
	e.Load = func() (Content, error) { return t.read(index.Path, t.folderName(d), addr) }
	return e
}

func (t *gitTree) fileEntry(f gitrepo.File, parent string) Entry {
	addr := t.a.WebURL(t.a.Ref, f.Path, false)
	return Entry{SourceID: f.Path, Dataset: f.Path, Title: strings.TrimSuffix(path.Base(f.Path), path.Ext(f.Path)), DatasetURL: addr,
		Parent: parent, Version: t.commit, Blob: f.Blob, Values: t.values(f.Path, addr),
		Load: func() (Content, error) { return t.read(f.Path, path.Base(f.Path), addr) }}
}

// repoPath names a file or folder with its repository, as the heading of
// its page does.
func (t *gitTree) repoPath(p string) string {
	if p == "" {
		return t.a.Repo
	}
	return t.a.Repo + ": " + p
}

// heading is the panel that says where a page comes from, the source
// linked.
func (t *gitTree) heading(addr, name string) item.Block {
	before, after, _ := strings.Cut(t.words.fromGit, "%s")
	text := before + name + after
	start := utf8.RuneCountInString(before)
	b := item.Block{Kind: item.Panel, Tone: "info", Text: text}
	if item.SafeHref(addr) {
		b.Marks = []item.Mark{{Type: item.MarkLink, Start: start, End: start + utf8.RuneCountInString(name), Href: addr}}
	}
	return b
}

// read reads one Markdown file and converts it: its links resolved
// within the repository at the followed ref, its pictures read at the
// commit (ADR-0135).
func (t *gitTree) read(file, name, addr string) (Content, error) {
	data, err := t.client.Read(t.ctx, t.a, t.commit, file, maxMarkdown)
	if errors.Is(err, gitrepo.ErrTooLarge) {
		return Content{}, leftOut{fmt.Sprintf("%s is larger than %d MB and left out", file, maxMarkdown>>20)}
	}
	if err != nil {
		return Content{}, err
	}
	d := markdown.Convert(data, markdown.Source{Name: name, Link: t.linkFrom(file), Picture: t.pictureFrom(file)})
	c := Content{Title: d.Title, Raw: strings.ToValidUTF8(string(data), "�"), Links: t.links}
	c.Body = append([]item.Block{t.heading(addr, t.repoPath(file))}, d.Blocks...)
	// The pictures of a page are read up to their budget together; the
	// ones beyond it stay links.
	left := budget.Get().MarkdownPictureBytes
	for _, p := range d.Pictures {
		if len(c.Pictures) == maxPictures || left <= 0 {
			break
		}
		if pic, ok := t.picture(p, min(left, budget.Get().AttachmentBytes)); ok {
			pic.Block++
			left -= int64(len(pic.Data))
			c.Pictures = append(c.Pictures, pic)
		}
	}
	return c, nil
}

// picture reads a picture a page shows of at most limit bytes, if it is
// one a page keeps.
func (t *gitTree) picture(p markdown.Picture, limit int64) (Picture, bool) {
	data, err := t.client.Get(t.ctx, t.a, p.Src, limit)
	if err != nil || !slices.Contains(pictureTypes, http.DetectContentType(data)) {
		return Picture{}, false
	}
	return Picture{Block: p.Block, Name: fileName(p.Src), Caption: strings.Join(strings.Fields(p.Alt), " "), Data: data}, true
}

// fileName is a picture's name from its address, fit for an attachment.
func fileName(addr string) string {
	name := "picture"
	if u, err := url.Parse(addr); err == nil {
		if n, err := url.PathUnescape(path.Base(u.EscapedPath())); err == nil && n != "/" && n != "." {
			name = n
		}
	}
	name = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || unicode.IsControl(r) {
			return '_'
		}
		return r
	}, name)
	if name == "" || name == "." || name == "/" || utf8.RuneCountInString(name) > 200 {
		return "picture"
	}
	return name
}

// within resolves a relative target of a file to a path in the
// repository, and reports whether it stays inside it.
func within(file, target string) (string, bool) {
	var p string
	if strings.HasPrefix(target, "/") {
		p = path.Clean(strings.TrimPrefix(target, "/"))
	} else {
		p = path.Join(dirOf(file), target)
	}
	if p == ".." || strings.HasPrefix(p, "../") {
		return "", false
	}
	if p == "." {
		p = ""
	}
	return p, true
}

// linkFrom resolves the links of a file: a relative one to the file or
// folder at the followed ref, an absolute one as it is.
func (t *gitTree) linkFrom(file string) func(string) string {
	return func(target string) string {
		u, err := url.Parse(target)
		if err != nil || target == "" || strings.HasPrefix(target, "#") {
			return ""
		}
		if u.Scheme != "" || u.Host != "" {
			if item.SafeHref(target) {
				return target
			}
			return ""
		}
		p, ok := within(file, u.Path)
		if !ok {
			return ""
		}
		folder := strings.HasSuffix(u.Path, "/") || path.Ext(p) == ""
		addr := t.a.WebURL(t.a.Ref, p, folder)
		if u.Fragment != "" {
			addr += "#" + url.PathEscape(u.Fragment)
		}
		return addr
	}
}

// pictureFrom resolves the pictures of a file: a relative one to the
// file in the repository at the commit read, an absolute one as it is.
func (t *gitTree) pictureFrom(file string) func(string) string {
	return func(src string) string {
		u, err := url.Parse(src)
		if err != nil || src == "" {
			return ""
		}
		if u.Scheme == "https" || u.Scheme == "http" {
			return src
		}
		if u.Scheme != "" || u.Host != "" {
			return ""
		}
		p, ok := within(file, u.Path)
		if !ok || p == "" {
			return ""
		}
		return t.a.RawURL(t.commit, p)
	}
}
