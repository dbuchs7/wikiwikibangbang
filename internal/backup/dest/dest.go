// Package dest stores backups at a destination (ADR-0073): a directory of
// the server, which may be a mounted file server or NAS.
//
// A destination is set by an address that holds no secret; the
// credentials a destination needs come from the server's environment, so
// they are in neither the data nor its backups.
package dest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Target is where backups are stored.
type Target interface {
	// Put stores r under name; a backup that fails half way is not left
	// under name.
	Put(ctx context.Context, name string, r io.Reader) error
	// List names the files stored.
	List(ctx context.Context) ([]string, error)
	// Open reads a stored file.
	Open(ctx context.Context, name string) (io.ReadCloser, error)
	// Delete removes a stored file.
	Delete(ctx context.Context, name string) error
}

// ErrInvalid is an address that names no destination this server can use.
var ErrInvalid = errors.New("invalid destination")

// Env reads the server's environment.
type Env func(string) string

// Check tells whether an address names a destination, without reaching it.
func Check(destination string) error {
	_, err := parse(destination)
	return err
}

// Open opens the destination an address names, with the credentials env
// holds.
func Open(destination string, env Env) (Target, error) {
	path, err := parse(destination)
	if err != nil {
		return nil, err
	}
	return local{dir: path}, nil
}

// Directory is the directory of the server an address names, if it names
// one.
func Directory(destination string) (string, bool) {
	path, err := parse(destination)
	return path, err == nil
}

func parse(destination string) (string, error) {
	d := strings.TrimSpace(destination)
	if d == "" {
		return "", fmt.Errorf("%w: no destination is set", ErrInvalid)
	}
	if strings.HasPrefix(d, "file://") {
		u, err := url.Parse(d)
		if err != nil || u.Host != "" && u.Host != "localhost" {
			return "", fmt.Errorf("%w: a file address names a directory of this server, file:///path", ErrInvalid)
		}
		d = u.Path
	}
	if u, err := url.Parse(d); err == nil && u.Scheme != "" && len(u.Scheme) > 1 {
		if _, ok := u.User.Password(); ok {
			return "", fmt.Errorf("%w: the address holds a password; set it in the server's environment instead", ErrInvalid)
		}
		return "", fmt.Errorf("%w: %s is not a kind of destination this server knows", ErrInvalid, u.Scheme)
	}
	if !filepath.IsAbs(d) {
		return "", fmt.Errorf("%w: a directory is given by its absolute path", ErrInvalid)
	}
	return filepath.Clean(d), nil
}

// validName keeps a stored file inside its destination.
var validName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func checkName(name string) error {
	if !validName.MatchString(name) || strings.HasSuffix(name, partSuffix) {
		return fmt.Errorf("dest: %q is not a name a backup is stored under", name)
	}
	return nil
}

// partSuffix marks a file while it is written.
const partSuffix = ".part"

// local is a directory of the server. It must exist: a missing mount
// should fail the backup, not fill the server's own disk.
type local struct{ dir string }

func (l local) ready() error {
	fi, err := os.Stat(l.dir)
	if err != nil {
		return fmt.Errorf("dest: the directory %s: %w", l.dir, err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("dest: %s is not a directory", l.dir)
	}
	return nil
}

func (l local) Put(ctx context.Context, name string, r io.Reader) error {
	if err := checkName(name); err != nil {
		return err
	}
	if err := l.ready(); err != nil {
		return err
	}
	path := filepath.Join(l.dir, name)
	f, err := os.OpenFile(path+partSuffix, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, readerCtx{ctx, r})
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(path+partSuffix, path)
	}
	if err != nil {
		os.Remove(path + partSuffix)
	}
	return err
}

func (l local) List(ctx context.Context) ([]string, error) {
	if err := l.ready(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.Type().IsRegular() && checkName(e.Name()) == nil {
			out = append(out, e.Name())
		}
	}
	return out, nil
}

func (l local) Open(ctx context.Context, name string) (io.ReadCloser, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	return os.Open(filepath.Join(l.dir, name))
}

func (l local) Delete(ctx context.Context, name string) error {
	if err := checkName(name); err != nil {
		return err
	}
	return os.Remove(filepath.Join(l.dir, name))
}

// readerCtx stops a copy when its context ends.
type readerCtx struct {
	ctx context.Context
	r   io.Reader
}

func (r readerCtx) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}
