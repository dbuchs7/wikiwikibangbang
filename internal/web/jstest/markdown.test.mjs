// Unit tests of the Markdown conversion (markdown.js) and of its insertion
// into the editor (editor.js). They run in Node.js without a browser and
// without dependencies:
//
//   node --test "internal/web/jstest/*.test.mjs"
//
// The browser flows (Ctrl+V, the dialog, live sessions) are in e2e/smoke.cjs.
import { test } from "node:test";
import assert from "node:assert/strict";
import { looksLikeMarkdown, pastesAsMarkdown, markdownToBlocks, markdownInline } from "../static/assets/markdown.js";
import { isSafeLink, schema, docFromBlocks, blocksFromDoc, markdownInsertion } from "../static/assets/editor.js";
import { EditorState, TextSelection, history, undo, redo } from "../static/vendor/prosemirror.js";

const cps = (s) => Array.from(s).length;

// span is the range of the n-th occurrence of sub in text, in code points
// as the model counts them.
function span(text, sub, n = 0) {
  let i = -1;
  for (let k = 0; k <= n; k++) i = text.indexOf(sub, i + 1);
  assert.ok(i >= 0, `${JSON.stringify(sub)} not in ${JSON.stringify(text)}`);
  const start = cps(text.slice(0, i));
  return [start, start + cps(sub)];
}

const mark = (type, text, sub, n = 0, href) => {
  const [start, end] = span(text, sub, n);
  return href ? { type, start, end, href } : { type, start, end };
};

// norm orders marks, so blocks compare whatever order they were made in.
const order = (a, b) => a.start - b.start || b.end - a.end || a.type.localeCompare(b.type);
const norm = (blocks) => blocks.map((b) => (b.marks ? { ...b, marks: [...b.marks].sort(order) } : b));

const md = (text) => norm(markdownToBlocks(text, isSafeLink).blocks);
const p = (text, marks) => (marks ? { kind: "paragraph", text, marks } : { kind: "paragraph", text });

// validBlock mirrors checkBlock in internal/item/store.go: what the server
// refuses would end a live session's batch.
function validBlock(b) {
  const textKinds = ["paragraph", "heading", "bullet", "ordered", "quote", "code", "task", "panel"];
  const kinds = [...textKinds, "table", "divider"];
  if (!kinds.includes(b.kind)) return `kind ${b.kind}`;
  const allowed = new Set(["kind", "text", "marks", "level", "checked", "tone"]);
  for (const k of Object.keys(b)) if (!allowed.has(k)) return `field ${k}`;
  if (b.kind === "divider" && (b.text || b.marks)) return "divider with text";
  if (b.kind === "table") {
    const rows = b.text.split("\n");
    const cols = rows[0].split("\t").length;
    if (!b.text || rows.some((r) => r.split("\t").length !== cols)) return "ragged table";
  }
  if ((b.level !== undefined) !== (b.kind === "heading") || (b.level !== undefined && (b.level < 1 || b.level > 3))) return "level";
  if (b.checked !== undefined && (b.kind !== "task" || b.checked !== true)) return "checked";
  if ((b.kind === "panel") !== ["info", "note", "warning", "tip", "error"].includes(b.tone)) return "tone";
  if (b.marks && ["code", "table", "divider"].includes(b.kind)) return "marks on " + b.kind;
  const n = cps(b.text || "");
  for (const m of b.marks || []) {
    if (!(m.start >= 0 && m.end > m.start && m.end <= n)) return `mark ${JSON.stringify(m)} outside ${n}`;
    if (!["bold", "italic", "code", "strike", "link"].includes(m.type)) return "mark " + m.type;
    if ((m.type === "link") !== (m.href !== undefined)) return "href";
    if (m.type === "link" && !isSafeLink(m.href)) return "unsafe " + m.href;
  }
  return null;
}

// Every input of these tests is also checked for valid blocks that the
// editor's schema keeps as they are.
const corpus = [];
const blocks = (text) => {
  corpus.push(text);
  return md(text);
};

