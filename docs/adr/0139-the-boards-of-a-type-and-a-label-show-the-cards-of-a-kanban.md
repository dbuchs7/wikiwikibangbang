# ADR-0139: The boards of a type and of a label show the cards of a kanban

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-26
- **Deciders:** Project initiators (the maintainer asked to bring the other boards up to the reworked kanban, 2026-10-26)

## Context and problem statement

Three packages reworked the kanban (ADR-0132, ADR-0134, ADR-0137): readable cards, details beside the board, a filter, colours, folded columns, finished cards by day, swimlanes and relations on the cards. Two other boards show cards in the columns of a workflow, and neither got any of it:

- **The board of a type** (`#/board/{space}/{type}`, ADR-0036) shows the items of one type in a space.
- **The board of a label** (`#/labelboard/{label}`, ADR-0087) shows every task with a label, one board per type.

Both still showed bare cards: a key, a title and a button per transition. They keep nothing of their own, so a kanban's settings (card fields, limits, colours, lanes) have no home there. How do these boards get the reworked cards without becoming stored objects?

## Decision drivers

- **One board, not three.** What a card shows and how it moves must not drift between the boards.
- **A view keeps nothing** (ADR-0036, ADR-0087). Making these boards stored objects would undo that decision.
- **D3:** a card still changes state only by a transition.
- **D6:** nothing may depend on a type's name.
- **No change to the server or the persisted format** unless a need appears.

## Considered options

1. **One component for all three boards. The view boards keep nothing; colour and lanes are chosen for the view and kept in its address.**
2. **Store the settings of the view boards,** one set per type and space and per label.
3. **Retire the view boards** and send people to make a kanban instead.
4. **Give the view boards only the new cards,** without filter, details or lanes.

## Decision outcome

Chosen option: **1**.

### What the boards share

The board of a type and the board of a label are now drawn by the same component as a kanban. They show:

- **the cards** of ADR-0132: the title first, the fields as chips, people as faces, at the foot the key and what is behind the card (text, checklist, comments, relations, files);
- **the signs** of ADR-0134 and ADR-0137: overdue and near dates, "waits on n";
- **the filter** of ADR-0134: text, person, label, due date, colour and waiting, kept in the address;
- **finished columns** listed by day, the latest 20 until one asks for all;
- **folded columns and lanes,** kept per board in the browser;
- **the details** of a card beside the board, with its status bar, fields, labels, text and relations. The card that is open is part of the address.

### What a view board shows by itself

- **Card fields:** the person, date and choice fields of the type, at most twelve, as on a new kanban (ADR-0132), and the card's labels. A board of a label does not repeat its own label on its cards.
- **Colour and lanes:** chosen for the view, not kept. Two choices above the board name a choice field to colour by and a person or choice field to lay out lanes by.
  - They are kept in the address (`color`, `lanes`), as the filter is, so a reload and a shared link show the same view.
  - A field is offered by its name. On a board of a label with several types, it applies to every type that has a field of that name and kind (D6).
  - Each value takes the tone of its place among the values. Tones of one's own choice are a kanban's setting.
- **No limits:** a column limit is a kanban's setting; a view board has nowhere to keep one.

### Moving and making cards

- **A move** is a transition, as decided in ADR-0036:
  - it is made when exactly one transition leads to the column;
  - when several do, the board says so, and one is chosen in the card's details;
  - when none does, an administrator can add the transition to the type at once, as on a kanban (ADR-0076).
- **The buttons per transition** leave the card. The details carry them in the status bar, and Enter on a card opens the details.
- **Order:** the cards keep the order of the listing. A drop into the card's own column changes nothing.
- **A move into another lane** sets the item's value of the lane's field first, then makes the transition. A move the workflow cannot make is refused before the value changes (ADR-0137).
- **A new card** is made with the "+" of a column, where one may edit the space:
  - on the board of a type, in its space;
  - on the board of a label, in the space the board is narrowed to or the only space it shows, and it carries the label;
  - in the first state, or in a state one transition away, which is then made at once.
- **Taking a card off:** on the board of a label, the details remove the label, as a kanban's take a card off (ADR-0132). The item keeps its other labels.
- **Edit rights** are those of each item's space, so a board of a label across spaces lets one move only the cards one may change.

### Server

Nothing changes on the server: no new route, no new field, no new codec version. The view boards read the item listing as before; its counts and waiting signs come from ADR-0134 and ADR-0137.

### Consequences

- **Positive:**
  - Every type with a workflow and every label shows the reworked cards at once.
  - The three boards cannot drift apart: they are one component.
  - A view with colour and lanes can be shared as a link without making a kanban.
- **Negative / trade-offs accepted:**
  - A move by keyboard takes two steps instead of one: open the details, then choose the transition.
  - Colour and lanes are lost when one reaches the board from a link without them, for example from the space's page. A kanban keeps them for everyone.
  - Making a card in a later state is two requests. When the transition is refused, for example for a required field, the card stays in the first state and the error says why.
  - A board of a label across several spaces offers no "+": it cannot know the space.
- **Follow-ups / risks to watch:**
  - Remembering a view's colour and lanes in the browser, as the folded columns are; [ADR-0140](0140-a-board-remembers-its-view-in-the-browser.md) does so.
  - A hand-made order of cards on the board of a type remains open (ADR-0036).

## Pros and cons of the options

### Option 2: store the settings of the view boards

Strongest form: a team that looks at the board of its type every day wants its colour, lanes and limits there for everyone, without making a kanban first.

- Good: the view board would remember everything a kanban does.
- Bad: it would make the view boards stored objects, with permissions, an API and a format of their own. That is what ADR-0036 decided against, and what a kanban already is.

### Option 3: retire the view boards

Strongest form: three boards are more to explain than one. A kanban does everything the view boards do, and more.

- Good: one kind of board.
- Bad: every type and label would need a kanban made by hand before it shows as a board. ADR-0087 made the board of a label exactly to spare that step.

### Option 4: only the new cards

Strongest form: the smallest change that makes the cards readable.

- Good: little code.
- Bad: the boards would differ in everything else, filter, details and lanes, and drift further with every change to the kanban.

## Links

- amended by [ADR-0140](0140-a-board-remembers-its-view-in-the-browser.md) (colour and lanes are also remembered in the browser)
- amends [ADR-0036](0036-a-board-is-a-view-of-a-query.md) (the cards, details and filter of a board of a type; moves by a transition chosen in the details) and [ADR-0087](0087-kanban-pictures-and-boards-by-label.md) (the board of a label)
- relates to ADR-0076 (adding a missing transition), ADR-0098 (the filter), ADR-0132, ADR-0134 and ADR-0137 (the reworked kanban)
