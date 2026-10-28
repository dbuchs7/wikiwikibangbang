# ADR-0151: Internal start forms start a process as the reader, with a token the sign-in provider issues for the engine

- **Status:** Proposed
- **Implementation:** Not started
- **Date:** 2026-10-28
- **Deciders:** Project initiators (requested 2026-10-28: a process an engine shares with a group may be started by its members from a page, under their own name; below the form, a reader may see the instances they started and open one for its status); proposed for the maintainer's acceptance
- **Open question:** Will atlas accept an access token its sign-in provider issued for it on the routes below, and map it to its account by a stable user ID? pblumer/atlas proposes it in `ADR-draft-provider-access-tokens-for-a-person`, not yet accepted. This record rests on it, and on the drafts named under "Context" being accepted.
- **Question checked:** 2026-10

## Context and problem statement

Since ADR-0148 a start form starts a process through a public link: anonymously, without the engine's credential. ADR-0148 left internal start forms, for signed-in readers or certain groups with the reader as initiator, as a follow-up. They need the engine to accept the reader's identity. Its option 4 rejected starting with the installation's credential while naming the reader, because the engine can neither check a name this server asserts nor tell the reader from the credential's account.

The engine side is being decided in pblumer/atlas, in two drafts of 2026-10-28:

- `ADR-draft-starters-start-processes-and-follow-their-instances`: an application's owner grants users and groups a `starter` grant. A starter may start the application's processes that have a start form, with values bounded by that form. The start records the starter. The start routes take the role `user` and check the grant per caller. Three routes answer only the caller's own:
  - `GET /api/v1/me/startable-processes`
  - `GET /api/v1/me/started-instances`
  - `GET /api/v1/instances/{key}/progress` (which steps are done and active, without people, times or values)
- `ADR-draft-every-user-task-is-addressed`: every user task names people or groups. It defines the progress route that the starter of an instance may read.

Each of these answers depends on who calls. Read with the installation's credential (ADR-0129), they would list that credential's account's processes and instances to every reader, and a start would record that account as its initiator.

What this server has of the reader:

- It signs people in with OpenID Connect: the authorization code flow with PKCE, against Entra ID, Google or a generic provider (ADR-0030, ADR-0066). It verifies the ID token and keeps the issuer and subject (`internal/auth`). It keeps no access or refresh token: `internal/sso` discards them after the sign-in.
- People who sign in with a local password have no provider identity at all.

What atlas has:

- atlas signs people in through OpenID Connect too, and binds an account by `sub` alone, without the issuer (atlas ADR-0475, its table of identities).
- Its API accepts session cookies, API tokens (which act as their account, a machine for every start) and access tokens it issues itself, today for its MCP endpoint only (atlas ADR-0200).

ADR-0129 rejected each person connecting their own engine account by OAuth (its option 7): the wiki would keep a refresh token per person, a store and a flow of their own.

The question is how a reader starts a process from a page so that the engine knows, by its own check, who started it; and how the reader sees what they started.

## Decision drivers

- The engine decides who may start what. The wiki shows what the engine answers for the reader and adds no authority.
- The engine verifies the reader's identity itself. A name this server asserts is not enough (ADR-0148, option 4).
- No credential reaches the browser; the UI's policy stays `default-src 'self'` (ADR-0031, ADR-0129).
- The wiki keeps no long-lived credential of a person on disk.
- Every new bound is a budget (ADR-0015); the body holds no copy of the form (ADR-0148).

## Considered options

