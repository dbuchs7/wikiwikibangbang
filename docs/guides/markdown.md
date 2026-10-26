# Pasting Markdown

Markdown written or copied elsewhere — an assistant's answer, a README, a file in a code editor — goes into a page as formatted text that stays editable. Markdown is only a way in: the page is stored as its usual blocks, never as Markdown ([ADR-0129](../adr/0129-markdown-is-pasted-into-the-editor-and-converted-in-the-browser.md)).

## With Ctrl+V

Edit the page, place the cursor and press **Ctrl+V** (on a Mac **Cmd+V**). When the clipboard holds Markdown as plain text, it becomes headings, lists, tasks, quotes, code blocks, tables, links and emphasis where the cursor is:

- A selection is replaced; the text before and after it stays.
- One paragraph of Markdown goes into the text at the cursor. Several blocks split the paragraph at the cursor, or follow it when the cursor is at its end.
- **Ctrl+Z** takes the whole paste back in one step; **Ctrl+Y** brings it again.
- **Ctrl+Shift+V** (Cmd+Shift+V) pastes the text as it is, without converting it.

The paste is recognised conservatively, so ordinary text is not changed by accident. It is converted when it holds something only Markdown writes — a code fence with three backticks or tildes that is closed again, a table with its `|---|` row, a task item `- [ ]`, a link `[text](https://…)` — or two different typical constructs, such as a heading and a list, or bold and italic text. A dash list alone, a single `#` or a star stays text. Text that reads like program code or commands stays text too.

What the clipboard holds besides plain text decides as well:

| The clipboard holds | The paste |
|---|---|
| a picture | becomes a picture of the page, as before |
| formatted HTML (copied from a web page, a word processor or a mail) | keeps the formatting the source shows, as before |
| a copy from this editor | is pasted exactly as copied |
| plain text, or a code editor's coloured source, that looks like Markdown | is converted |
| anything else | is pasted as text |

In a code block and in a protected passage a paste stays text.

## Through the Insert menu

For Markdown the paste does not recognise, choose **Insert… → Paste Markdown…** in the toolbar. Paste or type the text into the field and choose **Insert** (or press Ctrl+Enter). The text is converted whatever the recognition would say and goes where the cursor was; the cursor returns to the text.

## What is converted

| Markdown | In the page |
|---|---|
| `# Title`, `## Title`, `### Title`; a line underlined with `===` or `---` | heading of level 1, 2 or 3 |
| `####` to `######` | heading of level 3 |
| `- item`, `* item`, `+ item` | bulleted item |
| `1. item`, `1) item` | numbered item |
| `- [ ] task`, `- [x] task` | task, open or done |
| `> text` | quote |
| `> [!NOTE]`, `> [!TIP]`, `> [!IMPORTANT]`, `> [!WARNING]`, `> [!CAUTION]` | info, tip, note, warning or error panel |
| a fence of three backticks or tildes, or text indented by four spaces | code block, its text exactly as written |
| `---`, `***`, `___` | divider |
| a table with a `\|---\|` row | table |
| `**bold**`, `__bold__` | bold |
| `*italic*`, `_italic_` | italic |
| `~~struck~~` | struck through |
| `` `code` `` | code |
| `[text](https://…)`, `[text][name]` with `[name]: https://…`, `<https://…>`, a bare `https://…` or `www.…` | link |
| `\*` and other backslash escapes, `&amp;` and other common entities | the character itself |

Formatting can be nested (`**bold with *italic* inside**`), and text in any script, emoji included, keeps its formatting in the right place.

## What is simplified

The page holds what its blocks can hold; the rest is simplified so that nothing disappears unnoticed:

- **Nested lists** become one flat list, items in their order. A paragraph or code block inside an item follows the item.
- **Numbered lists** count from 1 in every run of numbered items: a list's own start number is not kept, and a block between two items starts the count again.
- **Lists inside a quote** become quotes that begin with the item's marker (`• `, `1. `, `☐ `, `☑ `).
- **Table cells** hold plain text: bold and italic are removed, and a link keeps its address in parentheses, e.g. `Docs (https://example.org)`. Missing cells are filled; a row with more cells than the header widens the table. Column alignment is not kept.
- **A code block's language** (```` ```python ````) is not kept; the code is.
- **Pictures** (`![description](https://…)`) become their description, linked to the picture's address: pictures from other servers are not loaded. To show a picture, save it and add it with **Insert… → Image**.
- **Links** lead only to `http`, `https` and `mailto` addresses. A link to anything else — a relative path, an anchor `#…`, `javascript:` — keeps its text without the link.
- **HTML** (`<div>`, `<br>`, `<script>`, …), **footnotes** and anything else Markdown has and the page has not stay visible as text.

After a paste, the status line beside the toolbar says how many pictures became links and how many links kept only their text.

## Safety

Nothing pasted runs: HTML stays text, and a link's address must be an `http`, `https` or `mailto` address. The server checks every block it stores the same way, whatever the browser sends ([ADR-0014](../adr/0014-closed-renderer-for-user-content.md)). Converting happens in the browser; the Markdown is sent nowhere.

In a page that several people edit at once ([ADR-0010](../adr/0010-real-time-collaboration-with-block-locks.md)), a paste never changes a paragraph someone else is editing: pasting into it is refused with a note in the status line, and blocks pasted at its end go after it.
