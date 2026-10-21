package dest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/pem"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// caFile writes the certificate of a test server for CAFileEnv.
func caFile(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "ca.pem")
	data := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return file
}

func envOf(m map[string]string) Env { return func(k string) string { return m[k] } }

// exercise runs a target through what the runner does with it.
func exercise(t *testing.T, tg Target) {
	t.Helper()
	ctx := context.Background()
	if names, err := tg.List(ctx); err != nil || len(names) != 0 {
		t.Fatalf("an empty destination lists %v, %v", names, err)
	}
	big := bytes.Repeat([]byte("0123456789abcdef"), 5000) // 80 kB
	if err := tg.Put(ctx, "a.wwbb", bytes.NewReader(big)); err != nil {
		t.Fatal(err)
	}
	if err := tg.Put(ctx, "b.wwbb", strings.NewReader("small")); err != nil {
		t.Fatal(err)
	}
	if err := tg.Put(ctx, "b.wwbb", strings.NewReader("replaced")); err != nil {
		t.Fatal("replacing:", err)
	}
	names, err := tg.List(ctx)
	sort.Strings(names)
	if err != nil || !slices.Equal(names, []string{"a.wwbb", "b.wwbb"}) {
		t.Fatalf("list %v %v", names, err)
	}
	for name, want := range map[string][]byte{"a.wwbb": big, "b.wwbb": []byte("replaced")} {
		r, err := tg.Open(ctx, name)
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(r)
		if cerr := r.Close(); err == nil {
			err = cerr
		}
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("%s: read %d bytes, want %d: %v", name, len(got), len(want), err)
		}
	}
	// A failing upload leaves nothing.
	if err := tg.Put(ctx, "c.wwbb", io.MultiReader(bytes.NewReader(big), errReader{})); err == nil {
		t.Fatal("a failing upload was stored")
	}
	if err := tg.Delete(ctx, "a.wwbb"); err != nil {
		t.Fatal(err)
	}
	if names, _ := tg.List(ctx); !slices.Equal(names, []string{"b.wwbb"}) {
		t.Fatalf("after deleting: %v", names)
	}
	if _, err := tg.Open(ctx, "missing.wwbb"); err == nil {
		t.Error("opened a missing file")
	}
	if err := tg.Delete(ctx, "missing.wwbb"); err == nil {
		t.Error("deleted a missing file")
	}
	for _, bad := range []string{"../x", "x.part"} {
		if tg.Put(ctx, bad, strings.NewReader("")) == nil || tg.Delete(ctx, bad) == nil {
			t.Errorf("accepted %q", bad)
		}
		if _, err := tg.Open(ctx, bad); err == nil {
			t.Errorf("opened %q", bad)
		}
	}
}

// fakeDAV is a WebDAV server holding files in memory under /dav/backups/.
type fakeDAV struct {
	mu    sync.Mutex
	files map[string][]byte
}

