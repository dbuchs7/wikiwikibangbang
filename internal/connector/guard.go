package connector

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/linkcheck"
)

// AllowPrivateVar, set to "on", lets connectors reach addresses in the
// server's own network: a source inside the organisation (ADR-0127). It
// is the operator's decision, so it comes from the environment, not from
// the settings an administrator changes in the UI.
const AllowPrivateVar = "WWBB_CONNECTOR_ALLOW_PRIVATE"

// ErrPrivateAddress is a source whose address is not on the public
// internet, while connectors may reach only public ones.
var ErrPrivateAddress = errors.New("connector: the source's address is not public")

// guard checks the address of every request a connector makes,
// redirects included, before it is sent: a name must resolve to public
// addresses only (linkcheck.Public, as for webhooks, ADR-0062). Requests
// still go through the proxy the environment names, as an organisation's
// network may require; a name the server cannot resolve itself is then
// left to the proxy.
type guard struct {
	next   http.RoundTripper
	lookup func(ctx context.Context, host string) ([]netip.Addr, error)
	proxy  func(*http.Request) bool
}

func (g guard) RoundTrip(req *http.Request) (*http.Response, error) {
	host := req.URL.Hostname()
	addrs := []netip.Addr{}
	if a, err := netip.ParseAddr(host); err == nil {
		addrs = append(addrs, a)
	} else {
		found, err := g.lookup(req.Context(), host)
		switch {
		case err != nil && g.proxy(req):
			return g.next.RoundTrip(req)
		case err != nil:
			return nil, err
		}
		addrs = found
	}
	for _, a := range addrs {
		if !linkcheck.Public(a) {
			return nil, fmt.Errorf("%w: %s is %s; set %s=on to allow sources in the server's own network", ErrPrivateAddress, host, a, AllowPrivateVar)
		}
	}
	return g.next.RoundTrip(req)
}

// httpClient is the client a connector reaches its source with: through
// the environment's proxy, with timeouts, and kept to public addresses
// unless AllowPrivateVar allows others.
func (r *Runner) httpClient() *http.Client {
	var transport http.RoundTripper = &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		DialContext:         (&net.Dialer{Timeout: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout: 30 * time.Second,
		IdleConnTimeout:     30 * time.Second,
	}
	if r.getenv(AllowPrivateVar) != "on" {
		lookup := func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		}
		transport = guard{next: transport, lookup: lookup, proxy: viaProxy}
	}
	return &http.Client{Timeout: 2 * time.Minute, Transport: transport}
}

// viaProxy reports whether the environment sends a request through a proxy.
func viaProxy(req *http.Request) bool {
	u, err := http.ProxyFromEnvironment(req)
	return err == nil && u != nil
}
