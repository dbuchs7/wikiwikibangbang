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
import { pickFromAlbum, pictureFile, captionOf } from "./album.js";

// Every block carries bid, its ID in a live session (ADR-0010); it is not
// stored. Text blocks keep the mentions they had without a mention mark
// (imported bodies), so saving does not drop their relations.
const bid = { bid: { default: null } };
const textAttrs = { ...bid, mentions: { default: [] } };

export const schema = new Schema({
  nodes: {
    doc: { content: "block+" },
    paragraph: { group: "block", content: "inline*", attrs: textAttrs, parseDOM: [{ tag: "p" }], toDOM: () => ["p", 0] },
    heading: {
      group: "block", content: "inline*", defining: true,
      attrs: { ...textAttrs, level: { default: 1 } },
      parseDOM: [1, 2, 3, 4, 5, 6].map((l) => ({ tag: "h" + l, attrs: { level: Math.min(l, 3) } })),
      toDOM: (n) => ["h" + (n.attrs.level + 1), 0], // h1 is the item's title
    },
    bullet: { group: "block", content: "inline*", attrs: textAttrs, parseDOM: [{ tag: "ul > li" }], toDOM: () => ["div", { class: "pm-bullet" }, 0] },
    ordered: { group: "block", content: "inline*", attrs: textAttrs, parseDOM: [{ tag: "ol > li" }], toDOM: () => ["div", { class: "pm-ordered" }, 0] },
    quote: { group: "block", content: "inline*", attrs: textAttrs, parseDOM: [{ tag: "blockquote" }], toDOM: () => ["blockquote", 0] },
    code_block: { group: "block", content: "text*", marks: "", code: true, defining: true, attrs: bid, parseDOM: [{ tag: "pre", preserveWhitespace: "full" }], toDOM: () => ["pre", ["code", 0]] },
    // ADR-0042. A task's checkbox and a table's cells are drawn by node
    // views (createEditor); toDOM is what a copy carries.
    task: { group: "block", content: "inline*", attrs: { ...textAttrs, checked: { default: false } }, toDOM: (n) => ["div", { class: "pm-task", "data-checked": String(n.attrs.checked) }, 0] },
    panel: { group: "block", content: "inline*", attrs: { ...textAttrs, tone: { default: "info" } }, toDOM: (n) => ["div", { class: "pm-panel", "data-tone": n.attrs.tone }, 0] },
    table: { group: "block", atom: true, selectable: true, attrs: { ...bid, text: { default: "\t\n\t" } }, toDOM: (n) => ["pre", { class: "pm-table" }, n.attrs.text] },
    toc: { group: "block", atom: true, selectable: true, attrs: bid, toDOM: () => ["div", { class: "pm-toc" }] },
    children: { group: "block", atom: true, selectable: true, attrs: bid, toDOM: () => ["div", { class: "pm-children" }] },
    // An image attachment of the item (ADR-0045), with a plain caption.
    image: { group: "block", atom: true, selectable: true, attrs: { ...bid, ref: {}, text: { default: "" } }, toDOM: (n) => ["figure", { class: "pm-image" }, ["figcaption", n.attrs.text]] },
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
    link: {
      attrs: { href: {} }, inclusive: false,
      // Pasted links keep only targets the model allows (ADR-0014).
      parseDOM: [{ tag: "a[href]", getAttrs: (dom) => (isSafeLink(dom.getAttribute("href")) ? { href: dom.getAttribute("href") } : false) }],
      toDOM: (m) => ["a", { href: safeHref(m.attrs.href), rel: "noopener noreferrer nofollow", target: "_blank" }, 0],
    },
  },
});

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
  if (b.kind === "toc" || b.kind === "children") return schema.nodes[b.kind].create({ bid: id });
  if (b.kind === "image") return schema.nodes.image.create({ bid: id, ref: b.ref, text: b.text || "" });
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
      .map((m) => (m.type === "link" ? schema.marks.link.create({ href: m.href }) : schema.marks[m.type].create()));
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
  if (node.type === schema.nodes.toc || node.type === schema.nodes.children) return { kind: node.type.name };
  if (node.type === schema.nodes.image) return { kind: "image", text: node.attrs.text, ref: node.attrs.ref };
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
      const prev = marks.findLast((x) => x.type === mark.type && x.href === mark.href && x.end === start);
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
      Enter: chainCommands(listItem, baseKeymap.Enter),
    }),
    keymap(baseKeymap),
  ];
}

// --- node views (ADR-0042) -----------------------------------------------------

// A node view's own controls are not editor content: the editor neither
// handles their events nor reads their changes as edits.
const own = { stopEvent: () => true, ignoreMutation: () => true };

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
  return { dom, ...own, update: (n) => { if (n.type !== node.type) return false; node = n; render(); return true; } };
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
  return { dom, ...own, update: (n) => n.type === node.type, destroy: () => tocs.delete(refresh) };
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
  return { dom, ...own, update: (n) => n.type === node.type };
}