test("1 a simple heading", () => {
  assert.deepEqual(blocks("# Title"), [{ kind: "heading", level: 1, text: "Title" }]);
  assert.deepEqual(blocks("# Title #"), [{ kind: "heading", level: 1, text: "Title" }]);
  assert.deepEqual(blocks("#Title"), [p("#Title")], "a hash without a space is text");
});

test("2 heading levels, capped at 3; setext headings", () => {
  assert.deepEqual(blocks("# A\n## B\n### C\n#### D\n##### E\n###### F").map((b) => [b.level, b.text]),
    [[1, "A"], [2, "B"], [3, "C"], [3, "D"], [3, "E"], [3, "F"]]);
  assert.deepEqual(blocks("####### G"), [p("####### G")]);
  assert.deepEqual(blocks("Big\n===\n\nSmall\n---"), [{ kind: "heading", level: 1, text: "Big" }, { kind: "heading", level: 2, text: "Small" }]);
});

test("3 paragraphs, blank lines and line breaks", () => {
  assert.deepEqual(blocks("First line\nsecond line\n\n\n\nNext paragraph"), [p("First line second line"), p("Next paragraph")]);
  assert.deepEqual(blocks("hard  \nbreak"), [p("hard\nbreak")]);
  assert.deepEqual(blocks("hard\\\nbreak"), [p("hard\nbreak")]);
  assert.deepEqual(blocks("   indented three"), [p("indented three")]);
  assert.deepEqual(blocks("Windows\r\nlines\r\n\r\nend"), [p("Windows lines"), p("end")]);
});

test("4 bulleted lists, nested ones flattened", () => {
  assert.deepEqual(blocks("- a\n* b\n+ c"), ["a", "b", "c"].map((text) => ({ kind: "bullet", text })));
  assert.deepEqual(blocks("- a\n  - b\n    - c\n- d").map((b) => [b.kind, b.text]), [["bullet", "a"], ["bullet", "b"], ["bullet", "c"], ["bullet", "d"]]);
  assert.deepEqual(blocks("1. a\n   - b\n2. c").map((b) => [b.kind, b.text]), [["ordered", "a"], ["bullet", "b"], ["ordered", "c"]]);
  assert.deepEqual(blocks("- item\n\n  more of the item\n- next").map((b) => [b.kind, b.text]),
    [["bullet", "item"], ["paragraph", "more of the item"], ["bullet", "next"]]);
  assert.deepEqual(blocks("- lazy\ncontinuation"), [{ kind: "bullet", text: "lazy continuation" }]);
});

test("5 numbered lists", () => {
  assert.deepEqual(blocks("1. a\n2. b\n10) c"), ["a", "b", "c"].map((text) => ({ kind: "ordered", text })));
  assert.deepEqual(blocks("The year:\n1986. A good year"), [p("The year: 1986. A good year")], "only 1. interrupts a paragraph");
  assert.deepEqual(blocks("Steps:\n1. one\n2. two").map((b) => b.kind), ["paragraph", "ordered", "ordered"]);
});

test("6 tasks, checked and unchecked", () => {
  assert.deepEqual(blocks("- [ ] open\n- [x] done\n- [X] also done\n* [ ] star\n1. [x] numbered\n- [ ]"), [
    { kind: "task", text: "open" }, { kind: "task", text: "done", checked: true }, { kind: "task", text: "also done", checked: true },
    { kind: "task", text: "star" }, { kind: "task", text: "numbered", checked: true }, { kind: "task", text: "" }]);
  assert.deepEqual(blocks("- [y] no task"), [{ kind: "bullet", text: "[y] no task" }]);
  assert.deepEqual(blocks("[ ] no list"), [p("[ ] no list")]);
  const t = "**Check** the backups";
  assert.deepEqual(blocks("- [x] " + t), [{ kind: "task", text: "Check the backups", checked: true, marks: [mark("bold", "Check the backups", "Check")] }]);
});

