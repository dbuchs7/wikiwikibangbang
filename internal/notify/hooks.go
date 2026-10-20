package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/linkcheck"
)

// maxHookAnswer is how much of a webhook's answer is read and dropped, so
// the connection can be reused.
const maxHookAnswer = 64 << 10

// hookClient posts to webhooks: only to the addresses allowed decides on,
// checked after name resolution (ADR-0039), never through a proxy and
// never following a redirect.
type hookClient struct {
	client *http.Client
}

func newHookClient(allowed func(netip.Addr) bool, timeout time.Duration) *hookClient {
	return &hookClient{client: &http.Client{
		Transport:     linkcheck.Transport(allowed, timeout),
		Timeout:       timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (h *hookClient) post(ctx context.Context, address string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, address, bytes.NewReader(body))
	if err != nil {
		return errors.New("the address is not usable")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "wikiwikibangbang")
	resp, err := h.client.Do(req)
	if err != nil {
		// The address is a credential: the error names the cause only.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		if strings.Contains(err.Error(), "internal address") {
			return errors.New("the address is not on the public internet")
		}
		return fmt.Errorf("the service cannot be reached: %v", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxHookAnswer))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("the service answered %s", resp.Status)
	}
	return nil
}

// slackEscape escapes the three characters Slack's text format reserves.
func slackEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// slackPayload is a message for a Slack incoming webhook.
func slackPayload(m Message) map[string]string {
	text := "*" + slackEscape(m.Subject) + "*"
	if t := strings.TrimSpace(m.Text); t != "" {
		text += "\n" + slackEscape(t)
	}
	if m.Link != "" {
		text += "\n<" + m.Link + "|" + slackEscape(m.Link) + ">"
	}
	return map[string]string{"text": text}
}

// teamsPayload is a message with an Adaptive Card, the format Teams
// workflows ("when a webhook request is received") take.
func teamsPayload(m Message) map[string]any {
	body := []map[string]any{{"type": "TextBlock", "text": m.Subject, "weight": "Bolder", "size": "Medium", "wrap": true}}
	if t := strings.TrimSpace(m.Text); t != "" {
		body = append(body, map[string]any{"type": "TextBlock", "text": t, "wrap": true})
	}
	card := map[string]any{"$schema": "http://adaptivecards.io/schemas/adaptive-card.json", "type": "AdaptiveCard", "version": "1.4", "body": body}
	if m.Link != "" {
		card["actions"] = []map[string]any{{"type": "Action.OpenUrl", "title": "Open", "url": m.Link}}
	}
	return map[string]any{"type": "message", "attachments": []map[string]any{{
		"contentType": "application/vnd.microsoft.card.adaptive", "content": card,
	}}}
}

// webhookPayload is the message as plain JSON, for any other service.
func webhookPayload(m Message) map[string]string {
	return map[string]string{"source": "wikiwikibangbang", "subject": m.Subject, "text": m.Text, "link": m.Link}
}