// imageView shows an image attachment; an editor edits its caption, and a
// drawing (ADR-0053) in the drawing editor.
function imageView(node, view, getPos, files) {
  const img = h("img", { src: files ? files.url(node.attrs.ref) : "", alt: node.attrs.text, loading: "lazy" });
  const caption = h("figcaption");
  const dom = h("figure", { class: "pm-image", contenteditable: "false" }, img, caption);
  let shown = null;
  // A drawing is edited from its picture and saved as a new attachment; the
  // block then points at it. The former picture stays, as earlier versions
  // of the body show it (ADR-0043).
  const redraw = async (status) => {
    status.textContent = "";
    try {
      const res = await fetch(files.url(node.attrs.ref), { credentials: "same-origin" });
      if (!res.ok) throw new Error(res.statusText);
      const png = await openDrawing(await res.blob());
      if (!png) return;
      const a = await files.upload(new File([png], drawingName(node.attrs.text), { type: "image/png" }));
      const pos = getPos();
      if (pos === undefined || dom.classList.contains("pm-locked")) throw new Error(t("draw.locked"));
      view.dispatch(view.state.tr.setNodeMarkup(pos, null, { ...node.attrs, ref: a.id }));
    } catch (err) {
      status.textContent = err.message;
    }
  };
  const render = () => {
    const key = node.attrs.ref + node.attrs.text + view.editable;
    if (shown === key) return;
    shown = key;
    img.src = files ? files.url(node.attrs.ref) : "";
    img.alt = node.attrs.text;
    if (!view.editable) return caption.replaceChildren(node.attrs.text);
    const input = h("input", { value: node.attrs.text, placeholder: t("editor.caption"), "aria-label": t("editor.caption") });
    input.addEventListener("change", () => {
      const pos = getPos();
      if (pos !== undefined && !dom.classList.contains("pm-locked")) {
        view.dispatch(view.state.tr.setNodeMarkup(pos, null, { ...node.attrs, text: input.value.replace(/[\r\n]+/g, " ") }));
      } else render(shown = null);
    });
    if (!files?.isDrawing?.(node.attrs.ref)) return caption.replaceChildren(input);
    const status = h("span", { class: "error", role: "status" });
    caption.replaceChildren(input, " ", h("button", { type: "button", class: "link pm-redraw", text: t("draw.edit"), onclick: () => redraw(status) }), status);
  };
  render();
  return { dom, ...own, update: (n) => { if (n.type !== node.type) return false; node = n; render(); return true; } };
}

