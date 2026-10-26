# ADR-0128: Markdown is pasted into the editor and converted in the browser

- **Status:** Proposed
- **Implementation:** Partial
- **Date:** 2026-10-26
- **Deciders:** Project initiators (requested 2026-10-26: Markdown written or copied elsewhere — by an assistant, in a code editor, from a README — goes into a new or existing page as formatted, editable text); proposed for the maintainer's acceptance
- **Open question:** a numbered list starts at 1 in every run of numbered items, so its own start number and its numbering across a block between its items (a code block inside an item) are lost; keeping them needs a model change (a start number on an item, or nested lists, ADR-0032). Is the loss acceptable?
- **Question checked:** 2026-10

## Context and problem statement

People write and copy Markdown in many places. Pasted into the editor today, it arrives as plain text: one paragraph per line, with `#`, `**`, `-` and backticks shown as written. The body is a structured block document (ADR-0001) that never stores Markdown or HTML (ADR-0014); the editor's schema is that model (ADR-0032).

The questions are: how does Markdown get into a body, where and by what is it converted, and when is a paste Markdown at all?

## Decision drivers

- Markdown is an input format only; the block document stays the one stored format (ADR-0001, D4).
- The conversion is closed: no HTML passes, links lead only to allowed targets, and the server validates whatever any client sends (ADR-0014).
- Markdown goes in like any paste: at the cursor, replacing a selection, as one undo step, through transactions that a live session's block locks filter (ADR-0010, ADR-0032).
- Ordinary text must not be taken for Markdown; when in doubt, a paste stays as it is.
- Nothing is lost without notice: what the model cannot hold is simplified visibly.
- No runtime besides the binary, no CDN, no second executable (ADR-0009); every vendored dependency costs review (ADR-0032).

## Considered options

1. **An own closed converter in the browser** (`assets/markdown.js`) from Markdown to the model's blocks, inserted through the editor's transactions; recognised conservatively on paste and offered explicitly in the Insert menu.
2. **`prosemirror-markdown` with markdown-it**, added to the vendored ProseMirror bundle.
3. **A server endpoint** that converts Markdown into blocks in Go and returns them to the editor.
4. **Markdown to HTML in the browser**, then ProseMirror's own HTML paste.

## Decision outcome

Chosen option: **1**.

- **Conversion.** `markdown.js` has no DOM and no dependency. It reads CommonMark's block structure and inline rules — emphasis by CommonMark's delimiter algorithm, code spans, inline and reference links, autolinks, backslash escapes, numeric and common named entities, hard and soft line breaks — and GitHub's tables, task items, strike-through (`~~`), bare web addresses and alerts. It returns the blocks `nodeFromBlock` reads, with mark ranges in Unicode code points as the model counts them:

  | Markdown | Block or mark |
  |---|---|
  | `#` to `###`; setext headings | heading, levels 1 to 3 |
  | `####` to `######` | heading, level 3 |
  | `-`, `*`, `+` items; `1.`, `1)` items | bullet; ordered |
  | `- [ ]`, `- [x]` | task, unchecked or checked |
  | `>` quote | quote, one per paragraph |
  | `> [!NOTE]`, `[!TIP]`, `[!IMPORTANT]`, `[!WARNING]`, `[!CAUTION]` | panel: info, tip, note, warning, error |
  | fenced (backticks or tildes) or indented code | code, its text unchanged |
  | `---`, `***`, `___` | divider |
  | table with a delimiter row | table |
  | other text | paragraph |
  | `**`/`__`, `*`/`_`, `~~`, backticks | bold, italic, strike, code |
  | `[text](url)`, `[text][ref]`, `<url>`, bare `https://`/`www.` | link, when `isSafeLink` allows the target |

