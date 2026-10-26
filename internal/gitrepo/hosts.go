package gitrepo

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// maxListPages bounds how many pages of a listing are read, so a host
// that names a next page for ever cannot keep a run going.
const maxListPages = 200

// splitRest is the ref and path of an address whose ref may hold a slash:
// a ref that is a commit's hash ends at once, any other is found by Locate.
func splitRest(a *Address, rest []string) bool {
	if len(rest) == 0 {
		return false
	}
	if Commit(rest[0]) || len(rest) == 1 {
		a.Ref, a.Path = rest[0], strings.Join(rest[1:], "/")
		return true
	}
	a.rest = rest
	return true
}

// ---- GitHub and GitHub Enterprise Server ----

func parseGitHub(base string, segs []string) (Address, bool) {
	if len(segs) < 2 {
		return Address{}, false
	}
	a := Address{Host: GitHub, Base: base, Repo: segs[0] + "/" + strings.TrimSuffix(segs[1], ".git")}
	if len(segs) == 2 {
		a.shape = "folder"
		return a, true
	}
	switch segs[2] {
	case "tree":
		a.shape = "folder"
	case "blob", "raw":
		a.shape = "file"
	default:
		return Address{}, false
	}
	return a, splitRest(&a, segs[3:])
}

func parseGitHubRaw(segs []string) (Address, bool) {
	if len(segs) < 4 {
		return Address{}, false
	}
	a := Address{Host: GitHub, Base: "https://github.com", Repo: segs[0] + "/" + segs[1], shape: "file"}
	return a, splitRest(&a, segs[2:])
}

type github struct{}

func (github) api(a Address) string {
	if a.Base == "https://github.com" {
		return "https://api.github.com/repos/" + escapeJoin(strings.Split(a.Repo, "/"))
	}
	return a.Base + "/api/v3/repos/" + escapeJoin(strings.Split(a.Repo, "/"))
}

func (h github) defaultRef(ctx context.Context, c Client, a Address) (string, error) {
	var repo struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := c.getJSON(ctx, a, h.api(a), &repo, nil, nil); err != nil {
		return "", err
	}
	return repo.DefaultBranch, nil
}

func (h github) commit(ctx context.Context, c Client, a Address, ref string) (string, error) {
	var commits []struct {
		SHA string `json:"sha"`
	}
	if err := c.getJSON(ctx, a, h.api(a)+"/commits?per_page=1&sha="+url.QueryEscape(ref), &commits, nil, nil); err != nil {
		return "", err
	}
	if len(commits) == 0 || !Commit(commits[0].SHA) {
		return "", fmt.Errorf("%w: %s has no commit %s", ErrNotFound, a.Repo, ref)
	}
	return commits[0].SHA, nil
}

func (h github) list(ctx context.Context, c Client, a Address, commit string) ([]File, bool, error) {
	var tree struct {
		Tree []struct {
			Path string `json:"path"`
			Type string `json:"type"`
			SHA  string `json:"sha"`
		} `json:"tree"`
		Truncated bool `json:"truncated"`
	}
	if err := c.getJSON(ctx, a, h.api(a)+"/git/trees/"+url.PathEscape(commit)+"?recursive=1", &tree, nil, nil); err != nil {
		return nil, false, err
	}
	var out []File
	for _, e := range tree.Tree {
		if e.Type == "blob" {
			out = append(out, File{Path: e.Path, Blob: e.SHA})
		}
	}
	return out, tree.Truncated, nil
}

func (h github) read(a Address, commit, file string) (*http.Request, error) {
	if a.Base == "https://github.com" {
		return http.NewRequest(http.MethodGet, h.raw(a, commit, file), nil)
	}
	req, err := http.NewRequest(http.MethodGet, h.api(a)+"/contents/"+pathEscape(file)+"?ref="+url.QueryEscape(commit), nil)
	if err == nil {
		req.Header.Set("Accept", "application/vnd.github.raw")
	}
	return req, err
}

