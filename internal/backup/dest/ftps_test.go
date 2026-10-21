package dest

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeFTPS is an FTP server with explicit TLS over a directory, enough
// for what the target does; it requires TLS on every data connection.
type fakeFTPS struct {
	ln   net.Listener
	tls  *tls.Config
	dir  string
	epsv bool
	wg   sync.WaitGroup
}

func newFakeFTPS(t *testing.T, epsv bool) (*fakeFTPS, string) {
	t.Helper()
	srv := httptest.NewUnstartedServer(nil)
	srv.StartTLS()
	ca := caFile(t, srv)
	cfg := srv.TLS.Clone()
	srv.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeFTPS{ln: ln, tls: cfg, dir: t.TempDir(), epsv: epsv}
	os.Mkdir(filepath.Join(f.dir, "backups"), 0o700)
	go f.serve()
	t.Cleanup(func() { ln.Close(); f.wg.Wait() })
	return f, ca
}

func (f *fakeFTPS) serve() {
	for {
		c, err := f.ln.Accept()
		if err != nil {
			return
		}
		f.wg.Add(1)
		go func() { defer f.wg.Done(); f.session(c) }()
	}
}

func (f *fakeFTPS) session(raw net.Conn) {
	defer raw.Close()
	var conn net.Conn = raw
	r, w := bufio.NewReader(conn), conn
	say := func(format string, args ...any) { fmt.Fprintf(w, format+"\r\n", args...) }
	say("220 fake")
	cwd, from, user, authed, prot := f.dir, "", "", false, false
	var pasv net.Listener
	defer func() {
		if pasv != nil {
			pasv.Close()
		}
	}()
	data := func() net.Conn {
		if pasv == nil || !prot {
			say("425 no data connection")
			return nil
		}
		c, err := pasv.Accept()
		pasv.Close()
		pasv = nil
		if err != nil {
			return nil
		}
		say("150 here it comes")
		tc := tls.Server(c, f.tls)
		if tc.Handshake() != nil {
			c.Close()
			return nil
		}
		return tc
	}
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd, arg, _ := strings.Cut(strings.TrimRight(line, "\r\n"), " ")
		if !authed && cmd != "AUTH" && cmd != "USER" && cmd != "PASS" && cmd != "QUIT" {
			say("530 sign in first")
			continue
		}
		switch strings.ToUpper(cmd) {
		case "AUTH":
			say("234 go ahead")
			tc := tls.Server(raw, f.tls)
			if tc.Handshake() != nil {
				return
			}
			conn = tc
			r, w = bufio.NewReader(conn), conn
		case "USER":
			user = arg
			say("331 password")
		case "PASS":
			if user == "wiki" && arg == "secret" && conn != raw {
				authed = true
				say("230 in")
			} else {
				say("530 no")
			}
		case "PBSZ", "TYPE":
			say("200 ok")
		case "PROT":
			prot = arg == "P"
			say("200 ok")
		case "CWD":
			p := filepath.Join(f.dir, filepath.FromSlash(arg))
			if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
				say("550 no such directory")
				continue
			}
			cwd = p
			say("250 ok")
		case "EPSV", "PASV":
			if cmd == "EPSV" && !f.epsv {
				say("500 not understood")
				continue
			}
			pasv, _ = net.Listen("tcp", "127.0.0.1:0")
			port := pasv.Addr().(*net.TCPAddr).Port
			if cmd == "EPSV" {
				say("229 Entering Extended Passive Mode (|||%d|)", port)
			} else {
				say("227 Entering Passive Mode (10,0,0,1,%d,%d)", port>>8, port&0xff)
			}
		case "STOR":
			dc := data()
			if dc == nil {
				continue
			}
			out, _ := os.Create(filepath.Join(cwd, arg))
			_, err := io.Copy(out, dc)
			out.Close()
			dc.Close()
			if err != nil {
				say("426 broken")
			} else {
				say("226 done")
			}
		case "RETR":
			in, err := os.Open(filepath.Join(cwd, arg))
			if err != nil {
				say("550 no such file")
				continue
			}
			dc := data()
			if dc != nil {
				io.Copy(dc, in)
				dc.Close()
				say("226 done")
			}
			in.Close()
		case "NLST":
			entries, _ := os.ReadDir(cwd)
			if len(entries) == 0 {
				say("550 no files")
				continue
			}
			dc := data()
			if dc != nil {
				for _, e := range entries {
					fmt.Fprintf(dc, "%s\r\n", e.Name())
				}
				dc.Close()
				say("226 done")
			}
		case "RNFR":
			from = arg
			say("350 ready")
		case "RNTO":
			if os.Rename(filepath.Join(cwd, from), filepath.Join(cwd, arg)) != nil {
				say("550 no")
			} else {
				say("250 ok")
			}
		case "DELE":
			if os.Remove(filepath.Join(cwd, arg)) != nil {
				say("550 no")
			} else {
				say("250 ok")
			}
		case "QUIT":
			say("221 bye")
			return
		default:
			say("502 not implemented")
		}
	}
}

func TestFTPS(t *testing.T) {
	for _, epsv := range []bool{true, false} {
		f, ca := newFakeFTPS(t, epsv)
		addr := "ftps://wiki@" + f.ln.Addr().String() + "/backups"
		tg, err := Open(addr, envOf(map[string]string{CAFileEnv: ca, PasswordEnv: "secret"}))
		if err != nil {
			t.Fatal(err)
		}
		exercise(t, tg)
		entries, _ := os.ReadDir(filepath.Join(f.dir, "backups"))
		if len(entries) != 1 {
			t.Errorf("left behind: %v", entries)
		}
	}
	f, ca := newFakeFTPS(t, true)
	host := f.ln.Addr().String()
	for name, c := range map[string]struct {
		addr string
		env  map[string]string
		want string
	}{
		"a wrong password": {"ftps://wiki@" + host + "/backups", map[string]string{CAFileEnv: ca, PasswordEnv: "wrong"}, "signing in"},
		"a missing folder": {"ftps://wiki@" + host + "/nowhere", map[string]string{CAFileEnv: ca, PasswordEnv: "secret"}, "CWD"},
		"an unknown cert":  {"ftps://wiki@" + host + "/backups", map[string]string{PasswordEnv: "secret"}, "TLS"},
		"no server":        {"ftps://wiki@127.0.0.1:1/backups", map[string]string{CAFileEnv: ca}, "connecting"},
	} {
		tg, err := Open(c.addr, envOf(c.env))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tg.List(t.Context()); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := Open("ftps://"+host+"/backups", nil); err == nil || !strings.Contains(err.Error(), UsernameEnv) {
		t.Errorf("without a user: %v", err)
	}
	if tg, err := Open("ftps://"+host+"/backups", envOf(map[string]string{UsernameEnv: "wiki", PasswordEnv: "secret", CAFileEnv: ca})); err != nil {
		t.Fatal(err)
	} else if _, err := tg.List(t.Context()); err != nil {
		t.Errorf("the user from the environment: %v", err)
	}
}
