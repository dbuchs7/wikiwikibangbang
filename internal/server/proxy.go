package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// ParseTrustedProxies reads a comma-separated list of addresses and
// networks (CIDR) whose forwarded headers are believed (ADR-0037). An
// empty list trusts no proxy. A list that trusts every address is refused:
// it would let any client choose the address it is throttled and audited
// under.
func ParseTrustedProxies(s string) ([]netip.Prefix, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	var out []netip.Prefix
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		var p netip.Prefix
		var err error
		if strings.Contains(part, "/") {
			p, err = netip.ParsePrefix(part)
		} else {
			var a netip.Addr
			if a, err = netip.ParseAddr(part); err == nil {
				p = netip.PrefixFrom(a, a.BitLen())
			}
		}
		if err != nil {
			return nil, fmt.Errorf("trusted proxy %q is not an address or network: %w", part, err)
		}
		if p.Bits() == 0 {
			return nil, fmt.Errorf("trusted proxy %q would trust every address", part)
		}
		out = append(out, p.Masked())
	}
	return out, nil
}

type secureKey struct{}

// isSecure reports whether the client reached the server over HTTPS:
// directly, or through a trusted proxy that terminated TLS.
func isSecure(r *http.Request) bool {
	return r.TLS != nil || r.Context().Value(secureKey{}) == true
}

// BehindProxies believes the forwarded headers of requests that come from
// a trusted proxy (ADR-0037): X-Forwarded-For names the client, whose
// address the audit trail and the login throttle then see, and
// X-Forwarded-Proto https marks the request as secure, so cookies are
// Secure. From any other peer the headers are ignored.
func BehindProxies(trusted []netip.Prefix, h http.Handler) http.Handler {
	if len(trusted) == 0 {
		return h
	}
	isTrusted := func(a netip.Addr) bool {
		a = a.Unmap()
		for _, p := range trusted {
			if p.Contains(a) {
				return true
			}
		}
		return false
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peer, err := netip.ParseAddrPort(r.RemoteAddr)
		if err != nil || !isTrusted(peer.Addr()) {
			h.ServeHTTP(w, r)
			return
		}
		r = r.Clone(r.Context())
		// The rightmost address no trusted proxy added is the client; what
		// stands left of it was written by the client and proves nothing.
		hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
		for i := len(hops) - 1; i >= 0; i-- {
			a, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
			if err != nil {
				break
			}
			r.RemoteAddr = net.JoinHostPort(a.Unmap().String(), "0")
			if !isTrusted(a) {
				break
			}
		}
		if strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https") {
			r = r.WithContext(context.WithValue(r.Context(), secureKey{}, true))
		}
		h.ServeHTTP(w, r)
	})
}
