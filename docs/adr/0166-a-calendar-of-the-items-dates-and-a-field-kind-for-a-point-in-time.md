# ADR-0166: A calendar of the items' dates, and a field kind for a point in time

- **Status:** Proposed
- **Implementation:** Not started
- **Date:** 2026-11-06
- **Deciders:** Project initiators (request of 2026-11-06 for a calendar on which the tasks appear)
- **Open question:** whether repeating dates (a weekly meeting) and a calendar that other programs subscribe to (iCalendar, RFC 5545) are needed; this record decides neither, and both would need a record of their own
- **Question checked:** 2026-11

## Context and problem statement

Items carry dates in fields of the kind `date`: the `due` of a task, a decision date, a review date (ADR-0074). The task list marks what is overdue, due today or due this week; the roadmap and the Gantt chart (ADR-0164) show items on an axis of weeks, months or quarters. Nothing shows the days of a month or a week with what falls on them, which is how people plan the next days and look up what is due when.

A `date` field names a day. The API accepts an RFC 3339 date-time for it and stores the instant in UTC, but every reader shows only the UTC day (`item.FormatValue`, the roadmap counts days in UTC). A meeting at 14:00 or a deadline at noon therefore cannot be recorded as a property; it ends up in the body text, where D4 forbids anything that should be queryable. The value written as `2026-11-06T00:30:00+01:00` is shown as 5 November.

How are items shown by day, week and month, and how does an item carry a point in time and not only a day?

## Decision drivers

- D1: a calendar is not content of its own; the items are the content. No event or appointment object beside them.
- D3 and D4: whatever the calendar places is a validated property, never something read from a body.
- D6: no type-specific code; every type with date fields is on the calendar.
- D10: the calendar shows only what the reader may read; a shared link shows another reader only what they may read.
- Types are the users' own: no field name may be built in (as in ADR-0164).
- A day stays a day: a due date must not move to another day when the reader is in another time zone. A point in time must be shown at the reader's local time.
- ADR-0009: nothing fetched from a CDN, no bundler; a new dependency needs a reason.
- What the browser can compute from the listing needs no new route, event or budget (as in ADR-0164).

## Considered options

1. **A view computed in the browser from the query listing, plus a field kind `datetime` for a point in time.**
2. **The view only; a `date` field shows a time when one was stored.**
3. **Stored calendars with entries of their own**, as groupware calendars keep them.
4. **An external calendar server** (CalDAV) linked to the items.
5. **Option 1, with a vendored calendar widget** instead of a grid of the product's own.

## Decision outcome

Chosen option: **1**, because it needs no new route and no new stored object, keeps the permission check where it is (the query listing, ADR-0136), puts every type with dates on the calendar at once, and gives a point in time a place as a validated, queryable property. The one change to the event format is the new field kind.

### The calendar view

