# ADR-0020: Embedding shows an item, containment places it — the two stay independent

- **Status:** Proposed
- **Implementation:** Not started
- **Date:** 2026-10-16
- **Deciders:** Project initiators
- **Open question:** Do users understand the difference between "this task lives here" and "this task is shown here" when the page renders both the same way, or does the UI need a visible distinction from the first release?
- **Question checked:** 2026-10

## Context and problem statement

The item-model prototype (ADR-0001, "Prototype findings", point 4) showed that two links can connect a page and a task:

- **Containment:** the task's `parent` is the page. It decides navigation, the space and permission inheritance (D5).
- **Embedding:** the page body contains an `embed` block pointing at the task. It decides where the task is *shown*, and yields a derived `embeds` relation (D4).

A task created inline in meeting notes has both. A task embedded from another space has only the embed. When the inline task is later moved, the embed stays and the two links disagree. ADR-0001 does not say which of the two is authoritative, or whether one implies the other.

The question is: **what does embedding an item mean for where the item belongs?**

Reasoning from the two functions: containment answers *who governs this item* (space, permissions, place in the tree); embedding answers *where is this item useful to see*. An item has one governor but can be useful in many places. That asymmetry — one parent, many embeds — is the core of the decision.

## Decision drivers

- One place governs an item's permissions; showing an item somewhere must never widen who may see it.
- An item can be shown in many places (a status page embeds tasks from several spaces).
- Removing a block from a page must never silently delete or move work.
- No state that can drift: two facts must not need to be kept in sync by hand.
- The system never rewrites a body on its own (D4: a body changes only through commits by people or agents acting for them).

## Considered options

1. **Independent links** — embedding never implies containment; inline creation sets both, atomically; the UI derives "lives here" from `parent == host`
2. **Embedding implies containment** — an embedded item becomes a child of the host
3. **Owned blocks** — a separate block kind "owned item" whose target is created, moved and deleted with the block
4. **Containment implies embedding** — every child is shown in its parent's body automatically

## Decision outcome

Proposed option: **1, independent links.**

- `parent` is the only link that governs: space, permission inheritance, navigation. An embed governs nothing.
- An embed is a pure presentation of the target. A viewer without access to the target sees "restricted item" (ADR-0001, decision 5); **an embed never grants access.**
- "Lives here" is **derived, not stored**: an embed whose target has `parent == host` is shown as a child ("created here"), any other embed as a reference ("shown from PLAT-42"). When the target moves, the rendering changes by itself; nothing drifts because nothing is stored twice.
- **Inline creation** ("turn this line into a task") is one command that creates the item with `parent = host` and commits the body with the embed, as **one atomic batch of events**. The prototype has no multi-event commands yet; this is a requirement on ADR-0008.
- **Removing an embed** removes only the presentation. The item stays where it lives and remains listed under its parent's children. The UI tells the editor so ("the task still exists in PLAT-42").
- **Moving an embedded item** never edits the bodies that embed it; they keep their block and render the item at its new place.

### Consequences

- **Positive:** one item, shown in many places, governed in one; no hidden data loss when editing pages; permissions follow a single rule; no synchronisation state.
- **Negative / trade-offs accepted:** a page can show items it does not contain and contain items it does not show; the UI must make the two cases distinguishable; deleting a page has to say what happens to children it no longer shows (ADR-0021).
- **Follow-ups / risks to watch:** atomic multi-event commands in ADR-0008; the children list of an item must be a first-class view, otherwise items whose embed was removed are hard to find.

## Prototype (2026-10-17)

`internal/item` implements this record: `CreateEmbedded` (one batch, space and parent taken from the host), `Embeds` (the derived child/reference rendering) and the guarantees above, with tests. Observations:

- **All-or-nothing needs validation before the first event.** The prototype validates the whole command, then records both events with a shared `Batch` marker. A refused command writes nothing (tested). For ADR-0008 this means a batch must be one framed unit in the log, as atlas does.
- **"Lives here" as a derived value works.** Moving an inline task elsewhere turns its embed into a reference without any write to the host body (tested).
- Nothing in the prototype contradicts the decision.

## Pros and cons of the options

### 1. Independent links

- Good: matches the asymmetry (one governor, many presentations); nothing to keep in sync; safe for permissions.
- Bad: two concepts users have to learn; the same visual block can mean "lives here" or "shown here".

### 2. Embedding implies containment

Strongest form: one mental model — "what you see in a page is in that page", as in Notion, where inline database rows belong to the page; no second concept to explain.

- Good: very simple for single-space note-taking.
- Bad: an item has one parent, so it could be embedded in only one place; embedding a task from another space would move it there and change its permissions — a status page would steal the work it reports on.

### 3. Owned blocks

Strongest form: makes ownership explicit in the document; deleting the block deletes the item, which matches how people treat a checklist line.

- Good: the user sees ownership; inline checklists behave like text.
- Bad: removing a line from a page would delete or orphan work that may already be on a board, assigned and in progress — the worst kind of silent loss; ownership is stored twice (block kind and `parent`) and can drift.

### 4. Containment implies embedding

Strongest form: every child is visible in its parent without extra work, so nothing is ever "lost below" a page.

- Good: children are always visible.
- Bad: the system would have to write into bodies, contradicting D4; a page with hundreds of child tasks becomes unreadable; the children list solves the same need without touching the body.

## Links

- refines [ADR-0001](0001-unified-item-data-model.md) (decisions 4 and 5, D4, D5; "Prototype findings", point 4)
- relates to [ADR-0008](0008-event-sourced-persistence.md) (atomic multi-event commands), [ADR-0021](0021-deletion-is-trash-then-purge.md) (deletion)
