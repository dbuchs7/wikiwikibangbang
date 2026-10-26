// Package gitrepo reads Markdown files from Git repositories through the
// public APIs of their hosts (ADR-0131): GitHub and GitHub Enterprise
// Server, GitLab, Bitbucket Cloud and Data Center, and Gitea and Forgejo.
// An address is what the host shows in the browser for a file or a
// folder; from it a client finds the commit a branch or tag points to, the
// files below a folder, and a file at a commit.
package gitrepo

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// The kinds of host.
const (
	GitHub          = "github"
	GitLab          = "gitlab"
	Bitbucket       = "bitbucket"
	BitbucketServer = "bitbucket-server"
	Gitea           = "gitea"
)

// ErrAddress is an address that names no file or folder of a repository
// on a host this package reads.
var ErrAddress = errors.New("gitrepo: not the address of a file or folder in a repository")

// ErrNotFound is a repository, branch, tag, commit or file the host does
// not know, or does not show without a credential.
var ErrNotFound = errors.New("gitrepo: not found")

// maxListing bounds an answer of a host's API, so a host that misbehaves
// cannot make the server hold an endless answer.
const maxListing = 32 << 20

// Address is a file or folder of a repository at a version.
type Address struct {
	// Host is the kind of host, e.g. GitHub.
	Host string
	// Base is the instance's web address, e.g. https://github.com, or
	// https://example.org/git for an instance below a path.
	Base string
	// Repo names the repository as its host does: "owner/name", a GitLab
	// project's full path, or a Bitbucket Data Center project's key and
	// the repository's slug, "KEY/slug".
	Repo string
	// Ref is the branch, tag or commit; empty is the default branch.
	Ref string
	// RefKind is how a Gitea or Forgejo address names its ref: branch, tag
	// or commit. Other hosts do not tell.
	RefKind string
	// Path is the file or folder within the repository, without a leading
	// slash; empty is the repository's root.
	Path string
	// Folder is set when the address names a folder.
	Folder bool
	// rest holds the ref and the path as one list of segments when the
	// address does not tell where the ref ends, as a branch name may hold
	// a slash; Locate finds out by asking the host.
	rest []string
	// shape is "file" or "folder" when the address tells which it names;
	// otherwise a Markdown file's name names a file.
	shape string
}

// folderFor reports whether the address names a folder when its path is p.
func (a Address) folderFor(p string) bool {
	switch a.shape {
	case "file":
		return false
	case "folder":
		return true
	}
	return !Markdown(p)
}

// Markdown reports whether a file is Markdown by its name.
func Markdown(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".md", ".markdown":
		return true
	}
	return false
}

// Commit reports whether a ref names a commit by its full hash.
func Commit(ref string) bool { return commitRE.MatchString(ref) }

var commitRE = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)

// Parse reads an address as a host shows a file or folder in the browser,
// e.g. https://github.com/owner/repo/tree/main/docs. Known instances are
// recognised by name; others by the shape of their paths.
func Parse(raw string) (Address, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
		return Address{}, ErrAddress
	}
	segs := segments(u.EscapedPath())
	if segs == nil {
		return Address{}, ErrAddress
	}
	base := u.Scheme + "://" + u.Host
	host := strings.ToLower(u.Hostname())
	var a Address
	var ok bool
	switch {
	case host == "github.com" || host == "www.github.com":
		a, ok = parseGitHub("https://github.com", segs)
	case host == "raw.githubusercontent.com":
		a, ok = parseGitHubRaw(segs)
	case host == "gitlab.com":
		a, ok = parseGitLab(base, segs)
	case host == "bitbucket.org":
		a, ok = parseBitbucket(segs)
	case host == "codeberg.org":
		a, ok = parseGitea(base, segs)
	default:
		a, ok = parseByShape(base, segs, u.Query())
	}
	if !ok {
		return Address{}, ErrAddress
	}
	a.Path = strings.Trim(a.Path, "/")
	if a.rest == nil {
		a.Folder = a.folderFor(a.Path)
	}
	return a, nil
}

// segments splits an escaped path into its unescaped segments, without
// empty ones; nil if a segment cannot be unescaped or is a dot segment.
func segments(escaped string) []string {
	out := []string{}
	for s := range strings.SplitSeq(escaped, "/") {
		if s == "" {
			continue
		}
		v, err := url.PathUnescape(s)
		if err != nil || v == "." || v == ".." || strings.Contains(v, "/") {
			return nil
		}
		out = append(out, v)
	}
	return out
}

