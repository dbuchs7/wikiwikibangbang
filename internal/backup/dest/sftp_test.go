package dest

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"
)

// fakeSFTP is an SSH server with an SFTP subsystem over a directory,
// enough for what the target does.
type fakeSFTP struct {
	ln       net.Listener
	root     string
	hostKey  ssh.Signer
	userKey  ssh.PublicKey
	wg       sync.WaitGroup
	pageSize int
}

func newFakeSFTP(t *testing.T) (*fakeSFTP, string) {
	t.Helper()
	_, hostPriv, _ := ed25519.GenerateKey(rand.Reader)
	hostKey, _ := ssh.NewSignerFromKey(hostPriv)
	_, userPriv, _ := ed25519.GenerateKey(rand.Reader)
	block, err := ssh.MarshalPrivateKey(userPriv, "")
	if err != nil {
		t.Fatal(err)
	}
	keyFile := filepath.Join(t.TempDir(), "id_ed25519")
	os.WriteFile(keyFile, pem.EncodeToMemory(block), 0o600)
	userSigner, _ := ssh.NewSignerFromKey(userPriv)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeSFTP{ln: ln, root: t.TempDir(), hostKey: hostKey, userKey: userSigner.PublicKey(), pageSize: 2}
	os.Mkdir(filepath.Join(f.root, "backups"), 0o700)
	go f.serve()
	t.Cleanup(func() { ln.Close(); f.wg.Wait() })
	return f, keyFile
}

func (f *fakeSFTP) serve() {
	cfg := &ssh.ServerConfig{
		PublicKeyCallback: func(c ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if c.User() == "wiki" && string(key.Marshal()) == string(f.userKey.Marshal()) {
				return nil, nil
			}
			return nil, errors.New("no")
		},
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == "wiki" && string(pass) == "secret" {
				return nil, nil
			}
			return nil, errors.New("no")
		},
	}
	cfg.AddHostKey(f.hostKey)
	for {
		c, err := f.ln.Accept()
		if err != nil {
			return
		}
		f.wg.Add(1)
		go func() {
			defer f.wg.Done()
			defer c.Close()
			_, chans, reqs, err := ssh.NewServerConn(c, cfg)
			if err != nil {
				return
			}
			go ssh.DiscardRequests(reqs)
			for nc := range chans {
				ch, reqs, err := nc.Accept()
				if err != nil {
					continue
				}
				go func() {
					for req := range reqs {
						ok := req.Type == "subsystem" && len(req.Payload) > 4 && string(req.Payload[4:]) == "sftp"
						req.Reply(ok, nil)
						if ok {
							go func() { f.subsystem(ch); ch.Close() }()
						}
					}
				}()
			}
		}()
	}
}

func (f *fakeSFTP) subsystem(ch io.ReadWriter) {
	handles := map[string]*os.File{}
	dirs := map[string][]fs.DirEntry{}
	next := 0
	reply := func(typ byte, id uint32, parts ...[]byte) {
		body := append([]byte{typ}, u32(id)...)
		for _, p := range parts {
			body = append(body, p...)
		}
		ch.Write(append(u32(uint32(len(body))), body...))
	}
	st := func(id uint32, code uint32, msg string) { reply(fxpStatus, id, u32(code), str(msg), str("")) }
	errStatus := func(id uint32, err error) {
		switch {
		case err == nil:
			st(id, fxOK, "")
		case errors.Is(err, fs.ErrNotExist):
			st(id, fxNoSuch, "no such file")
		default:
			st(id, 4, err.Error())
		}
	}
	local := func(p string) string { return filepath.Join(f.root, filepath.FromSlash(p)) }
	for {
		var hdr [5]byte
		if _, err := io.ReadFull(ch, hdr[:]); err != nil {
			return
		}
		body := make([]byte, binary.BigEndian.Uint32(hdr[:4])-1)
		if _, err := io.ReadFull(ch, body); err != nil {
			return
		}
		if hdr[4] == fxpInit {
			ch.Write(append(u32(5), fxpVersion, 0, 0, 0, 3))
			continue
		}
		id := binary.BigEndian.Uint32(body)
		b := body[4:]
		s1, rest, _ := readString(b)
		switch hdr[4] {
		case fxpOpen:
			flags := binary.BigEndian.Uint32(rest)
			mode := os.O_RDONLY
			if flags&fxfWrite != 0 {
				mode = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
			}
			file, err := os.OpenFile(local(s1), mode, 0o600)
			if err != nil {
				errStatus(id, err)
				continue
			}
			next++
			h := strconv.Itoa(next)
			handles[h] = file
			reply(fxpHandle, id, str(h))
		case fxpWrite:
			off := binary.BigEndian.Uint64(rest)
			data, _, _ := readString(rest[8:])
			_, err := handles[s1].WriteAt([]byte(data), int64(off))
			errStatus(id, err)
		case fxpRead:
			off := binary.BigEndian.Uint64(rest)
			n := binary.BigEndian.Uint32(rest[8:])
			buf := make([]byte, n)
			got, err := handles[s1].ReadAt(buf, int64(off))
			if got == 0 && errors.Is(err, io.EOF) {
				st(id, fxEOF, "eof")
				continue
			}
			reply(fxpData, id, str(string(buf[:got])))
		case fxpClose:
			if file, ok := handles[s1]; ok {
				file.Close()
				delete(handles, s1)
			}
			delete(dirs, s1)
			st(id, fxOK, "")
		case fxpOpendir:
			entries, err := os.ReadDir(local(s1))
			if err != nil {
				errStatus(id, err)
				continue
			}
			next++
			h := strconv.Itoa(next)
			dirs[h] = entries
			reply(fxpHandle, id, str(h))
		case fxpReaddir:
			entries := dirs[s1]
			if len(entries) == 0 {
				st(id, fxEOF, "eof")
				continue
			}
			n := min(f.pageSize, len(entries))
			parts := [][]byte{u32(uint32(n))}
			for _, e := range entries[:n] {
				perm := uint32(0o100644)
				if e.IsDir() {
					perm = 0o040755
				}
				parts = append(parts, str(e.Name()), str("-rw-r--r-- "+e.Name()),
					u32(attrSize|attrPerm|attrExtend), u64(1), u32(perm), u32(1), str("k"), str("v"))
			}
			dirs[s1] = entries[n:]
			reply(fxpName, id, parts...)
		case fxpRemove:
			errStatus(id, os.Remove(local(s1)))
		case fxpRename:
			to, _, _ := readString(rest)
			if _, err := os.Stat(local(to)); err == nil {
				st(id, 4, "exists")
				continue
			}
			errStatus(id, os.Rename(local(s1), local(to)))
		default:
			st(id, 8, "unsupported")
		}
	}
}

