# ADR-0008: Event-sourced persistence in an embedded store, with a single writer

- **Status:** Proposed
- **Implementation:** Not started
- **Date:** 2026-10-16
- **Deciders:** Project initiators
- **Open question:** Does one writer goroutine sustain the write rate of a large installation (many spaces, many concurrent editors), or does the model need partitioning by space from the start?
- **Question checked:** 2026-10

## Context and problem statement

ADR-0001 defines items whose properties, state and relations change through discrete, validated events, and whose document body is committed as versions. Every change must have an author, a timestamp and a history (item history, audit, "who changed this field"). The question is **how items are persisted**.

Adapted from atlas [ADR-0001](https://github.com/pblumer/atlas/blob/main/docs/adr/0001-event-sourcing-and-log-structured-state.md), [ADR-0002](https://github.com/pblumer/atlas/blob/main/docs/adr/0002-single-writer-partition-model.md) and [ADR-0003](https://github.com/pblumer/atlas/blob/main/docs/adr/0003-pebble-as-state-store.md). Atlas uses this model for a workflow engine with a high write rate; here the write rate is lower, but history and auditability matter as much.

## Decision drivers

- Complete, ordered history of every item change, for free (history view, audit, notifications).
- Crash recovery that is simple and correct by construction.
- No divergence between what is stored and what the history says.
- Single-binary deployment without an external database (ADR-0004, ADR-0009).
- Derived views (search index, boards, counters) can be rebuilt from the source of truth.

## Considered options

1. **Mutable rows in an external SQL database** (PostgreSQL)
2. **Mutable state in an embedded key-value store**
3. **Event sourcing: an append-only log as source of truth, state materialised in an embedded key-value store (Pebble), one writer**

## Decision outcome

Proposed option: **3.**

- Every discrete change (property, state transition, relation, comment, permission, committed body version) is an immutable **event** appended to a write-ahead log. The log is the source of truth; current state is a fold of the log.
- State is materialised in **Pebble** (pure Go, no CGO). Indexes for queries, boards and search are further projections of the log and can be rebuilt.
- **One writer goroutine** applies commands, so no locks guard item state and replay is deterministic. Non-deterministic values (timestamps, generated IDs) are frozen into the events. The function that applies an event to state runs both live and on recovery and must be side-effect-free.
- **Durable before visible:** a change is acknowledged, broadcast or notified only after its batch is synced to disk.
- **Live collaboration is not event-sourced.** Keystroke-level editing state (ADR-0010) is ephemeral server memory; only committed versions of a body become events (ADR-0001, "two speeds of change").

### Consequences

- **Positive:** history and audit are inherent; recovery is a replay through the same code that runs live; derived views can be rebuilt after a bug; no database to operate.
- **Negative / trade-offs accepted:** no ad-hoc SQL; every query needs a maintained projection; snapshotting and log compaction are needed so replay does not start at the beginning of time; schema evolution of events must be designed (versioned event types).
- **Follow-ups / risks to watch:** answer the open question with a load test before the first release; design snapshots/compaction; decide how the full-text search index is built (embedded, pure Go) as a projection; GDPR-style erasure in an append-only log needs a design of its own (e.g. per-person encryption keys that can be destroyed).

## Pros and cons of the options

### External SQL database

Strongest form: the default choice for collaboration tools (Confluence, Jira, OpenProject, Plane all use a relational database); ad-hoc queries, mature backup and replication tooling, and operators know it.

- Good: flexible queries; proven at scale; familiar operations.
- Bad: an external dependency contradicting the single-binary goal; history must be built as a second mechanism next to mutable rows, and the two can diverge.

### Embedded key-value store, mutable

Strongest form: no external service, faster than SQL, simplest code.

- Good: single binary.
- Bad: history and audit still bolted on; recovery consistency remains a hand-made concern.

### Event sourcing with Pebble and one writer

- Good: history, audit, recovery and rebuildable projections from one mechanism; proven in atlas.
- Bad: projections for every query; compaction and event versioning to design; erasure needs special handling.

## Links

- atlas [ADR-0001](https://github.com/pblumer/atlas/blob/main/docs/adr/0001-event-sourcing-and-log-structured-state.md), [ADR-0002](https://github.com/pblumer/atlas/blob/main/docs/adr/0002-single-writer-partition-model.md), [ADR-0003](https://github.com/pblumer/atlas/blob/main/docs/adr/0003-pebble-as-state-store.md)
- realises the "persistence" follow-up of [ADR-0001](0001-unified-item-data-model.md)
