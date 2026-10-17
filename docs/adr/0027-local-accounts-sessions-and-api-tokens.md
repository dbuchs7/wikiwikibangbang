# ADR-0027: Local accounts with PBKDF2, in-memory sessions and scoped API tokens

- **Status:** Proposed
- **Implementation:** Partial
- **Date:** 2026-10-17
- **Deciders:** Project initiators (maintainer request 2026-10-17 to build authentication before the API)
- **Open question:** Behind a reverse proxy every client has the proxy's address, so the per-address login throttle throttles everyone at once. Should the server trust a forwarding header from a configured proxy address, and how is that configuration kept from becoming a way to forge addresses?
- **Question checked:** 2026-10

## Context and problem statement

ADR-0013 decides that authentication is on by default, behind one principal boundary, with local accounts, API tokens and later OIDC. Before any content route can exist, the binary needs that boundary. This record decides how the first part is built: password storage, sessions, tokens, CSRF, throttling, the first administrator, and where accounts are kept.

## Decision drivers

- No content route may ever be reachable without a principal; a forgotten declaration fails closed (ADR-0011).
- Credentials are never stored in clear; machine credentials are scoped, expiring and revocable.
- Login answers and timing do not reveal which usernames exist.
- No dependency outside the standard library unless it buys something the standard library cannot (AGENTS.md).
- A first start yields a usable, protected installation.

## Considered options

1. **bcrypt** (golang.org/x/crypto) for passwords, as ADR-0013 names
2. **PBKDF2-HMAC-SHA256** from the standard library (`crypto/pbkdf2`, Go 1.24+)
3. **Argon2id** (golang.org/x/crypto)

## Decision outcome

Proposed option: **2**, with the rest of the boundary as follows.

- **Passwords:** PBKDF2-HMAC-SHA256, 600 000 iterations (the OWASP figure for this function), 16-byte random salt, stored as `pbkdf2-sha256$<iterations>$<salt>$<key>` so the cost can be raised later. At least 12 characters. Comparison in constant time. A login for an unknown username checks a dummy hash of the same cost.
- **Accounts** are event-sourced in `accounts.wal` beside the event log, under the same directory lock. Users have a random, permanent ID; the ID, not the username, is the principal of the item store (ADR-0022), so usernames can change. Administrators are those holding the `admin` system role in the item store.
- **Sessions:** `POST /api/login` returns a random 256-bit secret in the cookie `wwbb_session` (`HttpOnly`, `SameSite=Strict`, `Path=/`, `Secure` on TLS). Only its SHA-256 is kept, in memory; sessions last 12 hours and end on logout, on a password change and on restart.
- **CSRF:** every request body must be `application/json`, which a cross-site form cannot send without a CORS preflight that the server never grants; together with `SameSite=Strict` this covers cookie sessions. Requests with a token in the `Authorization` header are not exposed to CSRF.
- **API tokens:** `wwbb_` followed by 256 random bits (the prefix lets secret scanners find leaks); only the SHA-256 is stored; shown once; scope `full` or `read` (read allows only GET and HEAD); a name and an expiry of at most 366 days are required; revocation deletes. A request that carries a token which does not resolve gets no principal, even if it also carries a valid cookie.
- **Throttle:** ten failed logins in 15 minutes block the client address and the username, counted before the account is looked up; memory is bounded. The peer address is used; forwarding headers are not trusted (open question).
- **First administrator:** on the first start the server creates `WWBB_ADMIN_USERNAME` (default `admin`) with `WWBB_ADMIN_PASSWORD`, or with a generated password logged once, and grants it the admin role. If accounts exist but no administrator (a crash between the two steps), the role goes to that username's account.
- **Endpoints:** `POST /api/login` (public), `POST /api/logout`, `GET /api/me`, `PUT /api/me/password`, `GET|POST /api/tokens`, `DELETE /api/tokens/{id}`, `GET|POST /api/users` (administrators). Request bodies are at most 64 KiB, unknown fields are refused.
- **Audit:** logins, failures, throttling, token creation and revocation, password changes and account creation are recorded in the audit trail ([ADR-0029](0029-persisted-security-audit-trail.md)).
- **Store access:** the HTTP layer reaches the item store only through `storage.DB.Read` and `Write`, which serialise access; this is the single writer of ADR-0008 for concurrent requests.

**Implementation is Partial:** no OIDC (ADR-0013); no account deactivation or deletion; no UI login form yet; no `-insecure-no-auth` mode (none is planned).

### Consequences

- **Positive:** no new dependency; the whole boundary is tested end to end through the route table, including that every API route but login refuses anonymous requests.
- **Negative / trade-offs accepted:** PBKDF2 is not memory-hard, so a GPU attacker who steals `accounts.wal` guesses faster than against bcrypt or Argon2id; a restart logs everyone out; the throttle cannot tell clients apart behind a proxy.
- **Follow-ups / risks to watch:** the open question; OIDC; deactivating and erasing accounts (usernames are personal data and are not crypto-shredded, see ADR-0026's open question); raising the iteration count as hardware gets faster.

## Pros and cons of the options

### bcrypt

Strongest form: the default of most web frameworks and of atlas; decades of analysis; its cost factor is easy to raise; moderately resistant to GPUs because it needs 4 KiB of fast memory.

- Good: proven and familiar; better GPU resistance than PBKDF2.
- Bad: a dependency (golang.org/x/crypto) for one function; passwords longer than 72 bytes are silently truncated.

### PBKDF2-HMAC-SHA256

- Good: in the standard library; FIPS-approved; no length limit; the format stores its cost.
- Bad: not memory-hard; needs a high iteration count, which makes each login cost more CPU.

### Argon2id

Strongest form: the winner of the Password Hashing Competition and the current first recommendation of OWASP; memory-hard, so the best protection of a stolen hash file.

- Good: strongest against offline guessing.
- Bad: a dependency; memory cost per login is a denial-of-service lever that needs its own budget (ADR-0015). A stored-format change to Argon2id later is possible because the format names its algorithm.

## Links

- implements part of [ADR-0013](0013-authentication-and-security-boundary.md); relates to ADR-0011 (access classes), ADR-0015 (budgets), ADR-0022 (principals), ADR-0026 (erasure)
