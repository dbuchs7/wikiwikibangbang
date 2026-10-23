# ADR-0041: The page tree: one level at a time, siblings in an order the move event carries

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-19
- **Deciders:** Project initiators (the maintainer chose the page tree as the first wiki feature, 2026-10-19)

## Context and problem statement

Items already form a tree: every item may have a parent in its space (ADR-0001, ADR-0020). The UI did not show it. A space was a flat table of items, so a wiki of a few hundred pages could not be read the way people read a wiki: from a home page down a hierarchy of pages, in an order the authors chose. Two things were missing: a way to show the tree, and an order among the children of one parent. Creation order is not an order people choose, and a wiki's table of contents is usually not alphabetical either.

## Decision drivers

- The order is part of the content's structure: it must survive a restart and a replay of the log (ADR-0008, ADR-0025), and every server must rebuild the same order from the same log.
- Moving and reordering are one gesture for the user (drag a page somewhere); a move must not be recorded half done.
- The tree shows only what the reader may read (ADR-0022), and placing a page must not let anyone learn about pages they cannot read, or move them.
- A large space must not be sent whole for one sidebar (ADR-0015).

## Considered options

1. **The move event carries the new order of the target's children** (a list of item IDs)
2. **A rank per item** (a fractional or lexicographic key between its neighbours), set by the move event
3. **A separate "children ordered" event**, besides the move
4. **No stored order**: children sorted by title

## Decision outcome

Chosen option: **1**.

- **Model:** `ItemMoved` may carry `Order`, the children of the new parent (or of the top of the space) after the move, as item IDs. Applying it stores that list for the parent; children it does not list (created later, restored from the trash, moved in without a place) follow the listed ones, oldest first. A move without `Order` keeps the stored order, so every existing move still means what it meant.
- **Command:** `Store.Place(item, space, parent, before)` moves an item like `Move` and puts it before the sibling `before`, or last when `before` is empty. It is one event, so moving and placing cannot come apart. `Session.Place` needs the right to move the item (its subtree must be editable, ADR-0022) and to create in the target, and `before` must be readable. Siblings the actor cannot read keep their places relative to each other: the order is computed by the store, from every child, and the actor names only a readable neighbour.
- **Reading:** `Session.Tree(space, parent)` returns one level, the readable live children in order, each with whether it has readable children. A restriction covers an item and its subtree, so an unreadable page hides its children too.
- **Format:** `CodecVersion` 5 adds the field `order`. Versions 1 to 4 are still read; a version 4 build refuses a version 5 log rather than lose the order. The order holds IDs only, no content, so it is not sealed (ADR-0026); it is not part of an item's history as the API shows it, since it names siblings the reader may not be allowed to see.
- **API:** `GET /api/spaces/{space}/tree?parent=` lists one level, paged (ADR-0015). `POST /api/items/{ref}/move` takes an optional `before` (ID or key; empty for last); without it, a move keeps the order as before. The MCP adapter (ADR-0012) does not offer moving, as before; it does not offer the tree either, since `find_items` with `parent` already lists children.
- **UI:** a space and every item in it show the space's page tree beside the content: a sidebar on wide screens, a block that folds away on narrow ones. The tree opens down to the page shown and shows the page's children; levels load when opened. Dragging a page onto the upper edge of another places it before that one, onto the rest of it, as its last child. *Move up* and *Move down* on the page do the same without a pointer. The space's home is its first page at the top of the tree, shown read-only on the space's page; a page gets children through *New child page*.

### Consequences

- Good: the order is in the log, applied the same way live and on replay; no second event to keep consistent with the move.
- Good: unreadable siblings stay where they are and are never named to the actor.
- Bad: a placement stores the whole list of the target's children. For a parent with thousands of children, each reorder writes an event of that size. Wikis rarely put thousands of pages under one parent; if they do, option 2 replaces this one.
- Bad: the order list may name children that later left the parent; they are skipped when reading, and the next placement drops them.
- Neutral: the tree is a list of links with disclosure buttons, not an ARIA tree: that role promises arrow-key navigation, which is not built.

## Pros and cons of the options

### 1. The move event carries the order

- Good: one event per gesture; deterministic replay; trivially correct order after any placement.
- Bad: event size grows with the number of siblings.

### 2. A rank per item

- Good: constant event size.
- Bad: ranks between neighbours run out of precision or grow without bound and need rebalancing, which is itself a multi-item change; ties between items without a rank still need a rule.

### 3. A separate "children ordered" event

- Good: the move event stays as it was.
- Bad: a drag becomes two events, recorded as a batch or not at all; reordering without a move writes an empty move or a second kind, and both must stay consistent.

### 4. Sorted by title

- Good: nothing to store.
- Bad: authors cannot put "Getting started" first; every wiki the product replaces lets them.

## Links

- amended by [ADR-0100](0100-a-tidy-top-bar-and-columns-that-fold-away.md) (the page tree folds away)
- ADR-0001 (items and their parent), ADR-0020 (containment), ADR-0022 (permissions), ADR-0025 (log format), ADR-0015 (paged listings), ADR-0031 (UI shell)
