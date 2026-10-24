// The rich-text editor (ADR-0032): ProseMirror with a schema that is exactly
// the item model's block document — paragraphs, headings (levels 1 to 3),
// bullet and ordered list items, quotes, code blocks and embeds; tasks,
// panels, tables, tables of contents and lists of child pages (ADR-0042);
// bold, italic, code, link and mention marks. Nothing outside the schema
// survives paste or input, so the editor cannot produce markup the model
// refuses.
import {
  Schema, EditorState, EditorView, Plugin, keymap, baseKeymap, toggleMark, setBlockType,
  splitBlockAs, chainCommands, history, undo, redo, inputRules, textblockTypeInputRule,
} from "../vendor/prosemirror.js";
import { h, safeHref } from "./dom.js";
import { t } from "./i18n.js";
import { anchorOf } from "./anchors.js";
import { openDrawing, drawingName } from "./drawing.js";
import { pickFromAlbum, pictureFile, captionOf, copiedPicture, clipMark } from "./album.js";
import { assist } from "./assist.js";

// Every block carries bid, its ID in a live session (ADR-0010); it is not
// stored. Text blocks keep the mentions they had without a mention mark
// (imported bodies), so saving does not drop their relations.
const bid = { bid: { default: null } };
const textAttrs = { ...bid, mentions: { default: [] } };
// Paragraphs, headings, quotes and panels can be aligned (ADR-0059). The
// alignment is a class, as the content security policy refuses style
// attributes; pasted text keeps an alignment it states.
const alignAttrs = { ...textAttrs, align: { default: "" } };
const alignClass = (n) => (n.attrs.align ? "pm-align-" + n.attrs.align : null);
const pastedAlign = (dom) => {
  const a = dom.style?.textAlign || dom.getAttribute("align") || "";
  return ["center", "right", "justify"].includes(a) ? a : "";
};

export const schema = new Schema({
  nodes: {
    doc: { content: "block+" },
    paragraph: { group: "block", content: "inline*", attrs: alignAttrs, parseDOM: [{ tag: "p", getAttrs: (dom) => ({ align: pastedAlign(dom) }) }],
      toDOM: (n) => ["p", { class: alignClass(n) }, 0] },
    heading: {
      group: "block", content: "inline*", defining: true,
      attrs: { ...alignAttrs, level: { default: 1 } },
      parseDOM: [1, 2, 3, 4, 5, 6].map((l) => ({ tag: "h" + l, getAttrs: (dom) => ({ level: Math.min(l, 3), align: pastedAlign(dom) }) })),
      toDOM: (n) => ["h" + (n.attrs.level + 1), { class: alignClass(n) }, 0], // h1 is the item's title
    },
    bullet: { group: "block", content: "inline*", attrs: textAttrs, parseDOM: [{ tag: "ul > li" }], toDOM: () => ["div", { class: "pm-bullet" }, 0] },
    ordered: { group: "block", content: "inline*", attrs: textAttrs, parseDOM: [{ tag: "ol > li" }], toDOM: () => ["div", { class: "pm-ordered" }, 0] },
    quote: { group: "block", content: "inline*", attrs: alignAttrs, parseDOM: [{ tag: "blockquote" }], toDOM: (n) => ["blockquote", { class: alignClass(n) }, 0] },
    code_block: { group: "block", content: "text*", marks: "", code: true, defining: true, attrs: bid, parseDOM: [{ tag: "pre", preserveWhitespace: "full" }], toDOM: () => ["pre", ["code", 0]] },
    // ADR-0042. A task's checkbox and a table's cells are drawn by node
    // views (createEditor); toDOM is what a copy carries.
    task: { group: "block", content: "inline*", attrs: { ...textAttrs, checked: { default: false } }, toDOM: (n) => ["div", { class: "pm-task", "data-checked": String(n.attrs.checked) }, 0] },
    panel: { group: "block", content: "inline*", attrs: { ...alignAttrs, tone: { default: "info" } }, toDOM: (n) => ["div", { class: ["pm-panel", alignClass(n)].filter(Boolean).join(" "), "data-tone": n.attrs.tone }, 0] },
    // A horizontal rule between blocks (ADR-0059).
    divider: { group: "block", atom: true, selectable: true, attrs: bid, parseDOM: [{ tag: "hr" }], toDOM: () => ["hr", { class: "pm-divider" }] },
    table: { group: "block", atom: true, selectable: true, attrs: { ...bid, text: { default: "\t\n\t" } }, toDOM: (n) => ["pre", { class: "pm-table" }, n.attrs.text] },
    toc: { group: "block", atom: true, selectable: true, attrs: bid, toDOM: () => ["div", { class: "pm-toc" }] },
    children: { group: "block", atom: true, selectable: true, attrs: bid, toDOM: () => ["div", { class: "pm-children" }] },
    // The tasks that carry a label, computed when shown (ADR-0114).
    tasklist: { group: "block", atom: true, selectable: true, attrs: { ...bid, label: { default: "" } }, toDOM: (n) => ["div", { class: "pm-tasklist", "data-label": n.attrs.label }] },
    // An image attachment of the item (ADR-0045), with a plain caption.
    // width is percent of the column, 0 for the picture's own size (ADR-0056).
    // align is left or right (the text flows beside it) or center (ADR-0059).
    image: { group: "block", atom: true, selectable: true, attrs: { ...bid, ref: {}, text: { default: "" }, width: { default: 0 }, align: { default: "" } }, toDOM: (n) => ["figure", { class: "pm-image" }, ["figcaption", n.attrs.text]] },
    // A video plays in the text, sized and placed like a picture (ADR-0097).
    video: { group: "block", atom: true, selectable: true, attrs: { ...bid, ref: {}, text: { default: "" }, width: { default: 0 }, align: { default: "" } }, toDOM: (n) => ["figure", { class: "pm-image pm-video" }, ["figcaption", n.attrs.text]] },
    // Content an importer could not convert (ADR-0023), kept as it came and
    // shown as text; it cannot be created in the editor.
    unsupported: { group: "block", content: "text*", marks: "", code: true, atom: true, attrs: bid, toDOM: () => ["pre", { class: "pm-unsupported", title: t("editor.unsupported") }, 0] },
    embed: {
      group: "block", atom: true, selectable: true,
      attrs: { ...bid, ref: {}, label: { default: "" }, href: { default: "" } },
      toDOM: (n) => ["div", { class: "pm-embed", contenteditable: "false" },
        n.attrs.href ? ["a", { href: safeHref(n.attrs.href) }, n.attrs.label] : ["span", { class: "muted" }, n.attrs.label || t("editor.hidden")]],
    },
    mention: {
      group: "inline", inline: true, atom: true, selectable: false,
      attrs: { ref: {}, label: {} },
      toDOM: (n) => ["a", { class: "pm-mention", href: "#/item/" + encodeURIComponent(n.attrs.ref) }, n.attrs.label],
    },
    text: { group: "inline" },
  },
  marks: {
    bold: { parseDOM: [{ tag: "strong" }, { tag: "b" }], toDOM: () => ["strong", 0] },
    italic: { parseDOM: [{ tag: "em" }, { tag: "i" }], toDOM: () => ["em", 0] },
    code: { parseDOM: [{ tag: "code" }], toDOM: () => ["code", 0] },
    // ADR-0059.
    underline: { parseDOM: [{ tag: "u" }, { style: "text-decoration=underline" }], toDOM: () => ["u", 0] },
    strike: { parseDOM: [{ tag: "s" }, { tag: "del" }, { tag: "strike" }, { style: "text-decoration=line-through" }], toDOM: () => ["s", 0] },
    highlight: { parseDOM: [{ tag: "mark" }], toDOM: () => ["mark", 0] },
    link: {
      attrs: { href: {} }, inclusive: false,
      // Pasted links keep only targets the model allows (ADR-0014).
      parseDOM: [{ tag: "a[href]", getAttrs: (dom) => (isSafeLink(dom.getAttribute("href")) ? { href: dom.getAttribute("href") } : false) }],
      toDOM: (m) => ["a", { href: safeHref(m.attrs.href), rel: "noopener noreferrer nofollow", target: "_blank" }, 0],
    },
    // A protected passage (ADR-0088): ref names its protection. It is made
    // and taken off from the item's page, not typed; text typed at its end
    // stays outside it.
    secret: {
      attrs: { ref: {} }, inclusive: false, excludes: "",
      parseDOM: [{ tag: "span[data-secret]", getAttrs: (dom) => ({ ref: dom.getAttribute("data-secret") }) }],
      toDOM: (m) => ["span", { class: "pm-secret " + secretClass(m.attrs.ref), "data-secret": m.attrs.ref, tabindex: "0", role: "button" }, 0],
    },
  },
});

