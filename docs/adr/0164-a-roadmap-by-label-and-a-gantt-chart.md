# ADR-0164: A roadmap by label and a Gantt chart

- **Status:** Proposed
- **Implementation:** Partial
- **Date:** 2026-11-03
- **Deciders:** Project initiators (the maintainer's request of 2026-11-03 for a roadmap by labels or a Gantt view)
- **Open question:** whether bars should be moved and stretched on the chart to change their dates, and whether the dependencies of ADR-0137 should be drawn as arrows between bars
- **Question checked:** 2026-11

## Context and problem statement

Items carry dates in fields of the kind `date` (a due date, a start, a decision date), and labels group them (ADR-0044). The task list, the boards (ADR-0036, ADR-0087) and the kanbans (ADR-0060) show items by state; nothing shows them by time. Whoever plans a release or a quarter has to read dates item by item.

How are items shown on a time axis, as a roadmap grouped by label and as a Gantt chart, without a new kind of stored object?

## Decision drivers

- D1: a roadmap is not content of its own; the items are the content.
- D4: what the axis shows must be queryable properties, never something hidden in a body.
- D10: the view shows only what the reader may read; a shared link shows another reader only what they may read.
- A label is a board at once (ADR-0087); a label should be a roadmap at once too, without a step.
- Types are the users' own: no field name may be built in.

## Considered options

1. **A view computed in the browser from the item listing, its state in the address.**
2. **A stored roadmap object per space**, with its lanes, fields and order, like a kanban.
3. **Dedicated server route** that computes the bars and lanes.

## Decision outcome

Chosen option: **1**, because it needs no new route, no event and no budget, keeps the permission check where it is (the listing), and makes every label a roadmap at once.

- **Route:** `#/roadmap`, with the parameters `mode` (`roadmap` or `gantt`), `space`, `type`, `labels` (comma-separated), `start` and `end` (field names) and `scale` (`week`, `month`, `quarter`; automatic when absent). Defaults are left out of the address.
- **Items:** read from `GET /api/items`, narrowed by room and type, once per chosen label and merged by ID; at most the boards' limit of 2 000 items, and the view says when there are more. Only items whose type has a date field are on the axis; others are not counted.
- **Fields:** the date fields of the types shown are offered for start and end. When the address names none, the start is guessed among names that mean a beginning (`start`, `begin`, `from`, …) and the end among names that mean an end (`due`, `end`, `deadline`, …), else the first other date field.
- **Bars:** an item with both dates is a bar from start to end, both days included. With one date, or with an end before its start, it is a milestone on the end (the promised date), else on the start; an inverted item is marked and says so. Items without a date are listed apart.
- **Roadmap:** one lane per label, in the order chosen, or in the order of the alphabet without a choice, plus a lane for items without a label; an item stands in each of its labels' lanes; bars are packed into as few rows as they need.
- **Gantt chart:** one row per item, ordered by start, end and key.
- **Axis:** whole weeks (ISO 8601), months or quarters, with one unit of room on either side; a line marks today when it falls in the frame. Days are counted in UTC so no time zone moves a bar.
- **Look:** finished items (ADR-0082) are hatched, items that wait on unfinished ones (ADR-0137) have a dashed outline, milestones are diamonds: shape and outline, not colour alone. Every bar is a link to its item and names it, its dates and its state in words.
- **Entry points:** a tab *Roadmap* beside *Kanbans* on the tasks pages, *As a roadmap* on a label's page, and *Roadmap* among a room's views.
- **Code:** `assets/timeline.js` holds the layout as plain functions, tested by `internal/web/jstest/timeline.test.mjs`; `assets/roadmap.js` draws it; `e2e/smoke.cjs` walks the view.

### Consequences

- **Positive:** every label and every room is a roadmap and a Gantt chart without anyone making one. A link shares a view.
- **Negative / trade-offs accepted:** the chart is read-only; dates are changed on the item. A lane has no order of its own beyond the order of labels in the address. Reading per label costs one listing per chosen label.
- **Follow-ups / risks to watch:** moving bars to change dates (each a validated property change, D3); arrows for `depends-on` relations, which need the relations of every shown item; a server-side listing by several labels if one per label proves slow.

## Pros and cons of the options

### Option 2: a stored roadmap

Strongest form: a roadmap could keep a curated order of lanes and items, a title and a picture, and be listed like a kanban; teams could keep several roadmaps for one label.

- Good: order and curation survive without long addresses.
- Bad: it is a second object to keep in step with the items, needs events, a codec change and permissions of its own, and a label would not be a roadmap until someone makes one. Its curation can follow as a kanban-like object later if asked for; option 1 does not exclude it.

### Option 3: a server route

Strongest form: the server could compute lanes and packing for very large sets and serve other clients, such as MCP, the same view.

- Good: one implementation for every client.
- Bad: lanes and packing are presentation; the listing already answers what agents need. A new route adds an access class, tests and a budget for a computation the browser does in milliseconds for 2 000 items.

## Links

- relates to ADR-0036 and ADR-0087 (boards computed on the fly), ADR-0044 (labels), ADR-0082 (finished states), ADR-0136 (queries), ADR-0137 (relations and waiting on)
