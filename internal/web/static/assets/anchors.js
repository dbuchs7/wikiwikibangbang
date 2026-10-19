// Inline comments (ADR-0047): a comment names a passage of the body by its
// text, with a little of what precedes and follows it. This module turns a
// selection into such an anchor and finds anchors again in the document,
// to highlight them, however the body changed since.
import { Plugin, PluginKey, Decoration, DecorationSet } from "../vendor/prosemirror.js";

const CONTEXT = 32; // characters of prefix and suffix kept with a quote

// blockTexts lists the text blocks of a document with their text as the
// model has it (a mention counts as its label) and where each piece of
// that text sits in the document.
function blockTexts(doc) {
  const out = [];
  doc.forEach((node, offset) => {
    if (!node.isTextblock) return;
    const segs = [];
    let text = "";
    node.forEach((child, at) => {
      const piece = child.isText ? child.text : child.type.name === "mention" ? child.attrs.label : "";
      segs.push({ from: text.length, to: text.length + piece.length, pos: offset + 1 + at, atom: !child.isText, size: child.nodeSize });
      text += piece;
    });
    out.push({ text, segs, start: offset + 1, end: offset + 1 + node.content.size });
  });
  return out;
}

// posOf is the document position of index i of a block's text; inside a
// mention it is the mention's start or, for an end, its end.
function posOf(block, i, end) {
  for (const s of block.segs) {
    if (i < s.from || i > s.to) continue;
    if (!s.atom) return s.pos + (i - s.from);
    if (i === s.from) return s.pos;
    if (i === s.to || end) return s.pos + s.size;
    return s.pos;
  }
  return end ? block.end : block.start;
}

// indexOf is the index in a block's text of a document position.
function indexOf(block, pos) {
  for (const s of block.segs) {
    if (pos < s.pos || pos > s.pos + s.size) continue;
    return s.atom ? (pos === s.pos ? s.from : s.to) : s.from + (pos - s.pos);
  }
  return pos <= block.start ? 0 : block.text.length;
}

// anchorOf makes the anchor of a selection, or null when it is empty, only
// blank or reaches over more than one block.
export function anchorOf(state) {
  const { from, to, empty } = state.selection;
  if (empty) return null;
  const block = blockTexts(state.doc).find((b) => from >= b.start && to <= b.end);
  if (!block) return null;
  const a = indexOf(block, from), b = indexOf(block, to);
  const quote = block.text.slice(a, b);
  if (!quote.trim()) return null;
  return { quote, prefix: block.text.slice(Math.max(0, a - CONTEXT), a), suffix: block.text.slice(b, b + CONTEXT) };
}

const commonEnd = (a, b) => { let n = 0; while (n < a.length && n < b.length && a[a.length - 1 - n] === b[b.length - 1 - n]) n++; return n; };
const commonStart = (a, b) => { let n = 0; while (n < a.length && n < b.length && a[n] === b[n]) n++; return n; };

// locate finds an anchor in a document: of the places its quote occurs,
// the one whose surroundings agree most with the prefix and suffix. It
// returns the range, or null when the quote occurs nowhere.
export function locate(doc, anchor) {
  if (!anchor?.quote) return null;
  let best = null;
  for (const block of blockTexts(doc)) {
    for (let i = block.text.indexOf(anchor.quote); i >= 0; i = block.text.indexOf(anchor.quote, i + 1)) {
      const j = i + anchor.quote.length;
      const score = commonEnd(block.text.slice(0, i), anchor.prefix || "") + commonStart(block.text.slice(j), anchor.suffix || "");
      if (!best || score > best.score) best = { score, from: posOf(block, i, false), to: posOf(block, j, true) };
    }
  }
  return best && { from: best.from, to: best.to };
}

const key = new PluginKey("comments");

// commentPlugin highlights the passages of the open threads. threads()
// returns them as [{id, anchor}]; found(ids) learns, after every change,
// which of them were found; open(id) runs when a highlight is clicked.
export function commentPlugin({ threads, found, open }) {
  const decorate = (doc) => {
    const decos = [];
    const ids = new Set();
    for (const c of threads()) {
      const r = locate(doc, c.anchor);
      if (!r || r.from >= r.to) continue;
      ids.add(c.id);
      decos.push(Decoration.inline(r.from, r.to, { class: "comment-anchor", "data-comment": c.id }));
    }
    return { set: DecorationSet.create(doc, decos), ids };
  };
  return new Plugin({
    key,
    state: {
      init: (_, state) => decorate(state.doc),
      apply: (tr, old) => (tr.docChanged || tr.getMeta(key) ? decorate(tr.doc) : old),
    },
    props: {
      decorations: (state) => key.getState(state).set,
      handleClick: (view, pos, ev) => {
        const hit = ev.target.closest?.(".comment-anchor");
        if (hit && open) open(hit.dataset.comment);
        return false;
      },
    },
    view: (view) => {
      let last = null;
      const report = () => {
        const { ids } = key.getState(view.state);
        if (ids !== last) { last = ids; if (found) found(ids); }
      };
      report();
      return { update: report };
    },
  });
}
