package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/credential"
	"github.com/dbuchs7/wikiwikibangbang/internal/site"
)

// storedCredentials are credentials stored on the administration page.
type storedCredentials map[string]string

func (s storedCredentials) Credential(name string) (credential.Entry, bool) {
	v, ok := s[name]
	return credential.Entry{Value: v}, ok
}

// A new credential takes effect at its next use, from a file or from the
// administration page, without a restart (ADR-0151).
func TestTheCredentialIsReadAtEachUse(t *testing.T) {
	srv, _ := fakeAtlas(t)
	e := site.Engine{ID: "atlas", Kind: site.EngineAtlas, BaseURL: srv.URL + "/base"}
	ctx := context.Background()

	t.Run("stored", func(t *testing.T) {
		stored := storedCredentials{"WWBB_ENGINE_ATLAS_TOKEN": "wrong"}
		r := NewReader(func(string) string { return "" }, srv.Client()).WithStore(stored)
		if _, err := r.Processes(ctx, e); !errors.Is(err, ErrRefused) {
			t.Fatalf("a wrong stored token: %v", err)
		}
		stored["WWBB_ENGINE_ATLAS_TOKEN"] = "secret"
		if _, err := r.Processes(ctx, e); err != nil {
			t.Fatalf("after the token was stored anew: %v", err)
		}
		if s := r.Credential("atlas"); s.Source != credential.Stored || !r.HasCredential("atlas") {
			t.Errorf("state %+v", s)
		}
	})

	t.Run("file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "token")
		if err := os.WriteFile(path, []byte("wrong\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		env := map[string]string{"WWBB_ENGINE_ATLAS_TOKEN_FILE": path}
		r := NewReader(func(k string) string { return env[k] }, srv.Client())
		if _, err := r.Processes(ctx, e); !errors.Is(err, ErrRefused) {
			t.Fatalf("a wrong token in the file: %v", err)
		}
		if err := os.WriteFile(path, []byte("secret\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := r.Processes(ctx, e); err != nil {
			t.Fatalf("after the file was replaced: %v", err)
		}
	})

	t.Run("the environment wins", func(t *testing.T) {
		env := map[string]string{"WWBB_ENGINE_ATLAS_TOKEN": "secret"}
		r := NewReader(func(k string) string { return env[k] }, srv.Client()).WithStore(storedCredentials{"WWBB_ENGINE_ATLAS_TOKEN": "wrong"})
		if _, err := r.Processes(ctx, e); err != nil {
			t.Fatalf("the variable is not sent before the stored value: %v", err)
		}
		if s := r.Credential("atlas"); s.Source != credential.Variable || s.Stored == nil {
			t.Errorf("state %+v", s)
		}
	})
}

func TestAMissingOrUnusableCredentialSaysWhatToSetAndNothingElse(t *testing.T) {
	srv, asked := fakeAtlas(t)
	e := site.Engine{ID: "atlas", Kind: site.EngineAtlas, BaseURL: srv.URL + "/base"}
	ctx := context.Background()

	r := NewReader(func(string) string { return "" }, srv.Client())
	_, err := r.Processes(ctx, e)
	if !errors.Is(err, ErrNoCredential) {
		t.Fatalf("no credential: %v", err)
	}
	for _, want := range []string{"WWBB_ENGINE_ATLAS_TOKEN", "WWBB_ENGINE_ATLAS_TOKEN_FILE", "administration page"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q does not name %q", err, want)
		}
	}

	dir := t.TempDir()
	env := map[string]string{"WWBB_ENGINE_ATLAS_TOKEN_FILE": filepath.Join(dir, "missing")}
	r = NewReader(func(k string) string { return env[k] }, srv.Client()).WithStore(storedCredentials{"WWBB_ENGINE_ATLAS_TOKEN": "secret"})
	_, err = r.Processes(ctx, e)
	if !errors.Is(err, ErrNoCredential) || !errors.Is(err, credential.ErrUnreadable) || strings.Contains(err.Error(), dir) {
		t.Fatalf("an unreadable file: %v; want it said, without its path and without falling back to the stored value", err)
	}
	if len(*asked) != 0 {
		t.Errorf("the engine was asked without a credential: %v", *asked)
	}
}
