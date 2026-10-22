# ADR-0060: Kanbans are named topics of a space whose cards carry the kanban's label

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-20
- **Deciders:** Project initiators (the maintainer asked for kanbans on different topics with tasks assigned to them, and chose that a task may stand on several kanbans, 2026-10-20)

## Context and problem statement

A board (ADR-0036) shows every item of one type in one space, in the columns of its workflow. Teams work on several topics at once in one space (a relaunch, operations, an audit) and want a board per topic, with tasks assigned to it; a task may concern more than one topic. What is such a kanban, and how does a task belong to it?

## Decision drivers

- One model for content (D1): a task stays one item in one place; no copy per board.
- A task may stand on several kanbans (the maintainer's choice).
- An empty kanban exists before its first card.
- What a kanban shows is what its viewer may read (D10), without a new permission check.

## Considered options

1. **A kanban is a definition of the space (name, type, label); its cards are the items of that type carrying the label.**
2. **A kanban is an item; its cards are its child items.**
3. **A kanban is only a label**, with no definition.

## Decision outcome

Chosen option: **1**, codec version 16.

- **Definition:** `KanbanSet` and `KanbanRemoved` events of a space hold a kanban's ID, name, type (one with a workflow) and label (its name as a label unless given). Like a space or a type, it is a system object, not content. Editors of the space make, rename and remove kanbans; everyone with a role in the space lists them. Type and label do not change after creation, so the cards stay the cards.
- **Membership:** a card is an item of the kanban's type in its space that carries the label (ADR-0044). Putting a task on a kanban adds the label; taking it off removes it; a task on several kanbans carries several labels. Removing a kanban leaves its cards' labels as they are.
- **The board** is the board of ADR-0036, narrowed to the label: columns are the workflow's states, cards move by transitions. Editors add a new card by title, put an existing item on the kanban, or take a card off.
- **In the UI:** a Kanbans tab under Tasks lists the kanbans by space with their number of cards and makes new ones.

### Consequences

- **Positive:** no second home for tasks; search, governance, history and agents see membership as the label it is.
- **Positive:** a task on several topics needs no copy.
- **Negative / trade-offs accepted:** the label is visible and editable on the task like any label; removing it by hand takes the task off the kanban.
- **Negative / trade-offs accepted:** a kanban does not narrow access; its cards are as readable as they are anywhere (restrict the tasks to narrow it).
- **Negative / trade-offs accepted:** the order of cards within a column is the board's (ADR-0036), not kept per kanban.

## Pros and cons of the options

### Option 2: a kanban is an item with child tasks

Strongest form: a kanban is a page in the tree, empty kanbans exist naturally, and restricting the kanban restricts its tasks.

- Bad: a task has one parent (D5), so it could stand on one kanban only, which the maintainer did not want; moving it would change where it lives in the tree.

### Option 3: only a label

Strongest form: nothing new to store.

- Bad: a kanban without cards would not exist, and nothing would name a kanban or tie it to a type.

## Links

- amended by [ADR-0076](0076-kanban-columns-card-fields-and-free-moves.md) (columns of the kanban's own, card fields, order per kanban)
- relates to ADR-0036 (boards), ADR-0044 (labels), ADR-0049 (task inbox)
- amended by [ADR-0087](0087-kanban-pictures-and-boards-by-label.md) (a picture per kanban; a board for every label)
