package dest

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"strings"

	"golang.org/x/crypto/ssh"
)

// sftp is a directory on an SSH server, reached with SFTP (version 3).
// The user is in the address or UsernameEnv; it signs in with the
// private key in the file SSHKeyEnv names, or the password in PasswordEnv.
// The server must present the host key whose SHA-256 fingerprint
// HostKeyEnv holds; without it the connection is refused, and the error
// names the fingerprint the server presented, to be checked and set.
type sftp struct {
	addr, dir string
	config    *ssh.ClientConfig
}

func newSFTP(a address, env Env) (Target, error) {
	user := a.url.User.Username()
	if user == "" {
		user = env(UsernameEnv)
	}
	if user == "" {
		return nil, fmt.Errorf("sftp: name the user in the address, sftp://user@host/folder, or in %s", UsernameEnv)
	}
	var auth []ssh.AuthMethod
	if file := strings.TrimSpace(env(SSHKeyEnv)); file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("sftp: %s: %w", SSHKeyEnv, err)
		}
		key, err := ssh.ParsePrivateKey(data)
		if err != nil {
			return nil, fmt.Errorf("sftp: %s: %w (a key protected by a passphrase is not supported)", SSHKeyEnv, err)
		}
		auth = append(auth, ssh.PublicKeys(key))
	}
	if pass := env(PasswordEnv); pass != "" {
		auth = append(auth, ssh.Password(pass))
	}
	if len(auth) == 0 {
		return nil, fmt.Errorf("sftp: set %s to a private key file or %s to the password", SSHKeyEnv, PasswordEnv)
	}
	want := strings.TrimSpace(env(HostKeyEnv))
	port := a.url.Port()
	if port == "" {
		port = "22"
	}
	return &sftp{addr: net.JoinHostPort(a.url.Hostname(), port), dir: a.dir, config: &ssh.ClientConfig{
		User: user,
		Auth: auth,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			got := ssh.FingerprintSHA256(key)
			if want == "" {
				return fmt.Errorf("sftp: %s is not set; the server presents the host key %s — set it after checking that this is your server's key", HostKeyEnv, got)
			}
			if got != want {
				return fmt.Errorf("sftp: the server presents the host key %s, not %s from %s", got, want, HostKeyEnv)
			}
			return nil
		},
		Timeout: dialTimeout,
	}}, nil
}

// The SFTP packet types and flags used (draft-ietf-secsh-filexfer-02).
const (
	fxpInit     = 1
	fxpVersion  = 2
	fxpOpen     = 3
	fxpClose    = 4
	fxpRead     = 5
	fxpWrite    = 6
	fxpOpendir  = 11
	fxpReaddir  = 12
	fxpRemove   = 13
	fxpRename   = 18
	fxpStatus   = 101
	fxpHandle   = 102
	fxpData     = 103
	fxpName     = 104
	fxfRead     = 0x01
	fxfWrite    = 0x02
	fxfCreat    = 0x08
	fxfTrunc    = 0x10
	fxOK        = 0
	fxEOF       = 1
	fxNoSuch    = 2
	attrSize    = 0x1
	attrUIDGID  = 0x2
	attrPerm    = 0x4
	attrTime    = 0x8
	attrExtend  = 0x80000000
	sftpChunk   = 32 << 10
	sftpPending = 16 // writes in flight
	// sftpPacket bounds a packet the server sends: a chunk with its header,
	// or a directory listing page.
	sftpPacket = 1 << 20
)

// sftpConn is one SFTP session over one SSH connection.
type sftpConn struct {
	client *ssh.Client
	in     io.WriteCloser
	out    io.Reader
	next   uint32
	stop   func() bool
}

