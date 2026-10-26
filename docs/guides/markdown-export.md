# Exporting pages as Markdown

A page leaves the wiki as Markdown, the format of repositories, of editors such as VS Code and Obsidian, and of AI assistants ([ADR-0129](../adr/0129-pages-export-as-markdown-and-agents-read-them-so.md)). The export holds what the Word and PDF exports hold ([ADR-0071](../adr/0071-exporting-pages-and-lists.md)): only what you may read, protected passages you may not read veiled as `▒▒▒▒▒▒▒▒`.

## From a page

Open the page, choose **Export → Markdown with pictures (.zip)**. The archive holds:

- `<key> <title>.md`, the page as Markdown;
- `media/`, the pictures the page shows, linked from the text by relative paths, so the text shows them wherever the archive is unpacked.

On a page with subpages, **Markdown with subpages (handbook, .zip)** holds the page and every page below it you may read: a list of the pages first, then each page under a heading by its depth.

## Through the API and for agents

- `GET /api/items/{key}/export/md` answers the Markdown text alone; pictures link to their attachments on the server. Add `?children=true` for a handbook.
- `GET /api/items/{key}/export/md.zip` answers the archive.
- An AI assistant connected over MCP reads a page as Markdown with the tool `get_item_markdown`, with the rights of the person it acts for. To change a page, it still writes blocks with `commit_body`.

## What the Markdown holds

| In the page | In the Markdown |
|---|---|
| title | `# Title`; the page's headings one level below it |
| properties (key, type, state, fields, labels) | a list under the title, `- **Key:** PLAT-1` |
| bulleted, numbered and task items | `- item`, `1. item`, `- [ ] task`, `- [x] done` |
| quote | `> text` |
| info, tip, note, warning and error panels | `> [!NOTE]`, `> [!TIP]`, `> [!IMPORTANT]`, `> [!WARNING]`, `> [!CAUTION]` |
| code block | a fenced code block, the code unchanged |
| table | a table, the first row as header |
| divider | `---` |
| picture | `![caption](media/01-name.png)`, the caption below in italics |
| video | its name, `▶ clip.mp4` (in the archive the file is not included) |
| embedded item | a quote: `↳ KEY Title`, its properties and its text |
| table of contents, lists of figures and tables, child pages, task lists | the list as it is at the time of the export |
| bold, italic, struck through, code, link | `**bold**`, `*italic*`, `~~struck~~`, `` `code` ``, `[text](https://…)` |
| underlined, highlighted | `<ins>underlined</ins>`, `<mark>highlighted</mark>` |

Text that looks like Markdown stays text: a `*`, `#` or `<script>` you wrote is escaped, so no Markdown viewer shows it as formatting or runs it.

## What is lost or simplified

- Alignment, a picture's width and placement, and a code block's language: Markdown has no place for them.
- Numbered lists count from 1 in every run of numbered items.
- Where Markdown cannot nest two formats, or a format would not count beside the characters around it, the export writes that format as HTML (`<strong>`, `<em>`, `<del>`), which common viewers show.
- Empty paragraphs, quotes and panels are left out.