func TestSFTP(t *testing.T) {
	f, keyFile := newFakeSFTP(t)
	host := f.ln.Addr().String()
	fp := ssh.FingerprintSHA256(f.hostKey.PublicKey())
	tg, err := Open("sftp://wiki@"+host+"/backups", envOf(map[string]string{SSHKeyEnv: keyFile, HostKeyEnv: fp}))
	if err != nil {
		t.Fatal(err)
	}
	os.Mkdir(filepath.Join(f.root, "backups", "sub"), 0o700)
	exercise(t, tg)
	entries, _ := os.ReadDir(filepath.Join(f.root, "backups"))
	if len(entries) != 2 {
		t.Errorf("left behind: %v", entries)
	}

	// With a password, and the user from the environment.
	tg, _ = Open("sftp://"+host+"/backups", envOf(map[string]string{UsernameEnv: "wiki", PasswordEnv: "secret", HostKeyEnv: fp}))
	if _, err := tg.List(t.Context()); err != nil {
		t.Errorf("password: %v", err)
	}
	for name, c := range map[string]struct {
		env  map[string]string
		want string
	}{
		"no host key":      {map[string]string{PasswordEnv: "secret"}, fp},
		"another host key": {map[string]string{PasswordEnv: "secret", HostKeyEnv: "SHA256:other"}, "not SHA256:other"},
		"a wrong password": {map[string]string{PasswordEnv: "wrong", HostKeyEnv: fp}, "unable to authenticate"},
	} {
		tg, err := Open("sftp://wiki@"+host+"/backups", envOf(c.env))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tg.List(t.Context()); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	tg, _ = Open("sftp://wiki@"+host+"/nowhere", envOf(map[string]string{PasswordEnv: "secret", HostKeyEnv: fp}))
	if _, err := tg.List(t.Context()); err == nil {
		t.Error("listed a missing folder")
	}
	if err := tg.Put(t.Context(), "x.wwbb", strings.NewReader("x")); err == nil {
		t.Error("stored in a missing folder")
	}
	for name, env := range map[string]map[string]string{
		"no credentials":    {HostKeyEnv: fp},
		"a missing key":     {SSHKeyEnv: "/missing", HostKeyEnv: fp},
		"an unreadable key": {SSHKeyEnv: os.Args[0], HostKeyEnv: fp},
	} {
		if _, err := Open("sftp://wiki@"+host+"/backups", envOf(env)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if _, err := Open("sftp://"+host+"/backups", envOf(map[string]string{PasswordEnv: "x"})); err == nil {
		t.Error("no user was accepted")
	}
	tg, _ = Open("sftp://wiki@127.0.0.1:1/backups", envOf(map[string]string{PasswordEnv: "x"}))
	if _, err := tg.List(t.Context()); err == nil || !strings.Contains(err.Error(), "connecting") {
		t.Errorf("no server: %v", err)
	}
}
