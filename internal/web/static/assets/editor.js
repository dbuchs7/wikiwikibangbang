// The rich-text editor (ADR-0032): ProseMirror with a schema that is exactly
// the item model's block document — paragraphs, headings (levels 1 to 3),
// bullet and ordered list items, quotes, code blocks and embeds; bold,
// italic, code, link and mention marks. Nothing outside the schema survives
// paste or input, so the editor cannot produce markup the model refuses.
import {
  Schema, EditorState, EditorView, keymap, baseKeymap, toggleMark, setBlockType,
  splitBlockAs, chainCommands, history, undo, redo, inputRules, textblockTypeInputRule,
} from "../vendor/prosemirror.js";
import { h, safeHref } from "./dom.js";
import { t } from "./i18n.js";

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
  return b;
}

// --- the editor -----------------------------------------------------------------

const listItem = (state, dispatch) => {
  const { $from, empty } = state.selection;
  const node = $from.parent;
  if (!empty || (node.type !== schema.nodes.bullet && node.type !== schema.nodes.ordered)) return false;
  // Enter on an empty item ends the list; otherwise the next item keeps the kind.
  if (node.content.size === 0) return setBlockType(schema.nodes.paragraph)(state, dispatch);
  return splitBlockAs((n) => ({ type: n.type }))(state, dispatch);
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

// createEditor mounts an editor for blocks and returns its element and a
// function reading the blocks back. With editable false it is a viewer.
// resolve(keyOrId) returns an item (for mentions and embeds) or throws.
// extra adds plugins, such as a live session's (live.js); viewProps adds
// editor view properties.
export function createEditor({ blocks, embeds, editable, resolve, extra = [], viewProps = {} }) {
  const mount = h("div", { class: "pm-editor" });
  const view = new EditorView(mount, {
    state: EditorState.create({ doc: docFromBlocks(blocks, embeds), plugins: [...extra, ...plugins()] }),
    editable: () => editable,
    attributes: { "aria-label": t("editor.body"), role: "textbox", "aria-multiline": "true" },
    ...viewProps,
  });
  const run = (cmd) => () => { cmd(view.state, view.dispatch); view.focus(); };
  const kindSelect = h("select", { "aria-label": t("editor.blockKind"), onchange: (e) => {
    const [kind, level] = e.target.value.split(":");
    const attrs = kind === "heading" ? { level: Number(level) } : null;
    run(setBlockType(schema.nodes[kind], attrs))();
  } }, [
    ["paragraph", t("editor.paragraph")], ["heading:1", t("editor.heading", { n: 1 })], ["heading:2", t("editor.heading", { n: 2 })], ["heading:3", t("editor.heading", { n: 3 })],
    ["bullet", t("editor.bullet")], ["ordered", t("editor.ordered")], ["quote", t("editor.quote")], ["code_block", t("editor.codeBlock")],
  ].map(([v, label]) => h("option", { value: v, text: label })));
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
  const toolbar = editable ? h("div", { class: "pm-toolbar", role: "toolbar" },
    kindSelect,
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
    h("button", { type: "button", title: t("editor.undo"), text: "↶", onclick: run(undo) }),
    h("button", { type: "button", title: t("editor.redo"), text: "↷", onclick: run(redo) }),
    status,
  ) : null;
  return { element: h("div", { class: "pm-wrap" }, toolbar, mount), blocks: () => blocksFromDoc(view.state.doc), view };
}
