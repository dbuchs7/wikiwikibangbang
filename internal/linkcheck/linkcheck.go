// Package linkcheck checks whether web links answer (ADR-0039). It never
// connects to an address that is not public: the check is made where the
// connection is opened, after name resolution and for every redirect, so
// a link cannot make the server reach into its own network.
package linkcheck

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Result is the outcome for one link.
type Result struct {
	OK bool
	// Status is the final HTTP status, or 0 when none was received.
	Status int
	// Problem says why the link is not OK, in words.
	Problem string
}

// Options configure a Checker. Zero values take the defaults.
type Options struct {
	// Allowed decides which addresses may be connected to; Public by
	// default. Tests widen it to reach a local server.
	Allowed func(netip.Addr) bool
	// Timeout bounds one link, redirects included; 10 s by default.
	Timeout time.Duration
	// Concurrency is how many links are checked at once; 4 by default.
	Concurrency int
}

// Checker checks links. It is safe for concurrent use.
type Checker struct {
	client      *http.Client
	timeout     time.Duration
	concurrency int
}

// redirectLimit is the number of redirects followed for one link.
const redirectLimit = 5

var errInternal = errors.New("internal address")

// New returns a Checker.
func New(o Options) *Checker {
	if o.Allowed == nil {
		o.Allowed = Public
	}
	if o.Timeout <= 0 {
		o.Timeout = 10 * time.Second
	}
	if o.Concurrency <= 0 {
		o.Concurrency = 4
	}
	dialer := &net.Dialer{
		Timeout: o.Timeout,
		// Control runs for the address actually connected to, after the
		// name was resolved: a name that resolves to an internal address,
		// or changes its answer between two lookups, is caught here.
		Control: func(_, address string, _ syscall.RawConn) error {
			ap, err := netip.ParseAddrPort(address)
			if err != nil || !o.Allowed(ap.Addr().Unmap()) {
				return errInternal
			}
			return nil
		},
	}
	// dial resolves the name itself, refuses it when any of its addresses
	// is not allowed, and connects to the address it checked, so the
	// answer cannot change between the check and the connection.
	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		var addrs []netip.Addr
		if a, err := netip.ParseAddr(host); err == nil {
			addrs = []netip.Addr{a}
		} else if addrs, err = net.DefaultResolver.LookupNetIP(ctx, "ip", host); err != nil {
			return nil, err
		}
		for _, a := range addrs {
			if !o.Allowed(a.Unmap()) {
				return nil, errInternal
			}
		}
		if len(addrs) == 0 {
			return nil, &net.DNSError{Err: "no address", Name: host, IsNotFound: true}
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(addrs[0].Unmap().String(), port))
	}
	transport := &http.Transport{
		// Never through a proxy: the address check would then see only
		// the proxy.
		Proxy:                 nil,
		DialContext:           dial,
		TLSHandshakeTimeout:   o.Timeout,
		ResponseHeaderTimeout: o.Timeout,
		MaxIdleConnsPerHost:   1,
	}
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= redirectLimit {
				return errors.New("too many redirects")
			}
			return nil
		},
	}
	return &Checker{client: client, timeout: o.Timeout, concurrency: o.Concurrency}
}

// Public reports whether an address is on the public internet: not
// loopback, private, link-local, shared (100.64/10), multicast or
// unspecified.
func Public(a netip.Addr) bool {
	a = a.Unmap()
	if !a.IsGlobalUnicast() || a.IsPrivate() {
		return false
	}
	shared := netip.MustParsePrefix("100.64.0.0/10")
	broadcast := netip.MustParseAddr("255.255.255.255")
	return !shared.Contains(a) && a != broadcast
}

// Check checks one link: a HEAD request, and a GET where the server does
// not answer HEAD. A final status below 400 is OK.
func (c *Checker) Check(ctx context.Context, link string) Result {
	u, err := url.Parse(link)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return Result{Problem: "not a web link"}
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	res := c.request(ctx, http.MethodHead, u.String())
	if res.Status == http.StatusMethodNotAllowed || res.Status == http.StatusNotImplemented {
		res = c.request(ctx, http.MethodGet, u.String())
	}
	return res
}

func (c *Checker) request(ctx context.Context, method, link string) Result {
	req, err := http.NewRequestWithContext(ctx, method, link, nil)
	if err != nil {
		return Result{Problem: "not a web link"}
	}
	req.Header.Set("User-Agent", "wikiwikibangbang-linkcheck")
	resp, err := c.client.Do(req)
	switch {
	case errors.Is(err, errInternal):
		return Result{Problem: "refused: the link points to an internal address"}
	case errors.Is(err, context.DeadlineExceeded):
		return Result{Problem: fmt.Sprintf("no answer within %s", c.timeout)}
	case err != nil && strings.Contains(err.Error(), "too many redirects"):
		return Result{Problem: "too many redirects"}
	case err != nil:
		var dns *net.DNSError
		if errors.As(err, &dns) {
			return Result{Problem: "the host name does not resolve"}
		}
		return Result{Problem: "no connection: " + shortError(err)}
	}
	resp.Body.Close() // the body is never read
	if resp.StatusCode >= 400 {
		return Result{Status: resp.StatusCode, Problem: resp.Status}
	}
	return Result{OK: true, Status: resp.StatusCode}
}

// shortError keeps the last part of a wrapped network error, which names
// the cause without repeating the URL.
func shortError(err error) string {
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i >= 0 {
		return s[i+2:]
	}
	return s
}

// CheckAll checks each distinct link once, several at a time, and returns
// the results by link.
func (c *Checker) CheckAll(ctx context.Context, links []string) map[string]Result {
	out := map[string]Result{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, c.concurrency)
	for _, l := range links {
		mu.Lock()
		_, seen := out[l]
		if !seen {
			out[l] = Result{}
		}
		mu.Unlock()
		if seen {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			r := c.Check(ctx, l)
			mu.Lock()
			out[l] = r
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out
}
