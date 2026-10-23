# ADR-0116: Every view fits a phone

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-23
- **Deciders:** Project initiators (the maintainer found that the app did not work well on small screens: one could not get out of the drawing editor, tables were not shown correctly, and more; they asked to check every view and fix what is wrong, 2026-10-23)

## Context and problem statement

The views were built for wide screens and adapted in places (ADR-0065, ADR-0100). A check of every view at 375, 768 and 320 px wide, with a script that reports what reaches past the screen's edge and screenshots of each view, found:

- **The drawing editor:** a dialog `100vh − 32px` high. On a phone, `100vh` is the height behind the browser's bars, so the dialog was taller than what one sees and its Cancel and Save could sit out of sight. Going back with the phone's back button left it open.
- **Tables in a text:** their cells shrank to a few letters each, edited cells cut their text, and nothing showed that the table scrolls sideways.
- **Wide tables in views** (governance, the AI access page): they widened the whole page, so everything scrolled sideways.
- **The history:** its narrow column broke "you" and "BodyCommitted" mid-word.
- **Keys:** a key such as PLAT-12 broke at its hyphen.
- **Too wide:**
  - the title field while editing, by 36 px;
  - the kanban's forms and the search form, at 320 px.
- **The export menu:** it opened partly off the screen's left edge.

## Decision drivers

- Nothing on a phone may make the page wider than the screen.
- A dialog's way out is always in sight.
- Wide content scrolls inside its own frame, and shows that it does.

## Considered options

1. Fix each finding with styles for narrow screens, and test every view at phone width.
2. A separate layout for phones.

## Decision outcome

Chosen option: **1**, because the findings are local, and one layout is easier to keep right than two.

- **The drawing editor:** below 640 px it fills the screen (`100dvh`, no margin), so its bar with Cancel and Save is at the top. Elsewhere its height is `100dvh − 32px`. Leaving the page, as the back button does, closes it unsaved, as Cancel does.
- **Tables in a text:** a cell keeps at least 7 rem and wraps beyond 22 rem. A shadow at an edge shows that the table scrolls that way, and edited cells keep their width.
- **Tables in views:** at 900 px and less, a table scrolls inside its frame instead of widening the page.
- **The history:** on phones, its cells do not break words, and the table scrolls.
- **Keys:** they do not wrap.
- **Forms of one line:** they wrap their button below the field.
- **The title field:** it takes the head's width.
- **The export menu:** it opens as a sheet at the bottom of a phone's screen.
- **Note dialogs:** they keep within the visible height and scroll.
- **e2e:** a test opens twelve views at 375 px and checks that none is wider than the screen. It also checks that the drawing editor's Cancel is in sight and closes it.

### Consequences

- **Positive:** every view fits 320 px and up; the drawing editor can always be left.
- **Negative / trade-offs accepted:** on phones a list table is as wide as its content, inside a full-width frame, rather than stretched.
- **Follow-ups / risks to watch:** new views need the same check; the e2e step lists the views it covers.

## Pros and cons of the options

### Option 2: a separate phone layout

Strongest form: each view could be designed for a thumb, e.g. lists as cards.

- Good: the best possible phone experience.
- Bad: two layouts to keep in step for every change; most views already work.

## Links

- relates to [ADR-0065](0065-a-uniform-airy-ui-system.md) (the UI system), [ADR-0100](0100-a-tidy-top-bar-and-columns-that-fold-away.md) (the top bar and columns), [ADR-0053](0053-drawings-with-excalidraw-on-a-page-of-their-own.md) (the drawing editor)