func (github) web(a Address, ref, file string, folder bool) string {
	kind := "blob"
	if folder {
		kind = "tree"
	}
	u := a.RepoURL()
	if ref == "" && file == "" {
		return u
	}
	return strings.TrimSuffix(u+"/"+kind+"/"+pathEscape(ref)+"/"+pathEscape(file), "/")
}

func (github) raw(a Address, commit, file string) string {
	if a.Base == "https://github.com" {
		return "https://raw.githubusercontent.com/" + escapeJoin(strings.Split(a.Repo, "/")) + "/" + url.PathEscape(commit) + "/" + pathEscape(file)
	}
	return a.RepoURL() + "/raw/" + url.PathEscape(commit) + "/" + pathEscape(file)
}

func (github) auth(token string) (string, string) { return "Authorization", "Bearer " + token }

// ---- GitLab ----

func parseGitLab(base string, segs []string) (Address, bool) {
	for i := 0; i+1 < len(segs); i++ {
		if segs[i] != "-" {
			continue
		}
		if i < 2 {
			return Address{}, false
		}
		a := Address{Host: GitLab, Base: base, Repo: strings.Join(segs[:i], "/")}
		switch segs[i+1] {
		case "tree":
			a.shape = "folder"
		case "blob", "raw":
			a.shape = "file"
		default:
			return Address{}, false
		}
		return a, splitRest(&a, segs[i+2:])
	}
	if len(segs) < 2 {
		return Address{}, false
	}
	return Address{Host: GitLab, Base: base, Repo: strings.TrimSuffix(strings.Join(segs, "/"), ".git"), shape: "folder"}, true
}

type gitlab struct{}

func (gitlab) api(a Address) string {
	return a.Base + "/api/v4/projects/" + url.PathEscape(a.Repo)
}

func (h gitlab) defaultRef(ctx context.Context, c Client, a Address) (string, error) {
	var p struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := c.getJSON(ctx, a, h.api(a), &p, nil, nil); err != nil {
		return "", err
	}
	return p.DefaultBranch, nil
}

func (h gitlab) commit(ctx context.Context, c Client, a Address, ref string) (string, error) {
	var commit struct {
		ID string `json:"id"`
	}
	if err := c.getJSON(ctx, a, h.api(a)+"/repository/commits/"+url.PathEscape(ref), &commit, nil, nil); err != nil {
		return "", err
	}
	if !Commit(commit.ID) {
		return "", fmt.Errorf("%w: %s has no commit %s", ErrNotFound, a.Repo, ref)
	}
	return commit.ID, nil
}

func (h gitlab) list(ctx context.Context, c Client, a Address, commit string) ([]File, bool, error) {
	var out []File
	page := "1"
	for n := 0; page != ""; n++ {
		if n == maxListPages {
			return out, true, nil
		}
		var entries []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
			Path string `json:"path"`
		}
		q := url.Values{"path": {a.Path}, "ref": {commit}, "recursive": {"true"}, "per_page": {"100"}, "page": {page}}
		var hd http.Header
		if err := c.getJSON(ctx, a, h.api(a)+"/repository/tree?"+q.Encode(), &entries, nil, &hd); err != nil {
			return nil, false, err
		}
		for _, e := range entries {
			if e.Type == "blob" {
				out = append(out, File{Path: e.Path, Blob: e.ID})
			}
		}
		page = hd.Get("X-Next-Page")
	}
	return out, false, nil
}

func (h gitlab) read(a Address, commit, file string) (*http.Request, error) {
	return http.NewRequest(http.MethodGet, h.api(a)+"/repository/files/"+url.PathEscape(file)+"/raw?ref="+url.QueryEscape(commit), nil)
}

func (gitlab) web(a Address, ref, file string, folder bool) string {
	kind := "blob"
	if folder {
		kind = "tree"
	}
	if ref == "" && file == "" {
		return a.RepoURL()
	}
	return strings.TrimSuffix(a.RepoURL()+"/-/"+kind+"/"+pathEscape(ref)+"/"+pathEscape(file), "/")
}

