package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Genesis is the Prev of the first entry of a journal begun with the
// chain: it names no entry before it.
var Genesis = strings.Repeat("0", 64)

// link is what the entry after e names as its Prev: the SHA-256 of e's
// record as stored. An expiry marker stands for the entries it replaced,
// so its link is the one it carries, that of the last removed entry; a
// marker made before the chain began carries none and is hashed.
func link(e Entry, raw []byte) string {
	if h := e.Detail["hash"]; e.Event == Expired && h != "" {
		return h
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// Why a chain is broken; the UI words them.
const (
	BrokenGap      = "gap"       // a sequence number is missing
	BrokenStart    = "start"     // the first entry names one before it, yet no marker says why it went
	BrokenMarker   = "marker"    // an expiry marker other than the first entry
	BrokenTooYoung = "too-young" // the marker removed entries younger than the retention period
	BrokenLink     = "link"      // an entry does not name the one before it
	BrokenUnlinked = "unlinked"  // an entry without a link after the chain began
)

// Check is the result of Verify.
type Check struct {
	// Entries is how many entries were checked.
	Entries int `json:"entries"`
	// Unchained counts entries recorded before the chain began. Only the
	// last of them is covered, by the first linked entry naming it.
	Unchained int `json:"unchained"`
	// Head and HeadSeq are the link and number of the newest entry: the
	// value a witness compares.
	Head    string `json:"head,omitempty"`
	HeadSeq int    `json:"headSeq,omitempty"`
	// Broken is the number of the first entry that breaks the chain, and
	// Reason why; Broken is 0 when the chain holds.
	Broken int    `json:"broken,omitempty"`
	Reason string `json:"reason,omitempty"`
}

var errBroken = errors.New("audit: chain broken")

// Verify checks the chain of a journal (ADR-0123): read passes its records
// in order. It finds any entry changed, removed or inserted in the middle,
// and a removal at the start that is not an expiry. It cannot tell a
// journal cut at its end, or one rewritten whole with new links, from a
// genuine one: that takes the links the log lines witnessed (Head).
func Verify(read func(each func([]byte) error) error, now time.Time) (Check, error) {
	var c Check
	var prev Entry
	prevLink := Genesis
	chained := false
	stop := func(e Entry, reason string) error {
		c.Broken, c.Reason = e.Seq, reason
		return errBroken
	}
	err := read(func(raw []byte) error {
		var e Entry
		if err := json.Unmarshal(raw, &e); err != nil {
			return fmt.Errorf("audit: record %d: %w", c.Entries+1, err)
		}
		first := c.Entries == 0
		switch {
		case !first && e.Seq != prev.Seq+1:
			return stop(e, BrokenGap)
		case e.Event == Expired && !first:
			return stop(e, BrokenMarker)
		case e.Event == Expired:
			if before, err := time.Parse(time.RFC3339, e.Detail["before"]); err != nil || before.After(Cutoff(now)) {
				return stop(e, BrokenTooYoung)
			}
		case first && e.Prev != "" && e.Prev != Genesis:
			return stop(e, BrokenStart)
		case !first && prev.Event == Expired && e.At.Before(expiredBefore(prev)):
			return stop(e, BrokenTooYoung)
		case e.Prev == "" && chained:
			return stop(e, BrokenUnlinked)
		case e.Prev == "":
			c.Unchained++
		case e.Prev != prevLink:
			return stop(e, BrokenLink)
		default:
			chained = true
		}
		c.Entries++
		prev, prevLink = e, link(e, raw)
		c.Head, c.HeadSeq = prevLink, e.Seq
		return nil
	})
	if errors.Is(err, errBroken) {
		return c, nil
	}
	return c, err
}

func expiredBefore(m Entry) time.Time {
	t, _ := time.Parse(time.RFC3339, m.Detail["before"])
	return t
}

// Each passes records to each in order, as a journal reader does.
func Each(records [][]byte) func(each func([]byte) error) error {
	return func(each func([]byte) error) error {
		for _, r := range records {
			if err := each(r); err != nil {
				return err
			}
		}
		return nil
	}
}

// Verify checks the chain of the whole journal; see the function Verify.
func (t *Trail) Verify(now time.Time) (Check, error) {
	t.mu.Lock()
	read := t.read
	t.mu.Unlock()
	if read == nil {
		return Check{}, errors.New("audit: the journal cannot be read")
	}
	return Verify(read, now)
}