// parseByShape recognises a self-hosted instance by its paths.
func parseByShape(base string, segs []string, q url.Values) (Address, bool) {
	for i, s := range segs {
		switch {
		// A self-managed GitLab is taken to be installed at the root of
		// its host, as the project's path cannot be told from a prefix.
		case s == "-" && i+1 < len(segs) && (segs[i+1] == "blob" || segs[i+1] == "tree" || segs[i+1] == "raw"):
			return parseGitLab(base, segs)
		case (s == "projects" || s == "users") && i+2 < len(segs) && segs[i+2] == "repos":
			return parseBitbucketServer(base+prefix(segs[:i]), segs[i:], q)
		case (s == "src" || s == "raw") && i >= 2 && i+1 < len(segs) && (segs[i+1] == "branch" || segs[i+1] == "tag" || segs[i+1] == "commit"):
			return parseGitea(base+prefix(segs[:i-2]), segs[i-2:])
		}
	}
	// GitHub Enterprise Server: owner/repo/blob|tree/..., possibly below
	// a path.
	for i := 2; i < len(segs); i++ {
		if segs[i] == "blob" || segs[i] == "tree" {
			return parseGitHub(base+prefix(segs[:i-2]), segs[i-2:])
		}
	}
	return Address{}, false
}

// prefix is the path below which an instance is installed.
func prefix(segs []string) string {
	if len(segs) == 0 {
		return ""
	}
	return "/" + escapeJoin(segs)
}

func escapeJoin(segs []string) string {
	esc := make([]string, len(segs))
	for i, s := range segs {
		esc[i] = url.PathEscape(s)
	}
	return strings.Join(esc, "/")
}

// File is a file below a folder, with its Git blob ID where the host's
// listing names it, so an unchanged file need not be read.
type File struct {
	Path string
	Blob string
}

// Client reads from hosts. Token, if set, is sent as the host expects a
// credential; a file in a text is read without one (ADR-0131).
type Client struct {
	HTTP  *http.Client
	Token string
}

func (c Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

// Locate finds the ref of an address that does not tell where it ends,
// and the commit the ref points to now: the default branch's for an
// address without a ref, the ref itself for a commit.
func (c Client) Locate(ctx context.Context, a Address) (Address, string, error) {
	h := hostOf(a)
	if a.rest != nil {
		var lastErr error
		for k := 1; k <= len(a.rest) && k <= 5; k++ {
			try := a
			try.Ref, try.Path, try.rest = strings.Join(a.rest[:k], "/"), strings.Join(a.rest[k:], "/"), nil
			try.Folder = a.folderFor(try.Path)
			commit, err := h.commit(ctx, c, try, try.Ref)
			if err == nil {
				return try, commit, nil
			}
			if !errors.Is(err, ErrNotFound) {
				return a, "", err
			}
			lastErr = err
		}
		return a, "", lastErr
	}
	if a.Ref == "" {
		ref, err := h.defaultRef(ctx, c, a)
		if err != nil {
			return a, "", err
		}
		a.Ref, a.RefKind = ref, "branch"
	}
	commit, err := h.commit(ctx, c, a, a.Ref)
	return a, commit, err
}

// At is the address of a file of the same repository at a ref, e.g. a
// file followed at its branch.
func (a Address) At(ref, file string) Address {
	a.Ref, a.Path, a.rest, a.shape = ref, strings.Trim(file, "/"), nil, "file"
	a.Folder = false
	return a
}

// Files lists the Markdown files at or below the address's path at a
// commit, with their paths in the repository; a file's address lists
// itself.
func (c Client) Files(ctx context.Context, a Address, commit string) ([]File, bool, error) {
	if !a.Folder {
		return []File{{Path: a.Path}}, false, nil
	}
	files, truncated, err := hostOf(a).list(ctx, c, a, commit)
	if err != nil {
		return nil, false, err
	}
	var out []File
	for _, f := range files {
		if Markdown(f.Path) && within(f.Path, a.Path) {
			out = append(out, f)
		}
	}
	return out, truncated, nil
}

// within reports whether p is at or below the folder dir.
func within(p, dir string) bool {
	return dir == "" || p == dir || strings.HasPrefix(p, dir+"/")
}

// ErrTooLarge is a file larger than a read allows.
var ErrTooLarge = errors.New("gitrepo: the file is too large")

// Read reads a file of the repository at a commit or a ref, at most
// limit bytes.
func (c Client) Read(ctx context.Context, a Address, commit, file string, limit int64) ([]byte, error) {
	h := hostOf(a)
	req, err := h.read(a, commit, file)
	if err != nil {
		return nil, err
	}
	return c.get(ctx, a, req, limit)
}

// Get reads any address, at most limit bytes, e.g. a picture on another
// site; the token is sent only to the repository's own instance.
func (c Client) Get(ctx context.Context, a Address, addr string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, addr, nil)
	if err != nil {
		return nil, err
	}
	if !sameSite(a, req.URL) {
		return Client{HTTP: c.HTTP}.do(req, limit)
	}
	return c.get(ctx, a, req, limit)
}

