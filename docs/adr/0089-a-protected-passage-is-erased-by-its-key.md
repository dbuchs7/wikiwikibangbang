# ADR-0089: A protected passage is erased by destroying its key

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-22
- **Deciders:** Project initiators (the maintainer asked that a protected passage can be deleted for good through its own key, 2026-10-22)

## Context and problem statement

A protected passage is sealed in the log with a key of its own (ADR-0088), so that destroying the key erases it from every version and leaves the rest of the text. ADR-0088 listed erasing a single passage as a follow-up: until now, only the purge of the whole item destroyed a passage's key.

Taking the protection off (ADR-0088) leaves the text in clear. Deleting the words from the text leaves them in earlier versions. Neither erases them.

How is one passage erased for good, while the item, its other text and its history stay?

## Decision drivers

- **Gone for good:** after erasure no answer, version, comment quote or restart gives the passage's text back, and the log holds nothing that opens it.
- **The rest stays:** the item, the rest of its text, its comments and its history stay.
- **Who:** those who decide about the passage (ADR-0088) decide about its end.
- **Crash-safe:** a crash between recording the erasure and destroying the key does not leave the key behind for good.

## Considered options

1. **An event that erases the passage, after which the store destroys the passage's key.**
2. **Rewrite the log without the passage.**
3. **Only remove the text from the current body.**

## Decision outcome

Chosen option: **1**.

### Command

- `SecretErased` names the passage, and the inline comments whose anchor quoted it. It comes in one batch with the body without the passage: its text and its mark leave the body, and the other marks keep what lies outside it.
- **Who:** the one who protected the passage and the space's administrators, as for taking the protection off. An administrator who cannot read the passage may erase it.
- **When:** on a live item, and not while its body is in a live session.
- **API:** `POST /api/items/{ref}/secrets/{id}/erase`.
- **Page:** *Erase for good*, beside *Take protection off*, after a confirmation that says it cannot be undone.

### Key

- Once the batch is durable, the store destroys the passage's key (`{item}~{passage}`), as a purge destroys an item's key (ADR-0026).
- The item remembers its erased passages. On start, a key of an erased passage that is still there is destroyed; that covers a crash between the batch and the key's destruction.

### What shows afterwards

- **Body:** the passage is gone from the text.
- **Versions since it was protected:** they hold its text sealed with its own key, which is gone. They show the placeholder, to everyone; before the next start, the history in memory still holds the text, so the placeholder is applied there as well.
- **Versions before it was protected:** they hold the text sealed with the item's key, which stays. They are withheld: the history marks them erased, and they can be neither shown nor restored.
- **Comments:** an inline comment whose quote, or the text before or after it, shared text with the passage quotes the placeholder from then on. The comment's own text stays.
- **Restoring:** restoring a version never brings an erased passage back. A body cannot refer to an erased passage.

### Format

Codec version 22 adds the kind `SecretErased`, with `secret` and, in `members`, the comments it veils. Versions 1 to 21 are still read.

### Consequences

- **Positive:** one passage can be erased while the item and the rest of its history stay.
- **Negative / trade-offs accepted:** versions from before the protection still hold the text in the log, sealed with the item's key. The application no longer shows them, but whoever holds the log and the keys file, a backup included, can still read them. Only the purge of the item, or its redaction, erases those.
- **Negative / trade-offs accepted:** backups made before the erasure hold the key. Erasure reaches backups only as they age out of retention.
- **Negative / trade-offs accepted:** a copy of the item has passages of its own, with its own keys. Erasing the passage of the original does not erase the copy's.
- **Negative / trade-offs accepted:** what someone wrote about the passage in a comment's own text, or anywhere else, stays.
- **Negative / trade-offs accepted:** withholding versions errs towards hiding: every version before the protection is withheld, even those that did not hold the text yet.

## Pros and cons of the options

### Option 2: rewrite the log

Strongest form: the earlier versions would be erased too, and no keys file could open them.

- Bad: the log is append-only, and its records are what the state is rebuilt from (ADR-0025). Rewriting it is a migration, not a command. It would also break every backup chain made from it.

### Option 3: only the current body

Strongest form: no new event or format.

- Bad: every earlier version would keep the text. That is not erasure.

## Links

- amends [ADR-0088](0088-protected-passages-of-a-body.md) (protected passages)
- relates to ADR-0025 (event log), ADR-0026 (keys per item, erasure), ADR-0043 (versions), ADR-0047 (inline comments), ADR-0067 (copying)
