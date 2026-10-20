# ADR-0066: Several identity providers side by side: Microsoft Entra ID and Google

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-20
- **Deciders:** Project initiators (the maintainer asked that Microsoft Entra ID and Google both serve as identity providers, 2026-10-20)

## Context and problem statement

Single sign-on (ADR-0030) knows one OpenID Connect provider. Organisations mix: staff sign in with Microsoft, partners or a second company with Google. Google, unlike a company tenant, signs in anyone with a Google account, and every new identity gets an account (just-in-time, without roles). How are several providers offered, and how is Google kept to the organisation's people?

## Decision drivers

- One sign-in page, one button per provider, no choice of provider to configure per user.
- Configuration stays in the environment (ADR-0030); secrets never in a flag or a journal.
- An identity is its issuer and subject (ADR-0013); a person is never matched across providers by e-mail address.
- Only what the provider asserts counts: a domain from an unverified address is no proof.

## Considered options

1. **A list of providers with presets for Entra ID and Google, one callback address, the provider named in the state cookie, and an optional list of allowed domains per provider.**
2. **One callback address per provider.**
3. **Trusting the e-mail domain alone, verified or not.**

## Decision outcome

Chosen option: **1**.

- **Configuration:** `WWBB_ENTRA_TENANT_ID` (a GUID), `_CLIENT_ID`, `_CLIENT_SECRET`, optionally `_DOMAINS`; `WWBB_GOOGLE_CLIENT_ID`, `_CLIENT_SECRET`, `_DOMAINS`; the generic `WWBB_OIDC_*` as before, plus `WWBB_OIDC_DOMAINS`. The callback is `WWBB_OIDC_REDIRECT_URL`, else `WWBB_PUBLIC_URL` + `/api/oidc/callback`. A misconfiguration stops the start.
- **Entra ID** uses the tenant's v2.0 issuer, `https://login.microsoftonline.com/<tenant ID>/v2.0`: only the tenant's accounts sign in, and a token's issuer matches. A tenant given by domain name or as `common` is refused, because the issuer in the tokens would not match it.
- **Google** uses `https://accounts.google.com`. Without `WWBB_GOOGLE_DOMAINS` any Google account may sign in; the server logs a warning at start.
- **Domains:** when set, an identity signs in only with a domain the provider asserts: Google's hosted domain (`hd`), a verified e-mail address, or, for Entra ID, the user principal name, whose domain the tenant verified. Otherwise the sign-in is refused, audited, and the page says the domain is not allowed.
- **Flow:** `GET /api/oidc/login?provider=<id>` starts it; the state cookie carries the provider's ID beside the state, so the one callback finds the provider and a state cannot be replayed against another. `GET /api/oidc` lists the providers with ID, name and kind; the page shows a neutral button with each provider's mark.

### Consequences

- **Positive:** both kinds of accounts sign in from one page; Google is limited to the organisation's domains.
- **Negative / trade-offs accepted:** the same person signing in with Microsoft and with Google gets two accounts; linking them is an administrator's later decision, never automatic.
- **Negative / trade-offs accepted:** guest users of an Entra tenant have principal names in the tenant's domain and pass a domain list that names it.

## Pros and cons of the options

### Option 2: one callback per provider

Strongest form: no provider in the cookie.

- Bad: each provider needs its own redirect registration and route; the cookie already binds the flow to the browser, so naming the provider there costs nothing.

### Option 3: the e-mail domain alone

Strongest form: works with every provider.

- Bad: a provider may return an address it never verified; anyone could claim the organisation's domain.

## Links

- relates to ADR-0013 (identities), ADR-0030 (OpenID Connect), ADR-0061 (account requests)
