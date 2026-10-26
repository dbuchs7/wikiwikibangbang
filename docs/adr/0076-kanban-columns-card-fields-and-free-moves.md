# ADR-0076: A kanban keeps its own columns, card fields and card order; moves stay workflow transitions

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-21
- **Deciders:** Project initiators (the maintainer asked that a click on a card shows all its details at the right, as among the tasks; that each kanban sets which attributes its cards show; that cards move freely back and forth between columns; and that columns of one's own can be added, 2026-10-21)

## Context and problem statement

A kanban (ADR-0060) shows the cards of one type and label in the columns of the type's workflow; a card moves by a transition, so only along the workflow, and the order within a column is the board's. Teams want to arrange a kanban themselves: show the attributes that matter on the cards, add columns such as "Blocked" or "Waiting for customer" that are not states, put a card anywhere in a column, and move it back as freely as forward. Invariant D3 says that an item's state changes only by a transition of its type's workflow. How does a kanban become this free without a second, hidden state?

## Decision drivers

- D3: state changes only through workflow transitions; a column must not set a state behind the workflow's back.
- D1: a card stays one item; what the kanban adds is about its place on this kanban, not the item.
- D10: a kanban shows only cards its viewer may read; an unreadable card leaves no trace.
- A kanban made before this change looks the same after it.

## Considered options

1. **Columns of the kanban, either of a state or of its own; a move to a state column performs the transition, and a missing one is offered for adding to the type.**
2. **Columns that set the state directly**, whatever the workflow says.
3. **Columns of the kanban's own only**, independent of the state.

## Decision outcome

Chosen option: **1**, codec version 19.

- **Columns:** a kanban holds an ordered list of columns. A *state column* stands for one state of the type's workflow (ID `state:<state>`, at most once per state) and may be renamed and moved; an *own column* has a name and no state. States the list leaves out are appended, so every card has a column; a kanban without a list shows the workflow's states, as before. At most 30 columns, names of 1 to 40 characters.
- **Card fields:** a kanban names up to 12 fields its cards show below key and title: fields of the type, `labels` (other than the kanban's) and `state`.
- **Placing a card:** `KanbanCardPlaced` records, for one kanban, a card, its column, the card's state at that moment and the column's new order. Whoever may edit the card may place it. To a state column, the workflow's single transition from the card's state to that state is performed first; with none, the move is refused and names what is missing, and with several it is refused as ambiguous. To an own column, the state stays as it is. A placement counts while the card's state is still the one recorded and its column still exists and fits; otherwise the card falls back to the column of its state.
- **Missing transitions:** an administrator, who defines types anyway, may add the missing one from the kanban: a transition named "to <state>" from the card's state, or the card's state added to an existing one of that name, in a new version of the type. Others see that an administrator can.
- **Details:** a click on a card opens its details beside the board — fields (editable for editors), state with its transitions, labels and the text — and keeps the card in the address, so the panel survives a redraw and a reload.
- **API:** `GET …/kanbans/{id}/board` returns the columns with their cards; `POST …/kanbans/{id}/place` places a card; `PUT …/kanbans/{id}` sets name, columns and card fields; `POST /api/types/{name}/transitions` adds a transition.

### Consequences

- **Positive:** a kanban can be arranged freely while every state change still runs through the workflow, with its history and its notifications.
- **Positive:** the same item on two kanbans may stand in different own columns and in a different order on each.
- **Negative / trade-offs accepted:** moving back along a workflow that has no way back needs an administrator once per pair of states; the type's workflow grows by it, for every use of the type.
- **Negative / trade-offs accepted:** a transition elsewhere (on the item's page, by an agent) takes a card out of its own column back to its state's column.
- **Negative / trade-offs accepted:** placements stay in the log when a column is removed or the card is taken off; they no longer count.

## Pros and cons of the options

### Option 2: columns set the state directly

Strongest form: the simplest model of "move anywhere"; it is what many tools do, and nobody needs to model a workflow for a kanban.

- Bad: it breaks D3; the workflow would no longer describe how items change, and every check, report and agent that relies on transitions would be bypassed.

### Option 3: own columns only

Strongest form: no question of state at all; a kanban is pure arrangement and moves are always free.

- Bad: a card's column and its state would drift apart, and moving a card to "Done" would not mark it done; the kanban would stop being a view of the work.

## Links

- amends [ADR-0060](0060-kanbans-are-topics-whose-cards-carry-their-label.md) (the order of cards is now kept per kanban)
- relates to ADR-0036 (boards), ADR-0044 (labels), ADR-0049 (task inbox)
- amended by [ADR-0087](0087-kanban-pictures-and-boards-by-label.md) (a picture per kanban; a board for every label)
- amended by [ADR-0132](0132-kanban-cards-columns-and-the-status-at-a-glance.md) (card fields as chips and faces; the settings in a dialog; the details on narrow screens)
- amended by [ADR-0134](0134-kanban-filter-limits-colours-and-finished-cards-by-day.md) (a column's limit; colours by a field of the task)