test("7 quotes, nested quotes and alerts", () => {
  assert.deepEqual(blocks("> a\n> b\n>\n> c"), [{ kind: "quote", text: "a b" }, { kind: "quote", text: "c" }]);
  assert.deepEqual(blocks("> lazy\ncontinuation"), [{ kind: "quote", text: "lazy continuation" }]);
  assert.deepEqual(blocks("> > deep"), [{ kind: "quote", text: "deep" }]);
  assert.deepEqual(blocks("> # Title\n> - item\n> 1. first\n> - [x] done"),
    [{ kind: "quote", text: "Title" }, { kind: "quote", text: "• item" }, { kind: "quote", text: "1. first" }, { kind: "quote", text: "☑ done" }]);
  assert.deepEqual(blocks("> ```\n> code\n> ```"), [{ kind: "code", text: "code" }]);
  assert.deepEqual(blocks("> [!WARNING]\n> Be careful.\n>\n> Really."), [{ kind: "panel", tone: "warning", text: "Be careful." }, { kind: "panel", tone: "warning", text: "Really." }]);
  assert.deepEqual(["NOTE", "TIP", "IMPORTANT", "CAUTION"].map((a) => blocks(`> [!${a}]\n> x`)[0].tone), ["info", "tip", "note", "error"]);
  assert.deepEqual(blocks("> [!NOTE]"), [{ kind: "quote", text: "[!NOTE]" }], "an alert without text stays a quote");
});

test("8 code blocks keep their text exactly", () => {
  const code = "line 1\n\n    indented **not bold**\n# not a heading\n- not a list\n<script>x</script>\ttab  ";
  assert.deepEqual(blocks("```\n" + code + "\n```"), [{ kind: "code", text: code }]);
  assert.deepEqual(blocks("~~~\na\n~~~"), [{ kind: "code", text: "a" }]);
  assert.deepEqual(blocks("````\n```\ninner\n```\n````"), [{ kind: "code", text: "```\ninner\n```" }]);
  assert.deepEqual(blocks("```\nnever closed\n\nstill code"), [{ kind: "code", text: "never closed\n\nstill code" }]);
  assert.deepEqual(blocks("    indented code\n      more\n\n    end"), [{ kind: "code", text: "indented code\n  more\n\nend" }]);
  assert.deepEqual(blocks("  ```\n  a\n    b\n  ```"), [{ kind: "code", text: "a\n  b" }]);
  assert.deepEqual(blocks("```\r\nwin\r\n```"), [{ kind: "code", text: "win" }]);
  assert.deepEqual(blocks("text\n    not code"), [p("text not code")], "indented code does not interrupt a paragraph");
  assert.deepEqual(blocks("```\n\n```"), [{ kind: "code", text: "" }]);
});

test("9 fenced code in several languages: the code stays, no field is added", () => {
  const py = 'def hello():\n    print("Hallo Welt")';
  assert.deepEqual(blocks("```python\n" + py + "\n```"), [{ kind: "code", text: py }]);
  assert.deepEqual(blocks("```bash\ndocker compose up -d\n```"), [{ kind: "code", text: "docker compose up -d" }]);
  assert.deepEqual(blocks("~~~ js {highlight}\nconst a = `x`;\n~~~"), [{ kind: "code", text: "const a = `x`;" }]);
});

test("10 bold, italic and strike-through", () => {
  const t = "b b i i s";
  assert.deepEqual(blocks("**b** __b__ *i* _i_ ~~s~~"), [p(t, [mark("bold", t, "b"), mark("bold", t, "b", 1), mark("italic", t, "i"), mark("italic", t, "i", 1), mark("strike", t, "s")])]);
  assert.deepEqual(blocks("snake_case_name and 2 * 3 * 4"), [p("snake_case_name and 2 * 3 * 4")]);
  assert.deepEqual(blocks("**unclosed and *open"), [p("**unclosed and *open")]);
});

