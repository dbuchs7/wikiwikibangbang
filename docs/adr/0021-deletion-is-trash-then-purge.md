# ADR-0021: Deletion is trash, then purge — an identity is never removed

- **Status:** Proposed
- **Implementation:** Not started
- **Date:** 2026-10-16
- **Deciders:** Project initiators
- **Open question:** How long does an item stay in the trash, who may purge before that, and does legal erasure (e.g. of personal data) require removing content from the event log itself (crypto-shredding)?
- **Question checked:** 2026-10

## Context and problem statement

The item-model prototype (ADR-0001, "Prototype findings", point 6) has no delete. In a model where everything is an item and items point at each other, deleting one item touches many others:

- its **children** (containment, D5);
- **relations** to and from it, by hand or derived from bodies (D4);
- **bodies of other items** that embed or mention it — often pages the deleting person may not edit;
- its **keys**, which other systems, links and bookmarks use (D2);
- its **history** in the append-only event log (ADR-0008).

Reasoning from these dependencies: the item's *identity* is what everything else points at; its *content* is what a person wants gone. Removing the identity breaks every pointer; removing the content does not have to. The decision follows from keeping the two apart.

The question is: **what happens when an item is deleted, to it and to everything that points at it?**

## Decision drivers

- No dangling pointers: every ID and every former key keeps resolving to something meaningful.
- A mistaken deletion can be undone for a while.
- Content that must go can really go, including for legal erasure.
- Deleting an item never edits other people's bodies (D4).
- Deletion is the same for every type (D6).

## Considered options

1. **Hard delete** — remove the item, its relations, and cascade to children
2. **Restrict** — refuse deletion while anything points at the item
3. **Archive only** — no deletion, items can only be archived
4. **Trash, then purge** — a reversible trash state, followed by an irreversible purge that removes content but keeps a tombstone

## Decision outcome

Proposed option: **4, trash then purge.**

**Trash (reversible).**
- Trashing is a lifecycle flag on the item, separate from its workflow state (a task can be trashed in state "in progress"); it is not a workflow transition and is the same for every type.
- The **subtree is trashed with it**, as one atomic batch, consistent with moves (ADR-0001 finding 1). Restoring restores the same subtree. A child moved out before trashing is unaffected.
- Trashed items are excluded from queries, boards and search by default and listed in the space's trash.
- Relations to and from a trashed item are kept but hidden; embeds and mentions in other bodies render as "item in trash".
- Keys keep resolving, to a page saying the item is in the trash and, for permitted users, offering restore.

**Purge (irreversible).**
- After the retention period, or earlier by a permitted user, a trashed item is purged: title, properties, body and attachments are removed from state; hand-made relations are removed; derived relations *from* its body disappear with the body.
- A **tombstone** remains: ID, type, space, former keys, purge time and actor. Embeds, mentions and links render as "deleted item"; nothing dangles. IDs are never reused.
- Other items' bodies are **not edited**; their embeds simply render the tombstone.

**The event log.** Past events still contain the purged content. For ordinary deletion that is acceptable (the log is internal and access-controlled). For legal erasure it is not; the proposed path is crypto-shredding — content in events encrypted with a per-item key that is destroyed on purge — to be decided with ADR-0008 (see open question).

### Derived invariant

**D7 – Identities are permanent.** An item's ID is never reused and always resolves: to the item, to the item in the trash, or to its tombstone. Deletion removes content, never identity.

### Consequences

- **Positive:** no broken links anywhere; mistakes are recoverable; pages owned by others are never altered; one mechanism for all types; a clear path to real erasure.
- **Negative / trade-offs accepted:** tombstones accumulate forever (small, but unbounded); two-step deletion is more to build and explain; until crypto-shredding exists, purged content survives in the log.
- **Follow-ups / risks to watch:** retention period and purge rights (open question); the permission model must define who may trash and restore; erasure design together with ADR-0008; the prototype can implement trash, restore and tombstones to test this record before it is accepted.

## Pros and cons of the options

### 1. Hard delete

Strongest form: simplest to build and to explain; what users of most tools expect from "delete"; nothing accumulates.

- Good: simple; nothing left behind.
- Bad: every embed, mention, relation and bookmark to the item dangles; a cascade to children deletes work the person may not have seen; not recoverable.

### 2. Restrict

Strongest form: the database answer — referential integrity guaranteed by refusing anything that would break it; nothing is ever deleted by accident.

- Good: no dangling pointers, no accidental loss.
- Bad: one mention in a page the person cannot edit blocks the deletion forever; in a wiki, popular items become undeletable precisely because they are popular.

### 3. Archive only

Strongest form: Jira's practical answer — issues are rarely deleted; archiving keeps history and audit intact, and nothing breaks.

- Good: complete history; no pointer problems.
- Bad: no way to remove content that must go (personal data, secrets pasted by mistake); clutter grows without bound.

### 4. Trash, then purge

- Good: recoverable, no dangling pointers, real removal possible, no edits to others' bodies.
- Bad: two states to build; tombstones remain; erasure in the log needs extra design.

## Links

- refines [ADR-0001](0001-unified-item-data-model.md) ("Prototype findings", point 6; D2, D4, D5)
- relates to [ADR-0008](0008-event-sourced-persistence.md) (erasure in an append-only log), [ADR-0020](0020-embedding-and-containment-are-independent.md) (children and embeds)
