# ADR-0015: One place names every resource budget, and a capped listing answers with a page

- **Status:** Accepted
- **Implementation:** Partial
- **Date:** 2026-10-16
- **Deciders:** Project initiators; accepted by the maintainer 2026-10-17

## Context and problem statement

A wiki and issue tracker grows without bound: spaces with tens of thousands of pages, projects with hundreds of thousands of issues, attachments of arbitrary size. Every request needs limits (body size, attachment size, result count), and every list needs a cap. Two failures are typical: limits scattered as constants across handlers, so nobody can say what they are; and lists that silently truncate while the UI shows the length of the page as "total".

Adapted from atlas [ADR-0291](https://github.com/pblumer/atlas/blob/main/docs/adr/0291-one-place-for-budgets.md) and [ADR-0378](https://github.com/pblumer/atlas/blob/main/docs/adr/0378-a-capped-listing-answers-with-a-page.md).

## Decision drivers

- Every limit is visible, documented and configurable in one place.
- A misconfigured limit never prevents the server from starting.
- A list never claims a total it does not know.
- Clients (UI, MCP, integrations) can page through any list the same way.

## Considered options

1. **Constants per handler; lists return arrays**
2. **One budgets struct with derived configuration; lists return a page envelope with an honest total**

## Decision outcome

Chosen option: **2.**

- **Budgets:** one Go struct holds every budget, grouped by what it limits (request body, attachment, document size, result count, …). Environment variable names and the configuration loader are derived from the struct by reflection, so a new budget is configurable the moment it exists. An unset, unparseable or out-of-range value leaves the default in place and is logged at startup — never an error.
- **Pages:** every capped listing returns an envelope with the items, a continuation cursor, and a total that states where it came from: exact (a maintained counter, or an untruncated page) or a lower bound. The constructor forces the caller to choose; there is no default that flatters.
- A number shown to users (e.g. "123 open issues") comes from a counter or a complete walk, never from the length of a page.

### Consequences

- **Positive:** operators can see and tune every limit; users are never shown a wrong total; one paging contract for UI, MCP and API.
- **Negative / trade-offs accepted:** counters must be maintained as projections (ADR-0008); clients must handle cursors.
- **Follow-ups / risks to watch:** decide which counts are worth a maintained counter (board columns, open issues per project) versus a bounded estimate.

## Pros and cons of the options

### Constants per handler, arrays

Strongest form: the simplest code; each handler knows its own needs; arrays are what every client expects.

- Good: minimal.
- Bad: limits are invisible and inconsistent; truncated arrays look complete.

### One budgets struct, page envelopes

- Good: visible, configurable limits; honest totals; proven in atlas.
- Bad: a central struct and an envelope type to maintain.

## Links

- atlas [ADR-0291](https://github.com/pblumer/atlas/blob/main/docs/adr/0291-one-place-for-budgets.md), [ADR-0378](https://github.com/pblumer/atlas/blob/main/docs/adr/0378-a-capped-listing-answers-with-a-page.md)
- relates to ADR-0008 (projections), ADR-0011 (API), ADR-0012 (MCP)
