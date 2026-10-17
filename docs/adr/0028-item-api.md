# ADR-0028: The item API exposes one command per request, through the permission model and one writer

- **Status:** Proposed
- **Implementation:** Partial
- **Date:** 2026-10-17
- **Deciders:** Project initiators (maintainer request 2026-10-17 for an HTTP API with a single writer)
- **Open question:** When the web UI and the MCP adapter (ADR-0012) use this API, do they need batch endpoints (several commands, one record) — for example to create an item and set its body at once — and how is that kept from bypassing the per-command validation?
- **Question checked:** 2026-10

## Context and problem statement

The item model (`internal/item`) has validated commands, sessions that enforce the permission model (ADR-0022), and a durable, crypto-shredded log (ADR-0025, ADR-0026). Authentication exists (ADR-0027). Nothing outside the process can use the model yet. This record decides the shape of the HTTP API for items, types, spaces and roles, and how concurrent requests reach a store that is not safe for concurrent use.

## Decision drivers

- No request can do what the model's session would refuse; unreadable items do not exist for the caller (D10).
- One request is one validated command, so a failure never leaves half a change.
- Concurrent requests apply in a single order, the order of the log (ADR-0008).
- What is served and what is documented cannot drift (ADR-0011).
- JSON values become the model's Go types exactly, so replay and validation see what the client meant.

## Considered options

1. **Resource PATCH**: `PATCH /items/{id}` with any subset of title, properties, state, body
2. **One command per request**: sub-resources for each model command (`PUT …/title`, `PUT …/properties/{field}`, `POST …/transitions`, …)
3. **GraphQL** over the model

## Decision outcome

Proposed option: **2.**

- **Routes** (all authenticated, ADR-0011): types and spaces (`GET|POST /api/types`, `GET|POST /api/spaces`), roles (`PUT /api/spaces/{space}/roles/{principal}`, `PUT /api/groups/{group}/members/{principal}`, `PUT /api/system-roles/{role}/{principal}`), items (`GET|POST /api/items`, `GET /api/items/{ref}`, and per command `title`, `properties/{field}`, `transitions`, `body`, `move`, `embedded`, `restriction`, `trash`, `restore`, `purge`, `redact`), reads (`embeds`, `history`, `relations`), relations (`POST /api/relations`, `DELETE /api/relations/{id}`).
- **Permission model.** Every item handler acts through `Store.As(user ID)`; only defining types, creating spaces, adding group members and granting system roles use trusted commands, after an explicit administrator check. An item the caller may not read answers 404, also for writes.
- **Single writer.** Handlers reach the store only through `storage.DB.Write` (commands) and `Read` (queries), which serialise on one lock: commands apply one at a time in log order, and a read never sees a half-applied batch.
- **References.** `{ref}` is an item ID or a current or former key (D2).
- **Values.** JSON is converted by the field's kind: whole numbers become `int`, others `float64`; dates accept RFC 3339 or `YYYY-MM-DD` and become UTC times; `null` unsets. Unknown fields pass through so the model's validation names them. Persons may write paragraph, heading and embed blocks; `unsupported` blocks come only from importers.
- **Errors.** Validation 400, still referenced 409 (ADR-0021), forbidden 403, not found or unreadable 404, journal failure 500 with no change made.
- **Listings** are paged (ADR-0015): `limit` 1–500 (default 100), an `after` cursor of the last item ID, and a total marked exact.
- **Description.** `GET /api/openapi.json` is generated from the route table; a test asserts that every served API operation is described and nothing else. Request and response schemas are not generated yet.
- **History** shows who did what and when; erased events (ADR-0026) show no content; bodies are not repeated in history entries.

**Implementation is Partial:** no schemas in the OpenAPI document; no web UI on top; no MCP adapter (ADR-0012); no batch commands (open question).

### Consequences

- **Positive:** every API write is exactly one model command, so the API inherits its validation, permissions, atomicity and history without new rules; the route table remains the single place that decides access.
- **Negative / trade-offs accepted:** a client setting three properties makes three requests and three events; one lock serialises reads with writes, which limits throughput (the open question of ADR-0008 applies); listings walk all matching items before paging.
- **Follow-ups / risks to watch:** the open question; schemas; an index for listings when walks get slow; read concurrency (a read–write lock once reads are proven free of side effects).

## Pros and cons of the options

### Resource PATCH

Strongest form: the most familiar REST shape; one request changes everything a form submitted; fewer round trips for the UI.

- Good: compact for clients.
- Bad: one request becomes several model commands, so a failure in the third leaves the first two applied, or it needs a batch mechanism that must re-implement validation across commands; state cannot be patched at all (D3 only allows transitions), so the resource view is a fiction.

### One command per request

- Good: maps one to one onto the model; atomic; history reads like the API calls.
- Bad: more routes and more round trips.

### GraphQL

Strongest form: clients ask for exactly the shape they need — an item with its relations, embeds and history in one round trip — and mutations can still be one per command.

- Good: flexible reads.
- Bad: a dependency and a second schema language; per-field authorisation and query-cost limits (ADR-0015) are much harder to get right than a route table with access classes.

## Links

- implements part of [ADR-0011](0011-http-api-route-table-and-access-classes.md); relates to ADR-0008 (single writer), ADR-0015 (paging), ADR-0021, ADR-0022, ADR-0026, ADR-0027
