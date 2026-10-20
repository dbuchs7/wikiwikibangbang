package notify

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/auth"
)

// fakeSMTP is a mail server that takes every message.
type fakeSMTP struct {
	mu   sync.Mutex
	got  []string
	addr *net.TCPAddr
	// refuse is the command the server refuses, e.g. "RCPT".
	refuse string
}

func startSMTP(t *testing.T) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	f := &fakeSMTP{addr: ln.Addr().(*net.TCPAddr)}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(c)
		}
	}()
	return f
}

func (f *fakeSMTP) serve(c net.Conn) {
	defer c.Close()
	f.mu.Lock()
	refuse := f.refuse
	f.mu.Unlock()
	r := bufio.NewReader(c)
	say := func(s string) { io.WriteString(c, s+"\r\n") }
	say("220 fake")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case refuse != "" && strings.HasPrefix(cmd, refuse):
			say("550 refused")
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			say("250 fake")
		case cmd == "DATA":
			say("354 go on")
			var msg strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				msg.WriteString(l)
			}
			f.mu.Lock()
			f.got = append(f.got, msg.String())
			f.mu.Unlock()
			say("250 queued")
		case cmd == "QUIT":
			say("221 bye")
			return
		default:
			say("250 ok")
		}
	}
}

func (f *fakeSMTP) refusing(cmd string) {
	f.mu.Lock()
	f.refuse = cmd
	f.mu.Unlock()
}

func (f *fakeSMTP) messages() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.got...)
}

func TestMailIsSent(t *testing.T) {
	srv := startSMTP(t)
	n, err := New(Config{PublicURL: "https://wiki.example.org/", Mail: &MailConfig{Host: "127.0.0.1", Port: srv.addr.Port, Security: SecurityNone, From: "Wiki <wiki@example.org>"}})
	if err != nil {
		t.Fatal(err)
	}
	m := Message{Subject: "Grüezi\r\nBcc: evil@example.org", Text: "Three changes.\nSee them.", Link: n.Link("#/notifications")}
	if m.Link != "https://wiki.example.org/#/notifications" {
		t.Fatalf("link %q", m.Link)
	}
	res := n.Send(context.Background(), auth.Contacts{Email: "anna@example.org"}, m)
	if len(res) != 1 || !res[0].OK {
		t.Fatalf("results %+v", res)
	}
	got := srv.messages()
	if len(got) != 1 {
		t.Fatalf("messages %v", got)
	}
	head, body, _ := strings.Cut(got[0], "\r\n\r\n")
	if strings.Contains(head, "\r\nBcc:") || !strings.Contains(head, "Subject: =?utf-8?q?Gr=C3=BCezi_Bcc:_evil@example.org?=") ||
		!strings.Contains(head, "To: <anna@example.org>") || !strings.Contains(head, "Auto-Submitted: auto-generated") {
		t.Fatalf("header %q", head)
	}
	text, _ := io.ReadAll(quotedprintable.NewReader(strings.NewReader(body)))
	if !strings.Contains(string(text), "Three changes.\r\nSee them.\r\n\r\nhttps://wiki.example.org/#/notifications") {
		t.Fatalf("body %q", text)
	}
}

