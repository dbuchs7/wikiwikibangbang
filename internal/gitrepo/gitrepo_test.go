package gitrepo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	for _, c := range []struct {
		addr string
		want Address
	}{
		{"https://github.com/o/r/tree/main/docs", Address{Host: GitHub, Base: "https://github.com", Repo: "o/r", shape: "folder", rest: []string{"main", "docs"}}},
		{"https://github.com/o/r/tree/main", Address{Host: GitHub, Base: "https://github.com", Repo: "o/r", Ref: "main", Folder: true, shape: "folder"}},
		{"https://www.github.com/o/r.git", Address{Host: GitHub, Base: "https://github.com", Repo: "o/r", Folder: true, shape: "folder"}},
		{"https://github.com/o/r/blob/feature/x/a.md", Address{Host: GitHub, Base: "https://github.com", Repo: "o/r", shape: "file", rest: []string{"feature", "x", "a.md"}}},
		{"https://github.com/o/r/blob/0123456789abcdef0123456789abcdef01234567/docs/a.md", Address{Host: GitHub, Base: "https://github.com", Repo: "o/r", Ref: "0123456789abcdef0123456789abcdef01234567", Path: "docs/a.md", shape: "file"}},
		{"https://raw.githubusercontent.com/o/r/main/README.md", Address{Host: GitHub, Base: "https://github.com", Repo: "o/r", shape: "file", rest: []string{"main", "README.md"}}},
		{"https://ghe.example.org/o/r/tree/v1/docs/", Address{Host: GitHub, Base: "https://ghe.example.org", Repo: "o/r", shape: "folder", rest: []string{"v1", "docs"}}},
		{"https://gitlab.com/g/sub/p/-/blob/main/a.md", Address{Host: GitLab, Base: "https://gitlab.com", Repo: "g/sub/p", shape: "file", rest: []string{"main", "a.md"}}},
		{"https://gitlab.com/g/p", Address{Host: GitLab, Base: "https://gitlab.com", Repo: "g/p", Folder: true, shape: "folder"}},
		{"https://git.example.org/g/p/-/tree/main", Address{Host: GitLab, Base: "https://git.example.org", Repo: "g/p", Ref: "main", Folder: true, shape: "folder"}},
		{"https://bitbucket.org/ws/repo/src/main/docs/", Address{Host: Bitbucket, Base: "https://bitbucket.org", Repo: "ws/repo", rest: []string{"main", "docs"}}},
		{"https://bitbucket.org/ws/repo/src/main/", Address{Host: Bitbucket, Base: "https://bitbucket.org", Repo: "ws/repo", Ref: "main", Folder: true}},
		{"https://bitbucket.org/ws/repo", Address{Host: Bitbucket, Base: "https://bitbucket.org", Repo: "ws/repo", Folder: true, shape: "folder"}},
		{"https://bb.example.org/ctx/projects/KEY/repos/slug/browse/docs/a.md?at=refs%2Fheads%2Fmain", Address{Host: BitbucketServer, Base: "https://bb.example.org/ctx", Repo: "KEY/slug", Ref: "refs/heads/main", Path: "docs/a.md"}},
		{"https://bb.example.org/users/me/repos/slug", Address{Host: BitbucketServer, Base: "https://bb.example.org", Repo: "~me/slug", Folder: true, shape: "folder"}},
		{"https://codeberg.org/o/r/src/branch/main/docs", Address{Host: Gitea, Base: "https://codeberg.org", Repo: "o/r", RefKind: "branch", rest: []string{"main", "docs"}}},
		{"https://git.example.org/sub/o/r/src/commit/0123456789abcdef0123456789abcdef01234567/a.md", Address{Host: Gitea, Base: "https://git.example.org/sub", Repo: "o/r", RefKind: "commit", Ref: "0123456789abcdef0123456789abcdef01234567", Path: "a.md"}},
		{"https://git.example.org/o/r/raw/tag/v1/a.md", Address{Host: Gitea, Base: "https://git.example.org", Repo: "o/r", RefKind: "tag", shape: "file", rest: []string{"v1", "a.md"}}},
	} {
		got, err := Parse(c.addr)
		if err != nil {
			t.Errorf("%s: %v", c.addr, err)
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s:\n got %#v\nwant %#v", c.addr, got, c.want)
		}
	}
	for _, bad := range []string{"", "ftp://github.com/o/r", "https://github.com/o", "https://github.com/o/r/issues/1", "https://u:p@github.com/o/r",
		"https://example.org/just/a/page", "https://github.com/o/r/blob", "https://github.com/o/r/blob/main/%2e%2e/x", "https://gitlab.com/g/-/blob/main/a.md",
		"https://bitbucket.org/ws", "https://bitbucket.org/ws/r/commits/x", "https://bb.example.org/projects/K/repos/r/commits", "https://codeberg.org/o/r/src/x/y/z",
		"https://codeberg.org/o", "https://raw.githubusercontent.com/o/r/main", "https://gitlab.com/g", "https://gitlab.com/g/p/-/issues/1", "https://bitbucket.org/ws/r/src", "https://github.com/%zz/r"} {
		if _, err := Parse(bad); !errors.Is(err, ErrAddress) {
			t.Errorf("%q: %v, want ErrAddress", bad, err)
		}
	}
}

