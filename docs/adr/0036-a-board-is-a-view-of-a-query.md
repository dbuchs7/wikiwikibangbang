# ADR-0036: A board is a view of a query, not a stored object

- **Status:** Accepted
- **Implementation:** Partial
- **Date:** 2026-10-19
- **Deciders:** Project initiators (the maintainer chose the board as the next feature, 2026-10-19)

## Context and problem statement

Work items move through workflow states (ADR-0001, D3), but the UI shows them only as a list per space. A team planning work needs to see at a glance what is open, in progress and done, and to move an item to its next state with one gesture. The questions are what a board is in the model, what its columns are, how a card moves, and in which order cards stand.

## Decision drivers

- One model for content (D1): no second home for items, no copy that can drift from them.
- State changes only through workflow transitions (D3).
- What a board shows is exactly what its viewer may read (ADR-0022, D10), without a new permission check.
- A first board useful without configuration; configuration only when a need is known.

## Considered options

1. **A board is a view of a query:** the items of one type in one space, grouped by state, computed when shown
2. **A board is a stored object** with its own columns, filters and card order
3. **A board is an item type** whose items hold the board configuration

## Decision outcome

Chosen option: **1**.

- **What a board is:** one space and one type that has a workflow. The UI offers a board for every such type on the space's page (`#/board/{space}/{type}`). Nothing is stored; the board reads the item listing (`GET /api/items?space=…&type=…`), which already returns only readable items.
- **Columns:** the type's workflow states, in the order the type lists them. A state without items is an empty column.
- **Moving a card:** only by a transition of the item's workflow. The card shows a button for every transition allowed from its state. Dragging a card onto a column performs the transition when exactly one leads there; when none or several do, the board says so and changes nothing. The server checks the transition, its required fields and the edit right as for every other change.
- **Order of cards:** creation order, oldest first. There is no manual ranking yet: ranking needs persisted order, which is a decision of its own.
- **Size:** the board loads at most 2,000 items, in pages within the page budget (ADR-0015), and says when it shows fewer than exist.

**Implementation is Partial:** no ranking, no filters beyond space and type, no swimlanes, no work-in-progress limits, no live updates (a board shows the state when it was opened).

### Consequences

- **Positive:** no new data, no new API route, no new permission path; a board can never disagree with the items it shows; every type with a workflow has a board at once.
- **Negative / trade-offs accepted:** a board cannot be saved, shared with its own filter, or ordered by hand; a board over many items is loaded into the browser in full, up to its bound.
- **Follow-ups / risks to watch:** manual ranking (a persisted order per item and board, its own ADR); filters by property and assignee; live updates through the event stream of ADR-0010; a server-side grouped listing if boards outgrow the client-side bound.

## Pros and cons of the options

### A board is a view of a query

- Good: nothing to keep consistent; permissions and erasure come with the items.
- Bad: no per-board configuration; no hand-made order.

### A board is a stored object

Strongest form: teams expect to name boards, give them filters and column mappings (several states in one column), and keep a hand-made order of cards; a stored board is the natural place for all of that.

- Good: configuration and order have a home; several boards over the same items.
- Bad: a new system object with its own permissions, API and lifecycle before any of its configuration is known to be needed; column mappings would have to follow every new workflow version.

### A board is an item type

Strongest form: D1 taken literally; a board would get history, comments, permissions and search for free.

- Good: maximal reuse of the item machinery.
- Bad: a board is configuration, not content; it would appear in content listings and search and need exceptions there, which D6 forbids.

## Links

- uses [ADR-0001](0001-unified-item-data-model.md) (workflows, D3), [ADR-0015](0015-resource-budgets-and-paged-listings.md), [ADR-0022](0022-permission-model.md), [ADR-0031](0031-web-ui-shell.md); live updates would use [ADR-0010](0010-real-time-collaboration-with-block-locks.md)