1. **A token of the reader for the engine, from the shared sign-in provider**, requested by this server with the authorization code flow when the reader first needs it, and sent with the reader's calls to the engine.
2. **Token exchange on behalf of the reader** (RFC 8693, or a provider's on-behalf-of grant): an access token the reader got for this server is exchanged for one for the engine, without another round trip through the browser.
3. **Each reader connects their atlas account** to the wiki by OAuth with atlas as the authorization server, as ADR-0129 option 7 describes.
4. **The installation's credential, with the reader named** in a header the engine trusts from this server.
5. **A link to the engine's own Start view**, where the reader signs in to the engine.

## Decision outcome

Chosen option: **1**, because:
- the provider both systems already trust vouches for the reader, and the engine checks that itself;
- it is plain OAuth 2.0 that any provider issuing access tokens for an API supports;
- the wiki keeps nothing beyond a short-lived token held in memory.

Option 5 stays the way for everything option 1 cannot serve.

### Preconditions

- The wiki and the engine sign people in with the same provider.
- The provider issues access tokens for the engine's API. As far as this record knows, Google's sign-in issues access tokens only for Google's own APIs (an assumption, not checked against a running installation). Installations that sign in with Google alone therefore do not get internal start forms.
- The engine accepts such a token on the routes below and maps it to its account by a stable user ID, as pblumer/atlas `ADR-draft-provider-access-tokens-for-a-person` proposes. Under that draft, the engine's administrator lists this server's client ID among the applications that may act for people, and the engine requires the delegated scope the settings below name. In Entra ID the `sub` claim is pairwise per application (atlas ADR-0332, checked against the provider's documentation in 2026-09). An access token the wiki obtains therefore carries a different `sub` than atlas's own sign-in sees, and the mapping uses the object ID (`oid`).

### Settings and the token

- **Settings:** an administrator turns internal starts on per engine. They name the sign-in provider (by its ID, ADR-0066) and the scope that provider issues for the engine, for example `api://atlas-prod/start`. Neither is secret. Both are kept with the engine's other settings.
- **Getting the token:** when a reader first needs one (an internal start form shown or sent), the server runs the authorization code flow with PKCE against that provider for the engine's scope:
  - first with `prompt=none`;
  - with the provider's own prompt only when the provider asks for it;
  - the reader returns to the page they were on.
- **Keeping the token:** the access token lives in memory only:
  - it is bound to the reader's session;
  - it is never written to the log, a journal or a cookie, and never sent to the browser;
  - it is dropped at sign-out, at the session's end, at a restart and when it expires.
- **No refresh token:** none is requested or kept. An expired token is fetched again the same way.
- **Readers without a provider account:** a reader who signed in locally, or with a provider the engine does not accept, is told to sign in with the named provider. Nothing else is shown.

### The block

- **Block kind:** `startform` as in ADR-0148.
  - With `Link`, it is a public start form, as before.
  - Without `Link` and with `Process`, the BPMN ID of the process, it is an **internal start form**.
  - A new field `Started`, `list` or empty, chooses whether the reader's instances of that process are listed below the form.
  - `Prefill` and the caption are as in ADR-0148.
  - **Codec version 36** adds `Started` and allows `Link` to be empty when `Process` is set; versions 1 to 35 are still read.
- **Inserting one:** *Insert → Start form* offers the processes the engine lets the editor start, read with the editor's token: `GET /api/engines/{id}/me/startable-processes`, answered from the engine's route of the same name. The editor chooses whether started instances are listed.
- **What the reader sees:** the form, read with the reader's token, filled in beforehand as ADR-0148 describes. The hint below the button says that the engine records who sends it. A reader the engine does not let start the process sees a line that says so and no form.
- **Sending:** `POST /api/engines/{id}/processes/{process}/start` with `{"variables": {…}}`. The server sends it with the reader's token to the engine's start by process ID. The engine's refusals are answered as follows:
  - 403: may not start;
  - 404: no such process;
  - 422 with the engine's reason in one line: values refused, not executable;
  - 503 with `Retry-After`: busy.
- **After sending:** the block thanks the reader, names the new instance's key and refreshes the list below it.
- **The list below the form:** when `Started` is `list`, the block reads `GET /api/engines/{id}/me/started-instances?process=…`, newest first, up to the budget `EngineStartedInstances` (default 20) with a link to more. Each row shows:
  - the key;
  - when the instance started;
  - its state: running, completed or ended;
  - the steps it waits at;
  - whether it is held up.
- **Opening an instance:** clicking its key opens a dialog. It shows the state, the start time and the current steps. It draws the model with bpmn-js, with the steps done and active marked, from `GET /api/engines/{id}/instances/{key}/progress` and the model's XML. Below, it shows the values the reader entered, as the engine lets the starter read them, in the form, read-only. Nothing in it names other people or carries times of other steps.

### Audit, exports and MCP

- **The audit trail** records each internal start as `engine.start` for the reader, with the engine and the process. The engine records the same person as the initiator.
- **Exports** name the block as ADR-0148 does: "[Start form: holidays (atlas)]".
- **Not an MCP tool**, as ADR-0148 decided for start forms.

### Consequences

- **Positive:**
  - A page about a service starts its process under the reader's name, for exactly the people the engine's owner shared it with.
  - The reader follows each request on the same page.
  - The engine decides and records everything. The wiki adds no authority and keeps no long-lived credential.
- **Negative / trade-offs accepted:**
  - The first internal start form a reader meets in a session sends the browser to the provider and back. That is usually without a prompt, but it is a full page load.
  - Without the shared provider, nothing works. Locally signed-in readers and Google-only installations get no internal start forms.
  - The list and the dialog call the engine on every view. There is no cache, as in ADR-0148.
  - While a token is valid, the wiki's server can act at the engine as the reader within the scope granted. The scope is limited to starting and reading one's own instances only if the engine's record makes it so.
- **Follow-ups / risks to watch:**
  - atlas's record on accepting the provider's tokens: the open question.
  - Whether a short-lived token without refresh causes too many round trips in practice.
  - Public start forms of ADR-0148 are unchanged and stay anonymous.

## Pros and cons of the options

### Option 2: token exchange on behalf of the reader

Strongest form: no second round trip through the browser; the provider decides, by its own policy, whether this server may act for the reader at the engine, and the engine still checks the token.

- Good: invisible to the reader; the provider's consent and policy apply.
- Bad: the wiki holds no access token of the reader today, so it would first have to request one for itself at every sign-in and keep it for the session. Support differs by provider: a grant of its own in Entra ID, RFC 8693 elsewhere, none in some. It is the better path where an installation's provider offers it; nothing in option 1 prevents adding it later behind the same block.

### Option 3: each reader connects their atlas account

Strongest form: atlas issues and checks its own tokens, no provider needs to know atlas's API, and it works with any sign-in method atlas offers.

- Good: independent of the provider.
- Bad: the store and flow ADR-0129 option 7 declined: a refresh token per person, kept on disk to be useful. atlas's OAuth tokens serve its MCP endpoint today (atlas ADR-0200), so atlas would have to widen them to its REST routes as well.

### Option 4: the installation's credential, with the reader named

Strongest form: no token per person at all, no round trip, one trusted relation between two servers an administrator configures.

- Good: simplest to run.
- Bad: the engine would trust whatever name this server sends. A compromised or misconfigured wiki could start processes as anybody, and the engine's record of who started what would be only as good as the wiki. ADR-0148 rejected it for that reason.

### Option 5: a link to the engine's own Start view

Strongest form: no new code path; the engine's own page signs the reader in, offers what they may start, and lists what they started, once atlas's starter draft is built.

- Good: complete and always current.
- Bad: the reader leaves the page and its instructions; nothing is filled in beforehand. Kept for engines and readers that option 1 does not reach, and as the block's fallback link.

## Links

- extends [ADR-0148](0148-start-forms-and-the-forms-of-a-process.md) (start forms; its follow-up on internal start forms)
- relates to [ADR-0015](0015-resource-budgets-and-paged-listings.md) (budgets), [ADR-0030](0030-openid-connect-single-sign-on.md) and [ADR-0066](0066-several-identity-providers-entra-id-and-google.md) (sign-in), [ADR-0031](0031-web-ui-shell.md) (the UI's policy), [ADR-0129](0129-bpmn-diagrams-and-processes-from-a-process-engine.md) (engines and their credential)
- depends on pblumer/atlas `ADR-draft-starters-start-processes-and-follow-their-instances`, `ADR-draft-every-user-task-is-addressed` and `ADR-draft-provider-access-tokens-for-a-person`
