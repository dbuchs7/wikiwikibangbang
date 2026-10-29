// Package credential reads the credentials the server sends to other
// systems, the process engines and the connectors, from the first place
// that sets them (ADR-0151): the variable of the credential's name, the
// file the variable <name>_FILE names, or the value an administrator
// stored on the administration pages. Each is read at each use, so a new
// credential takes effect without a restart.
//
// The environment, a variable or a file, takes precedence: what the
// operator sets is never overridden from the browser. A value or a file's
// content never appears in an error or a state, nor does a file's path.
package credential

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"time"
	"unicode"
)

// Source says where a credential comes from.
type Source string

// The places a credential comes from, in the order they are asked.
const (
	None     Source = ""
	Variable Source = "variable"
	File     Source = "file"
	Stored   Source = "stored"
)

// maxBytes bounds a credential, a token or an e-mail address, in a file
// and as stored.
const maxBytes = 8 << 10

var (
	// ErrUnreadable is a credential the environment sets in a way that
	// cannot be used. The stored value is not used in its place.
	ErrUnreadable = errors.New("credential: cannot be used")
	// ErrInvalid is a value that is not stored.
	ErrInvalid = errors.New("credential: invalid")
)

// failure is an error that says what is wrong in its own words and is
// one of the sentinel errors above.
type failure struct {
	is  error
	msg string
}

func (f failure) Error() string        { return f.msg }
func (f failure) Is(target error) bool { return target == f.is }

func failed(is error, format string, args ...any) error {
	return failure{is: is, msg: fmt.Sprintf(format, args...)}
}

// FileVar names the variable that names a credential's file.
func FileVar(name string) string { return name + "_FILE" }

// Entry is a stored credential: its value, and when and by whom it was
// stored.
type Entry struct {
	Value string
	At    time.Time
	By    string
}

// Store holds the credentials stored on the administration pages.
type Store interface {
	Credential(name string) (Entry, bool)
}

// Resolver reads credentials. It is safe for concurrent use if its store
// is.
type Resolver struct {
	getenv func(string) string
	store  Store
}

// New returns a resolver over the environment getenv reads (os.Getenv when
// nil) and the stored credentials, if store is not nil.
func New(getenv func(string) string, store Store) *Resolver {
	if getenv == nil {
		getenv = os.Getenv
	}
	return &Resolver{getenv: getenv, store: store}
}

// Lookup returns the credential name and where it came from, or "" and
// None when nothing sets it.
func (r *Resolver) Lookup(name string) (string, Source, error) {
	v, file := r.getenv(name), r.getenv(FileVar(name))
	switch {
	case v != "" && file != "":
		return "", None, failed(ErrUnreadable, "both %s and %s are set; set one of them", name, FileVar(name))
	case v != "":
		return v, Variable, nil
	case file != "":
		v, err := readFile(FileVar(name), file)
		if err != nil {
			return "", None, err
		}
		return v, File, nil
	}
	if r.store != nil {
		if e, ok := r.store.Credential(name); ok {
			return e.Value, Stored, nil
		}
	}
	return "", None, nil
}

// InEnvironment reports whether the operator sets the credential, as a
// variable or as a file, which a stored value then does not override.
func (r *Resolver) InEnvironment(name string) bool {
	return r.getenv(name) != "" || r.getenv(FileVar(name)) != ""
}

// State is what an administrator sees of a credential: its name, the
// variable that would name its file, where it comes from, what is wrong
// with it, and whether a value is stored, also when the environment
// overrides it. Never its value.
type State struct {
	Name    string      `json:"name"`
	FileVar string      `json:"fileVar"`
	Source  Source      `json:"source"`
	Error   string      `json:"error,omitempty"`
	Stored  *StoredInfo `json:"stored,omitempty"`
}

// StoredInfo says when and by whom a credential was stored.
type StoredInfo struct {
	At time.Time `json:"at"`
	By string    `json:"by"`
}

// Set reports whether the credential can be sent.
func (s State) Set() bool { return s.Source != None }

// State reports a credential's state.
func (r *Resolver) State(name string) State {
	s := State{Name: name, FileVar: FileVar(name)}
	_, source, err := r.Lookup(name)
	s.Source = source
	if err != nil {
		s.Error = err.Error()
	}
	if r.store != nil {
		if e, ok := r.store.Credential(name); ok {
			s.Stored = &StoredInfo{At: e.At, By: e.By}
		}
	}
	return s
}

// Clean returns a value without surrounding white space, or ErrInvalid
// when it is empty, longer than 8 KiB or holds a control character, a line
// break among them.
func Clean(value string) (string, error) {
	v, what := check(value)
	if what != "" {
		return "", failed(ErrInvalid, "a credential %s", what)
	}
	return v, nil
}

// check returns the value without surrounding white space, or what is
// wrong with it.
func check(value string) (string, string) {
	if len(value) > maxBytes {
		return "", "is longer than 8 KiB"
	}
	v := strings.TrimSpace(value)
	switch {
	case v == "":
		return "", "is empty"
	case strings.ContainsFunc(v, unicode.IsControl):
		return "", "holds more than one line or a control character"
	}
	return v, ""
}

// readFile reads the credential in the file at path, which the variable
// fileVar names. Its errors name the variable, never the path.
func readFile(fileVar, path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fileError(fileVar, err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return "", fileError(fileVar, err)
	}
	v, what := check(string(data))
	if what != "" {
		return "", failed(ErrUnreadable, "the file that %s names %s", fileVar, what)
	}
	return v, nil
}

func fileError(fileVar string, err error) error {
	what := "cannot be read"
	switch {
	case errors.Is(err, fs.ErrNotExist):
		what = "does not exist"
	case errors.Is(err, fs.ErrPermission):
		what = "may not be read by the server"
	}
	return failed(ErrUnreadable, "the file that %s names %s", fileVar, what)
}
