# ADR-0131: Pages export as Markdown, and agents read them so

- **Status:** Proposed
- **Implementation:** Partial
- **Date:** 2026-10-26
- **Deciders:** Project initiators (the maintainer asked for a Markdown export of pages, chose a ZIP archive with a folder of pictures for people, and asked for the export to be offered to agents over MCP as a read, 2026-10-26); proposed for the maintainer's acceptance
- **Open question:** Markdown has no underline, highlight, alignment, picture width or start number of a numbered list. The export writes underline and highlight as HTML (`<ins>`, `<mark>`), and formatting Markdown cannot nest as `<em>`, `<strong>` or `<del>`; it drops the rest. Is HTML acceptable in a file meant for other tools, or should those formats be dropped as well?
- **Question checked:** 2026-10

## Context and problem statement

Pages leave the wiki as Word documents and PDFs, tables and lists as CSV (ADR-0071). Documentation is also kept beside code in repositories, written in editors such as VS Code or Obsidian, and handed to AI assistants; Markdown is the format all of them read. Agents read pages over MCP only as the block document of `get_item` (ADR-0012), which is exact but not the text a person reads.

How do pages export as Markdown, how do pictures travel with the text, and how do agents read a page as Markdown?

## Decision drivers

- The same content as the Word and PDF exports, gathered once through the reader's session: nothing the reader may not read (D10), protected passages veiled (ADR-0088).
- Output that every CommonMark and GitHub-flavoured renderer reads as meant; text a person wrote never becomes markup or HTML (ADR-0014).
- Pictures travel with the text and read without the server.
- Agents read with exactly their caller's rights, and only read (ADR-0012).
- One binary, no dependency (ADR-0024, ADR-0071).

## Considered options

1. **A Markdown writer in `internal/export`** from the export's document: a text route, an archive route with the pictures, and a read tool for agents.
2. **Markdown made in the browser** from the body the page holds.
3. **A library**, e.g. the HTML export converted with an HTML-to-Markdown library.
4. **Only the text**, pictures as `data:` addresses or as links to the server.

## Decision outcome

Chosen option: **1**.

- **Routes.**
  - `GET /api/items/{ref}/export/md` answers `text/markdown; charset=utf-8`, with pictures and videos linked to their attachments (`/api/items/{id}/attachments/{id}`).
  - `GET /api/items/{ref}/export/md.zip` answers a ZIP archive: `<key> <title>.md`, and the pictures it shows in `media/NN-name`, linked by relative paths. A file name keeps letters, digits, dots and dashes only, so it cannot leave the folder. A video is named, not copied (ADR-0097). A picture without its file keeps its caption.
  - Both take `children=true` for a handbook, as ADR-0071 has it: a list of the pages, then each page under a heading by its depth.
- **Where.** *Export* on a page offers *Markdown with pictures (.zip)*, and on a page with subpages *Markdown with subpages (handbook, .zip)*.
- **Agents.** The MCP tool `get_item_markdown` (ref, children) is one GET of the text route with the caller's credential. The adapter gains an argument kind `boolean` for it. The archive is not offered, as a tool answers text.
- **Mapping.**

  | Block or mark | Markdown |
  |---|---|
  | title; a handbook page's title | `#`; `#` by depth |
  | properties (key, type, state, fields, labels) | a list, `- **Key:** value` |
  | heading 1–3 | one level below its page's title |
  | bullet, ordered, task | `- `, `1. ` (numbered from 1 in each run), `- [ ] ` / `- [x] ` |
  | quote | `> ` |
  | panel | `> [!NOTE]`, `[!TIP]`, `[!IMPORTANT]`, `[!WARNING]`, `[!CAUTION]` for info, tip, note, warning, error |
  | code, unsupported | a fence longer than any run of backticks in it, the text unchanged |
  | table | a table with the first row as header; cells as text |
  | divider | `---` |
  | picture | `![caption](target)`, the caption again as an italic line below |
  | video | `▶ name`, a link where the target is reachable |
  | BPMN diagram, process | `BPMN: file`, a link where the target is reachable; `BPMN: process (engine)` |
  | embedded item | a quote: `↳ key title`, its properties and body; restricted ones as restricted (D10) |
  | table of contents, lists of figures and tables, child pages, task list | the list as computed at export |
  | bold, italic, strike, code, link | `**`, `*`, `~~`, backticks, `[text](address)` for allowed addresses only |
  | underline, highlight | `<ins>`, `<mark>` |

