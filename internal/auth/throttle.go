package auth

import (
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
)

// Login throttling (ADR-0013): after MaxFailures failed logins within
// FailureWindow, a client address or a username is refused until the oldest
// failure leaves the window.
const (
	MaxFailures   = 10
	FailureWindow = 15 * time.Minute
)

// throttle remembers recent failures per key. The memory an attacker can
// make it use is bounded by budget.LoginTracked (ADR-0015); when that is
// reached, an entry is dropped.
type throttle struct {
	failures map[string][]time.Time
}

func (t *throttle) init() { t.failures = map[string][]time.Time{} }

func (t *throttle) recent(now time.Time, key string) []time.Time {
	f := t.failures[key]
	i := 0
	for i < len(f) && now.Sub(f[i]) >= FailureWindow {
		i++
	}
	if i == len(f) {
		delete(t.failures, key)
		return nil
	}
	f = f[i:]
	t.failures[key] = f
	return f
}

// blocked returns how long the longest-blocked key must wait, or 0.
func (t *throttle) blocked(now time.Time, keys ...string) time.Duration {
	var wait time.Duration
	for _, k := range keys {
		if f := t.recent(now, k); len(f) >= MaxFailures {
			if w := f[len(f)-MaxFailures].Add(FailureWindow).Sub(now); w > wait {
				wait = w
			}
		}
	}
	return wait
}

func (t *throttle) fail(now time.Time, keys ...string) {
	if len(t.failures) >= budget.Get().LoginTracked {
		for k := range t.failures { // map order: an arbitrary key goes
			delete(t.failures, k)
			break
		}
	}
	for _, k := range keys {
		t.failures[k] = append(t.recent(now, k), now)
	}
}

func (t *throttle) reset(key string) { delete(t.failures, key) }