const (
	main1   = "1111111111111111111111111111111111111111"
	branch2 = "2222222222222222222222222222222222222222"
)

// repo is the content of the fake repository every host serves.
var repo = map[string]string{
	"README.md":           "# Read me",
	"docs/index.md":       "# Docs",
	"docs/guide/setup.md": "# Set up",
	"docs/img/logo.png":   "PNG",
	"docs/notes.txt":      "notes",
}

func refs(ref string) (string, bool) {
	switch ref {
	case "main", "refs/heads/main", main1:
		return main1, true
	case "feature/x":
		return branch2, true
	}
	return "", false
}

func paths() []string {
	var out []string
	for p := range repo {
		out = append(out, p)
	}
	slices.Sort(out)
	return out
}

func blob(p string) string { return fmt.Sprintf("%040x", len(p)) }

// fake answers as the hosts' APIs do. It is reached through a transport
// that keeps the host a request was meant for.
func fake(t *testing.T) *http.Client {
	t.Helper()
	write := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	file := func(w http.ResponseWriter, commit, p string) {
		if c, ok := refs(commit); !ok || c != commit {
			http.NotFound(w, nil)
			return
		}
		if content, ok := repo[p]; ok {
			_, _ = w.Write([]byte(content))
			return
		}
		http.NotFound(w, nil)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "wikiwikibangbang" {
			t.Errorf("%s: no user agent", r.URL)
		}
		host, p, q := r.Host, r.URL.EscapedPath(), r.URL.Query()
		switch {
		// GitHub
		case host == "api.github.com" || host == "ghe.test" && strings.HasPrefix(p, "/api/v3/"):
			p = strings.TrimPrefix(p, "/api/v3")
			switch {
			case p == "/repos/o/r":
				write(w, map[string]string{"default_branch": "main"})
			case p == "/repos/o/r/commits":
				if c, ok := refs(q.Get("sha")); ok {
					write(w, []map[string]string{{"sha": c}})
					return
				}
				http.Error(w, "no commit", http.StatusUnprocessableEntity)
			case p == "/repos/o/r/git/trees/"+main1:
				var tree []map[string]string
				for _, f := range paths() {
					tree = append(tree, map[string]string{"path": f, "type": "blob", "sha": blob(f)})
				}
				tree = append(tree, map[string]string{"path": "docs", "type": "tree"})
				write(w, map[string]any{"tree": tree, "truncated": false})
			case strings.HasPrefix(p, "/repos/o/r/contents/"):
				if r.Header.Get("Accept") != "application/vnd.github.raw" {
					t.Errorf("contents without the raw media type")
				}
				file(w, q.Get("ref"), strings.TrimPrefix(r.URL.Path, "/api/v3/repos/o/r/contents/"))
			default:
				http.NotFound(w, r)
			}
		case host == "raw.githubusercontent.com":
			parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/o/r/"), "/", 2)
			file(w, parts[0], parts[1])
		// GitLab
		case host == "gitlab.com":
			const api = "/api/v4/projects/g%2Fsub%2Fp"
			switch {
			case p == api:
				write(w, map[string]string{"default_branch": "main"})
			case strings.HasPrefix(p, api+"/repository/commits/"):
				ref, _ := url.PathUnescape(strings.TrimPrefix(p, api+"/repository/commits/"))
				if c, ok := refs(ref); ok {
					write(w, map[string]string{"id": c})
					return
				}
				http.NotFound(w, r)
			case p == api+"/repository/tree":
				var entries []map[string]string
				for _, f := range paths() {
					if strings.HasPrefix(f, q.Get("path")+"/") {
						entries = append(entries, map[string]string{"id": blob(f), "type": "blob", "path": f})
					}
				}
				if q.Get("page") == "1" {
					w.Header().Set("X-Next-Page", "2")
					write(w, entries[:1])
					return
				}
				write(w, entries[1:])
			case strings.HasPrefix(p, api+"/repository/files/") && strings.HasSuffix(p, "/raw"):
				f, _ := url.PathUnescape(strings.TrimSuffix(strings.TrimPrefix(p, api+"/repository/files/"), "/raw"))
				file(w, q.Get("ref"), f)
			default:
				http.NotFound(w, r)
			}
		// Bitbucket Cloud
		case host == "api.bitbucket.org":
			const api = "/2.0/repositories/ws/repo"
			switch {
			case p == api:
				write(w, map[string]any{"mainbranch": map[string]string{"name": "main"}})
			case strings.HasPrefix(p, api+"/commit/"):
				ref, _ := url.PathUnescape(strings.TrimPrefix(p, api+"/commit/"))
				if c, ok := refs(ref); ok {
					write(w, map[string]string{"hash": c})
					return
				}
				http.NotFound(w, r)
			case p == api+"/src/"+main1+"/docs/":
				if q.Get("page") == "" {
					write(w, map[string]any{"values": []map[string]string{{"path": "docs/guide", "type": "commit_directory"}, {"path": "docs/index.md", "type": "commit_file"}},
						"next": "https://api.bitbucket.org" + api + "/src/" + main1 + "/docs/?page=2"})
					return
				}
				write(w, map[string]any{"values": []map[string]string{{"path": "docs/guide/setup.md", "type": "commit_file"}, {"path": "docs/notes.txt", "type": "commit_file"}}})
			case strings.HasPrefix(p, api+"/src/"):
				parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, api+"/src/"), "/", 2)
				file(w, parts[0], parts[1])
			default:
				http.NotFound(w, r)
			}
		// Bitbucket Data Center below /ctx
		case host == "bb.test":
			const api = "/ctx/rest/api/1.0/projects/KEY/repos/slug"
			switch {
			case p == api+"/commits":
				ref := q.Get("until")
				if ref == "" {
					ref = "main"
				}
				if c, ok := refs(ref); ok {
					write(w, map[string]any{"values": []map[string]string{{"id": c}}})
					return
				}
				http.NotFound(w, r)
			case p == api+"/files/docs":
				if q.Get("start") == "0" {
					write(w, map[string]any{"values": []string{"index.md"}, "isLastPage": false, "nextPageStart": 1})
					return
				}
				write(w, map[string]any{"values": []string{"guide/setup.md", "notes.txt"}, "isLastPage": true})
			case strings.HasPrefix(p, api+"/raw/"):
				file(w, q.Get("at"), strings.TrimPrefix(r.URL.Path, api+"/raw/"))
			default:
				http.NotFound(w, r)
			}
		// Gitea / Forgejo
		case host == "codeberg.org":
			const api = "/api/v1/repos/o/r"
			switch {
			case p == api:
				write(w, map[string]string{"default_branch": "main"})
			case p == api+"/commits":
				if c, ok := refs(q.Get("sha")); ok {
					write(w, []map[string]string{{"sha": c}})
					return
				}
				write(w, []map[string]string{})
			case p == api+"/git/trees/"+main1:
				var tree []map[string]string
				for _, f := range paths() {
					tree = append(tree, map[string]string{"path": f, "type": "blob", "sha": blob(f)})
				}
				if q.Get("page") == "1" {
					write(w, map[string]any{"tree": tree[:2], "truncated": true, "total_count": len(tree)})
					return
				}
				write(w, map[string]any{"tree": tree[2:], "truncated": false, "total_count": len(tree)})
			case strings.HasPrefix(p, api+"/raw/"):
				file(w, q.Get("ref"), strings.TrimPrefix(r.URL.Path, api+"/raw/"))
			default:
				http.NotFound(w, r)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	target, _ := url.Parse(srv.URL)
	return &http.Client{Transport: redirect{target: target}}
}

// redirect sends every request to the fake, keeping the host it was
// meant for.
type redirect struct{ target *url.URL }

func (rt redirect) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	r.Host = req.URL.Host
	r.URL.Scheme, r.URL.Host = rt.target.Scheme, rt.target.Host
	return http.DefaultTransport.RoundTrip(r)
}