func (f *fakeDAV) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	user, pass, _ := r.BasicAuth()
	if user != "wiki" || pass != "secret" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	const dir = "/dav/backups/"
	f.mu.Lock()
	defer f.mu.Unlock()
	if !strings.HasPrefix(r.URL.Path, dir) {
		w.WriteHeader(http.StatusConflict)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, dir)
	switch r.Method {
	case http.MethodPut:
		data, err := io.ReadAll(r.Body)
		if err != nil {
			return
		}
		f.files[name] = data
		w.WriteHeader(http.StatusCreated)
	case "MOVE":
		d, _ := url.Parse(r.Header.Get("Destination"))
		data, ok := f.files[name]
		if !ok || d == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		delete(f.files, name)
		f.files[strings.TrimPrefix(d.Path, dir)] = data
		w.WriteHeader(http.StatusCreated)
	case http.MethodGet:
		data, ok := f.files[name]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Write(data)
	case http.MethodDelete:
		if _, ok := f.files[name]; !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		delete(f.files, name)
		w.WriteHeader(http.StatusNoContent)
	case "PROPFIND":
		if r.Header.Get("Depth") != "1" || name != "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var b strings.Builder
		b.WriteString(`<?xml version="1.0"?><D:multistatus xmlns:D="DAV:">`)
		b.WriteString(`<D:response><D:href>` + dir + `</D:href><D:propstat><D:prop><D:resourcetype><D:collection/></D:resourcetype></D:prop></D:propstat></D:response>`)
		b.WriteString(`<D:response><D:href>` + dir + `sub/</D:href><D:propstat><D:prop><D:resourcetype><D:collection/></D:resourcetype></D:prop></D:propstat></D:response>`)
		for n := range f.files {
			if !strings.HasSuffix(n, ".part") {
				fmt.Fprintf(&b, `<D:response><D:href>https://host%s%s</D:href><D:propstat><D:prop><D:resourcetype/></D:prop></D:propstat></D:response>`, dir, url.PathEscape(n))
			}
		}
		b.WriteString(`</D:multistatus>`)
		w.WriteHeader(http.StatusMultiStatus)
		io.WriteString(w, b.String())
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func TestWebDAV(t *testing.T) {
	fake := &fakeDAV{files: map[string][]byte{}}
	srv := httptest.NewTLSServer(fake)
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	env := map[string]string{CAFileEnv: caFile(t, srv), PasswordEnv: "secret"}
	tg, err := Open("https://wiki@"+u.Host+"/dav/backups", envOf(env))
	if err != nil {
		t.Fatal(err)
	}
	exercise(t, tg)
	for n := range fake.files {
		if strings.HasSuffix(n, ".part") {
			t.Errorf("left behind: %s", n)
		}
	}

	// The user may come from the environment too; a wrong password, an
	// unknown certificate and a missing folder fail with a hint.
	env[UsernameEnv] = "wiki"
	if tg, _ := Open("https://"+u.Host+"/dav/backups", envOf(env)); tg == nil {
		t.Fatal("no target")
	} else if _, err := tg.List(context.Background()); err != nil {
		t.Errorf("user from the environment: %v", err)
	}
	env[PasswordEnv] = "wrong"
	tg, _ = Open("https://"+u.Host+"/dav/backups", envOf(env))
	if _, err := tg.List(context.Background()); err == nil || !strings.Contains(err.Error(), "credentials") {
		t.Errorf("a wrong password: %v", err)
	}
	if err := tg.Put(context.Background(), "x.wwbb", strings.NewReader("x")); err == nil {
		t.Error("stored with a wrong password")
	}
	env[PasswordEnv] = "secret"
	tg, _ = Open("https://"+u.Host+"/elsewhere", envOf(env))
	if _, err := tg.List(context.Background()); err == nil || !strings.Contains(err.Error(), "folder exists") {
		t.Errorf("a missing folder: %v", err)
	}
	tg, _ = Open("https://wiki@"+u.Host+"/dav/backups", envOf(map[string]string{PasswordEnv: "secret"}))
	if _, err := tg.List(context.Background()); err == nil {
		t.Error("an unknown certificate was trusted")
	}
	if _, err := Open("https://"+u.Host+"/dav", envOf(map[string]string{CAFileEnv: "/missing"})); err == nil {
		t.Error("a missing CA file was accepted")
	}
	empty := filepath.Join(t.TempDir(), "empty.pem")
	os.WriteFile(empty, []byte("no certificate"), 0o600)
	if _, err := Open("https://"+u.Host+"/dav", envOf(map[string]string{CAFileEnv: empty})); err == nil {
		t.Error("a CA file without a certificate was accepted")
	}
}

// fakeS3 is an S3 server holding objects in memory; it checks every
// request's signature by computing it again.
type fakeS3 struct {
	t       *testing.T
	mu      sync.Mutex
	objects map[string][]byte
	uploads map[string]map[int][]byte
	parts   int
	pageMax int
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	sum := sha256.Sum256(body)
	if r.Header.Get("x-amz-content-sha256") != hex.EncodeToString(sum[:]) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	at, _ := time.Parse("20060102T150405Z", r.Header.Get("x-amz-date"))
	check, _ := http.NewRequest(r.Method, "https://"+r.Host+r.URL.RequestURI(), nil)
	check.Header.Set("x-amz-content-sha256", r.Header.Get("x-amz-content-sha256"))
	sign(check, "AKID", "SECRET", "eu-central-1", "s3", at, hex.EncodeToString(sum[:]), []string{"host", "x-amz-content-sha256", "x-amz-date"})
	if check.Header.Get("Authorization") != r.Header.Get("Authorization") {
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, "SignatureDoesNotMatch")
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	const bucket = "/backups"
	if !strings.HasPrefix(r.URL.Path, bucket) {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	key := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, bucket), "/")
	q := r.URL.Query()
	switch {
	case r.Method == http.MethodGet && key == "":
		var keys []string
		for k := range f.objects {
			if strings.HasPrefix(k, q.Get("prefix")) {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		start := 0
		if tok := q.Get("continuation-token"); tok != "" {
			fmt.Sscan(tok, &start)
		}
		end := min(start+f.pageMax, len(keys))
		var b strings.Builder
		b.WriteString(`<ListBucketResult>`)
		for _, k := range keys[start:end] {
			fmt.Fprintf(&b, `<Contents><Key>%s</Key></Contents>`, k)
		}
		if end < len(keys) {
			fmt.Fprintf(&b, `<IsTruncated>true</IsTruncated><NextContinuationToken>%d</NextContinuationToken>`, end)
		}
		b.WriteString(`</ListBucketResult>`)
		io.WriteString(w, b.String())
	case r.Method == http.MethodPost && q.Has("uploads"):
		id := fmt.Sprintf("u%d", len(f.uploads)+1)
		f.uploads[id] = map[int][]byte{}
		fmt.Fprintf(w, `<InitiateMultipartUploadResult><UploadId>%s</UploadId></InitiateMultipartUploadResult>`, id)
	case r.Method == http.MethodPut && q.Has("uploadId"):
		var n int
		fmt.Sscan(q.Get("partNumber"), &n)
		f.uploads[q.Get("uploadId")][n] = body
		f.parts++
		w.Header().Set("ETag", fmt.Sprintf(`"e%d"`, n))
	case r.Method == http.MethodPost && q.Has("uploadId"):
		var c struct {
			Parts []completedPart `xml:"Part"`
		}
		xml.Unmarshal(body, &c)
		var data []byte
		for i, p := range c.Parts {
			if p.Number != i+1 || p.ETag != fmt.Sprintf(`"e%d"`, i+1) {
				io.WriteString(w, `<Error><Code>InvalidPart</Code><Message>bad part</Message></Error>`)
				return
			}
			data = append(data, f.uploads[q.Get("uploadId")][p.Number]...)
		}
		delete(f.uploads, q.Get("uploadId"))
		f.objects[key] = data
		io.WriteString(w, `<CompleteMultipartUploadResult/>`)
	case r.Method == http.MethodDelete && q.Has("uploadId"):
		delete(f.uploads, q.Get("uploadId"))
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPut:
		f.objects[key] = body
	case r.Method == http.MethodGet:
		data, ok := f.objects[key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Write(data)
	case r.Method == http.MethodDelete:
		if _, ok := f.objects[key]; !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		delete(f.objects, key)
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func TestS3(t *testing.T) {
	old := partSize
	partSize = 32 << 10
	defer func() { partSize = old }()
	fake := &fakeS3{t: t, objects: map[string][]byte{"other/x.wwbb": nil}, uploads: map[string]map[int][]byte{}, pageMax: 1}
	srv := httptest.NewTLSServer(fake)
	defer srv.Close()
	env := map[string]string{CAFileEnv: caFile(t, srv), AccessKeyEnv: "AKID", SecretKeyEnv: "SECRET"}
	addr := "s3://backups/wiki/daily?endpoint=" + url.QueryEscape(srv.URL) + "&region=eu-central-1"
	tg, err := Open(addr, envOf(env))
	if err != nil {
		t.Fatal(err)
	}
	exercise(t, tg)
	if fake.parts < 3 {
		t.Errorf("the large file went up in %d parts", fake.parts)
	}
	if len(fake.uploads) != 0 {
		t.Errorf("an upload was left open: %v", fake.uploads)
	}
	if _, ok := fake.objects["wiki/daily/b.wwbb"]; !ok {
		t.Errorf("objects %v", fake.objects)
	}

	env[SecretKeyEnv] = "WRONG"
	tg, _ = Open(addr, envOf(env))
	if _, err := tg.List(context.Background()); err == nil || !strings.Contains(err.Error(), "credentials") {
		t.Errorf("a wrong key: %v", err)
	}
	if err := tg.Put(context.Background(), "x.wwbb", strings.NewReader("x")); err == nil {
		t.Error("stored with a wrong key")
	}
	if err := tg.Put(context.Background(), "x.wwbb", bytes.NewReader(make([]byte, partSize+1))); err == nil {
		t.Error("a multipart upload with a wrong key")
	}
	delete(env, SecretKeyEnv)
	if _, err := Open(addr, envOf(env)); err == nil || !strings.Contains(err.Error(), SecretKeyEnv) {
		t.Errorf("without a key: %v", err)
	}
	// A bucket without a folder.
	env[SecretKeyEnv] = "SECRET"
	tg, _ = Open("s3://backups?endpoint="+url.QueryEscape(srv.URL)+"&region=eu-central-1", envOf(env))
	if err := tg.Put(context.Background(), "top.wwbb", strings.NewReader("x")); err != nil {
		t.Fatal(err)
	}
	if _, ok := fake.objects["top.wwbb"]; !ok {
		t.Errorf("objects %v", fake.objects)
	}
}
