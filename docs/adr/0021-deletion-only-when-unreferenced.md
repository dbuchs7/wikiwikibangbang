# ADR-0021: Deletion only when unreferenced, then trash, then purge

- **Status:** Accepted
- **Implementation:** Partial
- **Date:** 2026-10-17
- **Deciders:** Project initiators (maintainer decision 2026-10-17: "no deletion while references exist, then trash")
- **Open question:** How long does an item stay in the trash, who may purge before that, and does legal erasure (e.g. of personal data) require removing content from the event log itself (crypto-shredding)?
- **Question checked:** 2026-10

## Context and problem statement

The item-model prototype (ADR-0001, "Prototype findings", point 6) had no delete. In a model where everything is an item and items point at each other, deleting one item touches many others:

- its **children** (containment, D5);
- **relations** to and from it, by hand or derived from bodies (D4);
- **bodies of other items** that embed or mention it — often pages the deleting person may not edit;
- its **keys**, which other systems, links and bookmarks use (D2);
- its **history** in the append-only event log (ADR-0008).

Reasoning from these dependencies: the item's *identity* is what everything else points at; its *content* is what a person wants gone. A first proposal let deletion proceed regardless of references and rendered them as "item in trash" or "deleted item". The maintainer chose a stricter rule: an item that is still referenced is not deleted at all.

The question is: **when may an item be deleted, and what happens to it and to everything that points at it?**

## Decision drivers

- No dangling pointers, and no live content that silently points at something deleted.
- A deletion is a deliberate act: whoever deletes sees what depends on the item.
- A mistaken deletion can be undone for a while.
- Content that must go can really go, including for legal erasure.
- Deleting an item never edits other people's bodies (D4).
- Deletion is the same for every type (D6).

## Considered options

1. **Hard delete** — remove the item, its relations, and cascade to children
2. **Restrict** — refuse deletion while anything points at the item
3. **Archive only** — no deletion, items can only be archived
4. **Trash, then purge** — a reversible trash state, followed by an irreversible purge that keeps a tombstone
5. **Restrict, then trash, then purge** — 2 as the gate, 4 as what deletion does

## Decision outcome

Chosen option: **5, restrict, then trash, then purge.**

**Gate: no deletion while referenced.**
- Trashing an item also trashes its subtree (consistent with moves, ADR-0001 finding 1). It is **refused** while a **live item outside that subtree** references the item or any of its descendants — by an embed or mention in its body, or by a hand-made relation pointing in.
- These do **not** block: references from inside the subtree; the subtree's own outgoing references; references from items that are themselves in the trash.
- The refusal names the referring items, so the person deleting can remove the references or ask their owners to.

**Trash (reversible).**
- Trashing is a lifecycle flag (presence), separate from the workflow state, the same for every type, recorded as one event for the whole subtree.
- Trashed items are **read-only** and excluded from queries, boards and search by default; they are listed in the space's trash; their keys keep resolving.
- **Restore and purge act on the trash root**: on the item the trash started from, and on exactly the items trashed with it. An item trashed earlier on its own stays where it is. An item cannot be restored while its parent is not live.
- A body that references a trashed item — possible only when the referrer was itself in the trash and has been restored — keeps that reference and renders it as "item in trash"; a new reference to a trashed or purged item is refused. Relations with an end that is not live are hidden, not removed.

**Purge (irreversible).**
- After the retention period, or earlier by a permitted user, a trash root is purged: title, properties, body and attachments of its batch are removed from state; hand-made relations touching them are removed; derived relations from their bodies disappear with the bodies.
- A **tombstone** remains: ID, type, space, former keys. IDs are never reused. A restored referrer renders it as "deleted item".

**The event log.** Past events still contain the purged content. For ordinary deletion that is acceptable (the log is internal and access-controlled). For legal erasure it is not; the proposed path is crypto-shredding — content in events encrypted with a per-item key that is destroyed on purge — to be decided with ADR-0008 (see open question).

