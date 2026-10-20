# ADR-0068: OAuth for AI assistants on the MCP endpoint

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-20
- **Deciders:** Project initiators (the maintainer asked that AI access work as it does in atlas, 2026-10-20)

## Context and problem statement

The MCP endpoint (ADR-0012) acts with the credential its request carries: a session cookie or an API token. That serves a client a person configures on their own machine — Claude Code, a `.mcp.json`, the stdio adapter. It does not serve a **hosted** assistant such as Claude on claude.ai: its connector dialog takes a URL and, under advanced settings, an OAuth client ID and secret, nothing else. Pasting a long-lived API token into a third party's configuration store is the opposite of what tokens were made for (ADR-0013): it outlives the session and is not tied to the person who uses it. Such a client meets a `401` without a pointer and stops.

The MCP authorization specification (revisions 2025-06-18 and 2025-11-25) says how this is solved: the MCP server is an OAuth resource server that publishes RFC 9728 metadata and points to it from its `401`; the authorization server publishes RFC 8414 metadata including `code_challenge_methods_supported` (a client must refuse to proceed without it); the client uses PKCE with S256 and names the resource (RFC 8707); pre-registered credentials come first, dynamic client registration (RFC 7591) is optional. atlas solved the same problem the same way (its ADR-0200, "AI access"); this record follows it.

## Decision drivers

- A hosted assistant must be connectable without a long-lived shared bearer.
- A tool call stays exactly as privileged as the person behind it, and is attributed to them (ADR-0012).
- An OAuth token reaches what the assistant needs — the MCP tools — and nothing else; the tools themselves leave moving, deleting, restrictions and administration to people.
- A person sees and withdraws their own approvals without an API client.
- What a public endpoint can make others store is bounded (ADR-0015).
- A half-built authorization server is worse than none: a compliant client must find everything the specification requires.

## Considered options

1. Leave it: hosted assistants cannot connect; local clients use API tokens.
2. A header-injecting reverse proxy in front of `/mcp`.
3. Resource server and a minimal authorization server, with clients registered by an administrator, and dynamic registration as an option that is off by default.
4. Delegate to an external identity provider (the single sign-on providers of ADR-0066) and be a resource server only.

## Decision outcome

Chosen option: **3**, because it is what atlas runs, it is the smallest set a compliant client talks to, and it keeps the person as the actor.