// secretClass says how a protected passage shows: readable or veiled for
// the reader (ADR-0088). The item's page sets it before drawing its body.
let secretClass = () => "";
export function setSecretClass(fn) { secretClass = fn || (() => ""); }

// selectionInBlock returns the selected text as a block of the body and
// characters of its text, as the model counts them, or null when the
// selection is empty or leaves one block of text.
export function selectionInBlock(state) {
  const { $from, $to, empty } = state.selection;
  if (empty || $from.depth !== 1 || $to.depth !== 1 || $from.index(0) !== $to.index(0) || !$from.parent.isTextblock) return null;
  const block = $from.parent;
  const count = (to) => cp(block.textBetween(0, to, "", (leaf) => leaf.attrs.label || ""));
  return { block: $from.index(0), start: count($from.parentOffset), end: count($to.parentOffset) };
}

export function isSafeLink(href) {
  try {
    const u = new URL(href);
    return (u.protocol === "http:" || u.protocol === "https:") ? u.host !== "" : u.protocol === "mailto:" && u.pathname !== "";
  } catch {
    return false;
  }
}

// Model block kinds and editor node names differ only for code, because a
// ProseMirror node and mark cannot share a name.
const nodeOf = (kind) => schema.nodes[kind === "code" ? "code_block" : kind] || schema.nodes.paragraph;
const kindOf = (node) => (node.type.name === "code_block" ? "code" : node.type.name);

// Offsets in the model count Unicode code points; JavaScript strings count
// UTF-16 units.
const cp = (s) => Array.from(s).length;

// --- model blocks -> editor document ----------------------------------------

export function docFromBlocks(blocks, embeds = []) {
  const nodes = blocks.map((b) => nodeFromBlock(b, embeds));
  if (nodes.length === 0) nodes.push(schema.nodes.paragraph.create());
  return schema.nodes.doc.create(null, nodes);
}

// nodeFromBlock makes the editor node of one model block; b.id becomes
// its session ID.
export function nodeFromBlock(b, embeds = []) {
  const id = b.id ?? null;
  if (b.kind === "table") return schema.nodes.table.create({ bid: id, text: b.text || "" });
  if (b.kind === "toc" || b.kind === "children" || b.kind === "divider") return schema.nodes[b.kind].create({ bid: id });
  if (b.kind === "tasklist") return schema.nodes.tasklist.create({ bid: id, label: b.text || "" });
  if (b.kind === "image" || b.kind === "video") return schema.nodes[b.kind].create({ bid: id, ref: b.ref, text: b.text || "", width: b.width || 0, align: b.align || "" });
  if (b.kind === "embed") {
    const v = embeds.find((e) => e.ref === b.ref);
    return schema.nodes.embed.create({
      bid: id,
      ref: b.ref,
      label: v && !v.restricted ? `${v.key} ${v.title}` : "",
      href: v && !v.restricted ? "#/item/" + encodeURIComponent(v.key) : "",
    });
  }
  const type = nodeOf(b.kind);
  const plain = type === schema.nodes.code_block || type === schema.nodes.unsupported;
  const attrs = plain ? { bid: id } : { bid: id, mentions: b.mentions || [] };
  if (type === schema.nodes.heading) attrs.level = b.level || 1;
  if (type === schema.nodes.task) attrs.checked = Boolean(b.checked);
  if (type === schema.nodes.panel) attrs.tone = b.tone || "info";
  if (type.spec.attrs?.align) attrs.align = b.align || "";
  return type.create(attrs, inlineContent(b.text || "", plain ? [] : b.marks || []));
}

function inlineContent(text, marks) {
  const chars = Array.from(text);
  const cuts = new Set([0, chars.length]);
  for (const m of marks) cuts.add(m.start).add(m.end);
  const points = [...cuts].sort((a, b) => a - b);
  const out = [];
  let i = 0;
  while (i < chars.length) {
    const mention = marks.find((m) => m.type === "mention" && m.start === i);
    if (mention) {
      out.push(schema.nodes.mention.create({ ref: mention.ref, label: chars.slice(mention.start, mention.end).join("") }));
      i = mention.end;
      continue;
    }
    const next = points.find((p) => p > i) ?? chars.length;
    const pm = marks
      .filter((m) => m.type !== "mention" && m.start <= i && m.end >= next)
      .filter((m) => schema.marks[m.type])
      .map((m) => (m.type === "link" ? schema.marks.link.create({ href: m.href })
        : m.type === "secret" ? schema.marks.secret.create({ ref: m.ref }) : schema.marks[m.type].create()));
    out.push(schema.text(chars.slice(i, next).join(""), pm));
    i = next;
  }
  return out;
}