### Derived invariants

- **D7 – Identities are permanent.** An item's ID is never reused and always resolves: to the item, to the item in the trash, or to its tombstone. Deletion removes content, never identity.
- **D8 – Nothing live points into the trash by deletion.** Deleting an item never leaves a live item referencing it; the only way a live body references a trashed item is by restoring that body's own item from the trash.

### Consequences

- **Positive:** no live page ever shows "deleted" because of someone else's deletion; dependencies are visible at the moment of deleting; mistakes are recoverable; real removal is possible; one mechanism for all types.
- **Negative / trade-offs accepted:**
  - An item referenced from a page the person cannot edit cannot be deleted by that person until the owner removes the reference. Frequently referenced items are the hardest to delete. The maintainer accepts this as the price of never breaking live content.
  - **An item created inline is embedded in its own parent's body**, and the parent is outside the item's subtree. Deleting such an item therefore requires removing the embed from the page first (found by the prototype). A UI can offer "remove from the page and delete" as one action for someone who may edit the page — two commands, or one batch, but never a hidden edit.
  - Tombstones accumulate (small, unbounded); until crypto-shredding exists, purged content survives in the log.
- **Follow-ups / risks to watch:** retention period and purge rights (open question); the permission model must define who may trash, restore and purge; a "where is this referenced" view is needed for the refusal to be actionable; erasure design together with ADR-0008.

## Pros and cons of the options

### 1. Hard delete

Strongest form: simplest to build and to explain; what users of most tools expect from "delete"; nothing accumulates.

- Good: simple; nothing left behind.
- Bad: every embed, mention, relation and bookmark to the item dangles; a cascade to children deletes work the person may not have seen; not recoverable.

### 2. Restrict

Strongest form: referential integrity guaranteed by refusing anything that would break it; nothing is ever deleted by accident.

- Good: no dangling pointers, no accidental loss.
- Bad on its own: once the references are gone, deletion is immediate and final — no way back from a mistake, and no real removal of content distinct from the identity.

### 3. Archive only

Strongest form: Jira's practical answer — issues are rarely deleted; archiving keeps history and audit intact, and nothing breaks.

- Good: complete history; no pointer problems.
- Bad: no way to remove content that must go (personal data, secrets pasted by mistake); clutter grows without bound.

### 4. Trash, then purge (first proposal)

Strongest form: deletion is never blocked; whoever deletes does not depend on owners of other pages; references degrade gracefully to "item in trash" and "deleted item".

- Good: no blocked deletions; recoverable; real removal possible.
- Bad: other people's live pages change their meaning without anyone touching them — a status page suddenly shows "deleted item" where a task was.

### 5. Restrict, then trash, then purge

- Good: live content never silently loses what it points at; recoverable; real removal possible.
- Bad: deletions can be blocked by references outside one's control; inline items need their embed removed first.

## Prototype (2026-10-17)

`internal/item` implements this record: `Trash` refuses with `ErrReferenced` (also an `ErrValidation`) and names the referring items; `Restore`, `Purge`, `Presence`, `Query.IncludeDeleted`. Tested: the gate (outside references block, inside / outgoing / trashed references do not, inline items blocked by their host), subtree trash, exact batch restore, read-only trash, kept vs new references after a referrer is restored, tombstones resolving by ID and key, replay of the whole lifecycle. A test (`TestPurgeLeavesContentInTheLog`) pins the known gap that purged content stays in past events; it must be inverted when crypto-shredding lands. Not implemented: retention periods (purge is an explicit command) and purge permissions.

## Links

- refines [ADR-0001](0001-unified-item-data-model.md) ("Prototype findings", point 6; D2, D4, D5)
- relates to [ADR-0008](0008-event-sourced-persistence.md) (erasure in an append-only log), [ADR-0020](0020-embedding-and-containment-are-independent.md) (children and embeds)
