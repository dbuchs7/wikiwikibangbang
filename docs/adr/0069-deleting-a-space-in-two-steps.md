# ADR-0069: Deleting a space in two steps

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-20
- **Deciders:** Project initiators (the maintainer asked that administrators can delete spaces, first into a trash and then for good, 2026-10-20)

## Context and problem statement

Spaces could be created but not deleted. A space made by mistake or no longer needed stayed in every list. Deleting a space touches the invariants on deletion: an item's identity is permanent (D7), and nothing is deleted while live content refers to it (D8). An item is deleted in two steps — into the trash, then purged (ADR-0021). What does deleting a space mean for its items, and can it be undone?

## Decision drivers

- Deleting a space with all its content is a large act; a mistake must be recoverable for a while.
- Identities stay: the items' keys keep resolving, to their tombstones (D7), so a deleted space's key is never used again.
- No live content outside the space may be left pointing into it (D8).
- What is in the trash is out of sight, as for items; it must not show up in lists, search or links (D10).
- Purged content leaves the history and its keys are destroyed (ADR-0026).

## Considered options

1. Delete with all content in one step, after the key is typed again.
2. Delete only empty spaces.
3. Into the trash first, restorable; then for good.

## Decision outcome

Chosen option: **3**, the maintainer's choice, because it is the item model applied to a space.

- **Trash.** An administrator puts a live space into the trash (`DELETE /api/spaces/{space}`, event `SpaceTrashed`). It is refused while a live item in another live space refers to an item in it, naming those items — as for an item (D8); references from what is in the trash do not block. In the trash, the space and everything in it are out of everyone's sight, administrators' included: the space role of every principal there is none, so the items are not found, listed, searched or linked, and nothing can be created or moved into the space. The items themselves are unchanged.
- **Restore.** `POST /api/spaces/{space}/restore` (event `SpaceRestored`) brings the space back exactly as it was; items trashed on their own before stay in the trash.
- **Delete for good.** `POST /api/spaces/{space}/purge` with `{"confirm": "<key>"}` (event `SpacePurged`) works only on a space in the trash. Every item of the space that is not yet a tombstone becomes one, as a purge does (content erased from the history, keys destroyed, files removed); the space's roles and kanbans go, and its icon. The key stays taken: `CreateSpace` refuses it, so the items' keys keep resolving to their tombstones.
- **Who.** Administrators only, for all three; each is recorded in the audit trail (`space.trash`, `space.restore`, `space.purge`).
- **UI.** Administration → Spaces has *Delete* per space, after a confirmation; below it, *Spaces in the trash* lists them with their number of items, *Restore* and *Delete for good*, which asks for the key to be typed.
- **Format.** Codec version 17 adds the three kinds; they carry the space key, and `SpacePurged` the items it purged.

### Consequences

- **Positive:** a space can be removed and, for a while, brought back; deleting for good is as thorough as purging an item.
- **Negative / trade-offs accepted:** a space in the trash keeps its content and its key until it is deleted for good; administrators cannot look into it before restoring it; a deleted space's key cannot be used again.
- **Follow-ups / risks to watch:** a time after which spaces in the trash are deleted automatically is not built; deleting is refused while references from other spaces exist, which may need to be removed one by one.

## Pros and cons of the options

### Option 1: one step

Strongest form: an administrator who types the key knows what they do; one step is simplest.

- Good: little to build.
- Bad: a mistake cannot be undone, and the content is gone at once.

### Option 2: only empty spaces

Strongest form: nothing is deleted that was not deleted item by item, each with its own trash.

- Good: no new deletion path for content.
- Bad: a space with many pages is impractical to empty; the pages' references among each other block their one-by-one deletion.

### Option 3: trash, then for good

Strongest form: the same two steps people know from items, at the level of a space.

- Good: recoverable, thorough, consistent with ADR-0021.
- Bad: three events, a state per space and an invisibility rule to keep in mind.

## Links

- amended by [ADR-0086](0086-a-space-is-renamed-its-key-stays.md) (the kind SpaceRenamed)
