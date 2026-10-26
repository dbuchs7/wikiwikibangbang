# ADR-0134: A kanban filters its cards, limits its columns, colours cards by a task's field and lists finished cards by day

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-26
- **Deciders:** Project initiators (the maintainer asked for the second package of the kanban rework: a filter, limits of work in progress, a colour per card, finished cards by day and counts on the cards; they decided that information arising on a kanban is kept as a field of the task, 2026-10-26)

## Context and problem statement

After the first package (ADR-0132), a kanban still lacked five things:

- **A filter.** ADR-0098 gave every list a filter, but left a kanban's board out: one could not show only one's own cards, the overdue ones or those of a label.
- **A sign of overload.** Nothing marked a column holding more cards than a team wants in progress.
- **A colour.** Cards could not be told apart by a category at a glance.
- **An end to the finished column.** A column of a finished state grew with every card ever finished, up to the 2 000 cards a board loads.
- **What is behind a card.** A card did not say whether it had comments or files.

ADR-0132 left one question open: where information that arises on a kanban is kept. The maintainer has answered it: as a field of the task.

How does a kanban get these five things without a second, hidden home for information about a task?

## Decision drivers

- **Information about a task lives with the task** (the maintainer's decision; D4: anything queryable is a property). A colour must mean the same on every kanban, in search, in lists and for agents.
- **State changes only through workflow transitions (D3).** No setting of a view may veto or bypass a transition.
- **Nothing a reader may not read shows (D10)**, not even as a count.
- **The persisted format is a contract (ADR-0025).** A new field of an event needs a codec version.
- **Nothing is stored that the log already says.** The time an item reached its state is derivable.
- **A board's filter looks and works like every other list's (ADR-0098).**

## Considered options

1. **The colour is a value of an enumeration field of the task; the kanban only names the field and the tones of its values.** A column names a limit as a signal. The time a state was reached and the counts of comments and files are derived.
2. **A colour per card, kept with the kanban,** like the column a card is placed in (ADR-0076).
3. **A colour property the system adds to every type.**
4. **A hard limit:** a move into a full column is refused.

## Decision outcome

Chosen option: **1**, with soft limits. Codec version 30.

### Filter

- **The bar:** above the board, the filter bar of ADR-0098, from five cards on.
  - **Text** matches key, title, labels, field values and the names of the people on a card.
  - **Person** offers everyone named in a person field, oneself first.
  - **Label** offers the card's labels other than the kanban's.
  - **Due** is "overdue", "within a week" or "without a date", read from the type's date fields. A finished card is neither overdue nor due.
  - **Colour** offers the values of the colour field, when the kanban has one.
- **The address** keeps the filter beside the open card, so a reload, the back button and a shared link keep both.
- **Counts:** the bar counts the cards it shows. A column's count still counts all its cards: it speaks of the column's load, not of the view.
- **Older finished cards:** a filter shows every card it finds, including the older finished cards otherwise held back.

### Column limits

- **Setting:** a column may name a limit of cards, from 1 to 999; 0 is none. It is set in the board settings.
- **Signal:** the column's head shows "n / limit". At the limit it shows in the warning tone, above it in the danger tone.
- **Never a rule:** a move into a full column is never refused.
- **What counts:** the cards the reader may read (D10). A reader who cannot read some of them may see a smaller count.

### Colours

- **The field:** a kanban may name an enumeration field of its type (`ColorField`). It may give values one of eight tones (`Colors`): blue, green, yellow, orange, red, purple, teal and grey. A value without a tone of its own takes the tone of its place among the values.
- **The value is the task's:** it is set in the card's details or wherever the item is edited. It shows in search, lists and exports and to agents. The kanban keeps only which field and which tones, as settings of a system object (D1).
- **On the board:** a card is tinted with its tone, mixed into the surface so that the light, dark and corporate themes keep their contrast.
- **The legend** above the filter names the values. A click shows only that value's cards.
- **A type without an enumeration field** has no colouring until an administrator adds one; the settings say so.

### Finished cards by day

- **Grouping:** a column of a finished state (ADR-0082) lists its cards by the day they reached the state, the newest first, under the headings "Today", "Yesterday" or the weekday and date.
- **The most recent 20** show until one asks for all. A hand-made order does not apply in such a column.
- **The day** comes from the time an item reached its state: when it was made in its first state, by its last transition, or by an import that changed its state. It is derived when the log is read; nothing new is stored.

### Counts on cards

The comments and files of an item that are not deleted are counted in its JSON. A card shows the counts at its foot, beside the signs of ADR-0132.

### Format and API

- **Codec version 30:** a kanban column may carry `Limit`, a kanban `ColorField` and `Colors`. They are omitted when empty, so a kanban without them encodes as before. Versions 1 to 29 are still read.
- **Changing a kanban:** `PUT /api/spaces/{space}/kanbans/{id}` takes `columns[].limit`, `colorField` and `colors`.
  - Left out, the colouring is kept.
  - An empty `colorField` takes the colouring off.
- **Items in JSON** carry `stateSince`, `commentCount` and `fileCount`, each left out when unset or zero.
- **The open question of ADR-0132** is answered: information that arises on a kanban is kept with its items, as their properties.

### Consequences

- **Positive:**
  - The board filters like every other list.
  - An overloaded column stands out.
  - A colour means the same everywhere the task appears.
  - The finished column no longer grows without end.
  - A card says whether it has comments and files.
- **Negative / trade-offs accepted:**
  - Colouring needs an enumeration field. Adding one changes the type for every use of it, which an administrator decides.
  - The tones are eight fixed ones, not free colours.
  - A limit is counted per reader, from what each may read.
  - A finished column ignores a hand-made order.
  - Twenty is fixed, not a setting.
  - Every item in a listing carries three more fields when they are set.
- **Follow-ups / risks to watch:**
  - Swimlanes by a field.
  - Relations shown on cards.
  - A filter by further fields.

## Pros and cons of the options

### Option 2: a colour per card, kept with the kanban

Strongest form: the fastest way to mark a card, with no field to define. Any card gets any colour, and the colour can differ per kanban when a team wants that.

- Good: no administrator needed.
- Bad: it contradicts the maintainer's decision. The colour would not show in search, lists, exports or to agents (D4), and the same task would mean different things on two kanbans.

### Option 3: a colour property on every type

Strongest form: a colour exists everywhere at once, with no step by anyone.

- Good: no configuration.
- Bad: the system would add a field the type does not define, the same for every type. A colour without a name says nothing to a reader, while an enumeration's values are named categories.

### Option 4: a hard limit

Strongest form: only a refusal stops people from starting new work while the column is full, which is the point of a limit.

- Good: the rule cannot be ignored on the board.
- Bad: it can be enforced only on the kanban's own move. A transition on the item's page or by an agent passes it, so the rule would be leaky.
- Bad: an item on two kanbans with different limits would face two rules.
- Bad: a setting of a view would veto a workflow transition, a second rule beside the workflow (D3).

## Links

- amends [ADR-0076](0076-kanban-columns-card-fields-and-free-moves.md) (limits of columns, colours), [ADR-0098](0098-every-list-can-be-filtered.md) (a board is filtered too) and [ADR-0132](0132-kanban-cards-columns-and-the-status-at-a-glance.md) (answers its open question)
- relates to ADR-0025 (the event log), ADR-0035 (comments), ADR-0045 (attachments), ADR-0082 (finished states)
