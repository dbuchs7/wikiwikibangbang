# ADR-0087: Kanban pictures, and a board for every label

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-22
- **Deciders:** Project initiators (the maintainer asked for a picture or logo per kanban, and for kanbans shown automatically from the labels of tasks, without making one first, 2026-10-22)

## Context and problem statement

A kanban (ADR-0060, ADR-0076) is made by hand: a name, a type with a workflow and a label. Its cards are the items of that type in its space that carry the label. Two things were missing.

- **A picture:** a kanban has no picture or logo of its own, while a space has an icon (ADR-0062). Several kanbans look alike on the list.
- **Labels without a kanban:** tasks are often labelled without anyone making a kanban for the label. To see those tasks by state, someone first had to make a kanban for each label.

## Decision drivers

- A kanban's picture behaves like a space's icon: same files, same checks, never run as a page (ADR-0052).
- A board for a label needs no step and keeps nothing: label a task and it is there.
- Moving a card on such a board is a workflow transition, as on every board (D3, ADR-0036).

## Considered options

1. **A picture per kanban as a site asset; a label board computed on the fly from the label filter.**
2. **Make a kanban automatically for every label in use.**
3. **A picture per card instead of per kanban.**

## Decision outcome

Chosen option: **1**.

### Kanban picture

- **Storage:** an asset of the site settings named `kanban-{space}-{id}`, like a space's icon.
- **Rules:** PNG, JPEG, GIF, WebP or SVG, within the design's size budget, served with the sandboxing headers of every icon.
- **Who:** editors of the space set and remove it. Everyone with a role in the space sees it. To others it does not exist.
- **API:** `GET`, `PUT` and `DELETE /api/spaces/{space}/kanbans/{id}/icon`. A kanban's JSON names its picture's content (`icon`).
- **Where it shows:** on the kanban's tile and beside its name on the board. It is set in the kanban's settings.
- **Cleanup:** removing a kanban removes its picture, and purging a space removes its kanbans' pictures.

### Board by label

- **Route:** `#/labelboard/{label}`, optionally narrowed to a space. It shows every readable item with the label and a workflow, one board per type, its columns the type's states.
- **Server:** no new route. The board is read from the item listing's label filter, as the task list's filter is. Nothing is stored.
- **Moving cards:** a card moves by a transition, by button or by dragging. Columns of a kept kanban and the order of its cards do not apply here.
- **Entry points:**
  - the Kanbans page lists the labels as boards;
  - a label's page offers "As a board";
  - a label chip in the task list leads to its board, unless a kanban holds that label.
- **Kept kanbans:** the board names the kanbans that are kept for the same label.

### Consequences

- **Positive:** every label is a board at once, across spaces or in one. Kanbans can be told apart by their picture.
- **Negative / trade-offs accepted:** a label board has no columns of its own and no card order; for those a kanban is made, as before. It reads at most 2 000 items, like the other boards, and says so when there are more.
- **Negative / trade-offs accepted:** the Kanbans page lists the most used labels of all readable items, not only of tasks. A label of pages only leads to an empty board, which says so.

## Pros and cons of the options

### Option 2: a kanban for every label

Strongest form: each label gets everything a kanban has, columns of its own and card order included.

- Bad: kanbans would appear and pile up without anyone deciding. A kanban needs one type, a label spans several. A label used once by mistake would leave a kanban behind.

### Option 3: a picture per card

Strongest form: cards could be told apart at a glance.

- Bad: the maintainer asked for a picture per kanban. A card's item can already hold pictures as attachments, and a cover per card can follow if asked for.

## Links

- amended by [ADR-0092](0092-labels-in-search-and-task-counts-on-label-boards.md) (counting tasks only on the Kanbans page)
- amends [ADR-0060](0060-kanbans-are-topics-whose-cards-carry-their-label.md) (kanbans) and [ADR-0076](0076-kanban-columns-card-fields-and-free-moves.md) (kanban settings)
- relates to ADR-0036 (the board of a type), ADR-0044 (labels), ADR-0052 (corporate design assets), ADR-0062 (space icons)