test("11 nested and combined formatting", () => {
  const t = "Dieser Text ist fett, kursiv und enthält fett mit kursivem Inhalt.";
  assert.deepEqual(blocks("Dieser Text ist **fett**, *kursiv* und\nenthält **fett mit *kursivem* Inhalt**."), [p(t, [
    mark("bold", t, "fett"), mark("italic", t, "kursiv"), mark("bold", t, "fett mit kursivem Inhalt"), mark("italic", t, "kursivem")])]);
  assert.deepEqual(blocks("***both***"), [p("both", [mark("bold", "both", "both"), mark("italic", "both", "both")])]);
  assert.deepEqual(blocks("*a **b** c*"), [p("a b c", [mark("italic", "a b c", "a b c"), mark("bold", "a b c", "b")])]);
  const l = "see the bold guide";
  assert.deepEqual(blocks("see [the **bold** guide](https://example.com)"),
    [p(l, [mark("link", l, "the bold guide", 0, "https://example.com"), mark("bold", l, "bold")])]);
});

test("12 inline code", () => {
  const t = "Run npm *install* now";
  assert.deepEqual(blocks("Run `npm *install*` now"), [p(t, [mark("code", t, "npm *install*")])]);
  assert.deepEqual(blocks("``a ` b``"), [p("a ` b", [mark("code", "a ` b", "a ` b")])]);
  assert.deepEqual(blocks("` x `"), [p("x", [mark("code", "x", "x")])]);
  assert.deepEqual(blocks("`never closed"), [p("`never closed")]);
});

test("13 links with allowed addresses", () => {
  assert.deepEqual(blocks("[Dokumentation](https://example.com)"), [p("Dokumentation", [mark("link", "Dokumentation", "Dokumentation", 0, "https://example.com")])]);
  assert.deepEqual(blocks("<https://example.org/a>"), [p("https://example.org/a", [mark("link", "https://example.org/a", "https://example.org/a", 0, "https://example.org/a")])]);
  const bare = "See https://example.com/x. And www.example.ch!";
  assert.deepEqual(blocks("See https://example.com/x. And www.example.ch!"), [p(bare, [
    mark("link", bare, "https://example.com/x", 0, "https://example.com/x"), mark("link", bare, "www.example.ch", 0, "https://www.example.ch")])]);
  assert.deepEqual(blocks("[Mail](mailto:a@b.ch) <c@d.ch>"), [p("Mail c@d.ch", [mark("link", "Mail c@d.ch", "Mail", 0, "mailto:a@b.ch"), mark("link", "Mail c@d.ch", "c@d.ch", 0, "mailto:c@d.ch")])]);
  assert.deepEqual(blocks('[t](https://x.ch "A title")'), [p("t", [mark("link", "t", "t", 0, "https://x.ch")])]);
  assert.deepEqual(blocks("[t](<https://x.ch/a b>)"), [p("t", [mark("link", "t", "t", 0, "https://x.ch/a%20b")])]);
  assert.deepEqual(blocks("[](https://x.ch)"), [p("https://x.ch", [mark("link", "https://x.ch", "https://x.ch", 0, "https://x.ch")])], "an empty label shows the address");
  const ref = "Docs, d and D";
  assert.deepEqual(blocks("[Docs][d], [d][] and [D]\n\n[d]: https://example.com/docs \"Title\""), [p(ref, [
    mark("link", ref, "Docs", 0, "https://example.com/docs"), mark("link", ref, "d", 0, "https://example.com/docs"), mark("link", ref, "D", 1, "https://example.com/docs")])]);
  const badge = markdownToBlocks("[![CI](https://img.example/badge.svg)](https://ci.example/run)", isSafeLink);
  assert.deepEqual(norm(badge.blocks), [p("CI", [mark("link", "CI", "CI", 0, "https://ci.example/run")])]);
  assert.equal(badge.pictures, 1);
  corpus.push("[![CI](https://img.example/badge.svg)](https://ci.example/run)");
});