func (gitlab) raw(a Address, commit, file string) string {
	return a.RepoURL() + "/-/raw/" + url.PathEscape(commit) + "/" + pathEscape(file)
}

func (gitlab) auth(token string) (string, string) { return "Authorization", "Bearer " + token }

// ---- Bitbucket Cloud ----

func parseBitbucket(segs []string) (Address, bool) {
	if len(segs) < 2 {
		return Address{}, false
	}
	a := Address{Host: Bitbucket, Base: "https://bitbucket.org", Repo: segs[0] + "/" + strings.TrimSuffix(segs[1], ".git")}
	if len(segs) == 2 {
		a.shape = "folder"
		return a, true
	}
	switch segs[2] {
	case "src":
	case "raw":
		a.shape = "file"
	default:
		return Address{}, false
	}
	return a, splitRest(&a, segs[3:])
}

type bitbucket struct{}

func (bitbucket) api(a Address) string {
	return "https://api.bitbucket.org/2.0/repositories/" + escapeJoin(strings.Split(a.Repo, "/"))
}

func (h bitbucket) defaultRef(ctx context.Context, c Client, a Address) (string, error) {
	var repo struct {
		MainBranch struct {
			Name string `json:"name"`
		} `json:"mainbranch"`
	}
	if err := c.getJSON(ctx, a, h.api(a), &repo, nil, nil); err != nil {
		return "", err
	}
	return repo.MainBranch.Name, nil
}

func (h bitbucket) commit(ctx context.Context, c Client, a Address, ref string) (string, error) {
	var commit struct {
		Hash string `json:"hash"`
	}
	if err := c.getJSON(ctx, a, h.api(a)+"/commit/"+url.PathEscape(ref), &commit, nil, nil); err != nil {
		return "", err
	}
	if !Commit(commit.Hash) {
		return "", fmt.Errorf("%w: %s has no commit %s", ErrNotFound, a.Repo, ref)
	}
	return commit.Hash, nil
}

func (h bitbucket) list(ctx context.Context, c Client, a Address, commit string) ([]File, bool, error) {
	var out []File
	dir := pathEscape(a.Path)
	if dir != "" {
		dir += "/"
	}
	next := h.api(a) + "/src/" + url.PathEscape(commit) + "/" + dir + "?max_depth=10&pagelen=100"
	for n := 0; next != ""; n++ {
		if n == maxListPages {
			return out, true, nil
		}
		var page struct {
			Values []struct {
				Path string `json:"path"`
				Type string `json:"type"`
			} `json:"values"`
			Next string `json:"next"`
		}
		if err := c.getJSON(ctx, a, next, &page, nil, nil); err != nil {
			return nil, false, err
		}
		for _, v := range page.Values {
			if v.Type == "commit_file" {
				out = append(out, File{Path: v.Path})
			}
		}
		next = page.Next
		if next != "" && !strings.HasPrefix(next, "https://api.bitbucket.org/") {
			return nil, false, fmt.Errorf("gitrepo: the next page of %s is elsewhere: %s", a.Repo, next)
		}
	}
	return out, false, nil
}

func (h bitbucket) read(a Address, commit, file string) (*http.Request, error) {
	return http.NewRequest(http.MethodGet, h.api(a)+"/src/"+url.PathEscape(commit)+"/"+pathEscape(file), nil)
}

func (bitbucket) web(a Address, ref, file string, _ bool) string {
	if ref == "" && file == "" {
		return a.RepoURL()
	}
	return strings.TrimSuffix(a.RepoURL()+"/src/"+url.PathEscape(ref)+"/"+pathEscape(file), "/")
}

func (bitbucket) raw(a Address, commit, file string) string {
	return a.RepoURL() + "/raw/" + url.PathEscape(commit) + "/" + pathEscape(file)
}

func (bitbucket) auth(token string) (string, string) { return "Authorization", "Bearer " + token }

// ---- Bitbucket Data Center ----