func TestHosts(t *testing.T) {
	client := Client{HTTP: fake(t)}
	ctx := context.Background()
	for _, c := range []struct {
		addr      string
		ref, path string
		commit    string
		files     []string
		blobs     bool
		web, raw  string
	}{
		{"https://github.com/o/r/tree/main/docs", "main", "docs", main1, []string{"docs/guide/setup.md", "docs/index.md"}, true,
			"https://github.com/o/r/tree/main/docs", "https://raw.githubusercontent.com/o/r/" + main1 + "/docs/img/logo.png"},
		{"https://github.com/o/r", "main", "", main1, []string{"README.md", "docs/guide/setup.md", "docs/index.md"}, true,
			"https://github.com/o/r/tree/main", "https://raw.githubusercontent.com/o/r/" + main1 + "/docs/img/logo.png"},
		{"http://ghe.test/o/r/blob/main/docs/index.md", "main", "docs/index.md", main1, []string{"docs/index.md"}, false,
			"http://ghe.test/o/r/blob/main/docs/index.md", "http://ghe.test/o/r/raw/" + main1 + "/docs/img/logo.png"},
		{"https://gitlab.com/g/sub/p/-/tree/main/docs", "main", "docs", main1, []string{"docs/guide/setup.md", "docs/index.md"}, true,
			"https://gitlab.com/g/sub/p/-/tree/main/docs", "https://gitlab.com/g/sub/p/-/raw/" + main1 + "/docs/img/logo.png"},
		{"https://bitbucket.org/ws/repo/src/main/docs/", "main", "docs", main1, []string{"docs/guide/setup.md", "docs/index.md"}, false,
			"https://bitbucket.org/ws/repo/src/main/docs", "https://bitbucket.org/ws/repo/raw/" + main1 + "/docs/img/logo.png"},
		{"http://bb.test/ctx/projects/KEY/repos/slug/browse/docs", "", "docs", main1, []string{"docs/guide/setup.md", "docs/index.md"}, false,
			"http://bb.test/ctx/projects/KEY/repos/slug/browse/docs", "http://bb.test/ctx/projects/KEY/repos/slug/raw/docs/img/logo.png?at=" + main1},
		{"https://codeberg.org/o/r/src/branch/main/docs", "main", "docs", main1, []string{"docs/guide/setup.md", "docs/index.md"}, true,
			"https://codeberg.org/o/r/src/branch/main/docs", "https://codeberg.org/o/r/raw/commit/" + main1 + "/docs/img/logo.png"},
	} {
		t.Run(c.addr, func(t *testing.T) {
			a, err := Parse(c.addr)
			if err != nil {
				t.Fatal(err)
			}
			a, commit, err := client.Locate(ctx, a)
			if err != nil {
				t.Fatal(err)
			}
			if a.Ref != c.ref || a.Path != c.path || commit != c.commit {
				t.Errorf("located %q %q at %s", a.Ref, a.Path, commit)
			}
			files, truncated, err := client.Files(ctx, a, commit)
			if err != nil || truncated {
				t.Fatal(err, truncated)
			}
			var got []string
			for _, f := range files {
				got = append(got, f.Path)
				if c.blobs && a.Folder && f.Blob != blob(f.Path) {
					t.Errorf("%s: blob %q", f.Path, f.Blob)
				}
			}
			slices.Sort(got)
			if !reflect.DeepEqual(got, c.files) {
				t.Errorf("files %v, want %v", got, c.files)
			}
			data, err := client.Read(ctx, a, commit, c.files[len(c.files)-1], 100)
			if err != nil || string(data) != repo[c.files[len(c.files)-1]] {
				t.Errorf("read %q: %v", data, err)
			}
			if w := a.String(); w != c.web {
				t.Errorf("web %s, want %s", w, c.web)
			}
			if r := a.RawURL(commit, "docs/img/logo.png"); r != c.raw {
				t.Errorf("raw %s, want %s", r, c.raw)
			}
			if _, err := client.Read(ctx, a, commit, "missing.md", 100); !errors.Is(err, ErrNotFound) {
				t.Errorf("a missing file: %v", err)
			}
		})
	}
}