func (s *sftp) session(ctx context.Context) (*sftpConn, error) {
	d := net.Dialer{Timeout: dialTimeout}
	raw, err := d.DialContext(ctx, "tcp", s.addr)
	if err != nil {
		return nil, fmt.Errorf("sftp: connecting to %s: %w", s.addr, err)
	}
	stop := ctxConn(ctx, raw)
	conn, chans, reqs, err := ssh.NewClientConn(raw, s.addr, s.config)
	if err != nil {
		stop()
		raw.Close()
		return nil, fmt.Errorf("sftp: %w", err)
	}
	client := ssh.NewClient(conn, chans, reqs)
	c := &sftpConn{client: client, stop: stop}
	sess, err := client.NewSession()
	if err == nil {
		c.in, err = sess.StdinPipe()
	}
	if err == nil {
		c.out, err = sess.StdoutPipe()
	}
	if err == nil {
		err = sess.RequestSubsystem("sftp")
	}
	if err == nil {
		err = c.send(fxpInit, nil, u32(3))
	}
	if err == nil {
		var typ byte
		typ, _, err = c.recv()
		if err == nil && typ != fxpVersion {
			err = fmt.Errorf("unexpected packet %d", typ)
		}
	}
	if err != nil {
		c.close()
		return nil, fmt.Errorf("sftp: starting the session: %w", err)
	}
	return c, nil
}

func (c *sftpConn) close() {
	c.client.Close()
	c.stop()
}

func u32(v uint32) []byte { return binary.BigEndian.AppendUint32(nil, v) }
func u64(v uint64) []byte { return binary.BigEndian.AppendUint64(nil, v) }
func str(s string) []byte { return append(u32(uint32(len(s))), s...) }

// send writes a packet; id is nil for the init packet.
func (c *sftpConn) send(typ byte, id []byte, parts ...[]byte) error {
	n := 1 + len(id)
	for _, p := range parts {
		n += len(p)
	}
	buf := make([]byte, 0, 4+n)
	buf = append(buf, u32(uint32(n))...)
	buf = append(buf, typ)
	buf = append(buf, id...)
	for _, p := range parts {
		buf = append(buf, p...)
	}
	_, err := c.in.Write(buf)
	return err
}

func (c *sftpConn) recv() (byte, []byte, error) {
	var hdr [5]byte
	if _, err := io.ReadFull(c.out, hdr[:]); err != nil {
		return 0, nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:4])
	if n < 1 || n > sftpPacket {
		return 0, nil, fmt.Errorf("a packet of %d bytes", n)
	}
	body := make([]byte, n-1)
	if _, err := io.ReadFull(c.out, body); err != nil {
		return 0, nil, err
	}
	return hdr[4], body, nil
}

// request sends a request and returns its answer; the id comes first in
// both.
func (c *sftpConn) request(typ byte, parts ...[]byte) (byte, []byte, error) {
	id := c.id()
	if err := c.send(typ, u32(id), parts...); err != nil {
		return 0, nil, err
	}
	rt, body, err := c.recv()
	if err != nil {
		return 0, nil, err
	}
	if len(body) < 4 || binary.BigEndian.Uint32(body) != id {
		return 0, nil, errors.New("an answer to another request")
	}
	return rt, body[4:], nil
}

func (c *sftpConn) id() uint32 {
	c.next++
	return c.next
}

// sftpStatus is an error the server reported.
type sftpStatus struct {
	code uint32
	msg  string
}

func (e *sftpStatus) Error() string {
	if e.msg != "" {
		return fmt.Sprintf("status %d: %s", e.code, e.msg)
	}
	return fmt.Sprintf("status %d", e.code)
}

func status(body []byte) error {
	if len(body) < 4 {
		return errors.New("a short status")
	}
	code := binary.BigEndian.Uint32(body)
	if code == fxOK {
		return nil
	}
	msg, _, _ := readString(body[4:])
	return &sftpStatus{code: code, msg: msg}
}

func readString(b []byte) (string, []byte, bool) {
	if len(b) < 4 {
		return "", nil, false
	}
	n := binary.BigEndian.Uint32(b)
	if uint32(len(b)-4) < n {
		return "", nil, false
	}
	return string(b[4 : 4+n]), b[4+n:], true
}

