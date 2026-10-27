# ADR-0147: Favourite spaces come first in every choice of a space

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-27
- **Deciders:** Project initiators (the maintainer asked to show favourite spaces first in the choices of a space as well, the follow-up ADR-0145 named, 2026-10-27)

## Context and problem statement

Since ADR-0145, one's favourite spaces come first on the start page. Wherever else a space is chosen — a new page, a new kanban, moving a page, the filters of lists, the album, granting a role, a connector's space — the spaces still come in their usual order, and with many spaces the ones a person works in are again lost among the rest. Should the favourites come first there too, and which space is chosen when none is given?

## Decision drivers

- **One order everywhere:** the spaces one marked come first wherever one chooses a space, as on the start page.
- **Nothing chosen behind one's back:** where the context names a space (the space one is in, the item's own space, a connector's space), that space stays chosen.
- **No change for other callers:** the API's list keeps its order (ADR-0145).

## Considered options

1. **Favourites first in every choice of a space; where no space is given, the first favourite is chosen**
2. **Favourites first, but the first space in the usual order stays the default**
3. **Favourites in a group of their own (`optgroup`) in each choice**

## Decision outcome

Chosen option: **1**.

- **Where:** every choice of a space in the UI orders the spaces with `favouritesFirst`: favourites first, then the others, each group in its usual order. That is the new page and new item forms, *New kanban*, moving a page, the space filter of the task, search and item lists, the label board, the album, granting a role in the administration and a connector's space.
- **Which is chosen:** a choice keeps the space its context names. Where none is named — *New* from the top bar outside a space, *New kanban* from the start page — the first space in the choice is chosen, which is now the first favourite.
- **Without favourites** nothing changes: the order and the default are as before.

### Consequences

- **Positive:** the spaces one works in are at the top of every choice, and *New* starts in one of them.
- **Negative / trade-offs accepted:** whoever marks favourites gets another default space for *New* outside a space than before; the choice shows it, and it is the space one marked.
- **Follow-ups / risks to watch:** the space switcher of the top bar, should it list spaces one day.

## Pros and cons of the options

### Option 2: favourites first, the old default stays

Strongest form: the order helps, the default stays predictable for people who never think about favourites.

- Bad: the default would then be a space further down the choice, not its first entry, which is surprising; and those without favourites see no change under option 1 either.

### Option 3: a group of their own

Strongest form: a heading *Favourites* makes the split visible.

- Good: one sees where the favourites end.
- Bad: the start page shows the favourites by a star, not by a heading; a group in every form adds a label to translate and to keep in step, for a split the order already makes.

## Links

- amends [ADR-0145](0145-favourite-spaces-come-first-on-the-start-page.md) (favourites first in every choice of a space, too)
- relates to ADR-0098 (the filters of lists)
