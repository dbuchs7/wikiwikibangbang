# ADR-0098: Every list can be filtered

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-23
- **Deciders:** Project initiators (the maintainer asked that every list in the whole application has a filter, 2026-10-23)

## Context and problem statement

Lists grew view by view. Some can be narrowed:
- the task list has a filter per column, labels included (ADR-0049);
- the album filters by label and kind (ADR-0090);
- search is a filter of all items (ADR-0083).

Most cannot. Among them are the notifications, the comment feed, the members of a space, the administration's lists (spaces, types, users, groups, roles, the audit trail, account requests), a page's files, relations and history, the kanbans, and the runs of a connector.

With tens of thousands of items (ADR-0081) and hundreds of accounts, a list without a filter means scrolling and reading. The same task, finding one entry, also works differently from list to list.

How does every list get a filter, the same everywhere?

## Decision drivers

- Same structure everywhere (ADR-0065, ADR-0091): a filter looks and works the same in every list.
- A filter is found where the list is, at once, without a menu.
- Lists that are paged from the server (ADR-0015) are filtered by the server, not only within the page shown.
- No one sees through a filter what they may not read (ADR-0022): a filter narrows what the reader may see already.
- Keyboard and screen readers: a filter is a labelled field, and the number of entries shown is announced.

## Considered options

1. **One filter bar for every list:** a text field, plus choices where the entries have them, above the list. It filters in the browser for lists loaded whole and through the API for paged lists.
2. **Only the browser's own find (Ctrl+F).**
3. **Filters only where many entries are expected.**

## Decision outcome

Chosen option: **1**.

### The rule

Every list of entries has a filter bar directly above it: tables, card lists, tiles and the lists of the administration.

- **Text:** a field "Filter" narrows the list as one types. It matches the words shown in an entry (title, key, name, label, text), not the labels of its controls. Several words must all match, as in search (ADR-0083). The browser ignores case and accents; the server ignores case (`item.HasWords`).
- **Choices:** where entries have a small set of values, those are offered as choices beside the text field, each with "All". Examples are type, state, label, space, role, kind of event, read or unread.
- **Count:** the bar says "{n} of {total} shown" while a filter applies, announced politely to screen readers.
- **Empty result:** "Nothing matches the filter", with a button to reset it, instead of the list's ordinary empty state.
- **Reset:** one action clears every filter of the list.
- **Address:** the filter is kept in the view's address (query), so it survives a reload and the back button and can be shared.
- **Short lists:** a list that holds fewer than five entries shows the bar only once it has five. The rule is about finding, not decoration. A list whose filter applies keeps showing the bar.

### How it filters

- **Lists loaded whole** (e.g. a page's files, the types, the groups) filter in the browser.
- **Paged lists** (e.g. items, notifications, the audit trail, the album) pass the filter to the API, which filters before paging. Where the API lacks a parameter for a filter, it gets one; it filters only what the caller may read.
- **One helper** in the UI (`filter.js`: `filtered` for lists loaded whole, `serverFilter` for paged ones) builds the bar for every list, so its look, wording and behaviour are the same. A list provides the text of each entry and its choices.

### Inventory

How each list follows the rule.

| List | Filter | Where |
|---|---|---|
| Tasks (inbox, all, by type) | per column and labels, count, reset, address (ADR-0049) | browser |
| Album | text, kind, space, label, count, reset (ADR-0090) | server |
| Search results | further words, space, type, state | server (`/api/search`) |
| Notifications | text, kind (mentions, comments, changes, governance), unread | server (`/api/notifications`) |
| Watched items and spaces; one's own findings | text; kind of finding | browser |
| Comment feed | text, space | server (`/api/comments`) |
| Start page: spaces | text, role | browser |
| Space page: all items | text, type, state, label; the CSV follows | server (`/api/items?q=`) |
| Items of a label | text, space, type | server (`/api/items?q=`) |
| Kanbans list; boards by label | text, space | browser |
| Item: files, relations, history, protected passages, open threads | text; kind, type, who | browser |
| Members of a space | text, role | browser |
| Link check, governance report and overview | text, responsible | browser |
| Administration: spaces, trash, types, account requests, users, groups | text; kind, role | browser |
| Audit trail | text, event, actor, with "More" | server (`/api/audit`) |
| Connectors, their runs and problems; backup runs and versions | text; state, kind, outcome | browser |
| Data protection regulations | text, state | browser |
| Account: access tokens; AI access: apps, approvals, tools | text; scope | browser |

Not lists in this sense: a kanban board's columns (the board is the view of a filtered list), the page tree, forms and their choices.

### Consequences

- **Positive:** one way of finding an entry in every list; long lists stay usable.
- **Positive:** a filter in the address makes a narrowed list something to link to.
- **Negative / trade-offs accepted:** every paged list needs a server-side filter. Where one is missing, the API grows a parameter and a test.
- **Negative / trade-offs accepted:** the bar takes a line above each long list.

## Pros and cons of the options

### Option 2: the browser's find

Strongest form: nothing to build.

- Bad: it finds only what is loaded, so paged lists hide most entries. It does not narrow the list, it only highlights, and it knows no choices such as state or role.

### Option 3: filters only for long lists

Strongest form: less to build, and no clutter on short lists.

- Bad: which lists grow long depends on the installation. A list of five users here is a list of five hundred elsewhere.
- Option 1 keeps the useful part of this: a list shows its bar from five entries.

## Links

- amends [ADR-0091](0091-rules-every-view-follows.md) (a rule every view follows)
- relates to ADR-0015 (paging), ADR-0022 (permissions), ADR-0049 (task inbox), ADR-0065 (UI system), ADR-0081 (scale), ADR-0083 (search), ADR-0090 (album)
