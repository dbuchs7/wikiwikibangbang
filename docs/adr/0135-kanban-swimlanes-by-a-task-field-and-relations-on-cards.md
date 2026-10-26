# ADR-0135: Kanban swimlanes by a field of the task, and relations on the cards

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-26
- **Deciders:** Project initiators (the maintainer asked for the third package of the kanban rework, swimlanes and relations on the cards; information arising on a kanban is kept as a field of the task, as decided for ADR-0134, 2026-10-26)

## Context and problem statement

After the first two packages (ADR-0132, ADR-0134) a kanban still lacked two things:

- **Rows.** A board showed its cards in columns only. A team that works by person, priority or category could not see the cards in rows of that kind, and could not move a card from one person or category to another on the board.
- **Links between cards.** Items carry typed relations (ADR-0001), but a card showed none of them. Above all, it did not say that it waits on another piece of work.

How does a kanban show rows and links, keeping what arises on it with the task (ADR-0134)?

## Decision drivers

- **Information about a task lives with the task.** A card's row must be a value of the task, not a place on the board.
- **D3:** a change of state is still a transition; a change of row is a validated change of a field.
- **D10:** a link to an item the reader may not read leaves no trace, not even in a count.
- **The persisted format is a contract (ADR-0025).**
- **A listing stays cheap:** it must not walk every relation once per item (ADR-0081).

## Considered options

1. **Lanes by a person or enumeration field of the task; the kanban only names the field. A move to another lane sets the item's value. Cards count their relations and say what they wait on.**
2. **Lanes of the kanban's own, with each card placed in one,** like the columns of its own (ADR-0076).
3. **Lanes by label.**
4. **Relations shown only in the details,** with no sign on the card.

## Decision outcome

Chosen option: **1**. Codec version 31.

### Swimlanes

- **The field:** a kanban may name a person or enumeration field of its type (`LaneField`).
- **The lanes:** one per value, and one for cards without a value, "Without {field}".
  - For an enumeration field, the lanes are its values in their order.
  - For a person field, they are the people the cards name, oneself first for editors, so one can take a card.
- **The grid:** the column heads stand once above the lanes. Each lane has a cell per column, and every cell takes cards.
- **Moving a card:**
  - into another lane, the item's value of the field is set first, a validated change (D3), then the card is placed;
  - into another column as well, the transition follows as on any kanban (ADR-0076);
  - a move the workflow cannot make is refused before the value changes.
- **A new card** is made in a cell with that lane's value.
- **Folding:** lanes fold like columns, kept in the browser.
- **Unchanged:** column counts and limits count the whole column, and the filter and colours work as before.

### Relations on the cards

- **The count:** a card counts its relations made by hand, both ways, to live items the reader may read (D10). Relations derived from a body are left out: they say that a text names an item, not that the work is linked.
- **Waiting:** a card waits on the unfinished items (ADR-0082) it depends on (`depends-on`) and on those that block it (`blocks`).
  - These are the relation types the UI suggests; a relation's type stays free.
  - A waiting card shows "waits on n" in the danger tone first among its chips, and the filter offers "waits on others".
- **The details** of a card list its relations and add new ones, as on the item's page.

### Format and API

- **Codec version 31:** a kanban may carry `LaneField`, omitted when empty. Versions 1 to 30 are still read.
- **Changing a kanban:** `PUT /api/spaces/{space}/kanbans/{id}` takes `laneField`. Left out, the lanes are kept; empty, they go.
- **Item listings** carry `relationCount` and `waitingOn` per item, each left out when zero. They are counted in one pass over the relations for the whole page, however many items it holds.

### Consequences

- **Positive:**
  - A team sees its work by person or category and moves it there.
  - The row is the task's own value, visible everywhere.
  - A card says that it waits on others.
- **Negative / trade-offs accepted:**
  - Lanes need a person or enumeration field; labels, which a card may carry several of, cannot form lanes.
  - A move across lanes is two changes, the value and the placement, not one. When the placement fails after the value changed, the value stays changed.
  - Waiting rests on the relation names `depends-on` and `blocks`. A team that names its relations otherwise sees no waiting.
  - Every item in a listing carries two more fields when they are set.
- **Follow-ups / risks to watch:**
  - Limits per lane.
  - The board of a type and of a label (ADR-0036, ADR-0087) still have neither the new cards nor lanes.

## Pros and cons of the options

### Option 2: lanes of the kanban's own

Strongest form: lanes for anything, "Urgent", "Later", "Team A", without a field to define. The same task may stand in different lanes on two kanbans.

- Good: no administrator needed.
- Bad: the row would be a place on the board, not information about the task. Search, lists, exports and agents would not see it. This is what the maintainer decided against for ADR-0134.

### Option 3: lanes by label

Strongest form: labels exist on every task and need no field.

- Good: no type change.
- Bad: a card may carry several labels and would stand in several lanes at once. A move between lanes would have to say which label to take away, a question the board cannot ask on a drag.

### Option 4: relations in the details only

Strongest form: the card stays simpler, and the details already list relations on the item's page.

- Good: nothing new on the card.
- Bad: a card that waits on other work looks like one that can start; one must open every card to find out.

## Links

- amends [ADR-0076](0076-kanban-columns-card-fields-and-free-moves.md) (lanes beside columns) and [ADR-0134](0134-kanban-filter-limits-colours-and-finished-cards-by-day.md) (signs and filter of the cards)
- relates to ADR-0001 (typed relations), ADR-0025 (the event log), ADR-0081 (scale), ADR-0082 (finished states)
