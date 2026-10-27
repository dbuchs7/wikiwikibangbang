# ADR-0145: Favourite spaces come first on the start page

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-27
- **Deciders:** Project initiators (the maintainer asked to mark spaces as favourites that come first on the start page, as tiles and in the list, 2026-10-27)

## Context and problem statement

The start page shows the spaces one holds a role in, as tiles or as a list (ADR-0062), in one fixed order. With more than a handful of spaces, the few one works in every day are lost among the rest. How does a person bring their own spaces to the front?

## Decision drivers

- **One's own choice, for oneself only:** a favourite changes nothing for anyone else.
- **The same everywhere one signs in:** a person who marks a space at the desk expects it first on the laptop too.
- **Tiles and list alike:** the layout is a way of looking, not a second set of favourites.
- **No new access:** marking a space gives nothing; one can mark only what one may see.

## Considered options

1. **A favourite per account, kept with the account; favourites first in both layouts**
2. **A favourite per browser,** in local storage, like the layout (ADR-0062) and a board's view (ADR-0141)
3. **A free order,** spaces dragged into place

## Decision outcome

Chosen option: **1**.

- **Where it is kept:** with the account, in the accounts journal like the language and the channels (ADR-0017, ADR-0061): an event `FavouritesSet` carries the whole list of space keys. A user marks at most 200.
- **API:** `PUT /api/spaces/{space}/favourite` marks a space, `DELETE` unmarks it; both answer 204 and change nothing when there is nothing to change. Marking needs a role in the space; a space one holds none in, or that does not exist, answers 404, as everywhere a space one may not see. Unmarking takes any valid key, so a favourite of a space one lost or that was removed can always be taken back. `GET /api/spaces` says `"favourite": true` for the caller's favourites; the order of the list is unchanged, so other callers are not affected.
- **UI:** a star on every tile, in its lower right corner, and in the first column of the list marks and unmarks a space; it says which it does. The favourites come first, then the other spaces, each group in the usual order. The filter of the overview (ADR-0098) offers *Favourite* as a choice.
- **What it does not do:** a favourite grants nothing, is shown to no one else and is not audited; it is a preference, as the language is.
- **A lost role:** a favourite of a space one no longer holds a role in is not listed, as the space is not; it stays in the account until unmarked and returns with the role.

### Consequences

- **Positive:** the spaces one works in are first, on every device, in both layouts.
- **Negative / trade-offs accepted:**
  - The favourites are one more thing kept per account. A backup and a restore keep them with the accounts; the export of a space does not carry them.
  - Favourites come first in the usual order among themselves, not in an order of one's own.
- **Follow-ups / risks to watch:**
  - favourites in the space switcher of the top bar and in the room selections of forms;
  - an order of one's own among favourites, if people ask for it.

## Pros and cons of the options

### Option 2: per browser

Strongest form: no server change at all, the same place the layout is kept, and nothing about a person's habits stored on the server.

- Good: the least code; nothing in the accounts journal.
- Bad: the favourites are gone on another device, in a private window or after cleared site data, and must be marked again there. A favourite is chosen once and meant to stay; the layout, by contrast, is a passing way of looking. The server already keeps per-person preferences of this weight, such as the language.

### Option 3: a free order

Strongest form: each person arranges the spaces exactly as they like.

- Good: the most control.
- Bad: dragging tiles and rows is harder to use by keyboard and on a phone than a star, and an order has to be kept for every space, new ones included. Favourites first answers the need with one click.

## Links

- amends [ADR-0062](0062-spaces-carry-an-icon-and-show-as-tiles-or-a-list.md) (favourites first, as tiles and in the list)
- relates to ADR-0017 and ADR-0061 (preferences kept with the account), ADR-0098 (the filter), ADR-0141 (a view kept in the browser)