// expect reads the answer of a request that answers with a status, or
// with a packet of type want.
func expect(rt byte, body []byte, err error, want byte) ([]byte, error) {
	if err != nil {
		return nil, err
	}
	if rt == fxpStatus {
		if err := status(body); err != nil || want != fxpStatus {
			if err == nil {
				err = errors.New("an unexpected status")
			}
			return nil, err
		}
		return nil, nil
	}
	if rt != want {
		return nil, fmt.Errorf("unexpected packet %d", rt)
	}
	return body, nil
}

func (c *sftpConn) open(p string, flags uint32) (string, error) {
	rt, body, err := c.request(fxpOpen, str(p), u32(flags), u32(0))
	body, err = expect(rt, body, err, fxpHandle)
	if err != nil {
		return "", err
	}
	h, _, ok := readString(body)
	if !ok {
		return "", errors.New("an unreadable handle")
	}
	return h, nil
}

func (c *sftpConn) closeHandle(h string) error { return c.simple(fxpClose, str(h)) }

func (c *sftpConn) simple(typ byte, parts ...[]byte) error {
	rt, body, err := c.request(typ, parts...)
	_, err = expect(rt, body, err, fxpStatus)
	return err
}

func (s *sftp) file(name string) string { return path.Join(s.dir, name) }

func (s *sftp) Put(ctx context.Context, name string, r io.Reader) error {
	if err := checkName(name); err != nil {
		return err
	}
	c, err := s.session(ctx)
	if err != nil {
		return err
	}
	defer c.close()
	part := s.file(name + partSuffix)
	err = func() error {
		h, err := c.open(part, fxfWrite|fxfCreat|fxfTrunc)
		if err != nil {
			return fmt.Errorf("sftp: creating %s: %w", part, err)
		}
		werr := c.write(h, readerCtx{ctx, r})
		if cerr := c.closeHandle(h); werr == nil {
			werr = cerr
		}
		if werr != nil {
			return fmt.Errorf("sftp: storing %s: %w", name, werr)
		}
		// Version 3 does not rename over an existing file.
		if err := c.simple(fxpRemove, str(s.file(name))); err != nil {
			var st *sftpStatus
			if !errors.As(err, &st) || st.code != fxNoSuch {
				return fmt.Errorf("sftp: replacing %s: %w", name, err)
			}
		}
		if err := c.simple(fxpRename, str(part), str(s.file(name))); err != nil {
			return fmt.Errorf("sftp: renaming %s: %w", part, err)
		}
		return nil
	}()
	if err != nil {
		c.simple(fxpRemove, str(part))
	}
	return err
}

