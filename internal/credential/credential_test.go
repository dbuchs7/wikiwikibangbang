package credential

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// env is an environment for tests.
type env map[string]string

func (e env) get(name string) string { return e[name] }

// stored is a store of credentials for tests.
type stored map[string]Entry

func (s stored) Credential(name string) (Entry, bool) {
	e, ok := s[name]
	return e, ok
}

const name = "WWBB_ENGINE_ATLAS_TOKEN"

func writeFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTheFirstPlaceThatSetsACredentialWins(t *testing.T) {
	file := writeFile(t, "from-file\n")
	saved := stored{name: {Value: "from-page", At: time.Unix(1, 0), By: "admin"}}
	for _, c := range []struct {
		about  string
		env    env
		stored stored
		value  string
		source Source
	}{
		{"nothing sets it", env{}, stored{}, "", None},
		{"the variable", env{name: "from-variable"}, saved, "from-variable", Variable},
		{"the file", env{name + "_FILE": file}, saved, "from-file", File},
		{"the stored value", env{}, saved, "from-page", Stored},
	} {
		t.Run(c.about, func(t *testing.T) {
			r := New(c.env.get, c.stored)
			value, source, err := r.Lookup(name)
			if err != nil || value != c.value || source != c.source {
				t.Fatalf("Lookup = %q, %q, %v; want %q, %q", value, source, err, c.value, c.source)
			}
		})
	}
}

func TestAFileIsReadAtEachUse(t *testing.T) {
	file := writeFile(t, "first")
	r := New(env{name + "_FILE": file}.get, nil)
	if v, _, err := r.Lookup(name); err != nil || v != "first" {
		t.Fatalf("Lookup = %q, %v", v, err)
	}
	if err := os.WriteFile(file, []byte("  second\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if v, _, err := r.Lookup(name); err != nil || v != "second" {
		t.Fatalf("after the file changed, Lookup = %q, %v; want the new content without surrounding white space", v, err)
	}
}

func TestWhatIsWrongWithACredentialIsSaidWithoutItsValueOrPath(t *testing.T) {
	dir := t.TempDir()
	long := writeFile(t, strings.Repeat("x", maxBytes+1))
	for _, c := range []struct {
		about string
		env   env
		want  string
	}{
		{"both set", env{name: "a", name + "_FILE": writeFile(t, "b")}, "both " + name + " and " + name + "_FILE are set"},
		{"no such file", env{name + "_FILE": filepath.Join(dir, "missing")}, "does not exist"},
		{"empty file", env{name + "_FILE": writeFile(t, " \n")}, "is empty"},
		{"too long", env{name + "_FILE": long}, "longer than 8 KiB"},
		{"two lines", env{name + "_FILE": writeFile(t, "secret-one\nsecret-two")}, "control character"},
		{"a directory", env{name + "_FILE": dir}, "cannot be read"},
	} {
		t.Run(c.about, func(t *testing.T) {
			r := New(c.env.get, stored{name: {Value: "from-page"}})
			v, _, err := r.Lookup(name)
			if err == nil || v != "" {
				t.Fatalf("Lookup = %q, %v; want an error and no value: the stored value is not used when the environment fails", v, err)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q does not say %q", err, c.want)
			}
			if !errors.Is(err, ErrUnreadable) {
				t.Errorf("error %v is not ErrUnreadable", err)
			}
			for _, leak := range []string{"secret-one", "from-page", dir} {
				if strings.Contains(err.Error(), leak) {
					t.Errorf("error %q names %q", err, leak)
				}
			}
		})
	}
}

func TestTheStateSaysWhereACredentialComesFromNeverItsValue(t *testing.T) {
	at := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)
	saved := stored{name: {Value: "from-page", At: at, By: "admin"}}

	s := New(env{}.get, saved).State(name)
	if s.Name != name || s.FileVar != name+"_FILE" || s.Source != Stored || s.Error != "" ||
		s.Stored == nil || !s.Stored.At.Equal(at) || s.Stored.By != "admin" {
		t.Errorf("stored state = %+v", s)
	}

	s = New(env{name: "from-variable"}.get, saved).State(name)
	if s.Source != Variable || s.Stored == nil {
		t.Errorf("a variable over a stored value: %+v; want the variable, the stored value still reported", s)
	}

	s = New(env{name: "a", name + "_FILE": "b"}.get, nil).State(name)
	if s.Source != None || s.Error == "" {
		t.Errorf("both set: %+v; want no source and the error", s)
	}

	s = New(env{}.get, nil).State(name)
	if s.Source != None || s.Error != "" || s.Stored != nil {
		t.Errorf("nothing set: %+v", s)
	}
	if s.Set() {
		t.Error("a credential nothing sets is reported as set")
	}
}

func TestInTheEnvironmentReportsWhetherTheOperatorSetsIt(t *testing.T) {
	if !New(env{name: "x"}.get, nil).InEnvironment(name) || !New(env{name + "_FILE": "f"}.get, nil).InEnvironment(name) {
		t.Error("a variable or a file is the environment")
	}
	if New(env{}.get, stored{name: {Value: "v"}}).InEnvironment(name) {
		t.Error("a stored value is not the environment")
	}
}

func TestAValueIsCleanedAndChecked(t *testing.T) {
	if v, err := Clean("  token\n"); err != nil || v != "token" {
		t.Errorf("Clean = %q, %v", v, err)
	}
	for _, bad := range []string{"", "  ", "a\x00b", "line\nline", strings.Repeat("x", maxBytes+1)} {
		if _, err := Clean(bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("Clean(%q) = %v; want ErrInvalid", bad[:min(len(bad), 20)], err)
		}
	}
	if _, err := Clean(strings.Repeat("x", maxBytes)); err != nil {
		t.Errorf("a value of 8 KiB is refused: %v", err)
	}
}

func TestWithoutAStoreOnlyTheEnvironmentCounts(t *testing.T) {
	r := New(nil, nil)
	t.Setenv(name, "from-process")
	if v, src, err := r.Lookup(name); err != nil || v != "from-process" || src != Variable {
		t.Errorf("Lookup = %q, %q, %v; want os.Getenv by default", v, src, err)
	}
}
