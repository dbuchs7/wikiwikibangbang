// Package notify sends messages to people over the channels on their
// accounts (ADR-0061, ADR-0064): mail through an SMTP server the operator
// configures, and Slack, Microsoft Teams and other webhooks the users set
// themselves. A channel left empty is not used.
//
// A message says little: what happened and a link into the product.
// Content stays behind the sign-in; a mailbox or a chat channel is no
// place for it.
package notify

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/auth"
	"github.com/dbuchs7/wikiwikibangbang/internal/linkcheck"
)

// Message is one message to one person.
type Message struct {
	// Subject is a single line.
	Subject string
	// Text is plain text; lines are kept.
	Text string
	// Link leads into the product, or is empty.
	Link string
}

// Result is the outcome on one channel.
type Result struct {
	Channel string `json:"channel"`
	OK      bool   `json:"ok"`
	// Problem says why the message did not go out, in words.
	Problem string `json:"problem,omitempty"`
}

// Config configures a Notifier.
type Config struct {
	// Mail is the SMTP server; nil means mail is not sent.
	Mail *MailConfig
	// PublicURL is the address people reach the product at, e.g.
	// https://wiki.example.org; links in messages start with it. Without
	// it, messages carry no links and no password reset is mailed.
	PublicURL string
	// AllowedHook decides which addresses a webhook may be posted to;
	// linkcheck.Public by default (ADR-0039).
	AllowedHook func(netip.Addr) bool
	// Timeout bounds one delivery; 15 s by default.
	Timeout time.Duration
}

// Notifier delivers messages. It is safe for concurrent use.
type Notifier struct {
	mail    *MailConfig
	public  string
	hooks   *hookClient
	timeout time.Duration
}

// New returns a Notifier.
func New(c Config) (*Notifier, error) {
	if c.Timeout <= 0 {
		c.Timeout = 15 * time.Second
	}
	if c.AllowedHook == nil {
		c.AllowedHook = linkcheck.Public
	}
	if c.PublicURL != "" {
		u, err := url.Parse(c.PublicURL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
			return nil, fmt.Errorf("the public address %q is no http or https address", c.PublicURL)
		}
		c.PublicURL = strings.TrimRight(c.PublicURL, "/")
	}
	if c.Mail != nil {
		if err := c.Mail.validate(); err != nil {
			return nil, err
		}
	}
	return &Notifier{mail: c.Mail, public: c.PublicURL, hooks: newHookClient(c.AllowedHook, c.Timeout), timeout: c.Timeout}, nil
}

// Mail reports whether mail is sent.
func (n *Notifier) Mail() bool { return n != nil && n.mail != nil }

// PublicURL is the product's public address, or empty.
func (n *Notifier) PublicURL() string {
	if n == nil {
		return ""
	}
	return n.public
}

// Link is the public address of a route of the UI, e.g. "#/notifications",
// or empty without a public address.
func (n *Notifier) Link(route string) string {
	if n == nil || n.public == "" {
		return ""
	}
	return n.public + "/" + route
}

// Status describes the configuration for administrators; it holds no
// secret.
type Status struct {
	Mail      bool   `json:"mail"`
	MailHost  string `json:"mailHost,omitempty"`
	MailFrom  string `json:"mailFrom,omitempty"`
	PublicURL string `json:"publicURL,omitempty"`
}

// Status returns the configuration as administrators see it.
func (n *Notifier) Status() Status {
	if n == nil {
		return Status{}
	}
	s := Status{PublicURL: n.public}
	if n.mail != nil {
		s.Mail, s.MailHost, s.MailFrom = true, n.mail.Host, n.mail.From
	}
	return s
}

// ErrNoChannel means none of the channels asked for is available.
var ErrNoChannel = errors.New("no channel is available")

// Send delivers a message on every channel the contacts have, mail only
// when mail is configured. It returns one result per channel tried.
func (n *Notifier) Send(ctx context.Context, c auth.Contacts, m Message) []Result {
	if n == nil {
		return nil
	}
	var out []Result
	try := func(channel string, send func(context.Context) error) {
		ctx, cancel := context.WithTimeout(ctx, n.timeout)
		defer cancel()
		if err := send(ctx); err != nil {
			out = append(out, Result{Channel: channel, Problem: err.Error()})
			return
		}
		out = append(out, Result{Channel: channel, OK: true})
	}
	if c.Email != "" && n.mail != nil {
		try(auth.ChannelEmail, func(ctx context.Context) error { return n.mail.send(ctx, c.Email, m) })
	}
	if c.Slack != "" {
		try(auth.ChannelSlack, func(ctx context.Context) error { return n.hooks.post(ctx, c.Slack, slackPayload(m)) })
	}
	if c.Teams != "" {
		try(auth.ChannelTeams, func(ctx context.Context) error { return n.hooks.post(ctx, c.Teams, teamsPayload(m)) })
	}
	if c.Webhook != "" {
		try(auth.ChannelWebhook, func(ctx context.Context) error { return n.hooks.post(ctx, c.Webhook, webhookPayload(m)) })
	}
	return out
}

// SendMail delivers a message by mail only, as a password reset must: a
// chat channel may be read by others.
func (n *Notifier) SendMail(ctx context.Context, to string, m Message) error {
	if n == nil || n.mail == nil {
		return ErrNoChannel
	}
	if to == "" {
		return ErrNoChannel
	}
	ctx, cancel := context.WithTimeout(ctx, n.timeout)
	defer cancel()
	return n.mail.send(ctx, to, m)
}

// body is the message as plain text, with its link on a line of its own.
func (m Message) body() string {
	text := strings.TrimRight(m.Text, "\n")
	if m.Link != "" {
		text += "\n\n" + m.Link
	}
	return text
}