// write sends r in chunks, with several writes in flight.
func (c *sftpConn) write(h string, r io.Reader) error {
	pending := map[uint32]bool{}
	settle := func() error {
		_, body, err := c.recv()
		if err != nil {
			return err
		}
		if len(body) < 4 || !pending[binary.BigEndian.Uint32(body)] {
			return errors.New("an answer to another request")
		}
		delete(pending, binary.BigEndian.Uint32(body))
		return status(body[4:])
	}
	buf := make([]byte, sftpChunk)
	var off uint64
	for {
		n, rerr := io.ReadFull(r, buf)
		if n > 0 {
			for len(pending) >= sftpPending {
				if err := settle(); err != nil {
					return err
				}
			}
			id := c.id()
			if err := c.send(fxpWrite, u32(id), str(h), u64(off), str(string(buf[:n]))); err != nil {
				return err
			}
			pending[id] = true
			off += uint64(n)
		}
		if errors.Is(rerr, io.EOF) || errors.Is(rerr, io.ErrUnexpectedEOF) {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	for len(pending) > 0 {
		if err := settle(); err != nil {
			return err
		}
	}
	return nil
}

func (s *sftp) List(ctx context.Context) ([]string, error) {
	c, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	defer c.close()
	rt, body, err := c.request(fxpOpendir, str(s.dir))
	body, err = expect(rt, body, err, fxpHandle)
	if err != nil {
		return nil, fmt.Errorf("sftp: opening %s: %w", s.dir, err)
	}
	h, _, ok := readString(body)
	if !ok {
		return nil, errors.New("sftp: an unreadable handle")
	}
	defer c.closeHandle(h)
	var out []string
	for {
		rt, body, err := c.request(fxpReaddir, str(h))
		if err == nil && rt == fxpStatus {
			var st *sftpStatus
			if err := status(body); errors.As(err, &st) && st.code == fxEOF {
				return out, nil
			} else if err != nil {
				return nil, fmt.Errorf("sftp: listing %s: %w", s.dir, err)
			}
		}
		body, err = expect(rt, body, err, fxpName)
		if err != nil {
			return nil, fmt.Errorf("sftp: listing %s: %w", s.dir, err)
		}
		names, err := parseNames(body)
		if err != nil {
			return nil, fmt.Errorf("sftp: listing %s: %w", s.dir, err)
		}
		for _, n := range names {
			if checkName(n) == nil {
				out = append(out, n)
			}
		}
	}
}

// parseNames reads a NAME packet, keeping regular files.
func parseNames(b []byte) ([]string, error) {
	bad := errors.New("an unreadable listing")
	if len(b) < 4 {
		return nil, bad
	}
	count := binary.BigEndian.Uint32(b)
	b = b[4:]
	var out []string
	for range count {
		name, rest, ok := readString(b)
		if !ok {
			return nil, bad
		}
		if _, rest, ok = readString(rest); !ok {
			return nil, bad
		}
		if len(rest) < 4 {
			return nil, bad
		}
		flags := binary.BigEndian.Uint32(rest)
		rest = rest[4:]
		regular := true
		for _, f := range []struct {
			bit  uint32
			size int
		}{{attrSize, 8}, {attrUIDGID, 8}, {attrPerm, 4}, {attrTime, 8}} {
			if flags&f.bit == 0 {
				continue
			}
			if len(rest) < f.size {
				return nil, bad
			}
			if f.bit == attrPerm {
				regular = binary.BigEndian.Uint32(rest)&0o170000 == 0o100000
			}
			rest = rest[f.size:]
		}
		if flags&attrExtend != 0 {
			if len(rest) < 4 {
				return nil, bad
			}
			n := binary.BigEndian.Uint32(rest)
			rest = rest[4:]
			for range 2 * n {
				if _, rest, ok = readString(rest); !ok {
					return nil, bad
				}
			}
		}
		b = rest
		if regular {
			out = append(out, name)
		}
	}
	return out, nil
}

func (s *sftp) Open(ctx context.Context, name string) (io.ReadCloser, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	c, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	h, err := c.open(s.file(name), fxfRead)
	if err != nil {
		c.close()
		return nil, fmt.Errorf("sftp: opening %s: %w", name, err)
	}
	return &sftpReader{c: c, h: h}, nil
}

type sftpReader struct {
	c   *sftpConn
	h   string
	off uint64
	buf []byte
	eof bool
}

func (r *sftpReader) Read(p []byte) (int, error) {
	if len(r.buf) == 0 && !r.eof {
		rt, body, err := r.c.request(fxpRead, str(r.h), u64(r.off), u32(sftpChunk))
		if err == nil && rt == fxpStatus {
			var st *sftpStatus
			if err := status(body); errors.As(err, &st) && st.code == fxEOF {
				r.eof = true
			} else if err != nil {
				return 0, err
			}
		} else {
			body, err = expect(rt, body, err, fxpData)
			if err != nil {
				return 0, err
			}
			data, _, ok := readString(body)
			if !ok {
				return 0, errors.New("sftp: unreadable data")
			}
			r.buf, r.off = []byte(data), r.off+uint64(len(data))
		}
	}
	if len(r.buf) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}

func (r *sftpReader) Close() error {
	err := r.c.closeHandle(r.h)
	r.c.close()
	return err
}

func (s *sftp) Delete(ctx context.Context, name string) error {
	if err := checkName(name); err != nil {
		return err
	}
	c, err := s.session(ctx)
	if err != nil {
		return err
	}
	defer c.close()
	if err := c.simple(fxpRemove, str(s.file(name))); err != nil {
		return fmt.Errorf("sftp: deleting %s: %w", name, err)
	}
	return nil
}
