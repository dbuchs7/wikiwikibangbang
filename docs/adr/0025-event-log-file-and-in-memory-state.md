# ADR-0025: The event log is one file of checksummed batch records; state is folded in memory until it outgrows it

- **Status:** Accepted
- **Implementation:** Partial
- **Date:** 2026-10-17
- **Deciders:** Project initiators (maintainer request 2026-10-17: "build persistence"); accepted by the maintainer 2026-10-17

## Context and problem statement

ADR-0008 proposes event sourcing: an append-only log as the source of truth, state materialised in Pebble, one writer. The item model (`internal/item`) already works that way in memory: every command records events, `apply` folds them into state, `Replay` rebuilds the state from a log. Nothing survived a restart.

This record decides the first persistent form: what is written, in which format, how a crash is recovered, and whether Pebble is needed now.

## Decision drivers

- An acknowledged change survives a crash; an unacknowledged one leaves no trace (durable before visible, ADR-0008).
- A crash in the middle of a write is recovered automatically; real damage is never silently "repaired".
- Live state and state rebuilt from disk are identical, bit for bit, or the history lies.
- No CGO, no external service, every release platform (ADR-0004, ADR-0024).
- The smallest mechanism that is correct, with a clear path to the full design of ADR-0008.

## Considered options

1. **Pebble now**, as ADR-0008 proposes: events and materialised state in Pebble
2. **SQLite** (pure-Go port) as an event table
3. **One append-only log file of checksummed records, one record per command; state folded in memory on startup**

## Decision outcome

Chosen option: **3.**

- **Journal hook.** `item.Store` takes a `Journal`. A command builds its batch of events, hands the whole batch to the journal, and applies it only if the journal returns success. A failed write is a failed command with unchanged state (`TestAChangeThatIsNotPersistedDoesNotHappen`).
- **One record per command.** A multi-event command (e.g. creating an embedded item) is one record, so it is durable as a whole or not at all. This makes the batch atomicity of ADR-0020 hold on disk.
- **File format** (`internal/wal`): header `WWBBLOG1`, then records of length, bitwise complement of the length, CRC-32C, payload. Every append is followed by `fsync`; a new file's directory is synced too.
- **Recovery.** An incomplete last record is a torn write from a crash, never acknowledged, and is truncated on open. A damaged length, header or checksum *before* the last record cannot come from a crash; opening refuses the directory with `ErrCorrupt` rather than guessing. The complement makes a damaged length detectable, so it cannot masquerade as a torn tail and silently drop the records after it. `TestEveryCrashPointRecovers` cuts a log at every byte.
- **Sync failures are fatal to the open log.** After a failed `fsync` the kernel may have dropped the dirty pages and cleared the error, so a retry can report success for lost data. The log refuses further appends until it is reopened, which re-reads what is really on disk.
- **One process per data directory**, by an OS lock on `LOCK` (flock on Unix, an unshared handle on Windows). The OS releases it when the process dies, so a crash leaves no stale lock.
- **Event format** (`item.EncodeBatch`): JSON, versioned (`"v":1`; newer versions are refused, unknown fields are refused). Property values carry their Go type, so an `int` stays an `int` and a date stays a time; dates and event timestamps are normalised to UTC before they are applied, so live and replayed state agree. Roles are stored by name, not by their numeric value. Numbers must be finite.
- **Guards.** Every test of `internal/item` runs through a journal that encodes, decodes and re-encodes each batch and, at the end, compares the state rebuilt from the decoded log with the live state. A golden file (`internal/item/testdata/events.golden.jsonl`) covering every event kind pins the format: changing it fails the build until `CodecVersion` and a migration say otherwise.
- **State stays in memory.** On startup the whole log is replayed. Pebble, snapshots and compaction (ADR-0008) are deferred until startup time or memory use require them; the journal interface is where they attach.
- **Binary.** `serve -data-dir` opens the directory (default `./data`; `/data` as a volume in the container image); `check -data-dir` verifies a directory without locking or changing it.

### Consequences

- **Positive:** restarts keep everything; crash recovery is automatic and tested at every byte; no dependency was added; the format is human-readable for debugging and pinned against accidental change.
- **Negative / trade-offs accepted:** startup time and memory grow with the whole history; JSON is larger and slower than a binary encoding; one `fsync` per command limits write throughput to what the disk syncs per second (no group commit yet).
- **Follow-ups / risks to watch:**
  - **Erasure on disk.** Purge (ADR-0021) and redaction (ADR-0022) cleared content in the state only; the earlier events in the log still contained it. Answered by [ADR-0026](0026-crypto-shredding-of-item-content.md): content is encrypted per item and erasure destroys the key.
  - The store is not safe for concurrent use; the API must funnel all commands through one writer before it uses the store.
  - Snapshots, compaction and Pebble when the history makes startup slow; group commit when write throughput matters.
  - Nested structs in the format (type definitions, documents) are encoded with their Go field names; the golden test catches a rename, but the fix then needs a codec version.

## Pros and cons of the options

### Pebble now

Strongest form: it is the destination ADR-0008 names; it solves state larger than memory, indexes and fast startup at once, it is pure Go, and atlas runs on it, so the design is proven.

- Good: no second migration later; range scans and indexes available.
- Bad: a large dependency and a second storage format before any query needs it; the WAL-and-state consistency of two stores must be designed now. The in-memory store answers every query the prototype has, so Pebble would add machinery without a current need.

### SQLite as an event table

Strongest form: the most battle-tested embedded storage there is, with crash safety, tooling and ad-hoc SQL for debugging.

- Good: durability and recovery are solved by the library.
- Bad: the pure-Go port is a large transpiled dependency; SQL adds nothing to an append-only log that is read once at startup; it moves away from ADR-0008's direction rather than towards it.

### Log file, state in memory

- Good: small, dependency-free, fully tested; the journal hook keeps the path to ADR-0008 open.
- Bad: bounded by memory and replay time; erasure on disk needs its own mechanism (ADR-0026).

## Links

- refines [ADR-0008](0008-event-sourced-persistence.md) (first stage of its implementation)
- relates to ADR-0004 (no CGO), ADR-0020 (batches), ADR-0021 (purge), ADR-0022 (redaction), ADR-0024 (platforms, container image)