// --- editor document -> model blocks ----------------------------------------

export function blocksFromDoc(doc) {
  const blocks = [];
  doc.forEach((node) => blocks.push(blockFromNode(node)));
  return blocks;
}

// blockFromNode is the model block of one editor node.
export function blockFromNode(node) {
  if (node.type === schema.nodes.embed) return { kind: "embed", ref: node.attrs.ref };
  if (node.type === schema.nodes.table) return { kind: "table", text: node.attrs.text };
  if (node.type === schema.nodes.toc || node.type === schema.nodes.children || node.type === schema.nodes.divider) return { kind: node.type.name };
  if (node.type === schema.nodes.tasklist) return { kind: "tasklist", text: node.attrs.label };
  if (node.type === schema.nodes.image || node.type === schema.nodes.video) {
    const b = { kind: node.type.name, text: node.attrs.text, ref: node.attrs.ref };
    if (node.attrs.width) b.width = node.attrs.width;
    if (node.attrs.align) b.align = node.attrs.align;
    return b;
  }
  let text = "";
  const marks = [];
  const mentions = new Set((node.attrs && node.attrs.mentions) || []);
  node.forEach((child) => {
    const start = cp(text);
    if (child.type === schema.nodes.mention) {
      text += child.attrs.label;
      marks.push({ type: "mention", start, end: cp(text), ref: child.attrs.ref });
      mentions.add(child.attrs.ref);
      return;
    }
    text += child.text;
    for (const m of child.marks) {
      const mark = { type: m.type.name, start, end: cp(text) };
      if (m.type.name === "link") mark.href = m.attrs.href;
      if (m.type.name === "secret") mark.ref = m.attrs.ref;
      const prev = marks.findLast((x) => x.type === mark.type && x.href === mark.href && x.ref === mark.ref && x.end === start);
      if (prev) prev.end = mark.end; // one mark across adjacent text nodes
      else marks.push(mark);
    }
  });
  const b = { kind: kindOf(node), text };
  if (node.type === schema.nodes.heading) b.level = node.attrs.level;
  if (marks.length) b.marks = marks;
  if (mentions.size) b.mentions = [...mentions];
  if (node.type === schema.nodes.task && node.attrs.checked) b.checked = true;
  if (node.type === schema.nodes.panel) b.tone = node.attrs.tone;
  if (node.attrs.align) b.align = node.attrs.align;
  return b;
}

// --- the editor -----------------------------------------------------------------

// Blocks that continue on Enter: list items, tasks (a new one unchecked)
// and panels (in the same tone).
const continued = () => [schema.nodes.bullet, schema.nodes.ordered, schema.nodes.task, schema.nodes.panel];

const listItem = (state, dispatch) => {
  const { $from, empty } = state.selection;
  const node = $from.parent;
  if (!empty || !continued().includes(node.type)) return false;
  // Enter on an empty item ends the list; otherwise the next item keeps the kind.
  if (node.content.size === 0) return setBlockType(schema.nodes.paragraph)(state, dispatch);
  return splitBlockAs((n) => ({ type: n.type, attrs: n.type === schema.nodes.panel ? { tone: n.attrs.tone } : {} }))(state, dispatch);
};