test("14 links with addresses that are not allowed keep their text only", () => {
  for (const href of ["javascript:alert(1)", "JaVaScRiPt:alert(1)", "data:text/html;base64,PHNjcmlwdD4=", "vbscript:msgbox", "docs/setup.md", "https://", "#anchor"]) {
    const r = markdownToBlocks(`[Click](${href})`, isSafeLink);
    assert.deepEqual(norm(r.blocks), [p("Click")], href);
    assert.equal(r.dropped, 1, href);
    corpus.push(`[Click](${href})`);
  }
  assert.deepEqual(blocks("<javascript:alert(1)>"), [p("javascript:alert(1)")]);
  const img = markdownToBlocks("![Architektur](javascript:alert(1))", isSafeLink);
  assert.deepEqual(norm(img.blocks), [p("Architektur")]);
  assert.deepEqual([img.pictures, img.dropped], [1, 1]);
});

test("15 tables with several columns", () => {
  assert.deepEqual(blocks("| Name | Status | Priorität |\n|---|---|---|\n| Atlas | Aktiv | Hoch |\n| Wiki | Planung | Mittel |"),
    [{ kind: "table", text: "Name\tStatus\tPriorität\nAtlas\tAktiv\tHoch\nWiki\tPlanung\tMittel" }]);
  assert.deepEqual(blocks("a | b\n:--- | ---:\n1 | 2\n\nafter"), [{ kind: "table", text: "a\tb\n1\t2" }, p("after")]);
  assert.deepEqual(blocks("Intro\n| a | b |\n|:-:|---|\n| 1 | 2 |"), [p("Intro"), { kind: "table", text: "a\tb\n1\t2" }]);
});

test("16 incomplete table rows are filled; nothing in a cell is lost", () => {
  assert.deepEqual(blocks("| A | B | C |\n|---|---|---|\n| 1 |\n| 1 | 2 | 3 | 4 |"), [{ kind: "table", text: "A\tB\tC\t\n1\t\t\t\n1\t2\t3\t4" }]);
  assert.deepEqual(blocks("| a \\| b | c |\n|---|---|\n| `x\\|y` | **z** |"), [{ kind: "table", text: "a | b\tc\nx|y\tz" }]);
  assert.deepEqual(blocks("| [Docs](https://x.ch) | [https://y.ch](https://y.ch) |\n|---|---|"), [{ kind: "table", text: "Docs (https://x.ch)\thttps://y.ch" }]);
  assert.deepEqual(blocks("| a | b |\n|---|\n| 1 | 2 |").map((b) => b.kind), ["paragraph"], "a delimiter row of another width is no table");
  assert.deepEqual(blocks("| tab\there |\n|---|"), [{ kind: "table", text: "tab here" }]);
});

test("17 Unicode and emoji: marks count code points", () => {
  const t = "😀 Grüsse und ü 𝔘 👩‍💻";
  assert.deepEqual(blocks("**😀 Grüsse** und _ü_ 𝔘 *👩‍💻*"), [p(t, [mark("bold", t, "😀 Grüsse"), mark("italic", t, "ü", 1), mark("italic", t, "👩‍💻")])]);
  assert.deepEqual(blocks("**😀 Grüsse** und _ü_ 𝔘 *👩‍💻*")[0].marks.map((m) => [m.start, m.end]), [[0, 8], [13, 14], [17, 20]]);
  assert.deepEqual(blocks("# Überschrift ÄÖÜ äöü"), [{ kind: "heading", level: 1, text: "Überschrift ÄÖÜ äöü" }]);
});

