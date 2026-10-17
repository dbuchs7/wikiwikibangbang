# ADR-0026: Item content is encrypted with a key per item; purge and redaction destroy the key

- **Status:** Accepted
- **Implementation:** Partial
- **Date:** 2026-10-17
- **Deciders:** Project initiators (maintainer request 2026-10-17 to close ADR-0025's open question before the API); accepted by the maintainer 2026-10-17
- **Open question:** Who and what is still identifiable after an erasure — actor names in every event, principals in groups and restrictions, the key and type of a tombstone — and does erasing a *person* (as opposed to an item) need its own mechanism once user accounts exist (ADR-0013)?
- **Question checked:** 2026-10

## Context and problem statement

ADR-0021 (purge) and ADR-0022 (redaction) promise that an item's content goes away. Since ADR-0025 the history is an append-only file: purge cleared the state, but every earlier event of the item still held its title, properties, body and raw import source, on disk and in `History`. For personal data this breaks the right to erasure (Swiss revDSG, EU GDPR), and it made "redacted" a false statement.

An append-only log and a deletion requirement contradict each other. The question is which one bends.

## Decision drivers

- Erased content is unrecoverable from the data directory, not merely hidden.
- The history stays auditable: who changed an item, and when, survives its erasure.
- The event log stays append-only; recovery stays a replay (ADR-0008, ADR-0025).
- Live state and replayed state stay identical.
- Cheap now, while no data directory with real content exists.

## Considered options

1. **Rewrite the event log** without the erased content (compaction on erasure)
2. **Keep content out of the log**, in a mutable content store the events point to
3. **Crypto-shredding:** encrypt each item's content with its own key; erasing destroys the key

## Decision outcome

Chosen option: **3.**

- **What is content.** The fields `Event.erase` removes: title, property values, body, and the raw import source, in the events `ItemCreated`, `TitleSet`, `PropertySet`, `BodyCommitted` and `ItemImported`. Structure stays readable: IDs, keys, types, spaces, parents, states, relations, restrictions, actors, times, and the reason of a redaction.
- **Encryption.** The storage layer seals the content of each such event with AES-256-GCM under its item's key, a random nonce, and the event's sequence number, kind and item as associated data, so sealed content cannot be moved to another event. The event format is version 2 (`sealed`, `erased`); version 1 logs (plaintext) are still read.
- **Keys** live in `keys.wal`, apart from the event log, one random 256-bit key per item, created with the item's first content. A new key is fsynced **before** the batch that uses it, so no event on disk lacks its key.
- **Erasure.** When a purge or redaction is durable, the keys of the affected items are destroyed by rewriting `keys.wal` without them (temporary file, fsync, rename, directory fsync). The file keeps the *IDs* of destroyed keys, so a key that is missing without having been destroyed is reported as damage, never silently treated as erasure.
- **Healing.** On start, keys of purged items (a crash between the purge and the key's destruction) and keys of items the log does not know (a crash between the key and its batch) are destroyed.
- **Memory and history.** Purge and redaction also erase the content of the item's past events in the in-memory log, so `History` shows the events, marked `Erased`, without content, before and after a restart alike. Decoding an event whose key was destroyed yields exactly the same erased event.
- **Guards:** a test proves item content never reaches the event file in plaintext; one that after purge and redaction the key is gone from disk and the history holds no content after a restart; crash healing; a lost keys file refused; authentication of sealed content. The sealed layout is pinned by its own golden file.

**Implementation is Partial:** the open question; backups (ADR-0016) do not exist yet and must treat `keys.wal` with a short retention; there is no master key, so the keys file protects deletion, not data at rest.

### Consequences

- **Positive:** erasure means unrecoverable; the log remains append-only and the audit trail of who did what remains; no dependency added (Go standard library crypto).
- **Negative / trade-offs accepted:**
  - Losing `keys.wal` loses all content, so it must be backed up with the log, and every backup keeps keys alive until it expires.
  - Destroying a key rewrites the whole keys file, linear in the number of items.
  - On SSDs and copy-on-write file systems, the blocks of the old keys file may physically survive until reused; full protection needs disk encryption underneath.
  - The data is not encrypted at rest against someone who copies the whole directory: the keys are next to it. That is a separate decision (a master key or OS-level encryption).
- **Follow-ups / risks to watch:** the open question; backup retention for keys; key rotation is not needed for erasure but would be for a compromised key.

## Pros and cons of the options

### Rewrite the event log

Strongest form: the data is physically absent afterwards, with no keys to manage or lose; the file is the whole truth.

- Good: simple model; nothing secret to back up.
- Bad: breaks append-only; every erasure rewrites the entire history (gigabytes eventually) under the writer; a crash in the middle needs its own recovery; sequence numbers and audit references into the old file must survive the rewrite.

### Content outside the log

Strongest form: the log holds only references, so deleting a row in a mutable store deletes content; large bodies leave the log, which helps replay time too.

- Good: deletion is an ordinary delete; smaller log.
- Bad: a second store whose consistency with the log must be designed; the log no longer suffices to rebuild state, which is the core of ADR-0008; history views need joins.

### Crypto-shredding

- Good: append-only preserved; deletion is one small rewrite; erasure is per item.
- Bad: key management and its backup; erasure depends on the keys file being destroyed on all copies.

## Links

- answers the open question of [ADR-0025](0025-event-log-file-and-in-memory-state.md); realises the content removal promised by [ADR-0021](0021-deletion-only-when-unreferenced.md) and [ADR-0022](0022-permission-model.md) on disk
- relates to ADR-0008 (event sourcing), ADR-0013 (accounts), ADR-0016 (backup)