// repoKind is where a Bitbucket Data Center repository lives in its
// addresses: a project's, or a person's, whose key starts with "~".
func repoKind(key string) string {
	if strings.HasPrefix(key, "~") {
		return "users"
	}
	return "projects"
}

func parseBitbucketServer(base string, segs []string, q url.Values) (Address, bool) {
	// projects|users KEY repos slug [browse|raw path...]
	if len(segs) < 4 || segs[2] != "repos" {
		return Address{}, false
	}
	key := segs[1]
	if segs[0] == "users" {
		key = "~" + key
	}
	a := Address{Host: BitbucketServer, Base: base, Repo: key + "/" + segs[3], Ref: q.Get("at")}
	if len(segs) == 4 {
		a.shape = "folder"
		return a, true
	}
	switch segs[4] {
	case "browse":
	case "raw":
		a.shape = "file"
	default:
		return Address{}, false
	}
	a.Path = strings.Join(segs[5:], "/")
	return a, true
}

type bitbucketServer struct{}

func (bitbucketServer) api(a Address) string {
	key, slug, _ := strings.Cut(a.Repo, "/")
	return a.Base + "/rest/api/1.0/projects/" + url.PathEscape(key) + "/repos/" + url.PathEscape(slug)
}

// defaultRef is empty: an empty ref reads the default branch.
func (bitbucketServer) defaultRef(context.Context, Client, Address) (string, error) { return "", nil }

func (h bitbucketServer) commit(ctx context.Context, c Client, a Address, ref string) (string, error) {
	var page struct {
		Values []struct {
			ID string `json:"id"`
		} `json:"values"`
	}
	q := url.Values{"limit": {"1"}}
	if ref != "" {
		q.Set("until", ref)
	}
	if err := c.getJSON(ctx, a, h.api(a)+"/commits?"+q.Encode(), &page, nil, nil); err != nil {
		return "", err
	}
	if len(page.Values) == 0 || !Commit(page.Values[0].ID) {
		return "", fmt.Errorf("%w: %s has no commit %s", ErrNotFound, a.Repo, ref)
	}
	return page.Values[0].ID, nil
}

func (h bitbucketServer) list(ctx context.Context, c Client, a Address, commit string) ([]File, bool, error) {
	var out []File
	start := 0
	for n := 0; ; n++ {
		if n == maxListPages {
			return out, true, nil
		}
		var page struct {
			Values        []string `json:"values"`
			IsLastPage    bool     `json:"isLastPage"`
			NextPageStart int      `json:"nextPageStart"`
		}
		addr := h.api(a) + "/files/" + pathEscape(a.Path) + "?" + url.Values{"at": {commit}, "start": {strconv.Itoa(start)}, "limit": {"1000"}}.Encode()
		if err := c.getJSON(ctx, a, addr, &page, nil, nil); err != nil {
			return nil, false, err
		}
		for _, v := range page.Values {
			p := v
			if a.Path != "" {
				p = a.Path + "/" + v
			}
			out = append(out, File{Path: p})
		}
		if page.IsLastPage || page.NextPageStart <= start {
			return out, false, nil
		}
		start = page.NextPageStart
	}
}

func (h bitbucketServer) read(a Address, commit, file string) (*http.Request, error) {
	return http.NewRequest(http.MethodGet, h.api(a)+"/raw/"+pathEscape(file)+"?at="+url.QueryEscape(commit), nil)
}

func (bitbucketServer) web(a Address, ref, file string, _ bool) string {
	u := a.RepoURL() + "/browse"
	if file != "" {
		u += "/" + pathEscape(file)
	}
	if ref != "" {
		u += "?at=" + url.QueryEscape(ref)
	}
	return u
}

func (bitbucketServer) raw(a Address, commit, file string) string {
	return a.RepoURL() + "/raw/" + pathEscape(file) + "?at=" + url.QueryEscape(commit)
}

func (bitbucketServer) auth(token string) (string, string) { return "Authorization", "Bearer " + token }

// ---- Gitea and Forgejo ----

