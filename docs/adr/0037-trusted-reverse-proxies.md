# ADR-0037: Forwarded headers count only from trusted reverse proxies

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-19
- **Deciders:** Project initiators (the maintainer runs the server behind a TLS-terminating reverse proxy, 2026-10-19)

## Context and problem statement

The server is commonly run behind a reverse proxy that terminates TLS and forwards plain HTTP. The server then sees the proxy, not the client: the session cookie is not marked `Secure`, because the request did not arrive over TLS; the audit trail (ADR-0029) records the proxy's address; and the login throttle (ADR-0027) counts every client's failed logins against that one address, so one attacker can lock everybody out. Forwarded headers name the client, but any client can send them too.

## Decision drivers

- A client must never choose the address it is throttled and audited under.
- Cookies are `Secure` whenever the client used HTTPS.
- Without configuration nothing changes: a server reached directly must not start believing headers.
- A misconfiguration stops the server rather than weakening it silently.

## Considered options

1. **Trust forwarded headers from a configured list of proxies**
2. **Always trust forwarded headers**
3. **A setting that forces `Secure` cookies**, without forwarded addresses

## Decision outcome

Chosen option: **1**.

- **Setting:** `WWBB_TRUSTED_PROXIES`, a comma-separated list of addresses and networks (`172.17.0.1, 10.0.0.0/8`). Empty means no proxy is trusted, the behaviour so far. An entry that is not an address or network, or a network that contains every address (`0.0.0.0/0`, `::/0`), stops the server at start. The start logs the list.
- **Client address:** only when the connection comes from a trusted address is `X-Forwarded-For` read, from the right: the rightmost address that is not itself a trusted proxy is the client. Addresses further left were written by the client and are ignored. An entry that is not an address ends the search; the connection's address stays. The client address is what the audit trail and the login throttle see.
- **HTTPS:** a request from a trusted proxy with `X-Forwarded-Proto: https` counts as secure, so the session and single sign-on cookies are `Secure`.
- **Container:** behind a port published by Docker, the connection comes from the bridge network's gateway (for example `172.17.0.1`), not from `127.0.0.1`. The audit trail shows that address for logins before the setting is made.

### Consequences

- **Positive:** behind a proxy, cookies are `Secure`, the audit trail names clients, and the throttle limits each client instead of all of them.
- **Negative / trade-offs accepted:** the operator must find the proxy's address as the server sees it; a proxy that does not send `X-Forwarded-Proto` leaves cookies non-`Secure`; only `X-Forwarded-For` and `X-Forwarded-Proto` are read, not the standard `Forwarded` header.
- **Follow-ups / risks to watch:** reading `Forwarded` (RFC 7239) if a proxy in use sends only that; `Strict-Transport-Security` once HTTPS is known to be in front.

## Pros and cons of the options

### Trust forwarded headers from a configured list

- Good: correct addresses and `Secure` cookies behind a proxy; no change for a server reached directly.
- Bad: one more setting the operator has to get right.

### Always trust forwarded headers

Strongest form: no configuration at all; most deployments sit behind a proxy anyway, and the proxy overwrites the headers.

- Good: works out of the box behind a well-configured proxy.
- Bad: a server reached directly, or a proxy that appends rather than overwrites, lets every client pick its address and evade the throttle; the safe default would be the unsafe one.

### A setting that forces Secure cookies

Strongest form: the smallest change that fixes the cookie, and it needs no header at all.

- Good: does not depend on the proxy sending `X-Forwarded-Proto`.
- Bad: leaves the throttle and the audit trail blind to clients, the more serious half of the problem.

## Links

- refines [ADR-0013](0013-authentication-and-security-boundary.md), [ADR-0027](0027-local-accounts-sessions-and-api-tokens.md) (cookies, throttle), [ADR-0029](0029-persisted-security-audit-trail.md) (addresses in the audit trail), [ADR-0030](0030-openid-connect-single-sign-on.md) (state cookie)