func TestLocateFindsARefWithASlash(t *testing.T) {
	client := Client{HTTP: fake(t)}
	for _, addr := range []string{"https://github.com/o/r/blob/feature/x/docs/index.md", "https://gitlab.com/g/sub/p/-/blob/feature/x/docs/index.md",
		"https://bitbucket.org/ws/repo/src/feature/x/docs/index.md", "https://codeberg.org/o/r/src/branch/feature/x/docs/index.md"} {
		a, _ := Parse(addr)
		a, commit, err := client.Locate(context.Background(), a)
		if err != nil || a.Ref != "feature/x" || a.Path != "docs/index.md" || commit != branch2 || a.Folder {
			t.Errorf("%s: %#v %s %v", addr, a, commit, err)
		}
	}
	a, _ := Parse("https://github.com/o/r/blob/nope/at/all/x.md")
	if _, _, err := client.Locate(context.Background(), a); !errors.Is(err, ErrNotFound) {
		t.Errorf("an unknown ref: %v", err)
	}
	a, _ = Parse("https://codeberg.org/o/r/src/branch/gone")
	if _, _, err := client.Locate(context.Background(), a); !errors.Is(err, ErrNotFound) {
		t.Errorf("an unknown Gitea branch: %v", err)
	}
}

func TestTokensGoWhereTheHostWantsThem(t *testing.T) {
	seen := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen[r.Host] = r.Header.Get("Authorization")
		_, _ = w.Write([]byte("x"))
	}))
	defer srv.Close()
	target, _ := url.Parse(srv.URL)
	client := Client{HTTP: &http.Client{Transport: redirect{target: target}}, Token: "secret"}
	for addr, want := range map[string]string{"https://github.com/o/r": "Bearer secret", "https://codeberg.org/o/r": "token secret"} {
		a, _ := Parse(addr)
		if _, err := client.Read(context.Background(), a, main1, "a.md", 10); err != nil {
			t.Fatal(err)
		}
		if _, err := client.Get(context.Background(), a, "https://elsewhere.example/p.png", 10); err != nil {
			t.Fatal(err)
		}
		host := map[string]string{"https://github.com/o/r": "raw.githubusercontent.com", "https://codeberg.org/o/r": "codeberg.org"}[addr]
		if seen[host] != want {
			t.Errorf("%s: %q", host, seen[host])
		}
		if seen["elsewhere.example"] != "" {
			t.Errorf("the token went to another site")
		}
	}
}

func TestLimitsAndErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/big":
			_, _ = w.Write([]byte(strings.Repeat("x", 20)))
		case "/fail":
			http.Error(w, "down", http.StatusBadGateway)
		case "/api/v1/repos/o/r":
			_, _ = w.Write([]byte("not json"))
		}
	}))
	defer srv.Close()
	client := Client{}
	a := Address{Host: Gitea, Base: srv.URL, Repo: "o/r"}
	if _, err := client.Get(context.Background(), a, srv.URL+"/big", 10); !errors.Is(err, ErrTooLarge) {
		t.Errorf("too large: %v", err)
	}
	if _, err := client.Get(context.Background(), a, srv.URL+"/fail", 10); err == nil || !strings.Contains(err.Error(), "502") {
		t.Errorf("failure: %v", err)
	}
	if _, _, err := client.Locate(context.Background(), a); err == nil || !strings.Contains(err.Error(), "not what the host's API answers") {
		t.Errorf("not json: %v", err)
	}
	if !Markdown("A.MD") || !Markdown("x.markdown") || Markdown("x.txt") || !Commit(main1) || Commit("main") {
		t.Error("names")
	}
}

func TestBlobID(t *testing.T) {
	// As git hash-object computes them.
	if got := BlobID([]byte("hello\n"), main1); got != "ce013625030ba8dba906f756967f9e9ca394464a" {
		t.Errorf("sha1 %s", got)
	}
	if got := BlobID([]byte("hello\n"), strings.Repeat("a", 64)); len(got) != 64 {
		t.Errorf("sha256 %s", got)
	}
}

func TestReadAtARef(t *testing.T) {
	client := Client{HTTP: fake(t)}
	a, _ := Parse("https://github.com/o/r/blob/main/README.md")
	a = a.At("main", "docs/index.md")
	if a.Folder || a.Path != "docs/index.md" || a.Ref != "main" {
		t.Fatalf("%#v", a)
	}
	a, commit, err := client.Locate(context.Background(), a)
	if err != nil || commit != main1 {
		t.Fatal(err)
	}
	if r := a.RawURL("feature/x", "a b.md"); r != "https://raw.githubusercontent.com/o/r/feature/x/a%20b.md" {
		t.Errorf("a ref with a slash keeps it: %s", r)
	}
}
