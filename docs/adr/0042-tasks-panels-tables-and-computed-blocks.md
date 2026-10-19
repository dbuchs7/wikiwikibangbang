# ADR-0042: Tasks, panels, tables, tables of contents and lists of child pages as flat blocks

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-19
- **Deciders:** Project initiators (the maintainer asked for the wiki to come as close as possible to the wiki it replaces, 2026-10-19)

## Context and problem statement

A body had paragraphs, headings, list items, quotes, code and embeds (ADR-0032). The most used elements of wiki pages were missing: checklists, highlighted notes (info, warning …), tables, a table of contents and a list of the page's child pages. Imported pages kept them as unsupported blocks (ADR-0023), shown as their source markup.

The body is a flat list of blocks: live editing locks one block at a time (ADR-0010), derived relations are a function of the blocks (D4), search, governance and the link check read each block's text (ADR-0033, ADR-0040, ADR-0039). Nested structures — a table of rich-text cells, a panel holding lists — would break each of those, or need them all to learn nesting.

## Decision drivers

- Keep the body flat, so locks, search, governance, derived relations and the log format stay as they are.
- Every text a person can write is text the governance check reads: a password typed into a table cell must restrict the page like one typed into a paragraph.
- What is computed (headings, child pages) is not stored, so it cannot go stale.
- The import keeps the common elements instead of their markup.

## Considered options

1. **Flat blocks**: a task and a panel are text blocks with one attribute; a table is one block whose text holds its cells; a table of contents and a list of child pages are empty blocks, computed when shown
2. **Nested blocks**: tables of cells, panels and tasks holding blocks
3. **Macros as one generic block** with a name and parameters, as the source wiki stores them

## Decision outcome

Chosen option: **1**.

- **Task** (`task`): a text block with marks and mentions, and `Checked`. Consecutive tasks read as one checklist, like list items (ADR-0032).
- **Panel** (`panel`): a text block with marks and mentions, and `Tone`: `info`, `note`, `warning`, `tip` or `error`. Consecutive panels of one tone read as one box, so a panel of several paragraphs is several blocks.
- **Table** (`table`): one block; its text holds the cells, rows separated by line breaks and cells by tabs, the first row being the header. Every row has the same number of cells; cells are plain text, without marks. Search, governance and the link check read the cells without change, since they read the text. The whole table is one block for live editing: one person edits a table at a time.
- **Table of contents** (`toc`) and **child pages** (`children`): empty blocks. The editor lists the body's headings, or the item's readable child pages in their order (ADR-0041), when it shows them.
- **Validation:** `Checked` only on tasks, `Tone` only on panels and always one of the five, a reference only on embeds, no text on a table of contents or a list of child pages, a table rectangular and not empty.
- **Format:** `CodecVersion` 6 adds the block fields `Checked` and `Tone` and the new kinds. Versions 1 to 5 are still read; a version 5 build refuses a version 6 log.
- **Editor:** the block menu offers tasks and the five panels; an *Insert* menu adds a table, a table of contents or a list of child pages after the current block. `[] ` starts a task, `[x] ` a done one; Enter continues a task list or a panel, and on an empty task or panel ends it. A task's checkbox ticks it; a table's cells are edited in place, with buttons that add and remove rows and columns.
- **Import (ADR-0023):** Confluence's info, note, warning and tip macros become panels of that tone, task lists become tasks (done when their status is complete), tables become tables (merged cells and formatting inside cells are lost), and the toc and children macros become their blocks.

### Consequences

- Good: nothing outside the item model and the editor had to learn the new kinds; locks, relations, search, governance and the link check work on them as they are.
- Good: computed blocks are never stale.
- Bad: table cells have no formatting, links or mentions; a link in a cell is plain text the link check still finds, but it is not clickable.
- Bad: a table is locked as a whole while one person edits it.
- Bad: a panel cannot hold a list or a code block; it holds text with marks.
- Neutral: expand (fold-away) sections, status badges and dates are not built.

## Pros and cons of the options

### 1. Flat blocks

- Good: no change to locking, relations, search, governance or the log beyond two fields.
- Bad: tables and panels are less rich than in the source wiki.

### 2. Nested blocks

- Good: rich cells and panels.
- Bad: every consumer of a body must learn nesting; a lock on a cell, a table and a page become three different things; the format changes deeply.

### 3. One generic macro block

- Good: any macro of the source survives as data.
- Bad: what a macro means is code that must exist anyway; parameters are untyped, so validation, search and governance cannot rely on them.

## Links

- ADR-0010 (live editing), ADR-0023 (import), ADR-0032 (rich text), ADR-0033 (search), ADR-0040 (governance), ADR-0041 (page tree)
