package dest

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"path"
	"strconv"
	"strings"
)

// ftps is a directory on an FTP server reached with explicit TLS (AUTH
// TLS on port 21), with the data connections encrypted as well (PROT P).
// The user is in the address or UsernameEnv, the password in PasswordEnv.
// Each call opens its own session.
type ftps struct {
	addr, dir  string
	user, pass string
	tls        *tls.Config
}

func newFTPS(a address, env Env) (Target, error) {
	user := a.url.User.Username()
	if user == "" {
		user = env(UsernameEnv)
	}
	if user == "" {
		return nil, fmt.Errorf("ftps: name the user in the address, ftps://user@host/folder, or in %s", UsernameEnv)
	}
	cfg, err := tlsConfig(env, a.url.Hostname())
	if err != nil {
		return nil, err
	}
	// Many servers require the data connections to resume the control
	// connection's TLS session.
	cfg.ClientSessionCache = tls.NewLRUClientSessionCache(4)
	port := a.url.Port()
	if port == "" {
		port = "21"
	}
	return &ftps{addr: net.JoinHostPort(a.url.Hostname(), port), dir: a.dir, user: user, pass: env(PasswordEnv), tls: cfg}, nil
}

// ftpConn is one session.
type ftpConn struct {
	f    *ftps
	conn net.Conn
	text *textproto.Conn
	stop func() bool
}

func (f *ftps) session(ctx context.Context) (*ftpConn, error) {
	d := net.Dialer{Timeout: dialTimeout}
	raw, err := d.DialContext(ctx, "tcp", f.addr)
	if err != nil {
		return nil, fmt.Errorf("ftps: connecting to %s: %w", f.addr, err)
	}
	c := &ftpConn{f: f, conn: raw, text: textproto.NewConn(raw), stop: ctxConn(ctx, raw)}
	fail := func(err error) (*ftpConn, error) {
		c.close()
		return nil, err
	}
	if _, _, err := c.text.ReadResponse(220); err != nil {
		return fail(fmt.Errorf("ftps: greeting: %w", err))
	}
	if _, err := c.cmd(234, "AUTH TLS"); err != nil {
		return fail(fmt.Errorf("ftps: the server does not offer TLS (AUTH TLS): %w", err))
	}
	tc := tls.Client(raw, f.tls)
	if err := tc.HandshakeContext(ctx); err != nil {
		return fail(fmt.Errorf("ftps: TLS: %w", err))
	}
	c.conn, c.text = tc, textproto.NewConn(tc)
	code, err := c.cmd(0, "USER %s", f.user)
	if err == nil && code == 331 {
		_, err = c.cmd(230, "PASS %s", f.pass)
	} else if err == nil && code != 230 {
		err = fmt.Errorf("unexpected answer %d", code)
	}
	if err != nil {
		return fail(fmt.Errorf("ftps: signing in as %s: %w", f.user, err))
	}
	for _, step := range []struct {
		code int
		cmd  string
	}{{200, "PBSZ 0"}, {200, "PROT P"}, {200, "TYPE I"}, {250, "CWD " + f.dir}} {
		if _, err := c.cmd(step.code, "%s", step.cmd); err != nil {
			return fail(fmt.Errorf("ftps: %s: %w", step.cmd, err))
		}
	}
	return c, nil
}

// cmd sends a command and reads its answer; expect 0 takes any
// non-error answer.
func (c *ftpConn) cmd(expect int, format string, args ...any) (int, error) {
	if _, err := c.text.Cmd(format, args...); err != nil {
		return 0, err
	}
	code, msg, err := c.text.ReadResponse(expect)
	if expect == 0 && err == nil && code >= 400 {
		err = &textproto.Error{Code: code, Msg: msg}
	}
	return code, err
}

func (c *ftpConn) close() {
	if c.text != nil {
		c.text.Cmd("QUIT")
	}
	c.stop()
	c.conn.Close()
}

