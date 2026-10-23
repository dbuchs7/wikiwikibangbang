# ADR-0113: One's notes from the start page

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-23
- **Deciders:** Project initiators (the maintainer found personal notes visible only on their page, and not prominently, and asked for a "My notes" tile on the start page, 2026-10-23)

## Context and problem statement

Personal notes (ADR-0107) show on the page they were made on: the passage is marked, and the side column lists them. `GET /api/notes` already answered all of one's notes on items one may still read, but no view used it. Someone who made notes on several pages had no way to find them again but to remember the pages. Where does one see all of one's notes?

## Decision drivers

- Notes stay private (ADR-0107): only their maker sees them, and only on items they may still read.
- The start page is where one's own things are reached (tasks, notifications).
- No new server state.

## Considered options

1. A "My notes" tile on the start page, leading to a list of one's notes.
2. A section on the start page listing the latest notes.
3. Notes in the notifications.

## Decision outcome

Chosen option: **1**, as the maintainer asked.

- **The tile:** "My notes" stands among the start page's tiles and says how many notes one has.
- **The list:** it is at `#/my-notes`, newest first. Each note shows its item's key and title, when it was last changed, the passage it is on, and its text. The key links to the item, where the note is marked.
- **Privacy:** the list says the notes are private, and it can be filtered (ADR-0098). It reads `GET /api/notes`, which leaves out items one may no longer read.

### Consequences

- **Positive:** notes are found again without remembering their pages.
- **Negative / trade-offs accepted:** the list loads all of one's notes at once; their number is bounded per item (`maxNotesItem`), not overall.
- **Follow-ups / risks to watch:** if people keep many notes, the list may need paging.

## Pros and cons of the options

### Option 2: a section on the start page

Strongest form: the newest notes at a glance, without a click.

- Good: no extra view.
- Bad: lengthens the start page for everyone, also for those without notes.

### Option 3: in the notifications

Strongest form: one place for everything personal.

- Good: no new tile.
- Bad: notifications are changes by others; a note is one's own, and stays.

## Links

- amends [ADR-0107](0107-personal-notes-seen-by-their-maker-alone.md) (personal notes)
