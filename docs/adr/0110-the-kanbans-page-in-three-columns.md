# ADR-0110: The kanbans page in three columns

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-23
- **Deciders:** Project initiators (the maintainer found the order of the kanbans page's sections unclear and asked for "New kanban" in a right column and the boards by label as a filterable navigation with suggestions in a left one, 2026-10-23)

## Context and problem statement

The kanbans page stacked four parts in one column: an introduction, the kanbans by space, the boards by label as chips (ADR-0087), and the form for a new kanban at the very bottom. The form was easily missed, and the boards by label read as a second list rather than a way to get somewhere. Other views keep navigation on the left (the page tree, ADR-0041) and actions and details on the right (ADR-0079, ADR-0100). How is the kanbans page laid out so it follows that pattern?

## Decision drivers

- One layout across views: navigation left, content in the middle, making and details right.
- Many labels must stay usable: a long chip cloud is hard to scan.
- Narrow screens keep one column, with the content first.

## Considered options

1. Three columns: boards by label left, kanbans middle, new kanban right.
2. One column, reordered: new kanban first, then kanbans, then boards by label.
3. Tabs: kanbans, boards by label, new kanban.

## Decision outcome

Chosen option: **1, three columns**, because it is the layout the other views already use.

- **Left:** the boards by label, as a list with each label's count. Its filter always shows. It suggests the labels as one types (a `datalist`), and Enter opens the board of the label typed in full, or of the one label left.
- **Middle:** the introduction and the kanbans by space, with their filter (ADR-0098).
- **Right:** the form for a new kanban, its fields one below the other.
- A column without content (no labels, or no space one may make a kanban in) is left out, and the others take its room.
- Below 1100 px the columns stack: kanbans, then the new kanban, then the boards by label.

### Consequences

- **Positive:** the page reads like the others; making a kanban is visible at once; a label is found by typing.
- **Negative / trade-offs accepted:** on mid-sized screens the middle column is narrower, so fewer kanban tiles stand side by side.
- **Follow-ups / risks to watch:** none.

## Pros and cons of the options

### Option 2: one column, reordered

Strongest form: no layout change, works on every width, and the form comes first.

- Good: simple.
- Bad: the form pushes the kanbans down for everyone who only wants to open one; the page still differs from the other views.

### Option 3: tabs

Strongest form: each part gets the whole width.

- Good: no crowding.
- Bad: hides two of the three parts; the page already sits under the task tabs, so tabs within tabs.

## Links

- amends [ADR-0087](0087-kanban-pictures-and-boards-by-label.md) (boards by label); relates to [ADR-0098](0098-every-list-can-be-filtered.md) (filters), [ADR-0100](0100-a-tidy-top-bar-and-columns-that-fold-away.md) (columns)