- **What the model cannot hold** is simplified, never dropped: nested lists become flat items in their order (the model's lists are flat, ADR-0032); paragraphs and code inside an item follow it; inside a quote, list items keep their marker as text (`• `, `1. `, `☐ `, `☑ `) and code stays a code block; table cells hold plain text (ADR-0042), so their formatting goes and a link keeps its address in parentheses; a row wider than the header widens the table; a picture becomes its description linked to its address, as pictures from other servers are not loaded without a person's action; a link whose target is not allowed keeps its text (ADR-0014); HTML, footnotes and anything else unknown stay visible text. The editor's status line names the pictures and link targets that were simplified. A code block's language is not kept: the model has no field for it, and adding one is a model change this record does not make.
- **Insertion.** `markdownInsertion` in `editor.js` makes a slice of the converted blocks and applies it with `replaceSelection`, marked as a paste — one transaction, so one undo step, filtered by the live session like every change. One paragraph goes into the text at the cursor; other blocks split the block there, follow it, or replace an empty paragraph. New blocks get new session IDs from the live plugin. Nothing is converted into a protected passage (ADR-0088), so a paste never splits or unprotects one; a paste into a code block stays text.
- **Recognition on paste**, in this order: a picture (ADR-0108) goes first; Ctrl+Shift+V (Cmd+Shift+V) pastes plain text; the editor's own copies (`data-pm-slice`) and HTML with formatting elements (headings, lists, tables, emphasis, links, pictures) paste as the source rendered them; otherwise plain text is converted when it holds a construct only Markdown writes (a closed code fence, a table with its delimiter row, a task item, a link or picture with a web address or path) or two different typical ones (heading, list of two or more items, bold, italic, inline code, quote, strike-through, rule) without reading like program code. A code editor's coloured source (only `div`, `span`, `pre`, `code`, `br`) counts as plain text.
- **Explicit insertion.** *Insert → Paste Markdown…* opens a dialog with a text field; its text is converted whatever recognition would say and goes where the cursor was; the focus returns to the text.
- **No context menu entry.** A menu of the editor's own would replace the browser's (spelling suggestions, copy, paste) or need a second menu beside it; Ctrl+V and the Insert menu cover the need.
- **Tests.** `internal/web/jstest` holds unit tests that run in Node.js with its own test runner and no dependency (`node --test`), for the converter and for the insertion on an `EditorState`; CI runs them in the job that sets up Node. `e2e/smoke.cjs` pastes with Ctrl+V and through the dialog, undoes and redoes, pastes hostile Markdown, and pastes while another person holds a block.
- **Relation to ADR-0014.** ADR-0014 has import and paste convert foreign formats on the server. ADR-0032 already converts pasted HTML in the browser, with the server validating what is stored; this record does the same for Markdown, and server validation stays the guarantee.

### Consequences

- **Positive:** Markdown from anywhere becomes ordinary editable blocks; the stored format, the server and the live session are unchanged; about 900 lines of owned, tested code instead of a vendored library; the conversion follows the model exactly, including tasks, panels and tables.
- **Negative / trade-offs accepted:** recognition is a heuristic and errs towards plain text, so some Markdown pastes as written and must go through the dialog; the converter covers CommonMark and GitHub's extensions only as far as the model reaches and must be kept in step with the model by hand; numbered lists lose their start number (the open question); a picture is a link until someone uploads it.
- **Follow-ups / risks to watch:** recognition errors reported by people; a model change for list numbering or a code language would extend the mapping; a Markdown export is a separate decision.

## Pros and cons of the options

### Own closed converter in the browser

- Good: the conversion yields exactly the model's blocks, so nothing needs to be mapped twice; small enough to read completely; no bundle change; the paste stays a local transaction, so undo and block locks work unchanged.
- Bad: CommonMark's corner cases are ours to get right; detection rules are ours to tune.

### prosemirror-markdown with markdown-it

Strongest form: the editor's own project maintains a Markdown parser for ProseMirror schemas, built on markdown-it, which passes the CommonMark test suite; it would follow the specification to the letter with little own code.

- Good: high fidelity to the specification; tables come with markdown-it.
- Bad: markdown-it and its plugins enlarge the vendored bundle that must be rebuilt and reviewed (ADR-0032); raw HTML is part of CommonMark and must be switched off by configuration — the "library plus sanitizer" ADR-0014 rejected; task items and alerts need further plugins; the token-to-node mapping for this schema (flat lists, tasks, panels, plain table cells) is still own code.

### Server endpoint in Go

Strongest form: one converter serves the editor, the API and MCP agents alike; it is written in the product's language and counts towards its coverage; ADR-0014's wording asks for conversion on the server.

- Good: one place for every client; Go tests.
- Bad: a paste waits for a request; a new route with its own budget and access class (ADR-0011, ADR-0015) for a conversion that changes no data; the result still has to be inserted in the browser, so the editor work remains; agents already send blocks.

### Markdown to HTML, then ProseMirror's paste

Strongest form: the smallest change — a Markdown-to-HTML step, then the paste path that already exists.

- Good: reuses the HTML paste.
- Bad: HTML is built from user text only to be parsed again, against ADR-0014's rule that no string becomes markup; tables and tasks have no HTML parse rules in the schema and would be lost; two conversions to keep correct instead of one.

## Links

- relates to [ADR-0001](0001-unified-item-data-model.md) (block document), [ADR-0010](0010-real-time-collaboration-with-block-locks.md) (block locks), [ADR-0014](0014-closed-renderer-for-user-content.md) (closed rendering), [ADR-0032](0032-prosemirror-rich-text-editor.md) (editor and schema), [ADR-0042](0042-tasks-panels-tables-and-computed-blocks.md) (tasks, panels, tables), [ADR-0059](0059-alignment-dividers-and-more-marks.md) (divider, strike-through), [ADR-0088](0088-protected-passages-of-a-body.md) (protected passages), [ADR-0108](0108-a-picture-is-copied-in-the-album-and-pasted-into-a-text.md) (pasted pictures)
- user guide: [Pasting Markdown](../guides/markdown.md)
