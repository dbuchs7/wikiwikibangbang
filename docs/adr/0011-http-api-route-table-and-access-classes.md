# ADR-0011: The HTTP API is one route table, described by OpenAPI, and every route declares its access class

- **Status:** Proposed
- **Implementation:** Not started
- **Date:** 2026-10-16
- **Deciders:** Project initiators

## Context and problem statement

The web UI, the MCP adapter (ADR-0012), automation and integrations all reach the product through an HTTP API. Two failures are typical for such APIs: the documentation drifts away from what is served, and a route ends up public because nobody said otherwise.

Adapted from atlas [ADR-0043](https://github.com/pblumer/atlas/blob/main/docs/adr/0043-openapi-spec-and-embedded-api-explorer.md) and [ADR-0199](https://github.com/pblumer/atlas/blob/main/docs/adr/0199-route-access-classes.md).

## Decision drivers

- A route cannot be served without being documented.
- A route cannot become public by omission.
- No code generator and no annotation parsing in the build.
- Integrators get a machine-readable contract and an interactive explorer.

## Considered options

1. **Hand-written OpenAPI file** next to the handlers
2. **OpenAPI generated from code comments** by an external tool
3. **One in-code route table** from which both the router and the OpenAPI document are built, with an access class per route

## Decision outcome

Proposed option: **3.**

- Every route is an entry in one table: method, pattern, handler, summary, schemas and **access class**. The server registers handlers by iterating the table; a second function builds an **OpenAPI 3.1** document from the same table at runtime.
- A test asserts that the set of served routes equals the set of documented routes.
- **Access classes:** every route declares `public` or `authenticated`; the zero value is `authenticated`. A request is classified by asking the serving router which pattern matched, so a route registered without a declaration is gated, not open.
- An interactive explorer is served behind authentication (ADR-0013) and works offline from a vendored asset.

### Consequences

- **Positive:** the documentation cannot drift; public exposure is always an explicit, reviewable decision; no generator in the toolchain.
- **Negative / trade-offs accepted:** schemas are written as Go values by hand; the route table becomes a central file that many changes touch.
- **Follow-ups / risks to watch:** API versioning (`/api/v1`) and deprecation policy need a record of their own before the first external integration.

## Pros and cons of the options

### Hand-written OpenAPI file

Strongest form: the contract is designed first, independent of implementation, and readable as one document.

- Good: contract-first design.
- Bad: drifts from the code unless a separate check compares them.

### Generated from comments

Strongest form: established tooling (e.g. swag) produces a complete document with little effort.

- Good: little manual work.
- Bad: an external generator in the build; annotations drift just like comments.

### One route table

- Good: drift and accidental exposure impossible by construction; proven in atlas.
- Bad: schemas maintained by hand in Go.

## Links

- atlas [ADR-0043](https://github.com/pblumer/atlas/blob/main/docs/adr/0043-openapi-spec-and-embedded-api-explorer.md), [ADR-0199](https://github.com/pblumer/atlas/blob/main/docs/adr/0199-route-access-classes.md)
- relates to ADR-0012 (MCP), ADR-0013 (authentication)