- **Route:** `#/calendar`, with the parameters `view` (`month`, `week` or `agenda`), `at` (a day `YYYY-MM-DD` in the period shown; today when absent), `space`, `type`, `labels` (comma-separated), `q` (a query, ADR-0136), and `start` and `end` (field names). Defaults are left out of the address, so a link shares a view.
- **Fields:** the date and datetime fields of the types shown are offered for start and end. When the address names none, they are guessed as the roadmap guesses them (`dateFields`, `guessFields` in `assets/timeline.js`, shared, not copied). An item with both is a span; an item with one is an entry on that day; an end before its start is shown on the end and marked, as on the roadmap.
- **Reading:** one listing, `GET /api/items?query=…`. The browser writes space, type and labels into the query as conditions (`space = …`, `type = …`, `label IN (…)`), joins the reader's `q` with `AND`, and adds a condition that the start or the end falls in the period shown, or that the span covers it. Only items of the period are read. At most the boards' limit of 2 000 items are read, and the view says when there are more. Permissions and veiling are those of the listing; the view adds none.
- **Placing:** a `date` value is a day and stands on that day wherever the reader is; days are counted in UTC, as on the roadmap. A `datetime` value is a point in time, shown in the reader's time zone (the browser's) with its time. In the week view it stands on an axis of hours; in the month view as a line with its time; a span longer than a day is a bar across the days.
- **Views:** *month* is a grid of whole weeks; *week* has seven days with a row for dates without a time and an axis of hours; *agenda* lists the period day by day. The agenda is the form for narrow screens and screen readers. Weeks begin on Monday (ISO 8601, as the roadmap's weeks); the names of months and days come from the browser's `Intl` in the language of the UI (ADR-0017).
- **Look:** finished items (ADR-0082) are hatched; an item whose end lies before today and is not finished is marked overdue in words and shape, not colour alone, as in the task list. Every entry is a link to its item and names it, its dates and its state.
- **Navigation:** *Previous*, *Today* and *Next*, the period as the heading, and the keyboard alone suffices.
- **Entry points:** a tab *Calendar* on the tasks pages beside *Kanbans* and *Roadmap*, which shows the tasks of the current tab with its filters, placed by their due field as the task list finds it; *As a calendar* on a label's page; *Calendar* among a room's views.
- **Read-only:** dates are changed on the item. Moving an entry to change its date is a follow-up; it would be a validated property change (D3).
- **Code:** the layout as plain functions in `assets/calendar.js`, tested by `internal/web/jstest/calendar.test.mjs`; the drawing in its own module beside it; `e2e/smoke.cjs` walks the view. No widget library is added.

### The field kind `datetime`

- **Meaning:** a field of kind `datetime` holds a point in time. A `date` field keeps meaning a whole day and is not changed.
- **API:** the value is an RFC 3339 string with an offset or `Z`. A value without an offset, and a bare date, are refused with the field's name: neither names one point in time. The answer gives the value in UTC, as RFC 3339. `FormatValue`, and with it `where` (ADR-0074) and the export, give RFC 3339 in UTC.
- **Storage:** the value is stored as an instant in UTC, to the second, with the wire value `t` that dates already use. The time zone it was written in is not kept.
- **Event format:** the kind is part of the type definition, so the codec version rises; a build that does not know it refuses the newer log rather than misread it, as with every rise. Stored values of `date` fields are not converted; a type that wants times adds a field of the new kind.
- **Queries (ADR-0136):** a `datetime` field is compared as `created` and `updated` are: `<`, `<=`, `>`, `>=`, `=` and `IS EMPTY`, a day literal naming the whole day in UTC, relative times (`+12h`, `-7d`) and the functions of the language.
- **UI:** the item view shows the value in the reader's time zone with the zone's short name; the editor is a `datetime-local` input, read in the reader's time zone and sent with its offset.
- **Other readers of dates:** the roadmap and the Gantt chart (ADR-0164) offer `datetime` fields too and place them on their UTC day. The governance check of passed deadlines (ADR-0040) treats a `datetime` field whose name means a deadline like a `date` field; a review (ADR-0074) still needs a `date` field. Connectors (ADR-0127) may map a source's date-times to the new kind.
- **Not part of this record:** the writer's time zone, repeating dates, a duration as a kind of its own (a span is two fields), invitations, and a calendar feed for other programs.

### Consequences

- **Positive:** every type with dates is on a calendar, in every room and for every label and query, without anyone making a calendar. The tasks a person works on are on their calendar through the tasks page. A meeting or a deadline at an hour becomes a property that queries, the calendar, the roadmap and agents (over MCP, through the query) read alike.
- **Negative / trade-offs accepted:** the calendar is read-only. An instant without its zone cannot keep "09:00 in Zurich" across a change of daylight saving time, which only matters for repeating dates, which this record leaves out. Days of `date` fields and day literals in queries are counted in UTC, as everywhere in the product; a reader far from UTC sees a `datetime` entry late in the evening on its local day while a query by day counts its UTC day. The codec version rises, so a data directory written by this build cannot be opened by an older one.
- **Follow-ups / risks to watch:** moving entries to change their dates; a calendar feed (iCalendar) for other programs, which copies titles and dates out of the product and so needs a record that weighs this against ADR-0064 (a message is a pointer, not a copy) and decides how a feed is authorised without a session; repeating dates; whether 2 000 items per period suffice for a large room.

## Pros and cons of the options

### Option 2: times in `date` fields

Strongest form: the API already accepts a date-time for a `date` field and stores the instant. Showing the time when it is not midnight UTC would give times without a codec change and without a second kind for users to choose between.

- Good: no change to the event format; no new kind.
- Bad: one kind would mean two things. A due date written as a day would be shown on the previous day to every reader west of UTC as soon as it is read as an instant, and a day could not be told from a time at midnight. Queries could not tell a whole day from a point in time. The ambiguity would be in every stored value and could not be undone later.

### Option 3: stored calendars

Strongest form: groupware calendars keep entries with a start, an end, a place, participants, repetitions and invitations. A stored calendar per room could have exactly that and be curated: a team's calendar holds what the team decides, not whatever has a date.

- Good: repetitions, participants and invitations have an obvious home.
- Bad: an entry is user-generated content, so as anything but an item it breaks D1; as an item it is option 1 with a type of the users' own, which option 1 already allows. A stored calendar would be a second object to keep in step with the items it shows, with events, permissions and a codec change of its own, and a task would not be on a calendar until someone puts it there.

### Option 4: an external calendar server

Strongest form: CalDAV is a standard every calendar program speaks; open-source servers exist; people would see and edit their dates in the program they already use, with repetitions and invitations, without the product implementing any of it.

- Good: rich clients, two-way synchronisation, nothing to build for the view.
- Bad: a second store with its own permissions, which cannot follow D9 and D10 and would show a restricted item's title to anyone with access to the calendar; content outside the event log, out of reach of purge and redaction (ADR-0026); a second server to run, against ADR-0009. Writing back from the calendar program would bypass validated changes (D3). A read-only feed for calendar programs is the useful part of this option; it remains a follow-up.

### Option 5: a vendored calendar widget

Strongest form: established open-source calendar widgets under permissive licences solve month, week and agenda views, time zones, overlapping entries and dragging; vendoring one as a pre-built bundle follows the editor's example (ADR-0032) and saves work, above all once entries can be moved.

- Good: mature layout of overlapping entries; dragging comes with it.
- Bad: a large opaque bundle for a grid of seven columns; its look, accessibility and language would have to be fitted to the product's themes (ADR-0120) and message catalogue (ADR-0017); the layout of the roadmap could not be shared. The month and agenda views are small; if the week view with overlapping entries and dragging proves hard, a widget can still replace the drawing without changing anything else this record decides.

## Links

- relates to ADR-0164 (roadmap and Gantt chart, whose field guesses and axis rules this record shares)
- relates to ADR-0136 (query language), ADR-0074 (`where`, value formats and reviews), ADR-0040 (governance checks), ADR-0082 (finished states), ADR-0017 (message catalogue), ADR-0009 (single binary), ADR-0032 (a vendored bundle), ADR-0120 (themes), ADR-0127 (connectors), ADR-0064 (a message is a pointer, not a copy)