func parseGitea(base string, segs []string) (Address, bool) {
	if len(segs) < 2 {
		return Address{}, false
	}
	a := Address{Host: Gitea, Base: base, Repo: segs[0] + "/" + strings.TrimSuffix(segs[1], ".git")}
	if len(segs) == 2 {
		a.shape = "folder"
		return a, true
	}
	if len(segs) < 5 || (segs[2] != "src" && segs[2] != "raw") {
		return Address{}, false
	}
	if segs[2] == "raw" {
		a.shape = "file"
	}
	switch a.RefKind = segs[3]; a.RefKind {
	case "commit":
		a.Ref, a.Path = segs[4], strings.Join(segs[5:], "/")
		return a, true
	case "branch", "tag":
		return a, splitRest(&a, segs[4:])
	}
	return Address{}, false
}

type gitea struct{}

func (gitea) api(a Address) string {
	return a.Base + "/api/v1/repos/" + escapeJoin(strings.Split(a.Repo, "/"))
}

func (h gitea) defaultRef(ctx context.Context, c Client, a Address) (string, error) {
	var repo struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := c.getJSON(ctx, a, h.api(a), &repo, nil, nil); err != nil {
		return "", err
	}
	return repo.DefaultBranch, nil
}

func (h gitea) commit(ctx context.Context, c Client, a Address, ref string) (string, error) {
	var commits []struct {
		SHA string `json:"sha"`
	}
	q := url.Values{"sha": {ref}, "limit": {"1"}, "stat": {"false"}, "verification": {"false"}, "files": {"false"}}
	if err := c.getJSON(ctx, a, h.api(a)+"/commits?"+q.Encode(), &commits, nil, nil); err != nil {
		return "", err
	}
	if len(commits) == 0 || !Commit(commits[0].SHA) {
		return "", fmt.Errorf("%w: %s has no commit %s", ErrNotFound, a.Repo, ref)
	}
	return commits[0].SHA, nil
}

func (h gitea) list(ctx context.Context, c Client, a Address, commit string) ([]File, bool, error) {
	var out []File
	for page := 1; ; page++ {
		if page > maxListPages {
			return out, true, nil
		}
		var tree struct {
			Tree []struct {
				Path string `json:"path"`
				Type string `json:"type"`
				SHA  string `json:"sha"`
			} `json:"tree"`
			Truncated  bool `json:"truncated"`
			TotalCount int  `json:"total_count"`
		}
		q := url.Values{"recursive": {"true"}, "per_page": {"1000"}, "page": {strconv.Itoa(page)}}
		if err := c.getJSON(ctx, a, h.api(a)+"/git/trees/"+url.PathEscape(commit)+"?"+q.Encode(), &tree, nil, nil); err != nil {
			return nil, false, err
		}
		for _, e := range tree.Tree {
			if e.Type == "blob" {
				out = append(out, File{Path: e.Path, Blob: e.SHA})
			}
		}
		if len(tree.Tree) == 0 || !tree.Truncated && page*1000 >= tree.TotalCount {
			return out, false, nil
		}
	}
}

func (h gitea) read(a Address, commit, file string) (*http.Request, error) {
	return http.NewRequest(http.MethodGet, h.api(a)+"/raw/"+pathEscape(file)+"?ref="+url.QueryEscape(commit), nil)
}

func (gitea) web(a Address, ref, file string, _ bool) string {
	if ref == "" && file == "" {
		return a.RepoURL()
	}
	kind := a.RefKind
	if Commit(ref) {
		kind = "commit"
	} else if kind == "" || kind == "commit" {
		kind = "branch"
	}
	return strings.TrimSuffix(a.RepoURL()+"/src/"+kind+"/"+pathEscape(ref)+"/"+pathEscape(file), "/")
}

func (gitea) raw(a Address, commit, file string) string {
	return a.RepoURL() + "/raw/commit/" + url.PathEscape(commit) + "/" + pathEscape(file)
}

func (gitea) auth(token string) (string, string) { return "Authorization", "token " + token }
