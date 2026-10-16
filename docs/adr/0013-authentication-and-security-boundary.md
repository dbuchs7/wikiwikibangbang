# ADR-0013: Authentication is required by default, behind one principal boundary

- **Status:** Proposed
- **Implementation:** Not started
- **Date:** 2026-10-16
- **Deciders:** Project initiators
- **Open question:** Which identity providers must be supported in the first release (OIDC only, or SAML as well, e.g. for public-sector identity services)?
- **Question checked:** 2026-10

## Context and problem statement

A knowledge and work tool holds internal documents, decisions and personal data. It is reached by people (browser), machines (CI, integrations) and agents (MCP). The question is **how callers are authenticated, and what is protected by default**.

Adapted from atlas [ADR-0044](https://github.com/pblumer/atlas/blob/main/docs/adr/0044-user-management-and-authentication-boundary.md), [ADR-0195](https://github.com/pblumer/atlas/blob/main/docs/adr/0195-auth-on-by-default.md), [ADR-0194](https://github.com/pblumer/atlas/blob/main/docs/adr/0194-api-tokens.md), [ADR-0210](https://github.com/pblumer/atlas/blob/main/docs/adr/0210-federated-authentication.md) and [ADR-0197](https://github.com/pblumer/atlas/blob/main/docs/adr/0197-login-throttle-and-audit-log.md). Atlas started with authentication off by default and later reversed that (ADR-0195); this project starts where atlas ended.

## Decision drivers

- A demo must not silently become an open deployment.
- Handlers depend on an authenticated principal, never on a specific mechanism (cookie, token, IdP).
- Machines get credentials that are safe to hand out: scoped, expiring, revocable, never stored in clear.
- Organisations can use their existing identity provider.
- Brute-force attempts and security-relevant events are limited and recorded.

## Considered options

1. **Authentication optional, off by default**
2. **Authentication required by default; local accounts, API tokens and OIDC behind one principal boundary**
3. **Delegate everything to a reverse proxy** that sets an identity header

## Decision outcome

Proposed option: **2.**

- **On by default.** Starting without authentication requires an explicit flag and logs a warning naming what is open. The first start seeds one administrator from environment variables or generates a password and logs it once.
- **Principal boundary.** Middleware resolves every request to a principal (user ID, roles, scope) in the request context. Users have a stable, never-reused ID independent of username and email, so an external IdP can change either.
- **Local accounts** with bcrypt password hashes, kept as break-glass access when federation is configured.
- **API tokens:** only a SHA-256 hash is stored; the secret is shown once; each token has a scope (e.g. full, read-only, metrics) and an expiry chosen at creation; revocation is deletion; a recognisable prefix lets secret scanners find leaked tokens.
- **Federation:** an OIDC relying party with just-in-time provisioning; roles are not taken from IdP claims in the first step.
- **Login throttle** per address and per account, charged before the account is looked up so it does not reveal which accounts exist. A **security audit trail** records logins, failures, token creation and revocation, and role changes.

### Consequences

- **Positive:** secure default; one seam for every authentication mechanism; machine credentials that can be handed out safely.
- **Negative / trade-offs accepted:** first start needs an admin credential; more to build before the first public release.
- **Follow-ups / risks to watch:** the permission model inside the product (spaces, item restrictions, relation visibility) is a separate record, as foreseen in ADR-0001; decide on SAML (open question).

## Pros and cons of the options

### Optional, off by default

Strongest form: the fastest first experience; a local trial needs no setup at all.

- Good: zero friction for evaluation.
- Bad: atlas reversed this decision because demos became deployments unnoticed.

### Required by default, one boundary

- Good: secure default; mechanisms interchangeable.
- Bad: more up-front work.

### Reverse proxy header

Strongest form: reuses whatever single sign-on an organisation already runs, with no identity code in the product.

- Good: no authentication code.
- Bad: trusting a header is not authentication; one misrouted request to the binary's own port is a public login.

## Links

- atlas [ADR-0044](https://github.com/pblumer/atlas/blob/main/docs/adr/0044-user-management-and-authentication-boundary.md), [ADR-0195](https://github.com/pblumer/atlas/blob/main/docs/adr/0195-auth-on-by-default.md), [ADR-0194](https://github.com/pblumer/atlas/blob/main/docs/adr/0194-api-tokens.md), [ADR-0210](https://github.com/pblumer/atlas/blob/main/docs/adr/0210-federated-authentication.md), [ADR-0197](https://github.com/pblumer/atlas/blob/main/docs/adr/0197-login-throttle-and-audit-log.md)
- relates to ADR-0011, ADR-0012
