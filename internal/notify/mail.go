package notify

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// Mail security, how the connection to the SMTP server is protected.
const (
	// SecurityStartTLS upgrades a plain connection, usually on port 587;
	// a server that does not offer it is refused.
	SecurityStartTLS = "starttls"
	// SecurityTLS speaks TLS from the start, usually on port 465.
	SecurityTLS = "tls"
	// SecurityNone sends in the clear; meant for a relay on the same host
	// or network. Credentials are then refused unless the host is local.
	SecurityNone = "none"
)

// MailConfig is the SMTP server mail is sent through.
type MailConfig struct {
	Host     string
	Port     int
	Security string
	// Username and Password log in; both empty sends without logging in.
	Username string
	Password string
	// From is the sender, e.g. "Wiki <wiki@example.org>".
	From string
	// Transport replaces the SMTP conversation, for tests.
	Transport func(ctx context.Context, to string, m Message) error
}

func (c *MailConfig) validate() error {
	if c.Transport != nil {
		return nil
	}
	if c.Host == "" {
		return fmt.Errorf("mail needs a server")
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("mail port %d is not a port", c.Port)
	}
	switch c.Security {
	case SecurityStartTLS, SecurityTLS, SecurityNone:
	default:
		return fmt.Errorf("mail security %q is none of starttls, tls or none", c.Security)
	}
	if _, err := mail.ParseAddress(c.From); err != nil {
		return fmt.Errorf("the sender %q is no mail address", c.From)
	}
	return nil
}

func (c *MailConfig) send(ctx context.Context, to string, m Message) error {
	if c.Transport != nil {
		return c.Transport(ctx, to, m)
	}
	from, err := mail.ParseAddress(c.From)
	if err != nil {
		return err
	}
	rcpt, err := mail.ParseAddress(to)
	if err != nil {
		return fmt.Errorf("%q is no mail address", to)
	}
	data, err := compose(from, rcpt, m, time.Now())
	if err != nil {
		return err
	}
	addr := net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	dialer := &net.Dialer{}
	var conn net.Conn
	if c.Security == SecurityTLS {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: &tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12}}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("the mail server cannot be reached: %w", err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	client, err := smtp.NewClient(conn, c.Host)
	if err != nil {
		return fmt.Errorf("the mail server does not answer as one: %w", err)
	}
	defer client.Close()
	if err := client.Hello(helloName(from.Address)); err != nil {
		return err
	}
	if c.Security == SecurityStartTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return fmt.Errorf("the mail server does not offer STARTTLS")
		}
		if err := client.StartTLS(&tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("STARTTLS failed: %w", err)
		}
	}
	if c.Username != "" {
		// PlainAuth refuses to send the password over a connection that is
		// neither TLS nor to a local host.
		if err := client.Auth(smtp.PlainAuth("", c.Username, c.Password, c.Host)); err != nil {
			return fmt.Errorf("the mail server refused the login: %w", err)
		}
	}
	if err := client.Mail(from.Address); err != nil {
		return fmt.Errorf("the mail server refused the sender: %w", err)
	}
	if err := client.Rcpt(rcpt.Address); err != nil {
		return fmt.Errorf("the mail server refused the recipient: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("the mail server did not take the message: %w", err)
	}
	return client.Quit()
}

// helloName is the name the server is greeted with: the sender's domain.
func helloName(from string) string {
	if i := strings.LastIndexByte(from, '@'); i >= 0 && i+1 < len(from) {
		return from[i+1:]
	}
	return "localhost"
}

// compose writes a plain-text message in UTF-8. Header values come from
// parsed addresses and an encoded subject, so no line break of the input
// reaches the header.
func compose(from, to *mail.Address, m Message, now time.Time) ([]byte, error) {
	id := make([]byte, 12)
	if _, err := rand.Read(id); err != nil {
		return nil, err
	}
	subject := strings.Join(strings.Fields(m.Subject), " ")
	var b bytes.Buffer
	for _, h := range [][2]string{
		{"From", from.String()},
		{"To", to.String()},
		{"Subject", mime.QEncoding.Encode("utf-8", subject)},
		{"Date", now.Format(time.RFC1123Z)},
		{"Message-ID", "<" + hex.EncodeToString(id) + "@" + helloName(from.Address) + ">"},
		{"MIME-Version", "1.0"},
		{"Content-Type", "text/plain; charset=utf-8"},
		{"Content-Transfer-Encoding", "quoted-printable"},
		// Out-of-office replies and the like are not sent back to it.
		{"Auto-Submitted", "auto-generated"},
	} {
		b.WriteString(h[0] + ": " + h[1] + "\r\n")
	}
	b.WriteString("\r\n")
	qp := quotedprintable.NewWriter(&b)
	if _, err := qp.Write([]byte(strings.ReplaceAll(m.body(), "\n", "\r\n") + "\r\n")); err != nil {
		return nil, err
	}
	if err := qp.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