test("18 escaped Markdown characters and entities", () => {
  assert.deepEqual(blocks("\\*not italic\\* \\[x\\](y) \\`code\\` C:\\Users"), [p("*not italic* [x](y) `code` C:\\Users")]);
  assert.deepEqual(blocks("\\# Not a heading"), [p("# Not a heading")]);
  assert.deepEqual(blocks("1\\. Not a list"), [p("1. Not a list")]);
  assert.deepEqual(blocks("\\- not a bullet"), [p("- not a bullet")]);
  assert.deepEqual(blocks("&amp; &lt;b&gt; &#169; &#x1F600; &copy; &unknown;"), [p("& <b> © 😀 © &unknown;")]);
});

test("19 empty input", () => {
  assert.deepEqual(blocks(""), []);
  assert.deepEqual(blocks("  \n\n\t\n"), []);
  assert.equal(looksLikeMarkdown(""), false);
  assert.equal(looksLikeMarkdown("   \n "), false);
});

test("20 ordinary text is not taken for Markdown", () => {
  for (const text of [
    "Hello team,\n\nthe meeting is moved to 3 pm.\n\nRegards\nAnna",
    "Einkaufsliste:\n- Milch\n- Brot",
    "# install dependencies\napt-get update\napt-get install -y curl",
    "for (let i = 0; i < n; i++) {\n  handlers[kind](JSON.parse(data));\n}",
    "def f(**kwargs):\n    return g(**kwargs)",
    "5 * 3 * 2 = 30",
    "#hashtag #wiki",
    "Price: $5 - $10",
    "> On Monday you wrote:\n> please check\n\nDone.",
    "https://example.com",
    "Use the *-flag carefully",
    "Total: 10 * 20 = 200; see `man bc`",
    "# Kommentar\n- name: web\n  image: nginx;\n- name: db\n  image: postgres;",
  ]) assert.equal(looksLikeMarkdown(text), false, text);
});

test("20b typical Markdown is recognised", () => {
  for (const text of [
    "## Architektur\n\nDie **Anwendung** verwendet folgende Komponenten:\n\n- REST API\n- Datenbank\n- Workflow Engine",
    "Dies ist **fett** und *kursiv*",
    "[Doku](https://example.com)",
    "| a | b |\n|---|---|\n| 1 | 2 |",
    "```\ncode\n```",
    "- [ ] task",
    "## Title\n\n- a\n- b",
    "1. Repository klonen\n2. Starten\n\n> Hinweis: wichtig",
  ]) assert.equal(looksLikeMarkdown(text), true, text);
});

test("20c the clipboard: formatted HTML and the editor's own copies paste as they are", () => {
  const text = "## Architektur\n\n- **REST** API\n- Datenbank";
  assert.equal(pastesAsMarkdown(text, ""), true);
  assert.equal(pastesAsMarkdown(text, "<h2>Architektur</h2><ul><li><strong>REST</strong> API</li></ul>"), false, "formatted HTML wins");
  assert.equal(pastesAsMarkdown(text, "<meta charset='utf-8'><div style=\"color: #d4d4d4;\"><div><span style=\"color: #569cd6;font-weight: bold;\">## Architektur</span></div><br></div>"), true, "a code editor's coloured source is Markdown");
  assert.equal(pastesAsMarkdown(text, "<p data-pm-slice=\"1 1 []\">## Architektur</p>"), false, "a copy within the editor");
  assert.equal(pastesAsMarkdown("Hello team,\nsee you", ""), false);
  assert.equal(pastesAsMarkdown("", ""), false);
});

