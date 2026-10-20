package auth

import (
	"fmt"
	"net/mail"
	"net/url"
	"strings"
)

// Contacts are the channels messages reach a user on (ADR-0061). An empty
// field is a channel the user does not have; nothing is sent there.
type Contacts struct {
	// Email is a mail address.
	Email string `json:"email,omitempty"`
	// Slack is the address of a Slack incoming webhook.
	Slack string `json:"slack,omitempty"`
	// Teams is the address of a Microsoft Teams webhook (an incoming
	// webhook or a workflow's).
	Teams string `json:"teams,omitempty"`
	// Webhook is any other address a message is posted to as JSON.
	Webhook string `json:"webhook,omitempty"`
}

// Channel names, in the order Channels lists them.
const (
	ChannelEmail   = "email"
	ChannelSlack   = "slack"
	ChannelTeams   = "teams"
	ChannelWebhook = "webhook"
)

// Channels names the channels that are set.
func (c Contacts) Channels() []string {
	var out []string
	for _, ch := range []struct {
		name, value string
	}{{ChannelEmail, c.Email}, {ChannelSlack, c.Slack}, {ChannelTeams, c.Teams}, {ChannelWebhook, c.Webhook}} {
		if ch.value != "" {
			out = append(out, ch.name)
		}
	}
	return out
}

// Normalize trims the fields.
func (c Contacts) Normalize() Contacts {
	return Contacts{Email: strings.TrimSpace(c.Email), Slack: strings.TrimSpace(c.Slack),
		Teams: strings.TrimSpace(c.Teams), Webhook: strings.TrimSpace(c.Webhook)}
}

// Validate checks the fields that are set.
func (c Contacts) Validate() error {
	if c.Email != "" && !validEmail(c.Email) {
		return fmt.Errorf("%w: %q is no mail address", ErrInvalid, c.Email)
	}
	if c.Slack != "" {
		if err := validHook(c.Slack, "hooks.slack.com"); err != nil {
			return fmt.Errorf("%w: Slack: %v", ErrInvalid, err)
		}
	}
	if c.Teams != "" {
		if err := validHook(c.Teams, ""); err != nil {
			return fmt.Errorf("%w: Teams: %v", ErrInvalid, err)
		}
	}
	if c.Webhook != "" {
		if err := validHook(c.Webhook, ""); err != nil {
			return fmt.Errorf("%w: webhook: %v", ErrInvalid, err)
		}
	}
	return nil
}

// validEmail accepts a bare address, as mail headers carry it, of at most
// 254 bytes.
func validEmail(s string) bool {
	if len(s) > 254 || strings.ContainsAny(s, " <>\r\n") {
		return false
	}
	a, err := mail.ParseAddress(s)
	return err == nil && a.Name == "" && a.Address == s && strings.Contains(s[strings.LastIndexByte(s, '@')+1:], ".")
}

// validHook accepts an https address without credentials; host, if set, is
// the only host allowed. Whether the address may be called is decided when
// a message is sent, against the addresses it resolves to.
func validHook(s, host string) error {
	if len(s) > 2048 {
		return fmt.Errorf("the address is too long")
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return fmt.Errorf("an https address is needed")
	}
	if host != "" && !strings.EqualFold(u.Hostname(), host) {
		return fmt.Errorf("the address is on %s", host)
	}
	return nil
}

// Contacts returns a user's contacts.
func (a *Accounts) Contacts(id UserID) Contacts {
	a.mu.Lock()
	defer a.mu.Unlock()
	if u, ok := a.users[id]; ok {
		return u.contacts
	}
	return Contacts{}
}

// SetContacts replaces a user's contacts; empty fields remove channels.
func (a *Accounts) SetContacts(id UserID, c Contacts) error {
	c = c.Normalize()
	if err := c.Validate(); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	u, ok := a.users[id]
	if !ok {
		return ErrNotFound
	}
	if u.contacts == c {
		return nil
	}
	return a.record(event{Kind: contactsSet, User: id, Contacts: &c})
}

// SetEmail replaces only a user's mail address, as an administrator does;
// the user's other channels stay.
func (a *Accounts) SetEmail(id UserID, email string) error {
	email = strings.TrimSpace(email)
	if email != "" && !validEmail(email) {
		return fmt.Errorf("%w: %q is no mail address", ErrInvalid, email)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	u, ok := a.users[id]
	if !ok {
		return ErrNotFound
	}
	c := u.contacts
	if c.Email == email {
		return nil
	}
	c.Email = email
	return a.record(event{Kind: contactsSet, User: id, Contacts: &c})
}