- **Text stays text.** Every character Markdown would read as syntax is escaped: `\ ` `` ` `` `* [ ] ~ |` everywhere, `_` beside a non-letter, `<` before a tag, `&` before an entity, and `# > - + =` and `1.` where a line starts. Whitespace starting a line is written as an entity. A heading's closing hashes are escaped. A line break is a backslash break; one at the end of a paragraph is left out.
- **Formatting that counts.** Formats nest; one still open stays open. A delimiter counts only where CommonMark's flanking rules let it: whitespace is kept outside, and so is punctuation that touches a letter outside. Where a delimiter would still not count beside the tokens around it, or would run into the same delimiter, that occurrence is written as `<strong>`, `<em>` or `<del>`. A link that would read as a link reference definition at a block's start is written as `<a href>`.
- **Dropped:** alignment, a picture's width and placement, a code block's language (the model has none), empty paragraphs, quotes and panels.
- **Tests.**
  - `internal/export` pins the Markdown of every block kind, the escaping, the nesting and flanking of marks, handbooks, embeds and the archive.
  - `internal/server` checks the routes, a veiled passage and that an outsider gets nothing.
  - `internal/mcp` checks the tool's request.
  - `e2e/smoke.cjs` downloads the archive from the menu, reads the text route and calls the tool.
  - Before this record, 20 000 random paragraphs and 4 000 random documents went through the export and back through a CommonMark converter (the Markdown paste proposed in parallel). The text and formats came back exactly, apart from the occurrences written as HTML.

### Consequences

- **Positive:** pages leave as Markdown with their pictures; agents read a page as a person does, with their own rights; no dependency; one gathering of content for every export.
- **Negative / trade-offs accepted:** some formatting is HTML (the open question), and some is lost. A numbered list's numbering restarts after a block between items. A caption shows twice where the importer reads the picture's description too. Renderers link bare web addresses in plain text.
- **Follow-ups / risks to watch:** the exact escaping rules depend on CommonMark's; a renderer that departs from it may show a stray backslash; a model change (list start, code language) would extend the mapping.

## Pros and cons of the options

### A Markdown writer in internal/export

- Good: reuses the document of ADR-0071, so D10, protected passages, embeds and pictures behave as in Word and PDF. The output is pinned by tests in Go. Text, archive and agents share one writer.
- Bad: Markdown's flanking rules are subtle; the writer must apply them itself.

### Markdown made in the browser

Strongest form: no new route, and the converter could share code and tests with a Markdown paste in the browser, so a round trip would be tested in one language.

- Good: no server change.
- Bad: the browser has the page, not a handbook's subpages or embedded items as a reader may read them; agents could not use it; content would be gathered a second way beside ADR-0071's.

### A library

Strongest form: an HTML-to-Markdown library handles the corner cases of escaping and nesting that took tests here.

- Good: less own code.
- Bad: a dependency for one format; the export's HTML is built for printing, so tasks, panels and embeds would come out as generic HTML; the library's escaping is not ours to fix.

### Only the text

Strongest form: one file, nothing to unpack.

- Good: simplest to download.
- Bad: `data:` addresses make files large, and many renderers refuse them. Links to the server show nothing without signing in. The maintainer chose the archive for people; the text with links remains for agents and the API.

## Links

- extends [ADR-0071](0071-exporting-pages-and-lists.md) (exports); offers a read over MCP as in [ADR-0012](0012-mcp-adapter-over-http-api.md)
- relates to [ADR-0014](0014-closed-renderer-for-user-content.md) (text never becomes markup), [ADR-0022](0022-permission-model.md) (D10), [ADR-0088](0088-protected-passages-of-a-body.md) (protected passages), [ADR-0097](0097-videos-in-the-text-the-instance-in-the-tab-done-beside-the-text.md) (videos), [ADR-0119](0119-lists-of-figures-and-tables.md) (lists of figures and tables)
- user guide: [Exporting pages as Markdown](../guides/markdown-export.md)
