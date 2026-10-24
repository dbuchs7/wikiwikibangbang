# ADR-0119: Lists of figures and tables, and the table of contents by its name

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-24
- **Deciders:** Project initiators (the maintainer asked to name the table of contents "Inhaltsverzeichnis" instead of "Inhalt" in the editor's Insert menu, and for a list of figures and a list of tables that can be inserted as well, 2026-10-24)

## Context and problem statement

A body can hold a table of contents (ADR-0042). It is a block that holds nothing and lists the headings when shown. The Insert menu called it "Inhalt" (English "Contents"), which reads like "the content" rather than a list of it.

Long documents, such as reports, concepts and manuals, also list their pictures and tables. A body has pictures, drawings included, as `image` blocks with a caption. It has tables as `table` blocks without a caption; the header row says what a table holds.

## Decision drivers

- A list stays right without being maintained, as the table of contents does.
- The same lists in the editor, in reading, and in the Word and PDF exports.
- No new data on pictures or tables that every older body lacks.

## Considered options

1. Two new block kinds, `figures` and `tables`, computed like the table of contents.
2. One kind, the table of contents, with a field that says what it lists.
3. Captions for tables, and numbered figures and tables in the text itself.

## Decision outcome

Chosen option: **1**, because it adds two blocks that work exactly like one people already know, and needs no new data.

### The two lists

- **Pictures:** the list of figures numbers the body's pictures in their order: "Abbildung 1: caption", or "Abbildung 1" without one. A video is not a figure.
- **Tables:** the list of tables numbers the tables the same way, with the header row's cells: "Tabelle 1: Item, Price".
- **In the text:** a click on an entry scrolls to the picture or table. Both lists refresh with every change, as the table of contents does.
- **When empty:** they say there are no pictures or no tables yet.
- **Exports:** the HTML (PDF) and Word exports write the same lists, in the reader's language.

### Names

- **The table of contents:** "Inhaltsverzeichnis" in the Insert menu and on the block; in English "Table of contents". The heading of an exported handbook's list of pages follows.
- **The two lists:** "Bildverzeichnis" and "Tabellenverzeichnis"; in English "List of figures" and "List of tables". The entries say "Abbildung" and "Tabelle" ("Figure", "Table").

### Codec

Codec version 28 adds the two kinds. Versions 1 to 27 are still read. A server before this one refuses a version 28 log rather than misreading it.

### Consequences

- **Positive:** reports get the usual lists, always current, also in exports.
- **Negative / trade-offs accepted:**
  - The numbers exist only in the lists; the pictures and tables in the text are not numbered.
  - A table is named by its header row, not a caption of its own.
- **Follow-ups / risks to watch:** a caption for tables, if header rows turn out not to say enough.

## Pros and cons of the options

### Option 2: one kind with a field

Strongest form: one block, one view and one export case, where the field says headings, pictures or tables.

- Good: less code.
- Bad:
  - The table of contents would change its stored form, which every older log has.
  - Readers of the API would have to look at a field to know what a block lists.
  - It needs a codec version all the same.

### Option 3: captions and numbers in the text

Strongest form: as in a word processor, every table has a caption, and "Figure 3" in the text matches "Figure 3" in the list.

- Good: references in the text are possible.
- Bad:
  - It needs a new field on tables, and an editor for it.
  - Numbering in the text must follow every move of a block.
  - None of that was asked for.

## Links

- relates to [ADR-0042](0042-tasks-panels-tables-and-computed-blocks.md) (the table of contents) and [ADR-0071](0071-exporting-pages-and-lists.md) (exports)
