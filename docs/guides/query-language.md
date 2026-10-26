# Selecting items with a query

A query selects items by their fields and puts them in order ([ADR-0136](../adr/0136-a-query-language-selects-items-by-their-fields.md)):

```text
type = task AND assignee = currentUser() AND finished = false ORDER BY due
```

The answer holds only what you may read: an item you may not read is not counted, not ordered and not named in an error, and a protected passage you may not read is not searched.

## Where to ask

- **In the UI:** on the search page, **Query the fields** opens the view *Query* with the search's words as `text ~ "…"`. **Run** or Ctrl+Enter runs the query. The query stays in the address, so a bookmark or a link to it asks it again. **CSV** exports the answer in its order.
- **Through the API:** `GET /api/items?query=…`, paged like every listing (`limit`, `after`); the listing's other parameters (`space`, `type`, `state`, `label`, …) narrow it further. `GET /api/items.csv?query=…` answers the same items as CSV.
- **For agents:** the MCP tool `query_items`, with the rights of the person the agent acts for.

## Conditions

A condition compares a field with a value. Conditions are joined with `AND`, `OR` and `NOT`; `AND` binds closer than `OR`, parentheses group.

| Comparison | Holds when | Example |
|---|---|---|
| `=`, `!=` | the value is, or is not, the one given | `state = open` |
| `<`, `<=`, `>`, `>=` | the value is smaller or larger: numbers, dates, text by its letters, a choice by the order of its values | `priority >= high` |
| `~`, `!~` | every word of the value occurs, or not, in the field's text | `title ~ "release notes"` |
| `IN (…)`, `NOT IN (…)` | the value is one of a list, or none of it | `state IN (open, review)` |
| `IS EMPTY`, `IS NOT EMPTY` | the field has no value, or has one | `assignee IS EMPTY` |

- Text, names, choices and labels compare without regard to case; `ß` and `ss` are the same letter.
- `!=`, `NOT IN` and `!~` also hold for items without the field: `assignee != anna` finds the unassigned items too. Add `AND assignee IS NOT EMPTY` to leave them out.
- Words such as `PLAT-12`, `2026-10-01` and `-7d` need no quotes. A value with spaces, commas or brackets is quoted, `"…"` or `'…'`; a quote inside is written `\"`.
- Keywords (`AND`, `OR`, `NOT`, `IN`, `IS`, `EMPTY`, `ORDER BY`, `ASC`, `DESC`) may be written in any case.

## Fields

Every item has these fields:

| Field | Holds |
|---|---|
| `key` | the key, e.g. `PLAT-12`; a former key finds the item too |
| `title` | the title |
| `type`, `space`, `state` | the type, the room's key, the workflow state (empty without a workflow) |
| `label` | the labels; `label = web` holds when one of them is `web` |
| `text` | title, key, text, text fields, labels and comments together; only `~` |
| `body`, `comment` | the text alone, the comments alone; `~` and `IS EMPTY` |
| `parent` | the item it lies directly below, by key; `parent IS EMPTY` for the top of a room |
| `ancestor` | any item it lies below, e.g. every page of a handbook: `ancestor = DOC-1` |
| `created`, `updated` | when it was made, and when it last changed itself: title, fields, state, text, labels, place, files or comments |
| `creator` | who made it |
| `finished` | `true` when its state is one the workflow does not leave, `false` before; empty without a workflow |
| `id` | the item's permanent ID |

Every other name is a field of a type, as the type defines it, e.g. `priority`, `due`, `assignee`. The view *Query* lists them under **How to write a query**; a click puts one at the cursor. A name with spaces is quoted, `"due date" < now()`. A field of a type that is named like a field of every item, e.g. a field `created` brought by an import, is quoted as well: `"created" = 2024-03-01`.

## Time

- A date, `2026-10-01`, names a whole day: `due = 2026-10-01` holds for that day. A date field always counts by its day.
- `-7d`, `2w`, `+12h` and `-1y` count from now: `updated >= -7d` is "changed in the last seven days".
- `now()`, `startOfDay()`, `endOfDay()`, `startOfWeek()`, `endOfWeek()`, `startOfMonth()`, `endOfMonth()`, `startOfYear()` and `endOfYear()` take an offset: a whole number in their own unit, `startOfMonth(-1)` is the first day of last month, or a span, `startOfDay(+8h)` is eight in the morning.
- Weeks start on Monday. Days are those of UTC, so around midnight `startOfDay()` differs from the local day.

## People

`currentUser()` is you. A user field and `creator` take a username, in any case, or a user ID: `assignee = anna`. A name nobody has finds nothing.

## Order

`ORDER BY` one or more fields, separated by commas, each `ASC` (the default) or `DESC`: `ORDER BY due, priority DESC`. Items without the value come last either way. A state orders by its place in the workflow, a choice by the order of its values. Without `ORDER BY`, items stand in the order they were made.

## Examples

| Question | Query |
|---|---|
| My open tasks, the most urgent first | `type = task AND assignee = currentUser() AND finished = false ORDER BY due` |
| Overdue | `due < startOfDay() AND finished = false` |
| Changed this week, newest first | `updated >= startOfWeek() ORDER BY updated DESC` |
| Pages of a handbook without an owner | `ancestor = DOC-1 AND owner IS EMPTY` |
| Decisions about the release | `type = decision AND text ~ release` |
| Labelled for the web, but not yet for security | `label = web AND label != security` |

## Mistakes

A query that cannot be read is answered with the character where the mistake is, e.g. `at character 12: "urgent" is not a value of priority`; the view puts the cursor there. A field no type has, a comparison a field does not take, a type, state or choice that does not exist, and a word that is not a date are mistakes. A query is at most 2048 bytes and 64 conditions long; administrators change that with `WWBB_BUDGET_QUERY_BYTES` and `WWBB_BUDGET_QUERY_TERMS`.
