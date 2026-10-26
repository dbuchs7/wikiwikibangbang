# ADR-0140: A board remembers its view in the browser

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-26
- **Deciders:** Project initiators (the maintainer asked to keep the colour and the swimlanes of the boards of a type and of a label in the browser as well, 2026-10-26)

## Context and problem statement

The boards of a type and of a label keep nothing (ADR-0036, ADR-0087). Since ADR-0139 one chooses a field to colour their cards by and a field to lay them out in lanes by. The choice is kept only in the address. Whoever reaches the board from a link without it, for example from the space's page, sees the plain board again and has to choose anew each time.

How is the choice kept for the person who made it, without making these boards stored objects?

## Decision drivers

- **The boards stay views** (ADR-0036): nothing on the server, nothing for other people.
- **A shared link shows what it names,** as the filter's does (ADR-0098).
- **Opening someone's link should not change one's own choice.**
- **Folded columns and lanes are already kept in the browser** (ADR-0132, ADR-0137); the view should behave alike.

## Considered options

1. **Remember one's own choice in the browser; the address wins when it names one.**
2. **Keep the choice on the server per person,** as a preference of the account.
3. **Keep it in the address only,** as before.

## Decision outcome

Chosen option: **1**.

- **What is kept:** the colour field and the lane field one chooses on a board, per board: per space and type for the board of a type, per label for the board of a label. Nothing else of the view, and nothing of the content.
- **Where:** in the browser's local storage, like folded columns. Another browser or device does not know it.
- **Which wins:**
  - when the address names a colour or lanes, they apply, as a shared link should show;
  - otherwise the remembered choice applies, and the address then names it, so a link copied from there shows the same.
- **What changes it:** only choosing in the board's own fields. A shared link that names another view shows it but does not change what one remembers.
- **Taking it off:** choosing "no colour" or "no swimlanes" is remembered too.
- **Unknown fields:** a remembered field the type no longer has is ignored.

### Consequences

- **Positive:** the board looks as one left it, however one reaches it. Nothing changes on the server.
- **Negative / trade-offs accepted:**
  - The choice is per browser: a second device, a private window or cleared site data starts plain.
  - A view in the address can differ from the remembered one after opening someone's link. The fields above the board always show which view applies.
- **Follow-ups / risks to watch:** none known.

## Pros and cons of the options

### Option 2: on the server per person

Strongest form: the choice follows the person to every device and survives cleared site data.

- Good: one view everywhere.
- Bad: a new preference per board in the account, with an API and a format of its own, for something the address and the browser already carry. The boards would gain stored state on the server through a side door, which ADR-0036 decided against.

### Option 3: address only

Strongest form: one place for the view, nothing hidden, every link exact.

- Good: no state anywhere.
- Bad: every way into the board without the address's view, the space's page, the label list or the item's labels, shows the plain board again. That is the complaint this record answers.

## Links

- amends [ADR-0139](0139-the-boards-of-a-type-and-a-label-show-the-cards-of-a-kanban.md) (colour and lanes of a view board)
- relates to ADR-0036 (a board is a view), ADR-0098 (the filter in the address), ADR-0132 and ADR-0137 (folded columns and lanes in the browser)
