# ADR-0107: Personal notes, seen by their maker alone

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-23
- **Deciders:** Project initiators (the maintainer asked for personal notes on passages or words while reading, visible only to the person who made them, 2026-10-23)

## Context and problem statement

Readers want to note something on a passage for themselves: a question to ask later, a reminder, a thought. The tools for this were all shared. An inline comment (ADR-0047) is seen by everyone who may read the item and notifies people (ADR-0084). A text edit changes the item for all.

How does a person keep notes on passages that no one else, not even administrators through the application, can see, and that go when the item goes?

## Decision drivers

- **Private by construction:** no shared view may ever show a note. That includes the history, versions, comments, search, notifications, export, governance and the AI's tools (ADR-0012). A filter in each of them would be one forgotten filter away from a leak.
- **Erasure (ADR-0026):** purging an item destroys everything about it, so its notes too.
- **Access (ADR-0022):** a person notes only what they may read, and loses sight of their notes on an item they may no longer read.
- **Familiar anchoring:** a note names its passage as an inline comment does (ADR-0047), so it finds the passage again after the text changes.

## Considered options

1. **A journal of its own for notes, apart from the items' log, sealed with each item's key.**
2. **Notes as events in the item's log, filtered out of every shared view.**
3. **Notes kept in the browser only.**

## Decision outcome

Chosen option: **1**.

### Storage

- **The journal:** notes live in `notes.wal`, apart from the item log. No view of an item reads it, so none can show a note.
- **Each record:**
  - names its owner, item and note;
  - holds the passage and the text sealed with the item's key (AES-GCM). The additional data binds the record to owner, item and note.
- **Purge:** purging an item destroys its key, so its notes can no longer be read. They leave memory at once and are skipped on the next start.
- **Backups:** the journal is part of every backup (an optional file, so older backups still restore).
- **Limits:**
  - a note's text: at most 4000 characters;
  - the passage it names: at most 1000 characters;
  - notes per person and item: at most 500.

### Access

- A note belongs to its owner alone. No one else lists, reads, changes or deletes it, administrators included. A request for someone else's note is answered as for one that does not exist.
- A person makes notes only on items they may read and that are live. When their access is taken away, their notes on that item are hidden from them too.
- **API:**
  - `GET /api/items/{ref}/notes`, `POST` to it, and `PUT` and `DELETE` on `…/notes/{id}`;
  - `GET /api/notes` lists one's notes on every item one may still read.

### Reading

- **Making a note:** while reading, including in reading mode (ADR-0101), one selects a passage and chooses **Note** in the bar above the text. A dialog takes the text and says that only the reader sees it.
- **Seeing a note:**
  - The passage is marked with a dotted blue line, apart from the yellow of comments.
  - A click on it opens the note, to read, change or delete it.
- **"My notes":** the side column lists one's notes on the item, with the passage and the text. A note whose passage the text no longer holds is listed with a hint.

### Consequences

- **Positive:** nothing that shows an item to others can show a note, by construction rather than by a filter.
- **Positive:** notes are erased with their item, as everything else about it.
- **Negative / trade-offs accepted:**
  - The server's operators hold the item keys, so someone with access to the data directory and its keys could read notes. They are private in the application, not secret from the operator.
  - A note's journal records stay in the file after a purge, unreadable, until the file is rewritten.
  - Notes are not part of an item's export (ADR-0071) or copy (ADR-0067).

## Pros and cons of the options

### Option 2: notes in the item log

Strongest form: one log, one backup, replay as for everything else.

- Bad: every reader of the log would have to leave notes out: history, versions, governance's "last changed", sensitive-data scanning (ADR-0040), notifications (ADR-0046), export and the AI's tools. One missed place shows a person's private note to others.

### Option 3: in the browser only

Strongest form: the server never holds a note at all.

- Bad: the notes are lost with the browser's data and missing on every other device. They are not erased with the item, and not backed up.

## Links

- relates to ADR-0047 (inline comments and their anchors), ADR-0026 (erasure), ADR-0022 (permissions), ADR-0016 and ADR-0073 (backups), ADR-0101 (reading mode)
