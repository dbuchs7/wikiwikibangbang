// Markdown as a way into a body (ADR-0126). Text pasted or typed as
// Markdown becomes blocks of the item model (ADR-0001, ADR-0032): the same
// blocks the editor reads with nodeFromBlock and the server validates.
// Markdown is an input format only; nothing is stored as Markdown.
//
// The conversion is closed, like the renderer (ADR-0014): every construct
// maps to a block kind or a mark the model has, or stays visible text. HTML
// is never interpreted; it stays text. A link keeps its target only when
// isSafe allows it (the editor passes isSafeLink); otherwise its text stays.
// The module does not touch the DOM, so it is tested in Node.js
// (internal/web/jstest).

// --- recognising Markdown ------------------------------------------------------

// Constructs that only Markdown writes: a code fence that is closed again
// (checked apart), a task item, a link or picture with a web address or a
// path, a table with its delimiter row (checked apart).
const taskRE = /^ {0,3}(?:[-*+]|\d{1,9}[.)])[ \t]+\[[ xX]\](?:[ \t]|$)/m;
const linkRE = /(?:!\[[^\]\n]{0,500}\]|\[[^\]\n]{1,500}\])\((?:https?:\/\/|mailto:|www\.|\.{0,2}\/|#)[^\s)]/;

// Constructs that are typical of Markdown but also occur in plain text;
// two different ones are needed.
const typical = {
  heading: /^ {0,3}#{1,6}[ \t]+\S/m,
  rule: /^ {0,3}(?:(?:\*[ \t]*){3,}|(?:-[ \t]*){3,}|(?:_[ \t]*){3,}|={3,}[ \t]*)$/m,
  bold: /(?:^|[\s\p{P}])(\*\*|__)(?=[^\s*_])[^\n]{0,300}?[^\s*_]\1(?=$|[\s\p{P}])/mu,
  italic: /(?:^|[\s(["'])([*_])(?=[^\s*_])[^*_\n]{0,300}?[^\s*_]\1(?=$|[\s.,;:!?)\]"'])/m,
  code: /`[^`\n]+`/,
  quote: /^ {0,3}>[ \t]?\S/m,
  strike: /~~(?=\S)[^\n~]{0,300}?\S~~/,
};
const itemLineRE = /^ {0,3}(?:[-*+]|\d{1,9}[.)])[ \t]+\S/;

// Recognition reads the start of a text: Markdown shows there, and the
// time it takes stays bounded however much is pasted.
const RECOGNISE = 65536;

// Lines of program code or commands: in text that has them, typical
// constructs are more likely comments and operators than Markdown.
const codeLineRE = /[;{}][ \t]*$|^\s*(?:import|export|from|def|class|function|const|let|var|return|public|private|protected|package|func|#include|#define|SELECT|INSERT|UPDATE|DELETE|CREATE)\b|^\s*(?:\$|>>>) |^\s*(?:sudo|apt|apt-get|yum|dnf|brew|npm|npx|yarn|pnpm|pip|pip3|go|cargo|docker|kubectl|git|cd|ls|cp|mv|rm|mkdir|chmod|chown|echo|curl|wget|make|python|python3|node)\s/;

// looksLikeMarkdown says whether text is Markdown, conservatively: it must
// hold a construct only Markdown writes, or two different typical ones and
// not read like program code. Ordinary text with a dash list, a "#" or a
// star stays text.
export function looksLikeMarkdown(text) {
  const src = normalise((text || "").slice(0, RECOGNISE));
  if (!src.trim()) return false;
  const lines = src.split("\n");
  if (taskRE.test(src) || linkRE.test(src) || closedFence(lines) || lines.some((_, i) => tableStart(lines, i))) return true;
  let kinds = Object.values(typical).filter((re) => re.test(src)).length;
  if (lines.filter((l) => itemLineRE.test(l)).length >= 2) kinds++;
  if (kinds < 2) return false;
  const filled = lines.filter((l) => !isBlank(l));
  const code = filled.filter((l) => codeLineRE.test(l)).length;
  return filled.length < 2 || code * 3 < filled.length;
}

function closedFence(lines) {
  let open = null;
  for (const line of lines) {
    const f = fenceOf(line);
    if (!f) continue;
    if (!open) open = f;
    else if (f.ch === open.ch && f.len >= open.len && !f.info.trim()) return true;
  }
  return false;
}

// HTML a paste carries that is formatted text: headings, lists, tables,
// emphasis, links, pictures. A code editor's coloured source (div, span,
// pre, code, br) is not.
const formattedHTML = /<(?:h[1-6]|ul|ol|li|table|blockquote|strong|b|em|i|u|s|del|strike|mark|img|hr)[\s>/]|<a\s[^>]*\bhref\s*=/i;

// pastesAsMarkdown decides a paste with the clipboard's plain text and
// HTML. The editor's own copies (they carry data-pm-slice) and formatted
// HTML paste as they are, as the source rendered them; otherwise plain
// text that looks like Markdown is converted.
export function pastesAsMarkdown(text, html = "") {
  if (!text || /data-pm-slice/.test(html) || formattedHTML.test(html)) return false;
  return looksLikeMarkdown(text);
}

// --- block structure -------------------------------------------------------------

const normalise = (s) => s.replace(/^﻿/, "").replace(/\r\n?/g, "\n").replace(/\0/g, "�");
const isBlank = (l) => /^[ \t]*$/.test(l);
const cps = (s) => Array.from(s).length;

// indentOf counts the columns of a line's indentation, a tab reaching the
// next multiple of four.
function indentOf(line) {
  let col = 0;
  for (const ch of line) {
    if (ch === " ") col++;
    else if (ch === "\t") col += 4 - (col % 4);
    else break;
  }
  return col;
}

// stripIndent removes up to n columns of indentation; what a tab holds
// beyond them stays as spaces.
function stripIndent(line, n) {
  let col = 0;
  let i = 0;
  while (i < line.length && col < n) {
    if (line[i] === " ") col++;
    else if (line[i] === "\t") {
      const w = 4 - (col % 4);
      if (col + w > n) return " ".repeat(col + w - n) + line.slice(i + 1);
      col += w;
    } else break;
    i++;
  }
  return line.slice(i);
}

const atxRE = /^ {0,3}(#{1,6})(?:[ \t]+(.*))?$/;
const hrRE = /^ {0,3}(?:(?:\*[ \t]*){3,}|(?:-[ \t]*){3,}|(?:_[ \t]*){3,})$/;
const setextRE = /^ {0,3}(=+|-+)[ \t]*$/;
const quoteRE = /^ {0,3}> ?(.*)$/;
const defRE = /^ {0,3}\[((?:[^\]\\]|\\.)+)\]:[ \t]*(<[^<>\n]*>|\S+)(?:[ \t]+(?:"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|\((?:[^()\\]|\\.)*\)))?[ \t]*$/;
const delimiterRowRE = /^ {0,3}\|?[ \t]*:?-+:?[ \t]*(?:\|[ \t]*:?-+:?[ \t]*)*\|?[ \t]*$/;
const alertRE = /^\[!(note|tip|important|warning|caution)\][ \t]*$/i;
const alertTones = { note: "info", tip: "tip", important: "note", warning: "warning", caution: "error" };

function fenceOf(line) {
  const m = /^( {0,3})(`{3,}|~{3,})(.*)$/.exec(line);
  if (!m || (m[2][0] === "`" && m[3].includes("`"))) return null;
  return { indent: m[1].length, ch: m[2][0], len: m[2].length, info: m[3] };
}

// listItem reads a list item's first line: its marker, where its content
// starts and that content.
function listItem(line) {
  const m = /^( {0,3})([-+*]|\d{1,9}[.)])([ \t]+|$)/.exec(line);
  if (!m) return null;
  const marker = m[2];
  const end = m[1].length + marker.length;
  let col = end;
  for (const ch of m[3]) col += ch === "\t" ? 4 - (col % 4) : 1;
  const rest = line.slice(m[0].length);
  const ordered = /\d/.test(marker[0]);
  const item = { ordered, delim: ordered ? marker.slice(-1) : marker, number: ordered ? parseInt(marker, 10) : 0, empty: rest === "" };
  // Five or more spaces after the marker start indented code in the item.
  if (item.empty) return { ...item, indent: end + 1, content: "" };
  if (col - end > 4) return { ...item, indent: end + 1, content: " ".repeat(col - end - 1) + rest };
  return { ...item, indent: col, content: rest };
}

// interrupts says whether a line ends a paragraph: only a numbered list
// that starts at 1, and no empty item, does.
function interrupts(line) {
  if (fenceOf(line) || atxRE.test(line) || hrRE.test(line) || quoteRE.test(line)) return true;
  const li = listItem(line);
  return Boolean(li && !li.empty && (!li.ordered || li.number === 1));
}

// paragraphText is a line a lazy continuation line may follow.
const paragraphText = (line) => !isBlank(line) && !fenceOf(line) && !atxRE.test(line) && !hrRE.test(line);

// cells splits a table row at pipes that are not escaped.
function cells(line) {
  let s = line.trim();
  if (s.startsWith("|")) s = s.slice(1);
  if (s.endsWith("|") && !s.endsWith("\\|")) s = s.slice(0, -1);
  const out = [];
  let cur = "";
  for (let i = 0; i < s.length; i++) {
    if (s[i] === "\\" && s[i + 1] === "|") {
      cur += "|";
      i++;
    } else if (s[i] === "|") {
      out.push(cur.trim());
      cur = "";
    } else cur += s[i];
  }
  out.push(cur.trim());
  return out;
}

// tableStart says whether a table starts at line i: a row with pipes, then
// a delimiter row with as many cells.
function tableStart(lines, i) {
  const head = lines[i];
  const delim = lines[i + 1];
  return delim !== undefined && head.includes("|") && delim.includes("|") && indentOf(head) < 4
    && delimiterRowRE.test(delim) && cells(head).length === cells(delim).length;
}

// A fence tracker knows whether the lines seen so far leave a code fence
// open, where no line continues lazily.
function fenceTracker() {
  let open = null;
  return {
    see(line) {
      const f = fenceOf(line);
      if (!f) return;
      if (!open) open = f;
      else if (f.ch === open.ch && f.len >= open.len && !f.info.trim()) open = null;
    },
    get open() { return open !== null; },
  };
}

const labelOf = (s) => s.trim().replace(/\s+/g, " ").toLowerCase();
const joinParagraph = (lines) => lines.map((l) => l.replace(/^[ \t]+/, "")).join("\n").replace(/[ \t]+$/, "");

// Containers deeper than this are read as text; nobody writes them, and
// recursion must stay bounded.
const MAX_DEPTH = 32;

// parseBlocks reads lines into Markdown's block structure: paragraphs,
// headings, code, rules, quotes, lists and tables, their text not yet read.
// Link reference definitions go to defs.
function parseBlocks(lines, defs, depth) {
  if (depth > MAX_DEPTH) return [{ t: "para", text: joinParagraph(lines.filter((l) => !isBlank(l))) }];
  const out = [];
  let para = null;
  const close = () => {
    if (para) out.push({ t: "para", text: joinParagraph(para) });
    para = null;
  };
  let i = 0;
  while (i < lines.length) {
    const line = lines[i];
    if (isBlank(line)) {
      close();
      i++;
      continue;
    }
    const setext = para && setextRE.exec(line);
    if (setext) {
      out.push({ t: "heading", level: setext[1][0] === "=" ? 1 : 2, text: joinParagraph(para) });
      para = null;
      i++;
      continue;
    }
    if (tableStart(lines, i)) {
      close();
      const rows = [cells(line)];
      i += 2;
      while (i < lines.length && !isBlank(lines[i]) && !interrupts(lines[i])) rows.push(cells(lines[i++]));
      out.push({ t: "table", rows });
      continue;
    }
    if (para && !interrupts(line)) {
      para.push(line);
      i++;
      continue;
    }
    close();
    if (indentOf(line) >= 4) {
      const code = [];
      while (i < lines.length && (isBlank(lines[i]) || indentOf(lines[i]) >= 4)) code.push(stripIndent(lines[i++], 4));
      while (code.length && isBlank(code[code.length - 1])) code.pop();
      out.push({ t: "code", text: code.join("\n") });
      continue;
    }
    const fence = fenceOf(line);
    if (fence) {
      const code = [];
      for (i++; i < lines.length; i++) {
        const end = fenceOf(lines[i]);
        if (end && end.ch === fence.ch && end.len >= fence.len && !end.info.trim()) {
          i++;
          break;
        }
        code.push(stripIndent(lines[i], fence.indent));
      }
      out.push({ t: "code", text: code.join("\n") });
      continue;
    }
    const atx = atxRE.exec(line);
    if (atx) {
      out.push({ t: "heading", level: atx[1].length, text: (atx[2] || "").replace(/(?:^|[ \t]+)#+[ \t]*$/, "").trim() });
      i++;
      continue;
    }
    if (hrRE.test(line)) {
      out.push({ t: "hr" });
      i++;
      continue;
    }
    if (quoteRE.test(line)) {
      const inner = [];
      const fences = fenceTracker();
      while (i < lines.length) {
        const q = quoteRE.exec(lines[i]);
        const last = inner[inner.length - 1];
        if (q) inner.push(q[1]);
        else if (!isBlank(lines[i]) && last !== undefined && paragraphText(last) && !fences.open && !interrupts(lines[i])) inner.push(lines[i]);
        else break;
        fences.see(inner[inner.length - 1]);
        i++;
      }
      out.push({ t: "quote", children: parseBlocks(inner, defs, depth + 1) });
      continue;
    }
    const first = listItem(line);
    if (first) {
      const list = { t: "list", ordered: first.ordered, items: [] };
      while (i < lines.length) {
        const li = listItem(lines[i]);
        if (!li || li.ordered !== first.ordered || li.delim !== first.delim) break;
        const content = [li.content];
        const fences = fenceTracker();
        fences.see(li.content);
        for (i++; i < lines.length; i++) {
          const l = lines[i];
          const last = content[content.length - 1];
          if (isBlank(l)) {
            if (li.empty && content.length === 1) break; // an item starts with one blank line at most
            content.push("");
          } else if (indentOf(l) >= li.indent) {
            content.push(stripIndent(l, li.indent));
            fences.see(content[content.length - 1]);
          } else if (paragraphText(last) && !fences.open && !listItem(l) && !interrupts(l) && !tableStart(lines, i)) {
            content.push(l);
          } else break;
        }
        while (content.length > 1 && isBlank(content[content.length - 1])) content.pop();
        list.items.push({ number: li.number, children: parseBlocks(content, defs, depth + 1) });
      }
      out.push(list);
      continue;
    }
    const def = defRE.exec(line);
    if (def && !def[1].startsWith("^")) {
      const label = labelOf(def[1]);
      if (label && !defs.has(label)) defs.set(label, unescape(def[2].replace(/^<(.*)>$/, "$1")));
      i++;
      continue;
    }
    para = [line];
    i++;
  }
  close();
  return out;
}

// --- blocks of the model -----------------------------------------------------------

// markdownToBlocks converts Markdown into blocks of the item model. It
// returns the blocks and what had to be simplified: pictures shown as links
// and link targets left out because isSafe refused them.
export function markdownToBlocks(text, isSafe = () => false) {
  const defs = new Map();
  const tree = parseBlocks(normalise(text || "").split("\n"), defs, 0);
  const ctx = { defs, isSafe, stats: { pictures: 0, dropped: 0 } };
  const blocks = [];
  emit(tree, blocks, null, ctx);
  return { blocks, ...ctx.stats };
}

// markdownInline converts one piece of inline Markdown into text and marks.
export function markdownInline(text, isSafe = () => false) {
  const stats = { pictures: 0, dropped: 0 };
  const { text: out, marks } = withMarks(parseInline(normalise(text || ""), new Map()), isSafe, stats);
  return { text: out, marks, ...stats };
}

// textBlock makes a block of text of the kind base names; prefix stands
// before the text (a list marker inside a quote).
function textBlock(base, raw, ctx, prefix = "") {
  const { text, marks } = withMarks(parseInline(raw, ctx.defs), ctx.isSafe, ctx.stats);
  const shift = cps(prefix);
  const b = { ...base, text: prefix + text };
  if (marks.length) b.marks = marks.map((m) => ({ ...m, start: m.start + shift, end: m.end + shift }));
  return b;
}

// emit maps Markdown's blocks to the model's. Lists are flat in the model
// (ADR-0032), so nested items follow their parent. Inside a quote, text
// becomes quotes (or panels for an alert, as GitHub writes them) and list
// items keep their marker as text.
function emit(nodes, out, quote, ctx) {
  for (const n of nodes) {
    if (n.t === "para") out.push(textBlock(quote || { kind: "paragraph" }, n.text, ctx));
    else if (n.t === "heading") out.push(textBlock(quote || { kind: "heading", level: Math.min(n.level, 3) }, n.text, ctx));
    else if (n.t === "code") out.push({ kind: "code", text: n.text });
    else if (n.t === "hr") out.push({ kind: "divider" });
    else if (n.t === "table") out.push(table(n.rows, ctx));
    else if (n.t === "quote") {
      const [head, ...rest] = n.children;
      const firstLine = head?.t === "para" ? head.text.split("\n")[0] : "";
      const alert = !quote && alertRE.exec(firstLine);
      const body = head?.t === "para" ? head.text.slice(firstLine.length + 1) : "";
      if (alert && (body.trim() || rest.length)) {
        const children = body.trim() ? [{ t: "para", text: body }, ...rest] : rest;
        emit(children, out, { kind: "panel", tone: alertTones[alert[1].toLowerCase()] }, ctx);
      } else emit(n.children, out, quote || { kind: "quote" }, ctx);
    } else if (n.t === "list") {
      n.items.forEach((item) => {
        const [head, ...rest] = item.children;
        let kind = n.ordered ? "ordered" : "bullet";
        let checked = false;
        if (!head || head.t === "para") {
          let text = head ? head.text : "";
          const task = /^\[([ xX])\](?:[ \t]+|$)/.exec(text);
          if (task) {
            kind = "task";
            checked = task[1] !== " ";
            text = text.slice(task[0].length);
          }
          if (quote) {
            const marker = kind === "task" ? (checked ? "☑ " : "☐ ") : kind === "ordered" ? `${item.number}. ` : "• ";
            out.push(textBlock(quote, text, ctx, marker));
          } else out.push(textBlock(checked ? { kind, checked } : { kind }, text, ctx));
          emit(rest, out, quote, ctx);
        } else emit(item.children, out, quote, ctx);
      });
    }
  }
}

// table makes a table block: cells hold plain text (ADR-0042), so
// formatting is dropped and a link keeps its address in parentheses. Rows
// are filled to the widest; a row wider than the header widens the table
// rather than lose cells.
function table(rows, ctx) {
  const text = rows.map((r) => r.map((c) => plain(parseInline(c, ctx.defs), ctx.isSafe, ctx.stats).replace(/[\t\n]/g, " ").trim()));
  const width = Math.max(...text.map((r) => r.length));
  return { kind: "table", text: text.map((r) => [...r, ...Array(width - r.length).fill("")].join("\t")).join("\n") };
}

// --- inline text -----------------------------------------------------------------

// A node of inline text: t is text, code, soft (a line break read as a
// space), br (a hard line break), em, strong, strike, link or img. Nodes
// form a tree with sibling links, so emphasis can wrap a run of them.
function node(t, v) {
  return { t, v, parent: null, first: null, last: null, prev: null, next: null };
}

function append(parent, child) {
  child.parent = parent;
  child.prev = parent.last;
  child.next = null;
  if (parent.last) parent.last.next = child;
  else parent.first = child;
  parent.last = child;
}

function unlink(n) {
  if (n.prev) n.prev.next = n.next;
  else if (n.parent) n.parent.first = n.next;
  if (n.next) n.next.prev = n.prev;
  else if (n.parent) n.parent.last = n.prev;
  n.parent = n.prev = n.next = null;
}

function insertAfter(ref, n) {
  n.parent = ref.parent;
  n.prev = ref;
  n.next = ref.next;
  if (ref.next) ref.next.prev = n;
  else if (ref.parent) ref.parent.last = n;
  ref.next = n;
}

const asciiPunct = /[!-/:-@[-`{-~]/;
const isSpace = (c) => c === "" || /\s/u.test(c);
const isPunct = (c) => /[\p{P}\p{S}]/u.test(c);

const entities = {
  amp: "&", lt: "<", gt: ">", quot: '"', apos: "'", nbsp: " ", shy: "­", copy: "©", reg: "®", trade: "™",
  hellip: "…", mdash: "—", ndash: "–", laquo: "«", raquo: "»", lsaquo: "‹", rsaquo: "›", lsquo: "‘", rsquo: "’",
  sbquo: "‚", ldquo: "“", rdquo: "”", bdquo: "„", bull: "•", middot: "·", deg: "°", euro: "€", pound: "£", yen: "¥",
  cent: "¢", sect: "§", para: "¶", plusmn: "±", times: "×", divide: "÷", micro: "µ", frac12: "½", frac14: "¼",
  frac34: "¾", larr: "←", rarr: "→", uarr: "↑", darr: "↓", harr: "↔", auml: "ä", ouml: "ö", uuml: "ü", Auml: "Ä",
  Ouml: "Ö", Uuml: "Ü", szlig: "ß", eacute: "é", egrave: "è", ecirc: "ê", agrave: "à", aacute: "á", acirc: "â",
  ccedil: "ç", iacute: "í", oacute: "ó", ograve: "ò", uacute: "ú", ugrave: "ù", ntilde: "ñ",
};
const entityRE = /&(?:#[xX]([0-9a-fA-F]{1,6})|#([0-9]{1,7})|([A-Za-z][A-Za-z0-9]{1,31}));/y;

// entityAt decodes an entity at i and returns it with its length, or null.
function entityAt(src, i) {
  entityRE.lastIndex = i;
  const m = entityRE.exec(src);
  if (!m) return null;
  if (m[3]) return Object.hasOwn(entities, m[3]) ? { text: entities[m[3]], length: m[0].length } : null;
  const code = parseInt(m[1] ?? m[2], m[1] ? 16 : 10);
  const valid = code > 0 && code <= 0x10ffff && !(code >= 0xd800 && code <= 0xdfff);
  return { text: valid ? String.fromCodePoint(code) : "�", length: m[0].length };
}

// unescape resolves backslash escapes and entities in a link target.
function unescape(s) {
  let out = "";
  for (let i = 0; i < s.length; i++) {
    if (s[i] === "\\" && asciiPunct.test(s[i + 1] || "")) out += s[++i];
    else if (s[i] === "&" && entityAt(s, i)) {
      const e = entityAt(s, i);
      out += e.text;
      i += e.length - 1;
    } else out += s[i];
  }
  return out;
}

const autolinkRE = /<([A-Za-z][A-Za-z0-9+.-]{1,31}:[^\s<>]*)>/y;
const emailRE = /<([a-zA-Z0-9.!#$%&'*+/=?^_`{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*)>/y;
const bareRE = /(?:https?:\/\/[^\s<]+|www\.[^\s<]+)/iy;
const angleRE = /<([^<>\n]*)>/y;
const refRE = /\[((?:[^\]\\[]|\\.)*)\]/y;

// bareLink reads a web address written without brackets at i (as GitHub
// links it), without the punctuation that ends a sentence after it.
function bareLink(src, i) {
  const before = i > 0 ? src[i - 1] : "";
  if (before && !/[\s*_~(]/.test(before)) return null;
  bareRE.lastIndex = i;
  const m = bareRE.exec(src);
  if (!m) return null;
  let url = m[0];
  for (;;) {
    const trimmed = url.replace(/[?!.,:*_~'"]+$/, "").replace(/&[A-Za-z0-9]+;$/, "");
    const open = trimmed.split("(").length;
    const shut = trimmed.split(")").length;
    const next = trimmed.endsWith(")") && shut > open ? trimmed.slice(0, -1) : trimmed;
    if (next === url) break;
    url = next;
  }
  const www = /^www\./i.test(url);
  if (www ? !/^www\.[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)+/i.test(url) : !/^https?:\/\/[A-Za-z0-9[]/i.test(url)) return null;
  return { url, href: www ? "https://" + url : url };
}

// parseInline reads inline Markdown into a tree of nodes, following
// CommonMark's rules for emphasis, code spans, links and escapes, with the
// strike-through and bare addresses of GitHub.
function parseInline(src, defs) {
  const root = node("root");
  let delims = null; // the top of the delimiter stack
  let brackets = null; // the top of the bracket stack
  let pos = 0;
  // Where a closing backtick run (by length) or title quote is known to be
  // missing from a position on; looking for it again would make some
  // inputs take quadratic time.
  const noTicks = new Map();
  const noTitle = new Map();

  const text = (s) => {
    const last = root.last;
    if (last && last.t === "text" && !last.delim) last.v += s;
    else append(root, node("text", s));
  };
  const removeDelim = (d) => {
    if (d.prev) d.prev.next = d.next;
    if (d.next) d.next.prev = d.prev;
    else delims = d.prev;
  };
  const charBefore = (i) => (i > 0 ? Array.from(src.slice(Math.max(0, i - 2), i)).pop() : "");
  const charAt = (i) => (i < src.length ? String.fromCodePoint(src.codePointAt(i)) : "");

  const pushDelim = (ch, n) => {
    const before = charBefore(pos);
    const after = charAt(pos + n);
    const left = !isSpace(after) && (!isPunct(after) || isSpace(before) || isPunct(before));
    const right = !isSpace(before) && (!isPunct(before) || isSpace(after) || isPunct(after));
    const canOpen = ch === "_" ? left && (!right || isPunct(before)) : left;
    const canClose = ch === "_" ? right && (!left || isPunct(after)) : right;
    const n0 = node("text", ch.repeat(n));
    n0.delim = true;
    append(root, n0);
    const d = { ch, n, orig: n, node: n0, canOpen, canClose, prev: delims, next: null };
    if (delims) delims.next = d;
    delims = d;
    pos += n;
  };

  // processEmphasis matches the delimiters above bottom into emphasis.
  const processEmphasis = (bottom) => {
    const floors = new Map();
    let closer = delims;
    while (closer && closer.prev !== bottom) closer = closer.prev;
    while (closer) {
      if (!closer.canClose) {
        closer = closer.next;
        continue;
      }
      const key = closer.ch + (closer.canOpen ? "1" : "0") + (closer.orig % 3);
      const floor = floors.has(key) ? floors.get(key) : bottom;
      let opener = closer.prev;
      let found = false;
      while (opener && opener !== bottom && opener !== floor) {
        const odd = (closer.canOpen || opener.canClose) && closer.orig % 3 !== 0 && (opener.orig + closer.orig) % 3 === 0;
        if (opener.ch === closer.ch && opener.canOpen && (closer.ch === "~" || !odd)) {
          found = true;
          break;
        }
        opener = opener.prev;
      }
      if (!found) {
        floors.set(key, closer.prev);
        const next = closer.next;
        if (!closer.canOpen) removeDelim(closer);
        closer = next;
        continue;
      }
      const use = closer.ch === "~" ? 2 : closer.n >= 2 && opener.n >= 2 ? 2 : 1;
      opener.n -= use;
      closer.n -= use;
      opener.node.v = opener.node.v.slice(use);
      closer.node.v = closer.node.v.slice(use);
      const el = node(closer.ch === "~" ? "strike" : use === 2 ? "strong" : "em");
      for (let n = opener.node.next; n && n !== closer.node;) {
        const next = n.next;
        unlink(n);
        append(el, n);
        n = next;
      }
      insertAfter(opener.node, el);
      for (let d = closer.prev; d && d !== opener;) {
        const prev = d.prev;
        removeDelim(d);
        d = prev;
      }
      if (opener.n === 0) {
        unlink(opener.node);
        removeDelim(opener);
      }
      if (closer.n === 0) {
        const next = closer.next;
        unlink(closer.node);
        removeDelim(closer);
        closer = next;
      }
    }
    while (delims && delims !== bottom) removeDelim(delims);
  };

  // linkTail reads "(destination "title")" at i; it returns the
  // destination and where the link ends, or null.
  const linkTail = (i) => {
    const ws = () => { while (i < src.length && /[ \t\n]/.test(src[i])) i++; };
    i++;
    ws();
    let dest = "";
    if (src[i] === "<") {
      angleRE.lastIndex = i;
      const m = angleRE.exec(src);
      if (!m) return null;
      dest = m[1];
      i += m[0].length;
    } else {
      const start = i;
      let depth = 0;
      while (i < src.length) {
        const c = src[i];
        if (c === "\\" && asciiPunct.test(src[i + 1] || "")) i += 2;
        else if (c === "(") {
          if (++depth > 32) return null; // as CommonMark's reference implementation
          i++;
        } else if (c === ")") {
          if (depth === 0) break;
          depth--;
          i++;
        } else if (/[\s\x00-\x1f]/.test(c)) break;
        else i++;
      }
      if (depth !== 0) return null;
      dest = src.slice(start, i);
    }
    const beforeTitle = i;
    ws();
    const q = src[i];
    if (i > beforeTitle && (q === '"' || q === "'" || q === "(")) {
      const shut = q === "(" ? ")" : q;
      const start = i;
      if (noTitle.has(shut) && noTitle.get(shut) <= start) return null;
      for (i++; i < src.length && src[i] !== shut; i++) if (src[i] === "\\") i++;
      if (i >= src.length) {
        noTitle.set(shut, start);
        return null;
      }
      i++;
      ws();
    }
    if (src[i] !== ")") return null;
    return { dest: unescape(dest), end: i + 1 };
  };

  const closeBracket = () => {
    const opener = brackets;
    const labelEnd = pos;
    pos++;
    if (!opener) return text("]");
    brackets = opener.prev;
    if (!opener.active) return text("]");
    let dest = null;
    if (src[pos] === "(") {
      const tail = linkTail(pos);
      if (tail) {
        dest = tail.dest;
        pos = tail.end;
      }
    }
    // A label has at most 999 characters (CommonMark).
    if (dest === null && defs.size && labelEnd - opener.at <= 999) {
      let label = src.slice(opener.at, labelEnd);
      let end = pos;
      refRE.lastIndex = pos;
      const ref = refRE.exec(src);
      if (ref) {
        end = pos + ref[0].length;
        if (ref[1].trim()) label = ref[1];
      }
      const found = defs.get(labelOf(label));
      if (found !== undefined && labelOf(label)) {
        dest = found;
        pos = end;
      }
    }
    if (dest === null) return text("]");
    const link = node(opener.image ? "img" : "link");
    link.href = dest.replace(/ /g, "%20");
    for (let n = opener.node.next; n;) {
      const next = n.next;
      unlink(n);
      append(link, n);
      n = next;
    }
    insertAfter(opener.node, link);
    unlink(opener.node);
    processEmphasis(opener.delims);
    // A link holds no other link: brackets opened before it no longer start one.
    if (!opener.image) for (let b = brackets; b; b = b.prev) if (!b.image) b.active = false;
  };

  while (pos < src.length) {
    const c = src[pos];
    if (c === "\n") {
      const last = root.last;
      let hard = false;
      if (last && last.t === "text" && !last.delim) {
        hard = / {2,}$/.test(last.v);
        last.v = last.v.replace(/[ \t]+$/, "");
      }
      append(root, node(hard ? "br" : "soft"));
      pos++;
      while (src[pos] === " " || src[pos] === "\t") pos++;
    } else if (c === "\\") {
      const next = src[pos + 1] || "";
      if (next === "\n") {
        append(root, node("br"));
        pos += 2;
        while (src[pos] === " " || src[pos] === "\t") pos++;
      } else if (asciiPunct.test(next)) {
        text(next);
        pos += 2;
      } else {
        text("\\");
        pos++;
      }
    } else if (c === "`") {
      const run = /`+/y;
      run.lastIndex = pos;
      const n = run.exec(src)[0].length;
      const search = /`+/g;
      search.lastIndex = pos + n;
      let m = null;
      if (!(noTicks.get(n) <= pos)) {
        while ((m = search.exec(src)) && m[0].length !== n);
        if (!m) noTicks.set(n, pos);
      }
      if (m) {
        let code = src.slice(pos + n, m.index).replace(/\n/g, " ");
        if (/^ [\s\S]* $/.test(code) && !/^ +$/.test(code)) code = code.slice(1, -1);
        append(root, node("code", code));
        pos = m.index + n;
      } else {
        text("`".repeat(n));
        pos += n;
      }
    } else if (c === "*" || c === "_") {
      let n = 1;
      while (src[pos + n] === c) n++;
      pushDelim(c, n);
    } else if (c === "~") {
      let n = 1;
      while (src[pos + n] === "~") n++;
      if (n === 2) pushDelim("~", 2);
      else {
        text("~".repeat(n));
        pos += n;
      }
    } else if (c === "[" || (c === "!" && src[pos + 1] === "[")) {
      const image = c === "!";
      const n0 = node("text", image ? "![" : "[");
      n0.delim = true;
      append(root, n0);
      pos += image ? 2 : 1;
      brackets = { node: n0, image, at: pos, active: true, delims, prev: brackets };
    } else if (c === "]") {
      closeBracket();
    } else if (c === "<") {
      autolinkRE.lastIndex = pos;
      emailRE.lastIndex = pos;
      const m = autolinkRE.exec(src);
      const e = m ? null : emailRE.exec(src);
      if (m || e) {
        const link = node("link");
        link.href = m ? m[1] : "mailto:" + e[1];
        append(link, node("text", m ? m[1] : e[1]));
        append(root, link);
        pos += (m || e)[0].length;
      } else {
        text("<");
        pos++;
      }
    } else if (c === "&" && entityAt(src, pos)) {
      const e = entityAt(src, pos);
      text(e.text);
      pos += e.length;
    } else if (/[hHwW]/.test(c) && bareLink(src, pos)) {
      const b = bareLink(src, pos);
      const link = node("link");
      link.href = b.href;
      append(link, node("text", b.url));
      append(root, link);
      pos += b.url.length;
    } else {
      text(c);
      pos++;
    }
  }
  processEmphasis(null);
  return root;
}

const markOf = { em: "italic", strong: "bold", strike: "strike" };

// plainOf is the text of a node without any formatting.
function plainOf(n) {
  if (n.t === "text" || n.t === "code") return n.v;
  if (n.t === "soft" || n.t === "br") return " ";
  let s = "";
  for (let c = n.first; c; c = c.next) s += plainOf(c);
  return s;
}

// withMarks flattens a tree into text and marks, counted in code points as
// the model counts them. A picture becomes its description linked to its
// address, as pictures from elsewhere are not loaded; a link inside a link
// keeps only the outer one.
function withMarks(root, isSafe, stats) {
  let text = "";
  let len = 0;
  const marks = [];
  const add = (s) => {
    text += s;
    len += cps(s);
  };
  const walk = (parent, inLink) => {
    for (let n = parent.first; n; n = n.next) {
      const start = len;
      if (n.t === "text") add(n.v);
      else if (n.t === "soft") add(" ");
      else if (n.t === "br") add("\n");
      else if (n.t === "code") {
        add(n.v);
        if (len > start) marks.push({ type: "code", start, end: len });
      } else if (markOf[n.t]) {
        walk(n, inLink);
        if (len > start) marks.push({ type: markOf[n.t], start, end: len });
      } else if (n.t === "link" || n.t === "img") {
        const safe = isSafe(n.href);
        if (n.t === "img") stats.pictures++;
        if (!safe && n.href) stats.dropped++;
        if (n.t === "img") add(plainOf(n));
        else walk(n, true);
        if (len === start && safe) add(n.href);
        if (safe && !inLink && len > start) marks.push({ type: "link", start, end: len, href: n.href });
      }
    }
  };
  walk(root, false);
  return { text, marks: mergeMarks(marks) };
}

// plain flattens a tree into text alone, for a table cell: a link keeps its
// address in parentheses unless its text is the address.
function plain(root, isSafe, stats) {
  const walk = (parent, inLink) => {
    let s = "";
    for (let n = parent.first; n; n = n.next) {
      if (n.t === "link" || n.t === "img") {
        const safe = isSafe(n.href);
        if (n.t === "img") stats.pictures++;
        if (!safe && n.href) stats.dropped++;
        const label = n.t === "img" ? plainOf(n) : walk(n, true);
        s += safe && !inLink && label !== n.href ? (label ? `${label} (${n.href})` : n.href) : label;
      } else if (n.first) s += walk(n, inLink);
      else s += plainOf(n);
    }
    return s;
  };
  return walk(root, false);
}

// mergeMarks joins marks of one kind (and target) that overlap or touch,
// as the editor holds them, and orders them by where they start.
function mergeMarks(marks) {
  const out = [];
  const sorted = [...marks].sort((a, b) => a.type.localeCompare(b.type) || (a.href || "").localeCompare(b.href || "") || a.start - b.start);
  for (const m of sorted) {
    const last = out[out.length - 1];
    if (last && last.type === m.type && last.href === m.href && m.start <= last.end) last.end = Math.max(last.end, m.end);
    else out.push({ ...m });
  }
  return out.sort((a, b) => a.start - b.start || b.end - a.end);
}
