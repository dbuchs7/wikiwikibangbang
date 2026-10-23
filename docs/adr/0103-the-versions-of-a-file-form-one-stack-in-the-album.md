# ADR-0103: The versions of a file form one stack in the album

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-23
- **Deciders:** Project initiators (the maintainer asked that a drawing edited several times shows as a stack in the album: the newest version on top, the older ones half hidden behind it, fanned out by a click on the newest, 2026-10-23)

## Context and problem statement

Editing a drawing saves a new file, which names the version it was drawn from (ADR-0053, ADR-0055). The older versions stay: a text may still show one, and they are the drawing's history. The album (ADR-0054, ADR-0090) listed every file as a picture of its own. A drawing edited five times filled five places in the album, each looking nearly the same, and nothing showed which was the current one.

How does the album show a file's versions as one thing, the current version first, without losing the older ones?

## Decision drivers

- The current version is what one looks for; it comes first.
- Older versions stay reachable: to compare, to insert an earlier state, to delete what no text uses (ADR-0090).
- One model (D1): versions are already recorded (`Attachment.Source`, ADR-0055). The album derives the stack from that and stores nothing new.
- Access stays per item (ADR-0022): a stack holds the versions within one item only.

## Considered options

1. **A stack per file, derived from the chain of versions within its item.** The newest version is on top, the older ones are half hidden behind it, and a click fans them out.
2. **Only the newest version in the album.** Older versions are seen on the item's page only.
3. **Deleting the older version when a drawing is saved again.**

## Decision outcome

Chosen option: **1**.

### What forms a stack

- **Members:** the files of one item linked by their sources form a stack. A new version names the version it was made from in the same item (ADR-0055).
- **Deleted versions:** a deleted version still links the chain, so deleting a middle version does not split the stack.
- **Copies:** a copy into another item begins a stack of its own in that item. It belongs to that item's access, not to the original's.
- **A single file:** a file that is no new version and has none is a stack of one, shown as before.
- **Order:** the newest version is the one added last. Of two added in the same second, the later is the newer.

### API

- `GET /api/album` lists stacks. Each entry is the newest version of a file, with its older versions under `versions`, newest first.
- **Filters:** a stack shows when any of its versions matches them (text, label, kind, folder). The top is always the newest version, so an older version's label finds the current drawing.
- **Paging:** `total`, `offset` and `limit` count stacks.

### The album

- **The stack:** a stack of more than one shows the newest version in front. Up to two older versions are offset behind it, half hidden. A badge counts the versions, and the card says "Version n".
- **Fanning out:** a click on the newest version (a button, by keyboard too) fans the older versions out. They appear in a row of their own right below the stack, newest first, each a card with its version number, date, labels and, as before, Insert or Delete. Another click folds them away.
- **Insert from the album:** while inserting from the album, the older versions can be fanned out and inserted too.

### Consequences

- **Positive:** a drawing edited many times takes one place in the album, with its current state in front.
- **Positive:** no codec change. Files saved since ADR-0055 (codec version 12) form stacks at once.
- **Negative / trade-offs accepted:** files saved before sources were recorded have none and show apart.
- **Negative / trade-offs accepted:** a copy that takes over a newer original (ADR-0055) is a new copy, so in its item it starts another stack beside the earlier copy.
- **Negative / trade-offs accepted:** a stack counts as one in the album's count, while the item's page still lists every version as a file.

## Pros and cons of the options

### Option 2: only the newest version

Strongest form: the simplest album, and the item's page already lists every file.

- Bad: an earlier state of a drawing can no longer be inserted from the album.
- Bad: older versions no text uses can no longer be cleared from the album (ADR-0090).

### Option 3: delete older versions on save

Strongest form: no clutter anywhere, and less storage.

- Bad: a text that shows an older version would lose its picture.
- Bad: the history of a drawing would be destroyed without being asked for.
- Bad: a copy's source (ADR-0055) would be gone.

## Links

- amends [ADR-0054](0054-an-album-of-readable-pictures-inserted-by-copy.md) (the album lists stacks of versions)
- relates to ADR-0053 (drawings), ADR-0055 (sources and versions), ADR-0090 (folders, labels, delete), ADR-0098 (filters)
