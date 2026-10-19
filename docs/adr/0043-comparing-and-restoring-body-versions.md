# ADR-0043: Earlier bodies are read from the log, compared in the browser and restored as a new commit

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-19
- **Deciders:** Project initiators (the maintainer asked for the wiki to come as close as possible to the wiki it replaces, 2026-10-19)

## Context and problem statement

Every body an item ever had is in the event log, as the body of a `BodyCommitted` event (ADR-0008, ADR-0025). The history listed these events but showed nothing of them: one could see *that* a page changed, not *what* changed, and could not go back to an earlier text. Wikis are trusted because a mistake is visible and undoable.

## Decision drivers

- No second copy of what the log already holds.
- An earlier body is content: readable exactly when the item is (ADR-0022), gone when the item is purged or redacted (ADR-0026).
- Going back must not lose anything either: the text replaced by a restore must stay recoverable.
- A restore must not overwrite people typing in a live session (ADR-0010).

## Considered options

1. **Read earlier bodies from the log; compare in the browser; restore by committing the earlier body again**
2. **Compute a diff on the server** and send it
3. **Revert by undoing the events** after the chosen version

## Decision outcome

Chosen option: **1**.

- **Reading:** `Session.BodyAt(item, seq)` returns the body of the item's `BodyCommitted` event `seq`, for anyone who may read the item; an erased event answers not found. `GET /api/items/{ref}/versions/{seq}` serves it with its time and author. The history already names the sequence numbers.
- **Restoring:** `Session.RestoreBody(item, seq)` commits that body again, with the rights and checks of any commit: the item must be editable and live, and references to items that are no longer live are refused unless the current body still has them. `POST /api/items/{ref}/versions/{seq}/restore`. The current body stays in the history, so a restore is undone by restoring the version before it; the UI therefore asks no confirmation. Like a commit from outside a session, it is refused while someone holds a block of the body in a live session.
- **Live sessions:** a session without locks or unsaved drafts takes the stored body when someone joins it, and sends it to everyone in it. Before, an idle session kept the body it was opened with until its next commit noticed the change; a body restored, or committed through the API, could thus stay invisible to those who had the page open.
- **Comparing:** the browser fetches the two bodies and compares them block by block, by a longest common subsequence over one line of text per block (with a marker for its kind: heading level, task state, panel tone, table rows). A removed block similar to an added one (half of their words in common) shows as one block changed word by word; other blocks show as removed or added. Large bodies (over a million block pairs) fall back to showing every block as removed and added.
- **UI:** each change of the body in the history has *Changes*, which shows the comparison with the body before it, and on a live item *Restore this version*.

### Consequences

- Good: nothing new is stored; erasure covers the versions by construction.
- Good: a restore is itself a version, with an author and a time.
- Bad: the comparison is by text: a change of formatting alone (bold, a link target) shows as no change; an embed shows as its item's ID.
- Bad: the comparison runs in the browser, so very large bodies are compared coarsely.
- Neutral: titles and properties are not restored; their history lists their old values.

## Pros and cons of the options

### 2. Diff on the server

- Good: one request; the browser does no work.
- Bad: a second code path to keep in step with how bodies are shown; the comparison is a presentation matter.

### 3. Undo the events after the version

- Good: also reverts titles and properties.
- Bad: undoing events rewrites what happened after, which an event log does not do; derived relations and other people's later changes would be lost.

## Links

- ADR-0008 and ADR-0025 (the log), ADR-0010 (live editing), ADR-0022 (permissions), ADR-0026 (erasure), ADR-0042 (block kinds)
