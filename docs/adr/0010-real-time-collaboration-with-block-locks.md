# ADR-0010: Real-time collaboration starts with block-level locks, not a CRDT

- **Status:** Accepted
- **Implementation:** Not started
- **Date:** 2026-10-16
- **Deciders:** Project initiators; accepted by the maintainer 2026-10-17
- **Open question:** Do users accept that two people cannot type in the same paragraph at the same time, or is character-level co-editing (CRDT) required for the product to be credible against Confluence?
- **Question checked:** 2026-10

## Context and problem statement

ADR-0001 separates the document body (continuous, collaborative edits) from properties (discrete events). It leaves open how several people — and AI agents — edit one body at the same time. Confluence offers character-level co-editing. The established technique for that is a CRDT (Yjs, Automerge); mature implementations exist in JavaScript and Rust, and the maturity of Go implementations is unverified.

Adapted from atlas [ADR-0140](https://github.com/pblumer/atlas/blob/main/docs/adr/0140-live-collaborative-modeling-sessions.md), which solved real-time co-editing of diagrams — by people and AI agents — with element-level locks instead of a CRDT.

## Decision drivers

- Several people and agents can work on one page at the same time without overwriting each other.
- No silent loss of edits.
- No CGO and no second server runtime (ADR-0004, ADR-0009).
- AI agents participate through the same mechanism as people (over MCP, ADR-0012).
- The persisted result after a session is an ordinary committed version (ADR-0001, ADR-0008).

## Considered options

1. **Last write wins** with conflict detection on save
2. **Block-level soft locks with broadcast** (atlas approach)
3. **Character-level CRDT** (Yjs-compatible) in the browser, with the server as relay and store

## Decision outcome

Chosen option: **2 as the first step, designed so that 3 can replace it.**

- A **collaboration session** is ephemeral server state attached to an item's body. It is not event-sourced and does not survive a restart; the committed body versions are the durable artifact.
- Participants acquire a **soft, auto-expiring lock per block** (paragraph, heading, list item, table) before editing it. Locks and changes are broadcast to all participants. Edits to different blocks proceed in parallel; edits to the same block are serialised. A lock lapses on release, on disconnect or after a short TTL.
- **Transport:** server-sent events for the stream from server to clients, plain HTTP `POST` for client actions — no new protocol and nothing for a reverse proxy to special-case.
- **Membership** follows the item's permissions: who may edit may lock; who may read may watch.
- **AI agents** join as participants over MCP and see and respect the same locks.
- The server folds accepted changes into the body and commits versions per ADR-0001.

### Consequences

- **Positive:** no CRDT library needed in Go; simple, explainable semantics ("held by Anna", "held by Claude"); agents participate on equal terms; small implementation risk.
- **Negative / trade-offs accepted:** two people cannot type in the same block at the same time — weaker than Confluence; very long blocks (e.g. a large table) become contention points; offline editing is not supported.
- **Follow-ups / risks to watch:** test the open question with users early; keep the body format and session protocol such that a CRDT can replace the lock layer without changing persistence.

## Pros and cons of the options

### Last write wins

Strongest form: trivial to build; most wiki engines worked this way for years.

- Good: minimal effort.
- Bad: conflicts surface after work was done; edits get lost or must be merged by hand.

### Block-level soft locks

- Good: no CRDT dependency; proven in atlas for people and agents; simple to reason about.
- Bad: no simultaneous editing within one block; weaker than the market reference.

### Character-level CRDT

Strongest form: the state of the art (Confluence, Google Docs, Notion); conflict-free by construction, offline-capable, and the browser libraries are mature. The server need not understand the CRDT if it only relays and stores updates.

- Good: best user experience; offline support.
- Bad: the server must still read the document (for search, mentions, derived relations), which needs a Go implementation of the CRDT format or a second runtime; considerable complexity in persistence and garbage collection of CRDT history.

## Links

- atlas [ADR-0140](https://github.com/pblumer/atlas/blob/main/docs/adr/0140-live-collaborative-modeling-sessions.md)
- realises the "document body format and CRDT/OT" follow-up of [ADR-0001](0001-unified-item-data-model.md) in part
