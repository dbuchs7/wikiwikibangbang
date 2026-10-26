# ADR-0132: Kanban cards, columns and the status, readable at a glance

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-26
- **Deciders:** Project initiators (the maintainer did not yet like the kanbans and, after a comparison of what a kanban shows and how, asked for a first package: the cards, the columns, the settings and the details, in today's look; they found the status pills unacceptable; and they decided that information may also arise on a kanban, 2026-10-26)

## Context and problem statement

A kanban (ADR-0060, ADR-0076) shows its cards in columns. Seen with eleven tasks on a laptop screen, it had these weaknesses:

- **Cards read like forms.** Below the key and the title, every field the kanban names stood in a row of its own, as name and value: "assignee you", "due 2026-10-30", "priority high". A card was about 150 px high, so about five fitted in a column.
- **Nothing stood out.** A task two days past its due date looked like every other. A person was the word "you", although profile pictures exist (ADR-0050).
- **A destructive link on every card.** "take off this kanban" was repeated on each card.
- **Columns of fixed width and unequal height.** On a wide screen a quarter of the width stayed empty. An empty column was a small box, a poor place to drop a card. The column's state stood beside its name ("Backlog · open").
- **One way to add a card.** A field above the board made a card in the first state only.
- **Settings in the way.** The settings were a block across the whole width above the board. Renaming and removing were links beside the title.
- **Details cut the board off.** With a card's details open, the last column was cut off.
- **The status as pills.** The status bar on an item's page and in a card's details showed every state of the workflow as a pill. The current state was filled. A state one transition away was a two-line pill with the transition's name below, and an unreachable state had a dashed outline. One row mixed three meanings: where the item is, where it can go, and where it cannot go.

How should a card, a column and the status show what matters, without a change to what is stored?

## Decision drivers

- **At a glance:** what a card is, who has it, whether it is late, and what is behind it.
- **Nothing new is stored:** the persisted format (ADR-0025) and the API stay as they are. Everything shown comes from what the board already reads, so it reveals nothing the reader may not read (D10).
- **Same layout as the other views:** navigation left, content in the middle, making and details right (ADR-0110). Every view fits a phone (ADR-0116).
- **A workflow is a graph, not a line:** a state is not "step 2 of 4".
- **No type-specific code in the UI's rules (D6):** a rule about dates applies to every date field, not to one named "due".

## Considered options

1. **Rework cards, columns, settings, details and status in the UI only,** from the data the board already loads.
2. **Do the same, and also add counts and colours** (comments, attachments, relations, a colour per card), with changes to the server.
3. **Show the details over the board on every width,** so the board never narrows.
4. **Keep the status as a row of all states,** and only restyle it.

## Decision outcome

Chosen option: **1**, with the details beside the board on wide screens and over it on narrow ones.

### Cards

- **Title first,** up to three lines.
- **Fields as chips:** the fields the kanban names are chips without their names. Name and value are the chip's tooltip.
  - An enumeration's value and the state show as they are.
  - A true yes/no field shows as "✓ name"; a false one shows nothing.
  - Text, numbers and links follow a muted name, since alone they say little.
  - Labels other than the kanban's are label chips.
- **People** (user fields) are faces at the right end of the card's foot. Their name is the tooltip and what a screen reader says.
- **Dates** say how far off they are: "today", "tomorrow", "in n days" (under a week), else the date in the reader's language.
  - On a card whose state is not finished (ADR-0082), a past date reads "n days overdue" in the danger tone, and today or tomorrow in the warning tone.
  - On a finished card, dates are only dates.
- **The foot** holds:
  - the key, still the handle people refer to;
  - a sign when the card has a text;
  - the progress of its checklist ("2/5"), counted from the task items of its text (ADR-0114).
- **Taking a card off** the kanban is offered in its details, not on every card.
- **Card fields of a new kanban:** a kanban made on the kanbans page names the type's person, date and enumeration fields (at most 12). Existing kanbans keep what they name.

### Columns

- **Width and height:** columns share the width (at least 220 px, at most 400 px) and reach down to the longest one. The whole column is a drop zone.
- **The head** holds the name, the number of cards, a "+" for a new card and a button that folds the column. The state a column stands for is the name's tooltip.
- **"+" is offered where a new card can land:**
  - a column of the kanban's own;
  - the column of the workflow's initial state;
  - a state column one transition away from the initial state.

  The card is made, labelled and placed at the top of the column; in a state column, placing it performs the transition (ADR-0076). The field above the board is gone.
- **Folding:** a column folds into a narrow strip with its name upright and its count. It stays a drop target.
  - Folding belongs to the viewer and is kept in the browser, per kanban.
  - Without browser storage it lasts until the view is left.

### Settings

- **One button,** "Board settings" in the head, opens a dialog.
- **The dialog** holds the name (replacing the rename prompt), the picture, the fields the cards show and the columns, with save and cancel. Removing the kanban is at its bottom.
- **"Put on this kanban"** stays above the board while there are items to put on it.

### Details

- **Wide screens:** the details stay beside the board. The columns narrow to make room instead of being cut off.
- **Below 900 px:** the details cover the screen rather than appearing below a long board.
- Escape closes them.
- A card without a text reads "No text yet.". The old message said the type had no text, which was wrong.

### Status, on an item's page and in a card's details

- **The current state** is a badge with a dot, in the accent tone, or in the success tone when the state is finished.
- **Each transition from it** is a button named by the transition, with its target beside it ("finish → done").
- **Unreachable states are not shown;** the type's workflow lists them. A transition that ends where it starts is a button without a target.

### Information on a kanban

The maintainer decided that information may also arise on a kanban. This record stores nothing new. It records that direction for the following packages:

- a filter of the board;
- limits of work in progress;
- a colour by a field;
- finished cards grouped by the day they were finished;
- counts of comments, attachments and relations;
- swimlanes.

Each of them decides in its own record where such information is kept. Answered by [ADR-0133](0133-kanban-filter-limits-colours-and-finished-cards-by-day.md): with its items, as their properties.

### Consequences

- **Positive:**
  - A card is about half as high and says who, when and what is behind it.
  - A late task stands out.
  - A column takes new cards where they belong.
  - The settings no longer push the board down.
  - The status says plainly what the item is in and what can be done.
- **Negative / trade-offs accepted:**
  - Field names on cards are tooltips only. A touch screen has no hover, so there the details show them.
  - The tone of a past date assumes that a date on an unfinished card is a deadline. A date of another meaning, such as a start date, is marked overdue too.
  - Folding is kept per browser, not per account.
  - A checklist counts the task items of a card's text, not items below it in the tree.
  - The board of a type (ADR-0036) and the board of a label (ADR-0087) keep their cards for now. They share only the column surface.
- **Follow-ups / risks to watch:**
  - The packages listed under "Information on a kanban".
  - The new card on the boards of a type and of a label.

## Pros and cons of the options

### Option 2: counts and colours in the same step

Strongest form: one change, and the cards are complete at once. Counting comments and attachments is cheap.

- Good: no second round over the same code.
- Bad: counts need new fields in the item listing, and a count of relations must leave out items the reader may not read (D10).
- Bad: colours depend on the open question of where information on a kanban is kept.
- Bad: it mixes decisions about the UI with decisions about data in one review.

### Option 3: details over the board on every width

Strongest form: the board keeps its full width and never narrows. Many tools open details this way.

- Good: no narrowing of columns.
- Bad: the columns at the right are hidden, and the open card cannot be dragged to a column under the details.
- Bad: the context of the card is lost while one reads it.

### Option 4: keep the row of all states

Strongest form: the row shows where the item stands in the whole workflow.

- Good: every state is visible.
- Bad: one row mixes where the item is, where it can go and where it cannot.
- Bad: two-line pills are uneven.
- Bad: a row suggests an order that a workflow, being a graph, does not have.

## Links

- amends [ADR-0060](0060-kanbans-are-topics-whose-cards-carry-their-label.md) (adding cards and taking them off) and [ADR-0076](0076-kanban-columns-card-fields-and-free-moves.md) (how card fields show, the details, the settings)
- relates to ADR-0036 (boards), ADR-0050 (profile pictures), ADR-0082 (finished states), ADR-0087 (kanban pictures), ADR-0110 (the kanbans page), ADR-0114 (task items in a text), ADR-0116 (phones)
- amended by [ADR-0133](0133-kanban-filter-limits-colours-and-finished-cards-by-day.md) (a filter, column limits, colours by a task's field, finished cards by day; answers the open question)