- **Resource server.** `GET /.well-known/oauth-protected-resource` and its path-suffixed form `…/mcp` are public. Every `401` of the gate carries `WWW-Authenticate: Bearer realm="wikiwikibangbang", resource_metadata="<origin>/.well-known/oauth-protected-resource/mcp"`.
- **Authorization server.** `GET /.well-known/oauth-authorization-server` advertises the authorization and token endpoints, `code_challenge_methods_supported: ["S256"]`, the grants `authorization_code` and `refresh_token`, client authentication `client_secret_basic`, `client_secret_post` and `none`, and `authorization_response_iss_parameter_supported`.
- **Authorize.** `GET /oauth/authorize` is public. It checks the client and its exact redirect address first; a request with an unknown client or a foreign redirect address is explained to the person and never sent anywhere (RFC 6749 §4.1.2.1). A missing S256 challenge or a foreign resource goes back to the client as an error. A good request leads to the consent page of the UI, `/#/oauth?…`, which survives signing in, single sign-on included. The page names the application, says when it registered itself, and lets the person choose what it may do: read only (preselected, the smaller right) or read and write. On *Allow* it posts `POST /api/oauth/approve` — a JSON request of a session, so CSRF-protected as every other (ADR-0027), and refused to tokens: approving is the person's act. The answer is the redirect address with a one-time code, the state and `iss`.
- **Token.** `POST /oauth/token` takes a form (RFC 6749), authenticates the client by Basic or by form fields, exchanges a code once for the right verifier, redirect address and resource, and turns refresh tokens over: a refresh token presented after it was turned over shows a copy and ends the approval (RFC 9700). Failed client or grant checks are throttled per address as failed logins are (ADR-0013). Codes live one minute, access tokens one hour, refresh tokens thirty days. Errors follow RFC 6749 §5.2.
- **Read only or read and write.** The choice is part of the approval and journaled with it; the tokens carry it through every refresh. A read-only token passes the gate as a read-only API token does (ADR-0013): the MCP endpoint takes its tool calls, and each call that would change something is refused on its own. The approvals list shows the choice; to change it, the person withdraws the approval and approves again.
- **Where a token counts.** An OAuth access token (`wwbba_…`) resolves to its person only on `POST /mcp`, and only when it was issued for this server's MCP address, and on the requests the MCP adapter makes in process for its tool calls. Those are marked by the in-process transport alone; nothing from outside can set that mark. Anywhere else the token is no credential at all.
- **Clients.** An administrator registers an application on the *AI access* page: Claude is known with its redirect address `https://claude.ai/api/mcp/auth_callback`; any other takes the address from its connector dialog. The page shows the three values the dialog asks for — MCP address, client ID, client secret — each with a copy button, the secret once. It also compares the address the server publishes with the one the page was opened at, and says what to set when they differ.
- **Dynamic registration.** `WWBB_OAUTH_DYNAMIC_REGISTRATION=on` opens `POST /oauth/register` (RFC 7591). Off means absent: no route and no `registration_endpoint` in the metadata, so a client learns the policy rather than meeting a refusal. A self-registered application is marked so on the consent page and in the lists. Their number is bounded by the budget `OAuthClients` (200); at the bound the oldest one nobody approved goes, and when all are approved the registration is refused.
- **Approvals.** Every person lists and withdraws their approvals on the *AI access* page; withdrawing ends the tokens at once. Removing an application ends every approval and token of it.
- **Storage.** Clients (with the hash of the secret), approvals and the hash of the current and the last refresh token are events in the accounts journal; codes and access tokens are in memory only, so a restart ends access tokens and the assistant refreshes. Audit entries record registration, removal, approval, withdrawal and failed token requests.
- **Origin.** The server names itself by the origin the request arrived at, as it sees it: `https` behind a trusted proxy that says so (ADR-0037). That is the address the client used, which RFC 9728 requires the `resource` to match. Responses carry `Cache-Control: no-store`, so a forged `Host` header misleads only its own sender.

### Consequences

- **Positive:** Claude and other hosted assistants connect with three values, and each person approves them for themselves; no long-lived secret of a person lives in a third party's store; every tool call is the person's; an approval is withdrawn on a page.
- **Negative / trade-offs accepted:** wikiwikibangbang is now an authorization server, a protocol surface of its own; a restart ends the access tokens, so an assistant refreshes once more; behind a proxy that is not trusted the server publishes its `http` address and a hosted assistant cannot connect — the page says so.
- **Follow-ups / risks to watch:** Client ID Metadata Documents, the specification's next registration step after pre-registration, are not built; delegating the authorization server to an external identity provider remains possible, because the resource-server half does not assume this server issues the tokens.

## Pros and cons of the options

### Option 1: leave it

Strongest form: the MCP endpoint already works for every client a person configures, and an authorization server is a security-relevant surface that a wiki need not have.

- Good: nothing new to secure.
- Bad: the most common assistants are hosted; they cannot connect, or only with a pasted long-lived token, which is worse than OAuth.

### Option 2: a header-injecting reverse proxy

Strongest form: no code in the product; an operator puts a proxy with one API token in front of `/mcp`.

- Good: no protocol work.
- Bad: every tool call acts as the proxy's token, not as the person; attribution and permissions (ADR-0012) are lost; the token is shared.

### Option 3: resource server and minimal authorization server

Strongest form: it is what the specification asks for, what atlas runs, and what a hosted client actually talks to.

- Good: the person stays the actor; pre-registered clients come first, as the specification orders; dynamic registration is there for operators who want it.
- Bad: a new public surface (authorize, token, optional registration), bounded and throttled but real.

### Option 4: delegate to an external identity provider

Strongest form: Entra ID or Google already issue tokens; wikiwikibangbang would only validate them.

- Good: no authorization server here.
- Bad: single sign-on is optional (ADR-0030) and many installations have none; the tokens would carry the provider's audience and claims, not a person of this wiki with an approval they can withdraw here; the resource-server half chosen now keeps this open for later.
