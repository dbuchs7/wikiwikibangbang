# ADR-0114: Tasks in a text: made below the page, shown as a link or whole, listed by label

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-23
- **Deciders:** Project initiators (the maintainer asked to make tasks directly in a page, as a link to the task or shown whole, and to show in a page a sortable, filterable list of the tasks of a label; they chose that a task made in a page lies below it, and that the list covers every space the reader may read, 2026-10-23)

## Context and problem statement

A page could already show a task: mentioned by "@" (ADR-0096) or embedded and shown whole (ADR-0106). The task had to exist first, made elsewhere. A page could not list tasks at all. Boards by label (ADR-0087) show the tasks of a label, but on their own view, not in a text.

How does someone writing a page make a task without leaving it, and show the tasks of a topic in the text?

## Decision drivers

- A task made in a page is a task like any other: on boards, in the task list, with its workflow.
- A list in a text stays current: it shows the tasks as they are when read, not as they were when written.
- Each reader sees only what they may read (D10).
- Nothing new to learn: the editor's Insert menu, the list's filter (ADR-0098).

## Considered options

1. "New task…" in the editor makes a task below the page and puts it in the text. A block kind `tasklist` names a label, and its tasks are computed when shown.
2. The same, but the list's tasks are copied into the text when it is written.
3. A task list as a view of its own, linked from the text.

## Decision outcome

Chosen option: **1**, because the list stays current and readers see only what they may read.

### New task
- **Where it is offered:** "Insert… → New task…" while editing.
- **The dialog:** a title, the type (types with a workflow that are tasks) and the type's required fields. It also asks how the task shows in the text: **as a link** or **shown whole**.
- **What is made:** the task is made with the page as its parent, in the page's space, through the existing `POST /api/items`.
- **What goes into the text:**
  - as a link, a paragraph with a mention of the task, which relates the page to it (ADR-0032);
  - shown whole, an embed (ADR-0106).
- **Live editing:** both go in at the cursor, through the live session (ADR-0010).

### Task list by label
- **Where it is offered:** "Insert… → Task list by label…". The label is typed with suggestions from the labels tasks carry, and stored as the label is stored (ADR-0044).
- **The block:** kind `tasklist`, its text the label. It holds no marks or references. This is codec version 27; versions 1 to 26 are still read.
- **What it shows:** when read, it lists every task (an item of a type with a workflow) that carries the label, in every space the reader may read. The API answers only those, veiled as for the reader.
- **The table:** key, title, state and space. Every column sorts both ways by its heading, and the list is filtered by text, state and space.
- **Exports:** a PDF or Word export lists the tasks by key, title and state, as the exporting reader may read them.

### Consequences

- **Positive:** tasks are made where they are discussed and stay ordinary tasks; a page can track a topic's tasks without being edited.
- **Negative / trade-offs accepted:**
  - A task list reads up to the API's page size several times (`readAll`); a label with very many tasks shows the first ones and says so.
  - An older build cannot read a log that holds a task list (codec version 27).
- **Follow-ups / risks to watch:** none.

## Pros and cons of the options

### Option 2: copy the tasks into the text

Strongest form: the text is self-contained, searchable and exported as written.

- Good: no computation when read.
- Bad: out of date at once; a copy shows tasks to readers who may not read them.

### Option 3: a view of its own

Strongest form: a full board or table without crowding the text.

- Good: room for more columns.
- Bad: the board by label is that view already; the request was to see the tasks in the page.

## Links

- relates to [ADR-0087](0087-kanban-pictures-and-boards-by-label.md) (boards by label), [ADR-0096](0096-links-mentions-and-embeds-as-in-a-word-processor.md) (mentions), [ADR-0106](0106-an-embedded-item-is-shown-whole.md) (embeds shown whole), [ADR-0098](0098-every-list-can-be-filtered.md) (filters)