function plugins() {
  const block = (type, attrs) => (re) => textblockTypeInputRule(re, type, attrs);
  return [
    inputRules({
      rules: [
        textblockTypeInputRule(/^(#{1,3})\s$/, schema.nodes.heading, (m) => ({ level: m[1].length })),
        block(schema.nodes.bullet)(/^[-*]\s$/),
        block(schema.nodes.ordered)(/^1[.)]\s$/),
        block(schema.nodes.quote)(/^>\s$/),
        block(schema.nodes.code_block)(/^```$/),
        block(schema.nodes.task)(/^\[\s?\]\s$/),
        block(schema.nodes.task, { checked: true })(/^\[x\]\s$/i),
      ],
    }),
    history(),
    keymap({
      "Mod-z": undo, "Mod-y": redo, "Shift-Mod-z": redo,
      "Mod-b": toggleMark(schema.marks.bold), "Mod-i": toggleMark(schema.marks.italic), "Mod-`": toggleMark(schema.marks.code),
      "Mod-u": toggleMark(schema.marks.underline), "Mod-Shift-x": toggleMark(schema.marks.strike), "Mod-Shift-h": toggleMark(schema.marks.highlight),
      "Mod-Shift-l": setAlign(""), "Mod-Shift-e": setAlign("center"), "Mod-Shift-r": setAlign("right"), "Mod-Shift-j": setAlign("justify"),
      Enter: chainCommands(listItem, baseKeymap.Enter),
    }),
    keymap(baseKeymap),
  ];
}

// setAlign aligns the blocks of the selection that can be aligned
// (ADR-0059); "" is the default, left.
export function setAlign(align) {
  return (state, dispatch) => {
    const { from, to } = state.selection;
    const tr = state.tr;
    state.doc.nodesBetween(from, to, (node, pos) => {
      if (node.isTextblock && "align" in node.attrs && node.attrs.align !== align) tr.setNodeMarkup(pos, null, { ...node.attrs, align });
      return !node.isTextblock;
    });
    if (!tr.docChanged) return false;
    if (dispatch) dispatch(tr);
    return true;
  };
}

// alignOf is the alignment of the block the selection starts in, or null
// for a block that cannot be aligned.
function alignOf(state) {
  const node = state.selection.$from.parent;
  return node.isTextblock && "align" in node.attrs ? node.attrs.align : null;
}

// markActive reports whether a mark applies at the selection.
function markActive(state, type) {
  const { from, $from, to, empty } = state.selection;
  if (empty) return Boolean(type.isInSet(state.storedMarks || $from.marks()));
  return state.doc.rangeHasMark(from, to, type);
}

// --- node views (ADR-0042) -----------------------------------------------------

// A node view's own controls are not editor content: the editor neither
// handles their events nor reads their changes as edits. A click elsewhere
// on the block selects it, like any block, so it can be cut, moved or
// deleted.
const selectable = { stopEvent: (ev) => Boolean(ev.target.closest?.("a, button, input, select")), ignoreMutation: () => true };

// taskView draws a task's checkbox; ticking it changes the task.
function taskView(node, view, getPos) {
  const box = h("input", { type: "checkbox", "aria-label": t("editor.taskDone") });
  const text = h("span", { class: "pm-task-text" });
  const dom = h("div", { class: "pm-task" }, h("span", { class: "pm-task-box", contenteditable: "false" }, box), text);
  const show = () => {
    box.checked = node.attrs.checked;
    box.disabled = !view.editable;
    dom.dataset.checked = String(node.attrs.checked);
  };
  box.addEventListener("change", () => {
    const pos = getPos();
    if (pos !== undefined) view.dispatch(view.state.tr.setNodeMarkup(pos, null, { ...node.attrs, checked: box.checked }));
    show(); // a refused change (someone else holds the task) shows as it is
  });
  show();
  return {
    dom, contentDOM: text,
    update: (n) => { if (n.type !== node.type) return false; node = n; show(); return true; },
    stopEvent: (ev) => ev.target === box,
    ignoreMutation: (m) => m.type === "attributes" && (m.target === box || m.target === dom),
  };
}

const rowsOf = (text) => text.split("\n").map((r) => r.split("\t"));
const textOf = (rows) => rows.map((r) => r.join("\t")).join("\n");

// tableView draws a table; an editor edits its cells in place and adds or
// removes rows and columns. Cells are plain text: tabs and line breaks
// typed into a cell become spaces.
function tableView(node, view, getPos) {
  const dom = h("div", { class: "pm-table", contenteditable: "false" });
  let shown = null;
  const commit = (rows) => {
    const pos = getPos();
    if (pos === undefined || dom.classList.contains("pm-locked")) return render(true);
    view.dispatch(view.state.tr.setNodeMarkup(pos, null, { ...node.attrs, text: textOf(rows) }));
  };
  const render = (force) => {
    const edit = view.editable;
    if (!force && shown === node.attrs.text + edit) return;
    shown = node.attrs.text + edit;
    const rows = rowsOf(node.attrs.text);
    const cell = (c, i, j) => {
      const tag = i === 0 ? "th" : "td";
      if (!edit) return h(tag, { text: c });
      const input = h("input", { value: c, "aria-label": t("editor.cell", { row: i + 1, col: j + 1 }) });
      input.addEventListener("change", () => {
        const next = rowsOf(node.attrs.text);
        next[i][j] = input.value.replace(/[\t\r\n]/g, " ");
        commit(next);
      });
      return h(tag, {}, input);
    };
    const table = h("table", {}, rows.map((r, i) => h("tr", {}, r.map((c, j) => cell(c, i, j)))));
    const change = (fn) => () => { const next = rowsOf(node.attrs.text); fn(next); commit(next); };
    const tools = edit ? h("div", { class: "pm-table-tools" },
      h("button", { type: "button", class: "link", text: t("editor.addRow"), onclick: change((rs) => rs.push(rs[0].map(() => ""))) }),
      h("button", { type: "button", class: "link", text: t("editor.addColumn"), onclick: change((rs) => rs.forEach((r) => r.push(""))) }),
      rows.length > 1 ? h("button", { type: "button", class: "link", text: t("editor.removeRow"), onclick: change((rs) => rs.pop()) }) : null,
      rows[0].length > 1 ? h("button", { type: "button", class: "link", text: t("editor.removeColumn"), onclick: change((rs) => rs.forEach((r) => r.pop())) }) : null,
    ) : null;
    dom.replaceChildren(...[table, tools].filter(Boolean)); // replaceChildren writes a null out as "null"
  };
  render();
  return { dom, ...selectable, update: (n) => { if (n.type !== node.type) return false; node = n; render(); return true; } };
}

// tocView lists the body's headings; a click scrolls to one. tocs holds
// the refresh functions of an editor's tables of contents, which a plugin
// calls after every change of the document.
function tocView(node, view, tocs) {
  const dom = h("nav", { class: "pm-toc", contenteditable: "false", "aria-label": t("editor.toc") });
  const refresh = () => {
    const heads = [];
    view.state.doc.forEach((n, offset) => {
      if (n.type === schema.nodes.heading && n.textContent.trim()) heads.push({ level: n.attrs.level, text: n.textContent, offset });
    });
    dom.replaceChildren(h("p", { class: "pm-box-title", text: t("editor.toc") }), heads.length
      ? h("ul", {}, heads.map((x) => h("li", { class: "pm-toc-" + x.level }, h("button", { type: "button", class: "link", text: x.text,
        onclick: () => view.nodeDOM(x.offset)?.scrollIntoView({ block: "start" }) }))))
      : h("p", { class: "muted", text: t("editor.tocEmpty") }));
  };
  tocs.add(refresh);
  refresh();
  return { dom, ...selectable, update: (n) => n.type === node.type, destroy: () => tocs.delete(refresh) };
}

// embedView shows an embedded item whole (ADR-0106): a heading that leads
// to it, then what load(ref) returns, its content read-only, or nothing
// where the reader may not see it. Events inside the content stay there.
function embedView(node, load) {
  const dom = h("div", { class: "pm-embed", contenteditable: "false" });
  const visible = Boolean(node.attrs.href);
  dom.append(visible ? h("a", { class: "pm-embed-title", href: safeHref(node.attrs.href), text: node.attrs.label })
    : h("span", { class: "muted", text: node.attrs.label || t("editor.hidden") }));
  const body = h("div", { class: "pm-embed-body", "aria-busy": "true" });
  if (visible && load) {
    dom.append(body);
    load(node.attrs.ref).then((el) => body.replaceChildren(...(el ? [el] : [])))
      .catch(() => body.replaceChildren(h("p", { class: "muted", text: t("embed.failed") })))
      .finally(() => body.removeAttribute("aria-busy"));
  }
  return { dom, ignoreMutation: () => true, stopEvent: (ev) => body.contains(ev.target) };
}

// taskListView shows the tasks that carry a label, from tasks.list(label),
// which the view of the item supplies: sortable and filterable (ADR-0114).
function taskListView(node, tasks) {
  const dom = h("section", { class: "pm-tasklist", contenteditable: "false", "data-label": node.attrs.label },
    h("p", { class: "pm-box-title", text: t("editor.taskListOf", { label: node.attrs.label }) }));
  const body = h("div", { "aria-busy": "true" }, h("p", { class: "muted", text: "…" }));
  dom.append(body);
  Promise.resolve(tasks?.list ? tasks.list(node.attrs.label) : null)
    .then((el) => body.replaceChildren(el || h("p", { class: "muted", text: t("editor.taskListNone") })))
    .catch(() => body.replaceChildren(h("p", { class: "muted", text: t("embed.failed") })))
    .finally(() => body.removeAttribute("aria-busy"));
  return { dom, ignoreMutation: () => true, stopEvent: (ev) => body.contains(ev.target), update: (n) => n.type === node.type && n.attrs.label === node.attrs.label };
}

// childrenView lists the child pages of the item, from childPages().
function childrenView(node, childPages) {
  const dom = h("nav", { class: "pm-children", contenteditable: "false", "aria-label": t("editor.children") },
    h("p", { class: "pm-box-title", text: t("editor.children") }));
  const list = h("div", {}, h("p", { class: "muted", text: "…" }));
  dom.append(list);
  Promise.resolve(childPages ? childPages() : []).then((kids) => {
    list.replaceChildren(kids.length
      ? h("ul", {}, kids.map((c) => h("li", {}, h("a", { href: "#/item/" + encodeURIComponent(c.key), text: c.title || c.key }))))
      : h("p", { class: "muted", text: t("editor.childrenNone") }));
  }).catch(() => list.replaceChildren(h("p", { class: "muted", text: t("editor.childrenNone") })));
  return { dom, ...selectable, update: (n) => n.type === node.type };
}

// imageSizes are the widths offered besides dragging, in percent of the
// column; 0 is the picture's own size (ADR-0056).
const imageSizes = [0, 25, 50, 75, 100];

// imageView shows an image attachment; an editor edits its caption and
// its width, and a drawing (ADR-0053) in the drawing editor.
function imageView(node, view, getPos, files) {
  // A video block plays its file with the browser's own controls (ADR-0097).
  const video = node.type.name === "video";
  const img = video
    ? h("video", { src: files ? files.url(node.attrs.ref) : "", controls: true, preload: "metadata", playsinline: true, title: node.attrs.text })
    : h("img", { src: files ? files.url(node.attrs.ref) : "", alt: node.attrs.text, loading: "lazy" });
  const handle = h("span", { class: "pm-resize", title: t("editor.imageResize"), "aria-hidden": "true" });
  const frame = h("span", { class: "pm-image-frame" }, img, handle);
  const caption = h("figcaption");
  const dom = h("figure", { class: video ? "pm-image pm-video" : "pm-image", contenteditable: "false" }, frame, caption);
  let shown = null;
  // The width is set through the style object, which the content security
  // policy allows (unlike a style attribute). A width in percent applies to
  // the frame, which is a share of the column; the picture fills it.
  // A picture floated left or right takes its width (half the column by
  // default) as a whole figure, so the text flows beside it (ADR-0059).
  const showWidth = (w) => {
    const floats = node.attrs.align === "left" || node.attrs.align === "right";
    dom.style.width = floats ? `${w || 50}%` : "";
    frame.style.width = floats ? "100%" : w ? `${w}%` : "";
    img.style.width = w || floats ? "100%" : "";
  };
  const setWidth = (w) => {
    const pos = getPos();
    if (pos === undefined || dom.classList.contains("pm-locked")) return showWidth(node.attrs.width);
    view.dispatch(view.state.tr.setNodeMarkup(pos, null, { ...node.attrs, width: w }));
  };
  // Left and right let the text flow beside the picture (ADR-0059).
  const setImageAlign = (align) => {
    const pos = getPos();
    if (pos !== undefined && !dom.classList.contains("pm-locked")) view.dispatch(view.state.tr.setNodeMarkup(pos, null, { ...node.attrs, align }));
  };
  // Dragging the corner sets the width in steps of 5 percent of the column.
  handle.addEventListener("pointerdown", (e) => {
    if (!view.editable) return;
    e.preventDefault();
    handle.setPointerCapture(e.pointerId);
    const column = dom.getBoundingClientRect().width;
    const start = frame.getBoundingClientRect().width;
    const x0 = e.clientX;
    let w = node.attrs.width;
    const move = (ev) => {
      w = Math.min(100, Math.max(10, Math.round(((start + ev.clientX - x0) / column) * 20) * 5));
      showWidth(w);
    };
    const up = () => {
      handle.removeEventListener("pointermove", move);
      handle.removeEventListener("pointerup", up);
      if (w !== node.attrs.width) setWidth(w);
    };
    handle.addEventListener("pointermove", move);
    handle.addEventListener("pointerup", up);
  });
  // A drawing is edited from its picture and saved as a new attachment; the
  // block then points at it. The former picture stays, as earlier versions
  // of the body show it (ADR-0043).
  const redraw = async (status) => {
    status.textContent = "";
    try {
      const res = await fetch(files.url(node.attrs.ref), { credentials: "same-origin" });
      if (!res.ok) throw new Error(res.statusText);
      // The new picture is a version of the one edited (ADR-0055), so its
      // copies elsewhere learn of it. The dialog stays open until it is
      // stored, so the block cannot be left before it points at it.
      const a = await openDrawing(await res.blob(), (png) =>
        files.upload(new File([png], drawingName(node.attrs.text), { type: "image/png" }), { item: files.item, attachment: node.attrs.ref }));
      if (!a) return;
      const pos = getPos();
      if (pos === undefined || dom.classList.contains("pm-locked")) throw new Error(t("draw.locked"));
      view.dispatch(view.state.tr.setNodeMarkup(pos, null, { ...node.attrs, ref: a.id }));
    } catch (err) {
      status.textContent = err.message;
    }
  };
  // A copy whose original was changed since takes the newer version over:
  // copied again, from the same source (ADR-0055).
  const takeOver = async (status, origin) => {
    status.textContent = "";
    try {
      const src = { item: origin.item.id, attachment: origin.newer.id };
      const res = await fetch(`/api/items/${src.item}/attachments/${src.attachment}`, { credentials: "same-origin" });
      if (!res.ok) throw new Error(res.statusText);
      const a = await files.upload(new File([await res.blob()], origin.newer.name, { type: origin.newer.type }), src);
      const pos = getPos();
      if (pos === undefined || dom.classList.contains("pm-locked")) throw new Error(t("draw.locked"));
      view.dispatch(view.state.tr.setNodeMarkup(pos, null, { ...node.attrs, ref: a.id }));
    } catch (err) {
      status.textContent = err.message;
    }
  };
  const render = () => {
    const key = node.attrs.ref + node.attrs.text + node.attrs.width + node.attrs.align + view.editable;
    if (shown === key) return;
    shown = key;
    const src = files ? files.url(node.attrs.ref) : "";
    // A video keeps its place in the playback while only its caption changes.
    if (img.getAttribute("src") !== src) img.src = src;
    if (video) img.title = node.attrs.text;
    else img.alt = node.attrs.text;
    showWidth(node.attrs.width);
    dom.classList.toggle("pm-sizable", view.editable);
    for (const a of ["left", "center", "right"]) dom.classList.toggle("pm-image-" + a, node.attrs.align === a);
    if (!view.editable) return caption.replaceChildren(node.attrs.text);
    const size = h("select", { class: "pm-image-size", "aria-label": t("editor.imageSize"), onchange: (e) => setWidth(Number(e.target.value)) },
      [...new Set([...imageSizes, node.attrs.width])].sort((a, b) => a - b).map((w) =>
        h("option", { value: String(w), text: w ? `${w} %` : t("editor.imageOwnSize"), selected: w === node.attrs.width })));
    const input = h("input", { value: node.attrs.text, placeholder: t("editor.caption"), "aria-label": t("editor.caption") });
    input.addEventListener("change", () => {
      const pos = getPos();
      if (pos !== undefined && !dom.classList.contains("pm-locked")) {
        view.dispatch(view.state.tr.setNodeMarkup(pos, null, { ...node.attrs, text: input.value.replace(/[\r\n]+/g, " ") }));
      } else render(shown = null);
    });
    const status = h("span", { class: "error", role: "status" });
    const origin = files?.origin?.(node.attrs.ref);
    // replaceChildren takes nodes and strings only: no arrays, no null.
    const place = h("select", { class: "pm-image-align", "aria-label": t("editor.imageAlign"), onchange: (e) => setImageAlign(e.target.value) },
      [["", t("editor.imageAlignDefault")], ["left", t("editor.imageAlignLeft")], ["center", t("editor.imageAlignCenter")], ["right", t("editor.imageAlignRight")]]
        .map(([v, label]) => h("option", { value: v, text: label, selected: v === node.attrs.align })));
    const remove = h("button", { type: "button", class: "link pm-image-remove", text: t("editor.imageRemove"), onclick: () => {
      const pos = getPos();
      if (pos === undefined || dom.classList.contains("pm-locked")) return;
      view.dispatch(view.state.tr.delete(pos, pos + node.nodeSize));
      view.focus(); // the button is gone; the text goes on where the picture was
    } });
    caption.replaceChildren(...[input, " ", size, " ", place, " ", remove,
      ...(files?.isDrawing?.(node.attrs.ref) ? [" ", h("button", { type: "button", class: "link pm-redraw", text: t("draw.edit"), onclick: () => redraw(status) })] : []),
      ...(origin?.newer ? [" ", h("button", { type: "button", class: "link pm-take-over", text: t("att.takeOver", { key: origin.item.key }), onclick: () => takeOver(status, origin) })] : []),
      status]);
  };
  render();
  // A click on a picture selects its block, so it can be deleted, cut or
  // moved like text; a video keeps its clicks for its controls.
  const stopEvent = (ev) => caption.contains(ev.target) || handle.contains(ev.target) || (video && img.contains(ev.target));
  return { dom, ignoreMutation: () => true, stopEvent, update: (n) => { if (n.type !== node.type) return false; node = n; render(); return true; } };
}

// createEditor mounts an editor for blocks and returns its element and a
// function reading the blocks back. With editable false it is a viewer.
// resolve(keyOrId) returns an item (for mentions and embeds) or throws;
// childPages() the item's child pages as [{key, title}], for a list of
// child pages. files, for an item's attachments (ADR-0045), has url(id),
// where a file is served, and upload(file), which stores a file and returns
// the attachment. extra adds plugins, such as a live session's (live.js);
// viewProps adds editor view properties. comment(anchor), if given, starts
// an inline comment on the selected text (anchors.js, ADR-0047); note(anchor)
// a personal note on it (notes.js, ADR-0107).
// embedContent(ref), if given, returns the content of an embedded item,
// shown in its block (ADR-0106).
export function createEditor({ blocks, embeds, editable, resolve, childPages, files, extra = [], viewProps = {}, comment, protect, note, actions = [], embedContent, tasks }) {
  const mount = h("div", { class: "pm-editor" });
  const tocs = new Set();
  const tocPlugin = new Plugin({ view: () => ({ update: (v, prev) => { if (!v.state.doc.eq(prev.doc)) tocs.forEach((f) => f()); } }) });
  // The toolbar shows what applies at the selection, after every change,
  // one's own or another's in a live session (ADR-0059).
  let onUpdate = () => {};
  const statePlugin = new Plugin({ view: () => ({ update: (v) => onUpdate(v.state) }) });
  // Links, "@" and embedding as in a word processor (ADR-0096).
  const aids = assist(schema, isSafeLink);
  const aidKeys = keymap({ "Mod-k": (state, dispatch, v) => { aids.openLink(v); return true; } });
  // Pictures pasted into the text (ADR-0108): one copied in the album
  // becomes a copy of its original file, any other image an upload.
  let onPaste = () => false;
  const pastePlugin = new Plugin({ props: { handlePaste: (v, ev) => onPaste(ev) } });
  const view = new EditorView(mount, {
    state: EditorState.create({ doc: docFromBlocks(blocks, embeds), plugins: [...extra, ...(editable ? [pastePlugin, ...aids.plugins, aidKeys] : []), ...plugins(), tocPlugin, statePlugin] }),
    editable: () => editable,
    attributes: { "aria-label": t("editor.body"), role: "textbox", "aria-multiline": "true" },
    nodeViews: {
      task: taskView,
      table: tableView,
      toc: (node, v) => tocView(node, v, tocs),
      children: (node) => childrenView(node, childPages),
      image: (node, v, getPos) => imageView(node, v, getPos, files),
      video: (node, v, getPos) => imageView(node, v, getPos, files),
      embed: (node) => embedView(node, embedContent),
      tasklist: (node) => taskListView(node, tasks),
    },
    ...viewProps,
  });
  const run = (cmd) => () => { cmd(view.state, view.dispatch); view.focus(); };
  const kindSelect = h("select", { "aria-label": t("editor.blockKind"), onchange: (e) => {
    const [kind, arg] = e.target.value.split(":");
    const attrs = kind === "heading" ? { level: Number(arg) } : kind === "panel" ? { tone: arg } : null;
    run(setBlockType(schema.nodes[kind], attrs))();
  } }, [
    ["paragraph", t("editor.paragraph")], ["heading:1", t("editor.heading", { n: 1 })], ["heading:2", t("editor.heading", { n: 2 })], ["heading:3", t("editor.heading", { n: 3 })],
    ["bullet", t("editor.bullet")], ["ordered", t("editor.ordered")], ["task", t("editor.task")], ["quote", t("editor.quote")], ["code_block", t("editor.codeBlock")],
    ["panel:info", t("editor.panelInfo")], ["panel:note", t("editor.panelNote")], ["panel:warning", t("editor.panelWarning")],
    ["panel:tip", t("editor.panelTip")], ["panel:error", t("editor.panelError")],
  ].map(([v, label]) => h("option", { value: v, text: label })));
  // insertBlock puts a block after the one the cursor is in.
  const insertBlock = (node) => {
    const { $to } = view.state.selection;
    view.dispatch(view.state.tr.insert($to.depth >= 1 ? $to.after(1) : $to.pos, node));
    view.focus();
  };
  const insertSelect = h("select", { "aria-label": t("editor.insert"), onchange: (e) => {
    const kind = e.target.value;
    e.target.value = "";
    if (kind === "table") insertBlock(schema.nodes.table.create({ text: `${t("editor.heading", { n: 1 })}\t${t("editor.heading", { n: 2 })}\n\t` }));
    else if (kind === "image") picker.click();
    else if (kind === "video") videoPicker.click();
    else if (kind === "drawing") draw();
    else if (kind === "album") fromAlbum();
    else if (kind === "clip") fromClip();
    else if (kind === "embed") embed();
    else if (kind === "newtask") newTask();
    else if (kind === "tasklist") taskList();
    else if (kind) insertBlock(schema.nodes[kind].create());
  } }, [["", t("editor.insert")], ["embed", t("assist.embedItem")], tasks?.create ? ["newtask", t("editor.newTask")] : null, tasks?.pickLabel ? ["tasklist", t("editor.taskList")] : null, ["table", t("editor.table")], ["divider", t("editor.divider")], ["toc", t("editor.toc")], ["children", t("editor.children")],
    files ? ["image", t("editor.image")] : null, files ? ["video", t("editor.video")] : null, files ? ["drawing", t("draw.insert")] : null, files ? ["album", t("album.fromAlbum")] : null, files ? ["clip", t("editor.copiedPicture")] : null].filter(Boolean)
    .map(([v, label]) => h("option", { value: v, text: label })));
  // An image is uploaded as an attachment of the item, then shown.
  const picker = h("input", { type: "file", accept: "image/png,image/jpeg,image/gif,image/webp", hidden: true, "aria-label": t("editor.image") });
  const uploadImage = async (file) => {
    status.textContent = "";
    try {
      const a = await files.upload(file);
      if (!a.inline) throw new Error(t("editor.notAnImage"));
      insertBlock(schema.nodes.image.create({ ref: a.id, text: a.name.replace(/\.[^.]*$/, "") }));
    } catch (err) {
      status.textContent = err.message;
    }
  };
  picker.addEventListener("change", () => {
    const file = picker.files[0];
    picker.value = "";
    if (file) uploadImage(file);
  });
  // A video is uploaded as an attachment of the item, then played in the
  // text (ADR-0097); the server reads its format from its bytes.
  const videoPicker = h("input", { type: "file", accept: "video/mp4,video/webm", hidden: true, "aria-label": t("editor.video") });
  videoPicker.addEventListener("change", async () => {
    const file = videoPicker.files[0];
    videoPicker.value = "";
    if (!file) return;
    status.textContent = t("editor.uploading");
    try {
      const a = await files.upload(file);
      if (!["video/mp4", "video/webm"].includes(a.type)) throw new Error(t("editor.notAVideo"));
      status.textContent = "";
      insertBlock(schema.nodes.video.create({ ref: a.id, text: a.name.replace(/\.[^.]*$/, "") }));
    } catch (err) {
      status.textContent = err.message;
    }
  });
  // A drawing (ADR-0053) is made in the drawing editor, stored as an
  // attachment that carries its scene, and shown like an image.
  const draw = async () => {
    status.textContent = "";
    try {
      const a = await openDrawing(null, (png) => files.upload(new File([png], drawingName(""), { type: "image/png" })));
      if (!a) return;
      insertBlock(schema.nodes.image.create({ ref: a.id, text: "" }));
    } catch (err) {
      status.textContent = err.message;
    }
  };
  // A picture of the album is copied into this item with its source
  // (ADR-0055), unless it is one of its own files already.
  const placeAlbum = async (p) => {
    const a = p.item.id === files.item ? p.attachment
      : await files.upload(await pictureFile(p), { item: p.item.id, attachment: p.attachment.id });
    insertBlock(schema.nodes.image.create({ ref: a.id, text: captionOf(p.attachment.name) }));
  };
  // A picture copied in the album (ADR-0108), named by the clipboard's
  // marker; its name and type come from this browser's copy or the item.
  const pasteAlbum = async (item, attachment) => {
    status.textContent = "";
    try {
      let p = copiedPicture();
      if (p?.item.id !== item || p?.attachment.id !== attachment) {
        const { attachments } = await (await fetch(`/api/items/${item}/attachments`, { credentials: "same-origin" })).json();
        const a = (attachments || []).find((x) => x.id === attachment);
        if (!a) throw new Error(t("editor.clipGone"));
        p = { item: { id: item }, attachment: a };
      }
      await placeAlbum(p);
    } catch (err) {
      status.textContent = err.message;
    }
  };
  const fromClip = () => {
    const p = copiedPicture();
    if (!p) status.textContent = t("editor.noClip");
    else pasteAlbum(p.item.id, p.attachment.id);
  };
  onPaste = (ev) => {
    if (!files) return false;
    const m = (ev.clipboardData?.getData("text/html") || "").match(clipMark);
    if (m) {
      pasteAlbum(m[1], m[2]);
      return true;
    }
    const file = [...(ev.clipboardData?.files || [])].find((f) => /^image\/(png|jpeg|gif|webp)$/.test(f.type));
    if (file) {
      uploadImage(file);
      return true;
    }
    return false;
  };
  // A picture from the album (ADR-0054) is copied into this item, unless
  // it is one of its own files already.
  const fromAlbum = async () => {
    status.textContent = "";
    try {
      const p = await pickFromAlbum();
      if (!p) return;
      await placeAlbum(p);
    } catch (err) {
      status.textContent = err.message;
    }
  };
  const status = h("span", { class: "error", role: "status" });
  // An item shown in the text, chosen by searching (ADR-0096).
  const embed = async () => {
    status.textContent = "";
    const it = await aids.pickItem(t("assist.embedItem"));
    if (!it) return view.focus();
    insertBlock(schema.nodes.embed.create({ ref: it.id, label: `${it.key} ${it.title}`, href: "#/item/" + encodeURIComponent(it.key) }));
  };
  // A new task, made as a child of this item, goes into the text as a
  // link or shown whole (ADR-0114).
  const newTask = async () => {
    status.textContent = "";
    try {
      const made = await tasks.create();
      if (!made) return view.focus();
      const { item: it, display } = made;
      const label = `${it.key} ${it.title}`;
      insertBlock(display === "full"
        ? schema.nodes.embed.create({ ref: it.id, label, href: "#/item/" + encodeURIComponent(it.key) })
        : schema.nodes.paragraph.create(null, schema.nodes.mention.create({ ref: it.id, label })));
    } catch (err) {
      status.textContent = err.message;
    }
  };
  // A list of the tasks that carry a label (ADR-0114).
  const taskList = async () => {
    status.textContent = "";
    const label = await tasks.pickLabel();
    if (!label) return view.focus();
    insertBlock(schema.nodes.tasklist.create({ label }));
  };
  // Commenting on a passage works while reading too (ADR-0047).
  const commentButton = comment ? h("button", { type: "button", id: "comment-selection", text: t("editor.comment"), onclick: () => {
    status.textContent = "";
    const anchor = anchorOf(view.state);
    if (!anchor) status.textContent = t("editor.commentNeedsSelection");
    else comment(anchor);
  } }) : null;
  // A personal note on a passage, seen by its maker alone (ADR-0107).
  const noteButton = note ? h("button", { type: "button", id: "note-selection", text: t("editor.note"), title: t("editor.noteTitle"), onclick: () => {
    status.textContent = "";
    const anchor = anchorOf(view.state);
    if (!anchor) status.textContent = t("editor.noteNeedsSelection");
    else note(anchor);
  } }) : null;
  // Protecting a passage starts from the text read (ADR-0088).
  const protectButton = protect ? h("button", { type: "button", id: "protect-selection", text: t("editor.protect"), onclick: () => {
    status.textContent = "";
    const at = selectionInBlock(view.state);
    if (!at) status.textContent = t("editor.protectNeedsSelection");
    else protect(at);
  } }) : null;
  // The toolbar's groups; buttons that show a state (a mark, an alignment)
  // are pressed while it applies at the selection (ADR-0059).
  const group = (...els) => h("span", { class: "pm-group" }, ...els.filter(Boolean));
  const toggle = (title, label, cmd, isOn, cls) => {
    const el = h("button", { type: "button", title, "aria-label": title, "aria-pressed": "false", class: cls || null, onclick: run(cmd) }, label);
    return { el, isOn };
  };
  const markButton = (name, title, label) => toggle(title, label, toggleMark(schema.marks[name]), (st) => markActive(st, schema.marks[name]));
  const markButtons = [
    markButton("bold", t("editor.bold"), h("strong", { text: "B" })),
    markButton("italic", t("editor.italic"), h("em", { text: "I" })),
    markButton("underline", t("editor.underline"), h("u", { text: "U" })),
    markButton("strike", t("editor.strike"), h("s", { text: "S" })),
    markButton("highlight", t("editor.highlight"), h("mark", { text: "ab" })),
    markButton("code", t("editor.code"), "</>"),
  ];
  // An alignment icon: four lines, drawn by the stylesheet.
  const alignIcon = (a) => h("span", { class: "pm-ico pm-ico-" + a, "aria-hidden": "true" }, h("i"), h("i"), h("i"), h("i"));
  const alignButtons = [["", t("editor.alignLeft"), "left"], ["center", t("editor.alignCenter"), "center"], ["right", t("editor.alignRight"), "right"], ["justify", t("editor.alignJustify"), "justify"]]
    .map(([a, title, icon]) => toggle(title, alignIcon(icon), setAlign(a), (st) => alignOf(st) === a));
  const listButton = (kind, label, title) => h("button", { type: "button", title, "aria-label": title, text: label, onclick: () => {
    const node = schema.nodes[kind];
    const inIt = view.state.selection.$from.parent.type === node;
    run(setBlockType(inIt ? schema.nodes.paragraph : node))();
  } });
  const stateful = [...markButtons, ...alignButtons];
  const refresh = (st) => {
    for (const b of stateful) b.el.setAttribute("aria-pressed", String(Boolean(b.isOn(st))));
    const canAlign = alignOf(st) !== null;
    for (const b of alignButtons) b.el.disabled = !canAlign;
  };
  const toolbar = !editable
    // The text's own actions, such as editing it, stand beside them.
    ? (commentButton || noteButton || protectButton || actions.length ? h("div", { class: "pm-toolbar pm-readbar", role: "toolbar" }, commentButton, noteButton, protectButton, ...actions, status) : null)
    : h("div", { class: "pm-toolbar", role: "toolbar", "aria-label": t("editor.toolbar") },
    group(kindSelect),
    group(...markButtons.map((b) => b.el)),
    group(...alignButtons.map((b) => b.el)),
    group(listButton("bullet", "•", t("editor.bullet")), listButton("ordered", "1.", t("editor.ordered")), listButton("task", "☑", t("editor.task"))),
    group(
      h("button", { type: "button", id: "pm-link", title: t("assist.linkTitle"), "aria-label": t("assist.linkTitle"), text: t("editor.link"), onclick: () => aids.openLink(view) }),
      h("button", { type: "button", id: "pm-at-button", title: t("assist.mentionTitle"), "aria-label": t("assist.mentionTitle"), text: "@", onclick: () => {
        const { from, to } = view.state.selection;
        const before = from > 0 ? view.state.doc.textBetween(from - 1, from, null, " ") : "";
        view.dispatch(view.state.tr.insertText(before && !/\s/.test(before) ? " @" : "@", from, to));
        view.focus();
      } })),
    group(insertSelect, picker, videoPicker),
    commentButton,
    h("button", { type: "button", title: t("editor.undo"), text: "↶", onclick: run(undo) }),
    h("button", { type: "button", title: t("editor.redo"), text: "↷", onclick: run(redo) }),
    // The text's own actions, such as ending the editing, stand where
    // they stand while reading (ADR-0091).
    actions.length ? h("span", { class: "pm-actions" }, ...actions) : null,
    status,
  );
  if (editable) {
    onUpdate = refresh;
    refresh(view.state);
  }
  return { element: h("div", { class: "pm-wrap" }, toolbar, mount), blocks: () => blocksFromDoc(view.state.doc), view };
}