// createEditor mounts an editor for blocks and returns its element and a
// function reading the blocks back. With editable false it is a viewer.
// resolve(keyOrId) returns an item (for mentions and embeds) or throws;
// childPages() the item's child pages as [{key, title}], for a list of
// child pages. files, for an item's attachments (ADR-0045), has url(id),
// where a file is served, and upload(file), which stores a file and returns
// the attachment. extra adds plugins, such as a live session's (live.js);
// viewProps adds editor view properties. comment(anchor), if given, starts
// an inline comment on the selected text (anchors.js, ADR-0047).
export function createEditor({ blocks, embeds, editable, resolve, childPages, files, extra = [], viewProps = {}, comment }) {
  const mount = h("div", { class: "pm-editor" });
  const tocs = new Set();
  const tocPlugin = new Plugin({ view: () => ({ update: (v, prev) => { if (!v.state.doc.eq(prev.doc)) tocs.forEach((f) => f()); } }) });
  const view = new EditorView(mount, {
    state: EditorState.create({ doc: docFromBlocks(blocks, embeds), plugins: [...extra, ...plugins(), tocPlugin] }),
    editable: () => editable,
    attributes: { "aria-label": t("editor.body"), role: "textbox", "aria-multiline": "true" },
    nodeViews: {
      task: taskView,
      table: tableView,
      toc: (node, v) => tocView(node, v, tocs),
      children: (node) => childrenView(node, childPages),
      image: (node, v, getPos) => imageView(node, v, getPos, files),
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
    else if (kind === "drawing") draw();
    else if (kind === "album") fromAlbum();
    else if (kind) insertBlock(schema.nodes[kind].create());
  } }, [["", t("editor.insert")], ["table", t("editor.table")], ["toc", t("editor.toc")], ["children", t("editor.children")],
    files ? ["image", t("editor.image")] : null, files ? ["drawing", t("draw.insert")] : null, files ? ["album", t("album.fromAlbum")] : null].filter(Boolean)
    .map(([v, label]) => h("option", { value: v, text: label })));
  // An image is uploaded as an attachment of the item, then shown.
  const picker = h("input", { type: "file", accept: "image/png,image/jpeg,image/gif,image/webp", hidden: true, "aria-label": t("editor.image") });
  picker.addEventListener("change", async () => {
    const file = picker.files[0];
    picker.value = "";
    if (!file) return;
    status.textContent = "";
    try {
      const a = await files.upload(file);
      if (!a.inline) throw new Error(t("editor.notAnImage"));
      insertBlock(schema.nodes.image.create({ ref: a.id, text: a.name.replace(/\.[^.]*$/, "") }));
    } catch (err) {
      status.textContent = err.message;
    }
  });
  // A drawing (ADR-0053) is made in the drawing editor, stored as an
  // attachment that carries its scene, and shown like an image.
  const draw = async () => {
    status.textContent = "";
    try {
      const png = await openDrawing();
      if (!png) return;
      const a = await files.upload(new File([png], drawingName(""), { type: "image/png" }));
      insertBlock(schema.nodes.image.create({ ref: a.id, text: "" }));
    } catch (err) {
      status.textContent = err.message;
    }
  };
  // A picture from the album (ADR-0054) is copied into this item, unless
  // it is one of its own files already.
  const fromAlbum = async () => {
    status.textContent = "";
    try {
      const p = await pickFromAlbum();
      if (!p) return;
      const a = p.item.id === files.item ? p.attachment : await files.upload(await pictureFile(p));
      insertBlock(schema.nodes.image.create({ ref: a.id, text: captionOf(p.attachment.name) }));
    } catch (err) {
      status.textContent = err.message;
    }
  };
  const target = h("input", { class: "pm-target", placeholder: t("editor.target"), "aria-label": t("editor.targetLabel") });
  const status = h("span", { class: "error", role: "status" });
  const withTarget = (fn) => async () => {
    status.textContent = "";
    const value = target.value;
    try {
      if (!value.trim()) throw new Error(t("editor.needTarget"));
      await fn(value.trim());
      // Clear only what this action used: the field may have been edited
      // while the item was being resolved.
      if (target.value === value) target.value = "";
    } catch (err) {
      status.textContent = err.message;
    }
    view.focus();
  };
  // Commenting on a passage works while reading too (ADR-0047).
  const commentButton = comment ? h("button", { type: "button", id: "comment-selection", text: t("editor.comment"), onclick: () => {
    status.textContent = "";
    const anchor = anchorOf(view.state);
    if (!anchor) status.textContent = t("editor.commentNeedsSelection");
    else comment(anchor);
  } }) : null;
  const toolbar = !editable
    ? (commentButton ? h("div", { class: "pm-toolbar pm-readbar", role: "toolbar" }, commentButton, status) : null)
    : h("div", { class: "pm-toolbar", role: "toolbar" },
    kindSelect,
    insertSelect,
    picker,
    h("button", { type: "button", title: t("editor.bold"), onclick: run(toggleMark(schema.marks.bold)) }, h("strong", { text: "B" })),
    h("button", { type: "button", title: t("editor.italic"), onclick: run(toggleMark(schema.marks.italic)) }, h("em", { text: "I" })),
    h("button", { type: "button", title: t("editor.code"), onclick: run(toggleMark(schema.marks.code)), text: "</>" }),
    target,
    h("button", { type: "button", text: t("editor.link"), onclick: withTarget(async (href) => {
      if (!isSafeLink(href)) throw new Error(t("editor.linkNeedsURL"));
      if (view.state.selection.empty) throw new Error(t("editor.linkNeedsSelection"));
      toggleMark(schema.marks.link, { href })(view.state, view.dispatch);
    }) }),
    h("button", { type: "button", text: t("editor.mention"), onclick: withTarget(async (ref) => {
      const it = await resolve(ref);
      view.dispatch(view.state.tr.replaceSelectionWith(schema.nodes.mention.create({ ref: it.id, label: "@" + it.key })));
    }) }),
    h("button", { type: "button", text: t("editor.embed"), onclick: withTarget(async (ref) => {
      const it = await resolve(ref);
      const node = schema.nodes.embed.create({ ref: it.id, label: `${it.key} ${it.title}`, href: "#/item/" + encodeURIComponent(it.key) });
      const { $to } = view.state.selection;
      view.dispatch(view.state.tr.insert($to.depth >= 1 ? $to.after(1) : $to.pos, node));
    }) }),
    commentButton,
    h("button", { type: "button", title: t("editor.undo"), text: "↶", onclick: run(undo) }),
    h("button", { type: "button", title: t("editor.redo"), text: "↷", onclick: run(redo) }),
    status,
  );
  return { element: h("div", { class: "pm-wrap" }, toolbar, mount), blocks: () => blocksFromDoc(view.state.doc), view };
}