func TestMailRefusesAServerWithoutStartTLS(t *testing.T) {
	srv := startSMTP(t)
	n, err := New(Config{Mail: &MailConfig{Host: "127.0.0.1", Port: srv.addr.Port, Security: SecurityStartTLS, From: "wiki@example.org"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := n.SendMail(context.Background(), "anna@example.org", Message{Subject: "x"}); err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("err %v", err)
	}
	if len(srv.messages()) != 0 {
		t.Fatal("nothing is sent in the clear")
	}
}

func TestConfigIsChecked(t *testing.T) {
	for name, c := range map[string]Config{
		"bad url":      {PublicURL: "ftp://x"},
		"no host":      {Mail: &MailConfig{Port: 25, Security: SecurityNone, From: "a@b.org"}},
		"bad security": {Mail: &MailConfig{Host: "h", Port: 25, Security: "ssl", From: "a@b.org"}},
		"bad sender":   {Mail: &MailConfig{Host: "h", Port: 25, Security: SecurityNone, From: "nobody"}},
	} {
		if _, err := New(c); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestWebhooks(t *testing.T) {
	var mu sync.Mutex
	bodies := map[string]map[string]any{}
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var v map[string]any
		_ = json.NewDecoder(r.Body).Decode(&v)
		mu.Lock()
		bodies[r.URL.Path] = v
		mu.Unlock()
		if r.URL.Path == "/broken/secret" {
			w.WriteHeader(http.StatusForbidden)
		}
	}))
	defer hook.Close()
	loopback := func(a netip.Addr) bool { return a.IsLoopback() }
	n, err := New(Config{AllowedHook: loopback})
	if err != nil {
		t.Fatal(err)
	}
	// The address checks of the contacts happen when they are set; here a
	// test server stands in for Slack and Teams.
	c := auth.Contacts{Email: "a@example.org", Slack: hook.URL + "/slack", Teams: hook.URL + "/teams", Webhook: hook.URL + "/broken/secret"}
	res := n.Send(context.Background(), c, Message{Subject: "Hi <all>", Text: "x & y", Link: "https://wiki.example.org/#/x"})
	if len(res) != 3 || !res[0].OK || res[0].Channel != auth.ChannelSlack || !res[1].OK || res[2].OK {
		t.Fatalf("results %+v (mail is not configured, so it is not tried)", res)
	}
	if strings.Contains(res[2].Problem, "secret") || !strings.Contains(res[2].Problem, "403") {
		t.Fatalf("problem %q", res[2].Problem)
	}
	if got := bodies["/slack"]["text"]; got != "*Hi &lt;all&gt;*\nx &amp; y\n<https://wiki.example.org/#/x|https://wiki.example.org/#/x>" {
		t.Fatalf("slack %q", got)
	}
	att := bodies["/teams"]["attachments"].([]any)[0].(map[string]any)
	if att["contentType"] != "application/vnd.microsoft.card.adaptive" {
		t.Fatalf("teams %v", bodies["/teams"])
	}

	// By default only public addresses are posted to.
	public, _ := New(Config{})
	res = public.Send(context.Background(), auth.Contacts{Webhook: hook.URL + "/x"}, Message{Subject: "x"})
	if len(res) != 1 || res[0].OK || !strings.Contains(res[0].Problem, "public internet") {
		t.Fatalf("a private address: %+v", res)
	}
}

func TestNoMailWithoutConfiguration(t *testing.T) {
	n, _ := New(Config{})
	if n.Mail() || n.Link("#/x") != "" {
		t.Fatal("no mail and no links without configuration")
	}
	if err := n.SendMail(context.Background(), "a@example.org", Message{}); err != ErrNoChannel {
		t.Fatalf("err %v", err)
	}
	var none *Notifier
	if none.Send(context.Background(), auth.Contacts{Slack: "x"}, Message{}) != nil || none.Mail() {
		t.Fatal("a nil notifier sends nothing")
	}
}

func TestMailFailures(t *testing.T) {
	srv := startSMTP(t)
	cfg := func(c MailConfig) *Notifier {
		t.Helper()
		c.Host, c.From = "127.0.0.1", "wiki@example.org"
		if c.Port == 0 {
			c.Port = srv.addr.Port
		}
		if c.Security == "" {
			c.Security = SecurityNone
		}
		n, err := New(Config{Mail: &c})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	ctx := context.Background()
	m := Message{Subject: "x", Text: "y"}
	// The fake server does not take AUTH, so a login fails.
	if err := cfg(MailConfig{Username: "u", Password: "p"}).SendMail(ctx, "a@example.org", m); err == nil || !strings.Contains(err.Error(), "login") {
		t.Errorf("login: %v", err)
	}
	if err := cfg(MailConfig{Security: SecurityTLS}).SendMail(ctx, "a@example.org", m); err == nil {
		t.Error("TLS to a plain server succeeded")
	}
	if err := cfg(MailConfig{}).SendMail(ctx, "not an address", m); err == nil {
		t.Error("a bad recipient was sent to")
	}
	srv.refusing("RCPT")
	if err := cfg(MailConfig{}).SendMail(ctx, "a@example.org", m); err == nil || !strings.Contains(err.Error(), "recipient") {
		t.Errorf("refused recipient: %v", err)
	}
	srv.refusing("MAIL")
	if err := cfg(MailConfig{}).SendMail(ctx, "a@example.org", m); err == nil || !strings.Contains(err.Error(), "sender") {
		t.Errorf("refused sender: %v", err)
	}
	srv.refusing("")
	closed, _ := net.Listen("tcp", "127.0.0.1:0")
	port := closed.Addr().(*net.TCPAddr).Port
	closed.Close()
	if err := cfg(MailConfig{Port: port}).SendMail(ctx, "a@example.org", m); err == nil || !strings.Contains(err.Error(), "cannot be reached") {
		t.Errorf("closed port: %v", err)
	}
	if len(srv.messages()) != 0 {
		t.Fatalf("nothing was delivered: %v", srv.messages())
	}
}

func TestStatusAndNames(t *testing.T) {
	n, err := New(Config{PublicURL: "https://wiki.example.org/", Mail: &MailConfig{Host: "mail.example.org", Port: 587, Security: SecurityStartTLS, From: "Wiki <wiki@example.org>", Password: "secret"}})
	if err != nil {
		t.Fatal(err)
	}
	st := n.Status()
	if !st.Mail || st.MailHost != "mail.example.org" || st.PublicURL != "https://wiki.example.org" || n.PublicURL() != st.PublicURL {
		t.Fatalf("status %+v", st)
	}
	if b, _ := json.Marshal(st); strings.Contains(string(b), "secret") {
		t.Fatal("the status holds no secret")
	}
	var none *Notifier
	if none.Status() != (Status{}) || none.PublicURL() != "" || none.Link("#/") != "" {
		t.Fatal("a nil notifier has no status")
	}
	if helloName("nobody") != "localhost" || helloName("a@b.org") != "b.org" {
		t.Fatal("hello names")
	}
	if got := (Message{Text: "a\n", Link: ""}).body(); got != "a" {
		t.Fatalf("body %q", got)
	}
}