test("21 several kinds of blocks in one document", () => {
  const input = [
    "# Installationsanleitung", "", "## Voraussetzungen", "", "- Docker installieren", "- Go installieren", "- **Zugriffsrechte** prüfen", "",
    "### Konfiguration", "", "1. Repository klonen", "2. Konfigurationsdatei anpassen", "3. Anwendung starten", "",
    "> Hinweis: Die Konfiguration ist erforderlich.", "", "Weitere Informationen findest du in der", "[Dokumentation](https://example.com).", "",
    "```bash", "docker compose up -d", "```", "", "---", "", "- [x] erledigt", "", "| Name | Status |", "|---|---|", "| Atlas | Aktiv |",
  ].join("\n");
  const more = "Weitere Informationen findest du in der Dokumentation.";
  assert.deepEqual(blocks(input), [
    { kind: "heading", level: 1, text: "Installationsanleitung" },
    { kind: "heading", level: 2, text: "Voraussetzungen" },
    { kind: "bullet", text: "Docker installieren" },
    { kind: "bullet", text: "Go installieren" },
    { kind: "bullet", text: "Zugriffsrechte prüfen", marks: [mark("bold", "Zugriffsrechte prüfen", "Zugriffsrechte")] },
    { kind: "heading", level: 3, text: "Konfiguration" },
    { kind: "ordered", text: "Repository klonen" },
    { kind: "ordered", text: "Konfigurationsdatei anpassen" },
    { kind: "ordered", text: "Anwendung starten" },
    { kind: "quote", text: "Hinweis: Die Konfiguration ist erforderlich." },
    p(more, [mark("link", more, "Dokumentation", 0, "https://example.com")]),
    { kind: "code", text: "docker compose up -d" },
    { kind: "divider" },
    { kind: "task", text: "erledigt", checked: true },
    { kind: "table", text: "Name\tStatus\nAtlas\tAktiv" },
  ]);
  assert.equal(looksLikeMarkdown(input), true);
});

test("22 HTML and scripts stay text", () => {
  assert.deepEqual(blocks('<script>alert("XSS")</script>'), [p('<script>alert("XSS")</script>')]);
  assert.deepEqual(blocks('<img src=x onerror="alert(1)">'), [p('<img src=x onerror="alert(1)">')]);
  assert.deepEqual(blocks("a <b>bold</b> c"), [p("a <b>bold</b> c")]);
  assert.deepEqual(blocks("<div>\n<p>x</p>\n</div>"), [p("<div> <p>x</p> </div>")]);
  assert.deepEqual(blocks("[<script>](https://x.ch)"), [p("<script>", [mark("link", "<script>", "<script>", 0, "https://x.ch")])]);
  assert.deepEqual(blocks("Footnote[^1].\n\n[^1]: The note."), [p("Footnote[^1]."), p("[^1]: The note.")], "footnotes stay as text");
});

test("markdownInline converts one line", () => {
  const r = markdownInline("**a** [b](https://x.ch)", isSafeLink);
  assert.equal(r.text, "a b");
  assert.deepEqual([...r.marks].sort(order), [mark("bold", "a b", "a"), mark("link", "a b", "b", 0, "https://x.ch")]);
});

test("a hostile link check refuses everything without one", () => {
  assert.deepEqual(norm(markdownToBlocks("[a](https://x.ch)").blocks), [p("a")]);
});

test("every block is valid for the server and kept by the editor as it is", () => {
  assert.ok(corpus.length > 50);
  for (const text of corpus) {
    const out = md(text);
    for (const b of out) assert.equal(validBlock(b), null, `${JSON.stringify(text)} → ${JSON.stringify(b)}`);
    // An empty body holds one empty paragraph in the editor.
    if (out.length) assert.deepEqual(norm(blocksFromDoc(docFromBlocks(out))), out, text);
  }
});

// --- insertion into the editor -------------------------------------------------

const stateOf = (blocks) => EditorState.create({ doc: docFromBlocks(blocks), plugins: [history()] });
const select = (state, from, to = from) => state.apply(state.tr.setSelection(TextSelection.create(state.doc, from, to)));
const insert = (state, text, explicit) => {
  const r = markdownInsertion(state, text, explicit);
  assert.ok(r.tr, "inserted: " + r.reason);
  return state.apply(r.tr);
};
const kinds = (state) => blocksFromDoc(state.doc).map((b) => `${b.kind}:${b.text ?? ""}`);
const run = (cmd, state) => {
  let next = null;
  assert.ok(cmd(state, (tr) => { next = state.apply(tr); }));
  return next;
};
const list = "## Title\n\n- a\n- b";

