# ADR-0155: Sealed data keeps its binding to the former name

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-11-01
- **Deciders:** Project initiators (found when an installation updated to the renamed version could not open its data directory, 2026-11-01)

## Context and problem statement

The rename to Orbithek (ADR-0153) replaced the old name in the code wherever it stood, and kept the identifiers it listed as stored data: the file magics, the backup names, the old token prefix and the keys in drawings. It missed four more. Every sealed content carries additional authenticated data (AES-GCM) that binds the ciphertext to its purpose, and those strings began with the product's name:

- `wikiwikibangbang event <seq> <kind> <item>` for the content of events and, with ` passage <id>`, for protected passages (ADR-0026, ADR-0088);
- `wikiwikibangbang attachment <item> <attachment> <chunk> <last>` for the chunks of attachments (ADR-0045);
- `wikiwikibangbang note <owner> <item> <note>` for personal notes (ADR-0107);
- `wikiwikibangbang engine connection <user> <engine> <key>` for sealed refresh tokens of engine connections (ADR-0151).

The renamed version used `orbithek …` instead, so no content sealed before authenticated: an updated server stopped at the first sealed event ("does not authenticate") and restarted again and again. Nothing was damaged: the server refused to start before it wrote anything.

What do these strings say, and how is a change of them prevented?

## Decision drivers

- **Stored data stays readable:** a version reads what an earlier one wrote (ADR-0008, ADR-0025).
- **The binding stays a binding:** the additional data exists so a ciphertext cannot be moved to another event, item or purpose; it is not a label.
- **A rename cannot break it again:** a test fails when such a string changes.

## Considered options

1. **Restore the former strings and fix them by tests.**
2. **Accept both, old and new, when opening,** and seal with the new one.
3. **Migrate the data:** decrypt with the old strings and seal again with the new ones.

## Decision outcome

Chosen option: **1, "restore the former strings and fix them by tests"**, because the strings are part of the stored format like the file magics, and the plain way to keep a format is not to change it.

The four strings keep `wikiwikibangbang`, with a comment that they are stored data. A test in each package (`TestEventAADIsStable`, `TestChunkAADIsStable`, `TestNoteAADIsStable`, `TestConnectionAADIsStable`) compares them to their exact text. A data directory written by the version before the rename was opened by the fixed one: all events, attachments and the album read.

### Consequences

- **Positive:** every data directory and backup written before the rename opens again; future renames meet a failing test.
- **Negative / trade-offs accepted:** the former name stays in four more places in the code, as in the magics. Content sealed by the broken version, between the merge of the rename and this fix, would not open; no installation is known to have written any, because the broken version stopped before it wrote.
- **Follow-ups / risks to watch:** any new sealed format names its additional data once, with a test of its exact text.

## Pros and cons of the options

### Option 1: restore and test

Strongest form: the format did not need to change; restoring it is the smallest correct fix.

- Good: no migration; old backups and old binaries agree with new ones.
- Bad: the former name stays visible in the code.

### Option 2: accept both

Strongest form: content sealed by the broken version opens too.

- Good: covers the window between the rename and the fix.
- Bad: every open tries twice; two bindings weaken what the binding states; it keeps code alive for a window in which, as far as known, nothing was written.

### Option 3: migrate

Strongest form: the new name everywhere, the data included.

- Good: no trace of the old name in the strings.
- Bad: rewriting an append-only log breaks its own invariants (ADR-0008, ADR-0025) and the audit chain's witness (ADR-0123), and backups would no longer match.

## Links

- amends [ADR-0153](0153-the-product-is-renamed-orbithek.md) (four more identifiers stay as stored data)
- relates to ADR-0026, ADR-0045, ADR-0088, ADR-0107, ADR-0151