// data opens a data connection for a command: passive mode, EPSV first,
// then PASV; the connection is encrypted and resumes the session.
func (c *ftpConn) data(ctx context.Context, format string, args ...any) (net.Conn, error) {
	host, _, _ := net.SplitHostPort(c.f.addr)
	var port string
	if _, msg, err := c.cmdMsg(229, "EPSV"); err == nil {
		// 229 Entering Extended Passive Mode (|||port|)
		i, j := strings.Index(msg, "(|||"), strings.LastIndex(msg, "|)")
		if i < 0 || j <= i+4 {
			return nil, fmt.Errorf("ftps: unreadable EPSV answer %q", msg)
		}
		port = msg[i+4 : j]
	} else {
		_, msg, err := c.cmdMsg(227, "PASV")
		if err != nil {
			return nil, fmt.Errorf("ftps: passive mode: %w", err)
		}
		// 227 Entering Passive Mode (h1,h2,h3,h4,p1,p2); the address is
		// ignored, as a server behind NAT often names an inner one.
		i, j := strings.Index(msg, "("), strings.LastIndex(msg, ")")
		f := strings.Split(msg[max(i+1, 0):max(j, 0)], ",")
		if i < 0 || j < i || len(f) != 6 {
			return nil, fmt.Errorf("ftps: unreadable PASV answer %q", msg)
		}
		p1, e1 := strconv.Atoi(strings.TrimSpace(f[4]))
		p2, e2 := strconv.Atoi(strings.TrimSpace(f[5]))
		if e1 != nil || e2 != nil {
			return nil, fmt.Errorf("ftps: unreadable PASV answer %q", msg)
		}
		port = strconv.Itoa(p1<<8 | p2)
	}
	d := net.Dialer{Timeout: dialTimeout}
	raw, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return nil, fmt.Errorf("ftps: data connection: %w", err)
	}
	if _, err := c.cmd(0, format, args...); err != nil {
		raw.Close()
		return nil, err
	}
	tc := tls.Client(raw, c.f.tls)
	if err := tc.HandshakeContext(ctx); err != nil {
		raw.Close()
		return nil, fmt.Errorf("ftps: TLS on the data connection: %w", err)
	}
	return tc, nil
}

func (c *ftpConn) cmdMsg(expect int, cmd string) (int, string, error) {
	if _, err := c.text.Cmd("%s", cmd); err != nil {
		return 0, "", err
	}
	return c.text.ReadResponse(expect)
}

// finish ends a transfer: the data connection is closed and the server
// confirms it.
func (c *ftpConn) finish(dc net.Conn) error {
	err := dc.Close()
	if _, _, rerr := c.text.ReadResponse(226); err == nil {
		err = rerr
	}
	return err
}

func (f *ftps) Put(ctx context.Context, name string, r io.Reader) error {
	if err := checkName(name); err != nil {
		return err
	}
	c, err := f.session(ctx)
	if err != nil {
		return err
	}
	defer c.close()
	part := name + partSuffix
	err = func() error {
		dc, err := c.data(ctx, "STOR %s", part)
		if err != nil {
			return fmt.Errorf("ftps: storing %s: %w", name, err)
		}
		_, err = io.Copy(dc, readerCtx{ctx, r})
		if ferr := c.finish(dc); err == nil {
			err = ferr
		}
		if err != nil {
			return fmt.Errorf("ftps: storing %s: %w", name, err)
		}
		if _, err := c.cmd(350, "RNFR %s", part); err != nil {
			return fmt.Errorf("ftps: renaming %s: %w", part, err)
		}
		if _, err := c.cmd(250, "RNTO %s", name); err != nil {
			return fmt.Errorf("ftps: renaming %s: %w", part, err)
		}
		return nil
	}()
	if err != nil {
		if c2, serr := f.session(context.WithoutCancel(ctx)); serr == nil {
			c2.cmd(0, "DELE %s", part)
			c2.close()
		}
	}
	return err
}

func (f *ftps) List(ctx context.Context) ([]string, error) {
	c, err := f.session(ctx)
	if err != nil {
		return nil, err
	}
	defer c.close()
	dc, err := c.data(ctx, "NLST")
	if err != nil {
		// An empty directory: some servers answer 450 or 550 to NLST.
		var perr *textproto.Error
		if errors.As(err, &perr) && (perr.Code == 450 || perr.Code == 550) {
			return nil, nil
		}
		return nil, fmt.Errorf("ftps: listing: %w", err)
	}
	data, err := io.ReadAll(io.LimitReader(dc, maxListing))
	if ferr := c.finish(dc); err == nil {
		err = ferr
	}
	if err != nil {
		return nil, fmt.Errorf("ftps: listing: %w", err)
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		if name := path.Base(strings.TrimSpace(line)); checkName(name) == nil {
			out = append(out, name)
		}
	}
	return out, nil
}

func (f *ftps) Open(ctx context.Context, name string) (io.ReadCloser, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	c, err := f.session(ctx)
	if err != nil {
		return nil, err
	}
	dc, err := c.data(ctx, "RETR %s", name)
	if err != nil {
		c.close()
		return nil, fmt.Errorf("ftps: reading %s: %w", name, err)
	}
	return &ftpReader{c: c, dc: dc}, nil
}

type ftpReader struct {
	c  *ftpConn
	dc net.Conn
}

func (r *ftpReader) Read(p []byte) (int, error) { return r.dc.Read(p) }

func (r *ftpReader) Close() error {
	err := r.c.finish(r.dc)
	r.c.close()
	return err
}

func (f *ftps) Delete(ctx context.Context, name string) error {
	if err := checkName(name); err != nil {
		return err
	}
	c, err := f.session(ctx)
	if err != nil {
		return err
	}
	defer c.close()
	if _, err := c.cmd(250, "DELE %s", name); err != nil {
		return fmt.Errorf("ftps: deleting %s: %w", name, err)
	}
	return nil
}