test("inserted at the end of a paragraph, before the next one", () => {
  // doc: paragraph "First" at 0..7, paragraph "Second" at 7..15
  const s = select(stateOf([p("First"), p("Second")]), 6);
  assert.deepEqual(kinds(insert(s, list)), ["paragraph:First", "heading:Title", "bullet:a", "bullet:b", "paragraph:Second"]);
});

test("inserted at the start of a paragraph, before it", () => {
  const s = select(stateOf([p("First"), p("Second")]), 8);
  assert.deepEqual(kinds(insert(s, list)), ["paragraph:First", "heading:Title", "bullet:a", "bullet:b", "paragraph:Second"]);
});

test("inserted within a paragraph, which is split", () => {
  const s = select(stateOf([p("First"), p("Second")]), 3);
  assert.deepEqual(kinds(insert(s, list)), ["paragraph:Fi", "heading:Title", "bullet:a", "bullet:b", "paragraph:rst", "paragraph:Second"]);
});

test("an empty paragraph is replaced", () => {
  const s = select(stateOf([p("A"), p(""), p("B")]), 4);
  assert.deepEqual(kinds(insert(s, list)), ["paragraph:A", "heading:Title", "bullet:a", "bullet:b", "paragraph:B"]);
});

test("one paragraph of Markdown goes into the text at the cursor", () => {
  const s = insert(select(stateOf([p("AB")]), 2), "**x** [y](https://x.ch)");
  assert.deepEqual(norm(blocksFromDoc(s.doc)), [p("Ax yB", [mark("bold", "Ax yB", "x"), mark("link", "Ax yB", "y", 0, "https://x.ch")])]);
});

test("a selection is replaced; the text around it stays", () => {
  const s = insert(select(stateOf([p("Keep THIS keep"), p("Other")]), 6, 10), "**bold**");
  assert.deepEqual(norm(blocksFromDoc(s.doc)), [p("Keep bold keep", [mark("bold", "Keep bold keep", "bold")]), p("Other")]);
  const across = insert(select(stateOf([p("One"), p("Two"), p("Three")]), 2, 12), list);
  assert.deepEqual(kinds(across), ["paragraph:O", "heading:Title", "bullet:a", "bullet:b", "paragraph:hree"]);
});

test("undo takes the whole insertion back, redo brings it again", () => {
  const before = select(stateOf([p("First"), p("Second")]), 6);
  const after = insert(before, list);
  const undone = run(undo, after);
  assert.ok(undone.doc.eq(before.doc));
  const redone = run(redo, undone);
  assert.ok(redone.doc.eq(after.doc));
  assert.equal(redone.doc.child(1).type, schema.nodes.heading);
});

test("not converted in a code block when pasted; inserted there on request", () => {
  const s = select(stateOf([{ kind: "code", text: "xy" }]), 2);
  assert.equal(markdownInsertion(s, list).reason, "code");
  assert.deepEqual(kinds(insert(s, list, true)), ["code:x", "heading:Title", "bullet:a", "bullet:b", "code:y"]);
});

test("not inserted into a protected passage", () => {
  const passage = p("secret text", [{ type: "secret", start: 0, end: 11, ref: "s1" }]);
  assert.equal(markdownInsertion(select(stateOf([passage]), 4), list, true).reason, "protected");
  assert.equal(markdownInsertion(select(stateOf([p("x"), passage]), 1, 6), list, true).reason, "protected");
  assert.ok(markdownInsertion(select(stateOf([passage, p("after")]), 15), list, true).tr, "beside a passage is fine");
});

test("nothing to insert; what was simplified is counted", () => {
  assert.equal(markdownInsertion(stateOf([p("")]), " \n").reason, "empty");
  const r = markdownInsertion(stateOf([p("")]), "![a](https://x.ch/a.png) [b](javascript:x)");
  assert.deepEqual([r.pictures, r.dropped], [1, 1]);
});
