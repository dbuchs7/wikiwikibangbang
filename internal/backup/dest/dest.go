// Package dest stores backups at a destination (ADR-0073): a directory of
// the server, which may be a mounted file server or NAS, a WebDAV folder,
// an S3-compatible bucket, or a directory on an FTPS or SFTP server.
//
// A destination is set by an address that holds no secret; the
// credentials a destination needs come from the server's environment, so
// they are in neither the data nor its backups. Only encrypted
// connections are used: plain HTTP and FTP are refused, because they would
// carry the password in the clear.
package dest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
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

// The environment variables that hold a destination's credentials.
const (
	UsernameEnv  = "WWBB_BACKUP_USERNAME"
	PasswordEnv  = "WWBB_BACKUP_PASSWORD"
	AccessKeyEnv = "WWBB_BACKUP_ACCESS_KEY"
	SecretKeyEnv = "WWBB_BACKUP_SECRET_KEY"
	SSHKeyEnv    = "WWBB_BACKUP_SSH_KEY"
	HostKeyEnv   = "WWBB_BACKUP_SSH_HOST_KEY"
	CAFileEnv    = "WWBB_BACKUP_CA_FILE"
)

// dialTimeout bounds connecting to a remote destination.
const dialTimeout = 30 * time.Second

// address is a parsed destination.
type address struct {
	kind string // "dir", "webdav", "s3", "ftps", "sftp"
	dir  string // the directory: local, or the path on a remote server
	url  *url.URL
}

// Check tells whether an address names a destination, without reaching it.
func Check(destination string) error {
	_, err := parse(destination)
	return err
}

// Open opens the destination an address names, with the credentials env
// holds.
func Open(destination string, env Env) (Target, error) {
	a, err := parse(destination)
	if err != nil {
		return nil, err
	}
	if env == nil {
		env = func(string) string { return "" }
	}
	switch a.kind {
	case "webdav":
		return newWebDAV(a, env)
	case "s3":
		return newS3(a, env)
	case "ftps":
		return newFTPS(a, env)
	case "sftp":
		return newSFTP(a, env)
	}
	return local{dir: a.dir}, nil
}

// Directory is the directory of the server an address names, if it names
// one.
func Directory(destination string) (string, bool) {
	a, err := parse(destination)
	return a.dir, err == nil && a.kind == "dir"
}

func parse(destination string) (address, error) {
	d := strings.TrimSpace(destination)
	if d == "" {
		return address{}, fmt.Errorf("%w: no destination is set", ErrInvalid)
	}
	if strings.HasPrefix(d, "file://") {
		u, err := url.Parse(d)
		if err != nil || u.Host != "" && u.Host != "localhost" {
			return address{}, fmt.Errorf("%w: a file address names a directory of this server, file:///path", ErrInvalid)
		}
		d = u.Path
	}
	if u, err := url.Parse(d); err == nil && u.Scheme != "" && len(u.Scheme) > 1 {
		return parseURL(u)
	}
	if !filepath.IsAbs(d) {
		return address{}, fmt.Errorf("%w: a directory is given by its absolute path; a remote destination by an address such as https://, s3://, ftps:// or sftp://", ErrInvalid)
	}
	return address{kind: "dir", dir: filepath.Clean(d)}, nil
}

func parseURL(u *url.URL) (address, error) {
	if _, ok := u.User.Password(); ok {
		return address{}, fmt.Errorf("%w: the address holds a password; set %s in the server's environment instead", ErrInvalid, PasswordEnv)
	}
	scheme := strings.ToLower(u.Scheme)
	switch scheme {
	case "http", "ftp":
		return address{}, fmt.Errorf("%w: %s is not encrypted and would carry the password in the clear; use https:// (WebDAV) or ftps://", ErrInvalid, scheme)
	case "https", "s3", "ftps", "sftp":
	default:
		return address{}, fmt.Errorf("%w: %s is not a kind of destination this server knows; use a directory, https:// (WebDAV), s3://, ftps:// or sftp://", ErrInvalid, u.Scheme)
	}
	if u.Hostname() == "" {
		return address{}, fmt.Errorf("%w: the address names no server", ErrInvalid)
	}
	a := address{kind: scheme, url: u}
	if scheme == "https" {
		a.kind = "webdav"
	}
	dir := path.Clean("/" + u.Path)
	switch a.kind {
	case "s3":
		if u.Query().Get("endpoint") == "" {
			return address{}, fmt.Errorf("%w: an s3 address names its endpoint, s3://bucket/folder?endpoint=https://s3.example.com&region=eu-central-1", ErrInvalid)
		}
		ep, err := url.Parse(u.Query().Get("endpoint"))
		if err != nil || ep.Scheme != "https" || ep.Host == "" {
			return address{}, fmt.Errorf("%w: the s3 endpoint is an https:// address", ErrInvalid)
		}
		dir = strings.Trim(path.Clean("/"+u.Path), "/")
	case "sftp", "ftps":
		if u.RawQuery != "" {
			return address{}, fmt.Errorf("%w: a %s address takes no parameters", ErrInvalid, scheme)
		}
	}
	a.dir = dir
	return a, nil
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
