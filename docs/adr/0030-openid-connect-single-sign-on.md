# ADR-0030: Single sign-on through OpenID Connect, with go-oidc and just-in-time accounts without roles

- **Status:** Accepted
- **Implementation:** Partial
- **Date:** 2026-10-17
- **Deciders:** Project initiators (maintainer request 2026-10-17); accepted by the maintainer 2026-10-17
- **Open question:** Must an existing local account be linkable to a provider identity (for example the first administrator), and by which proof — a logged-in user confirming the link, or an administrator binding issuer and subject — so that linking never relies on a claimed e-mail address?
- **Question checked:** 2026-10

## Context and problem statement

ADR-0013 requires an OpenID Connect relying party so organisations can use their identity provider (Microsoft Entra ID, Keycloak, Google, a public-sector IdP). ADR-0027 built local accounts and sessions behind one principal boundary. This record decides how OIDC is implemented and how provider identities become accounts.

## Decision drivers

- A forged, replayed or misdirected login must fail: signature, issuer, audience, expiry, nonce, state bound to the browser, PKCE.
- Whoever controls a provider must not thereby control existing accounts or roles in the product.
- Security-critical protocol code is the last place to write from scratch.
- The server starts even while the provider is down.
- Every new dependency has a reason and a compatible license (AGENTS.md).

## Considered options

1. **Own implementation** on the standard library (JWT parsing, JWKS, discovery)
2. **github.com/coreos/go-oidc/v3 with golang.org/x/oauth2**
3. **An authenticating reverse proxy** (oauth2-proxy) in front of the binary

## Decision outcome

Chosen option: **2.**

- **Dependencies:** `github.com/coreos/go-oidc/v3` (Apache-2.0) verifies ID tokens with keys from the provider's JWKS; `golang.org/x/oauth2` (BSD-3-Clause) runs the code exchange; `github.com/go-jose/go-jose/v4` (Apache-2.0) comes with go-oidc. Their notices ship in `THIRD_PARTY_NOTICES.md`, in the release archives and in the image (`/licenses`); a test fails when `go.mod` names a module the file does not.
- **Flow:** authorization code with PKCE (S256), a random state and nonce per login, kept in memory for 10 minutes and usable once (at most 10 000 open logins). The state is also put in a cookie (`HttpOnly`, `SameSite=Lax` because the provider's redirect is a cross-site navigation, path `/api/oidc/`); the callback must carry the same state as the cookie, so a callback cannot be replayed into another browser. The ID token's signature, issuer, audience and expiry are verified by go-oidc, the nonce here.
- **Accounts:** an identity is the pair issuer and subject, never the e-mail address. A first login creates a new account (just-in-time) without a password and **without any role**; the username comes from `preferred_username` or the e-mail address, suffixed if taken. An identity is never linked to an existing account automatically, in particular not by e-mail, which would let whoever controls the provider take that account. Provider claims never grant roles (ADR-0013).
- **Sessions** after a sign-on are ordinary product sessions (ADR-0027); provider tokens are not kept, so there is no refresh and no logout at the provider.
- **Configuration** from the environment: `WWBB_OIDC_ISSUER`, `WWBB_OIDC_CLIENT_ID`, `WWBB_OIDC_CLIENT_SECRET` (environment only, not a flag), `WWBB_OIDC_REDIRECT_URL`, `WWBB_OIDC_NAME`. Issuer and redirect URL must be https, except for localhost. A misconfiguration stops the start; an unreachable provider does not — discovery happens at the first login.
- **Routes** (public, listed in the test of public routes): `GET /api/oidc` (enabled, display name), `GET /api/oidc/login`, `GET /api/oidc/callback`. Failures redirect to `/?login_error=sso` without the provider's details, and are audited (ADR-0029).

**Implementation is Partial:** one provider only; no linking of existing accounts (open question); no logout at the provider; no group or role mapping, by design.

### Consequences

- **Positive:** standard, well-reviewed token verification; users of an organisation sign in with their existing identity; the product's authorisation stays entirely in the product.
- **Negative / trade-offs accepted:** three new modules to keep updated; every new sign-on user needs roles granted by an administrator before seeing anything; the local first administrator cannot use single sign-on.
- **Follow-ups / risks to watch:** the open question; several providers; SAML (ADR-0013's open question); dependency updates (a vulnerability scanner in CI).

## Pros and cons of the options

### Own implementation

Strongest form: no dependency; the subset needed (RS256/ES256 verification, a JWKS fetch, discovery) is small and the standard library has every primitive.

- Good: nothing to update but our own code.
- Bad: JWT verification has a long history of subtle failures (algorithm confusion, `none`, key selection, clock skew); a mistake is an authentication bypass. Reviewed libraries are the cheaper risk.

### go-oidc with x/oauth2

- Good: the de-facto Go implementation, maintained, narrow in scope; permissive licenses.
- Bad: dependencies to track.

### Authenticating reverse proxy

Strongest form: no identity code in the product at all; operators already run such proxies; supports every provider oauth2-proxy supports.

- Good: zero code.
- Bad: the product must then trust a header, which ADR-0013 rejects: one request that reaches the binary directly is authenticated as anyone.

## Links

- implements the federation part of [ADR-0013](0013-authentication-and-security-boundary.md); relates to ADR-0027 (sessions), ADR-0029 (audit), ADR-0002 (license)
