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

// Text blocks keep the mentions they had without a mention mark (imported
// bodies), so saving does not drop their relations.
const textAttrs = { mentions: { default: [] } };

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
    code_block: { group: "block", content: "text*", marks: "", code: true, defining: true, parseDOM: [{ tag: "pre", preserveWhitespace: "full" }], toDOM: () => ["pre", ["code", 0]] },
    // Content an importer could not convert (ADR-0023), kept as it came and
    // shown as text; it cannot be created in the editor.
    unsupported: { group: "block", content: "text*", marks: "", code: true, atom: true, toDOM: () => ["pre", { class: "pm-unsupported", title: "Imported content that could not be converted" }, 0] },
    embed: {
      group: "block", atom: true, selectable: true,
      attrs: { ref: {}, label: { default: "" }, href: { default: "" } },
      toDOM: (n) => ["div", { class: "pm-embed", contenteditable: "false" },
        n.attrs.href ? ["a", { href: safeHref(n.attrs.href) }, n.attrs.label] : ["span", { class: "muted" }, n.attrs.label || "an item you cannot see"]],
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
  const nodes = [];
  for (const b of blocks) {
    if (b.kind === "embed") {
      const v = embeds.find((e) => e.ref === b.ref);
      nodes.push(schema.nodes.embed.create({
        ref: b.ref,
        label: v && !v.restricted ? `${v.key} ${v.title}` : "",
        href: v && !v.restricted ? "#/item/" + encodeURIComponent(v.key) : "",
      }));
      continue;
    }
    const type = nodeOf(b.kind);
    const plain = type === schema.nodes.code_block || type === schema.nodes.unsupported;
    const attrs = plain ? {} : { mentions: b.mentions || [] };
    if (type === schema.nodes.heading) attrs.level = b.level || 1;
    nodes.push(type.create(attrs, inlineContent(b.text || "", plain ? [] : b.marks || [])));
  }
  if (nodes.length === 0) nodes.push(schema.nodes.paragraph.create());
  return schema.nodes.doc.create(null, nodes);
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
  doc.forEach((node) => {
    if (node.type === schema.nodes.embed) {
      blocks.push({ kind: "embed", ref: node.attrs.ref });
      return;
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
        const prev = marks.findLast((x) => x.type === mark.type && x.href === mark.href && x.end === start);
        if (prev) prev.end = mark.end; // one mark across adjacent text nodes
        else marks.push(mark);
      }
    });
    const b = { kind: kindOf(node), text };
    if (node.type === schema.nodes.heading) b.level = node.attrs.level;
    if (marks.length) b.marks = marks;
    if (mentions.size) b.mentions = [...mentions];
    blocks.push(b);
  });
  return blocks;
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
export function createEditor({ blocks, embeds, editable, resolve }) {
  const mount = h("div", { class: "pm-editor" });
  const view = new EditorView(mount, {
    state: EditorState.create({ doc: docFromBlocks(blocks, embeds), plugins: plugins() }),
    editable: () => editable,
    attributes: { "aria-label": "Body", role: "textbox", "aria-multiline": "true" },
  });
  const run = (cmd) => () => { cmd(view.state, view.dispatch); view.focus(); };
  const kindSelect = h("select", { "aria-label": "Block kind", onchange: (e) => {
    const [kind, level] = e.target.value.split(":");
    const attrs = kind === "heading" ? { level: Number(level) } : null;
    run(setBlockType(schema.nodes[kind], attrs))();
  } }, [
    ["paragraph", "Paragraph"], ["heading:1", "Heading 1"], ["heading:2", "Heading 2"], ["heading:3", "Heading 3"],
    ["bullet", "Bulleted item"], ["ordered", "Numbered item"], ["quote", "Quote"], ["code_block", "Code block"],
  ].map(([v, label]) => h("option", { value: v, text: label })));
  const target = h("input", { class: "pm-target", placeholder: "URL or item key", "aria-label": "Link target or item key" });
  const status = h("span", { class: "error", role: "status" });
  const withTarget = (fn) => async () => {
    status.textContent = "";
    try {
      await fn(target.value.trim());
      target.value = "";
    } catch (err) {
      status.textContent = err.message;
    }
    view.focus();
  };
  const toolbar = editable ? h("div", { class: "pm-toolbar", role: "toolbar" },
    kindSelect,
    h("button", { type: "button", title: "Bold (Ctrl+B)", onclick: run(toggleMark(schema.marks.bold)) }, h("strong", { text: "B" })),
    h("button", { type: "button", title: "Italic (Ctrl+I)", onclick: run(toggleMark(schema.marks.italic)) }, h("em", { text: "I" })),
    h("button", { type: "button", title: "Code", onclick: run(toggleMark(schema.marks.code)), text: "</>" }),
    target,
    h("button", { type: "button", text: "Link", onclick: withTarget(async (href) => {
      if (!isSafeLink(href)) throw new Error("A link needs an http, https or mailto URL.");
      if (view.state.selection.empty) throw new Error("Select the text to link first.");
      toggleMark(schema.marks.link, { href })(view.state, view.dispatch);
    }) }),
    h("button", { type: "button", text: "Mention", onclick: withTarget(async (ref) => {
      const it = await resolve(ref);
      view.dispatch(view.state.tr.replaceSelectionWith(schema.nodes.mention.create({ ref: it.id, label: "@" + it.key })));
    }) }),
    h("button", { type: "button", text: "Embed", onclick: withTarget(async (ref) => {
      const it = await resolve(ref);
      const node = schema.nodes.embed.create({ ref: it.id, label: `${it.key} ${it.title}`, href: "#/item/" + encodeURIComponent(it.key) });
      const { $to } = view.state.selection;
      view.dispatch(view.state.tr.insert($to.depth >= 1 ? $to.after(1) : $to.pos, node));
    }) }),
    h("button", { type: "button", title: "Undo (Ctrl+Z)", text: "↶", onclick: run(undo) }),
    h("button", { type: "button", title: "Redo (Ctrl+Y)", text: "↷", onclick: run(redo) }),
    status,
  ) : null;
  return { element: h("div", { class: "pm-wrap" }, toolbar, mount), blocks: () => blocksFromDoc(view.state.doc), view };
}