// sameSite reports whether u is on the repository's instance, or on the
// host that serves its raw files.
func sameSite(a Address, u *url.URL) bool {
	b, err := url.Parse(a.Base)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Host)
	return host == strings.ToLower(b.Host) || (a.Host == GitHub && b.Host == "github.com" && host == "raw.githubusercontent.com") ||
		(a.Host == Bitbucket && host == "api.bitbucket.org")
}

func (c Client) get(ctx context.Context, a Address, req *http.Request, limit int64) ([]byte, error) {
	req = req.WithContext(ctx)
	if c.Token != "" {
		k, v := hostOf(a).auth(c.Token)
		req.Header.Set(k, v)
	}
	return c.do(req, limit)
}

func (c Client) do(req *http.Request, limit int64) ([]byte, error) {
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "wikiwikibangbang")
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusUnprocessableEntity:
		return nil, fmt.Errorf("%w: %s", ErrNotFound, req.URL.Redacted())
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("gitrepo: %s answered %s", req.URL.Redacted(), resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%w: %s holds more than %d bytes", ErrTooLarge, req.URL.Redacted(), limit)
	}
	return data, nil
}

// getJSON reads an answer of a host's API into v. A header named in
// headers is set on the request; next, if not nil, receives the answer's
// headers.
func (c Client) getJSON(ctx context.Context, a Address, addr string, v any, headers map[string]string, next *http.Header) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, addr, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	for k, val := range headers {
		req.Header.Set(k, val)
	}
	if c.Token != "" {
		k, val := hostOf(a).auth(c.Token)
		req.Header.Set(k, val)
	}
	req.Header.Set("User-Agent", "wikiwikibangbang")
	resp, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusUnprocessableEntity:
		return fmt.Errorf("%w: %s", ErrNotFound, req.URL.Redacted())
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("gitrepo: %s answered %s", req.URL.Redacted(), resp.Status)
	}
	if next != nil {
		*next = resp.Header
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxListing+1))
	if err != nil {
		return err
	}
	if len(data) > maxListing {
		return fmt.Errorf("gitrepo: the answer of %s is larger than %d MB", req.URL.Redacted(), maxListing>>20)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("gitrepo: the answer of %s is not what the host's API answers: %w", req.URL.Redacted(), err)
	}
	return nil
}

// WebURL is the address of a file or folder of the repository at a ref,
// as the host shows it in the browser.
func (a Address) WebURL(ref, file string, folder bool) string {
	return hostOf(a).web(a, ref, strings.Trim(file, "/"), folder)
}

// RawURL is the address a file of the repository is read from at a
// commit without the API, e.g. a picture a page shows.
func (a Address) RawURL(commit, file string) string {
	return hostOf(a).raw(a, commit, strings.Trim(file, "/"))
}

// RepoURL is the repository's address in the browser.
func (a Address) RepoURL() string {
	if a.Host == BitbucketServer {
		key, slug, _ := strings.Cut(a.Repo, "/")
		return a.Base + "/" + repoKind(key) + "/" + url.PathEscape(strings.TrimPrefix(key, "~")) + "/repos/" + url.PathEscape(slug)
	}
	return a.Base + "/" + escapeJoin(strings.Split(a.Repo, "/"))
}

// String is the address of the file or folder at its ref.
func (a Address) String() string { return a.WebURL(a.Ref, a.Path, a.Folder) }

// host is what a kind of host answers to the three questions, and how it
// shows files.
type host interface {
	defaultRef(ctx context.Context, c Client, a Address) (string, error)
	commit(ctx context.Context, c Client, a Address, ref string) (string, error)
	list(ctx context.Context, c Client, a Address, commit string) ([]File, bool, error)
	read(a Address, commit, file string) (*http.Request, error)
	web(a Address, ref, file string, folder bool) string
	raw(a Address, commit, file string) string
	auth(token string) (string, string)
}

func hostOf(a Address) host {
	switch a.Host {
	case GitLab:
		return gitlab{}
	case Bitbucket:
		return bitbucket{}
	case BitbucketServer:
		return bitbucketServer{}
	case Gitea:
		return gitea{}
	}
	return github{}
}

// pathEscape escapes each segment of a path in a repository.
func pathEscape(p string) string {
	if p == "" {
		return ""
	}
	return escapeJoin(strings.Split(p, "/"))
}

// BlobID is the Git blob ID of a file's content, as a listing names it:
// SHA-1, or SHA-256 for a repository that uses it, told by the length of
// like, an ID of the same repository. It tells an unchanged file without
// reading it again.
func BlobID(content []byte, like string) string {
	header := "blob " + strconv.Itoa(len(content)) + "\x00"
	if len(like) == 64 {
		sum := sha256.Sum256(append([]byte(header), content...))
		return hex.EncodeToString(sum[:])
	}
	sum := sha1.Sum(append([]byte(header), content...))
	return hex.EncodeToString(sum[:])
}
