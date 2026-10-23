// Live editing of a body (ADR-0010): the server streams the session as
// server-sent events; this module keeps a ProseMirror editor in step with
// it and sends the local changes as operations.
//
// A block someone else holds cannot be changed here: a plugin refuses any
// transaction that would, and marks the block with the holder's name. The
// operations are not recorded keystroke by keystroke; they are the
// difference between the document and what the server last confirmed,
// computed when a batch is sent. Leaving a block releases it, and the
// server commits the body on every release.
import { Plugin, PluginKey, Decoration, DecorationSet, EditorState } from "../vendor/prosemirror.js";
import { createEditor, nodeFromBlock, blockFromNode, schema } from "./editor.js";
import { post, ApiError } from "./api.js";
import { h } from "./dom.js";
import { t } from "./i18n.js";

const key = new PluginKey("live");
const SEND_AFTER = 250; // ms of quiet before a batch is sent
const RELEASE_AFTER = 1500; // ms after the cursor left a block

const newID = () => crypto.randomUUID().replaceAll("-", "").slice(0, 16);

// byBid maps the session IDs of a document's blocks to their nodes.
function byBid(doc) {
  const m = new Map();
  doc.forEach((n) => { if (n.attrs.bid) m.set(n.attrs.bid, n); });
  return m;
}

// blockAt is the session ID of the top-level block holding a position.
function blockAt(doc, pos) {
  const $p = doc.resolve(Math.min(pos, doc.content.size));
  return $p.depth >= 1 ? $p.node(1).attrs.bid : $p.nodeAfter?.attrs.bid ?? null;
}

// liveBody joins the live session of an item and returns the editor
// element and a close function. resolve(keyOrId) finds an item;
// childPages() lists the item's child pages and files serves and stores
// its attachments (editor.js); extra adds plugins and comment starts an
// inline comment (anchors.js).
export function liveBody({ ref, embeds, resolve, childPages, files, extra = [], comment, actions = [], embedContent, tasks }) {
  const el = h("div", { class: "live" });
  const presence = h("p", { class: "muted live-presence", "aria-live": "polite" });
  const status = h("p", { class: "status live-status", role: "status" });
  el.append(presence, status);

  let me = null; // our client ID
  let view = null;
  let editable = false;
  const foreign = new Map(); // bid -> holder name, for blocks others hold
  const mine = new Set(); // bids we hold
  const known = new Map(); // bid -> JSON of the block as the server has it
  const asked = new Map(); // bid -> "lock" or "release" sent, until the lock event arrives
  let sending = false;
  let sendTimer = null;
  let releaseTimer = null;
  let closed = false;

  // touchesForeign reports whether the selection reaches into a block
  // someone else holds. It prefers the browser's selection: right after a
  // click the editor state has not caught up with it yet.
  const touchesForeign = (v) => {
    let { from, to } = v.state.selection;
    const dom = v.root.getSelection?.();
    if (dom && dom.anchorNode && v.dom.contains(dom.anchorNode)) {
      try {
        const a = v.posAtDOM(dom.anchorNode, dom.anchorOffset);
        const b = v.posAtDOM(dom.focusNode, dom.focusOffset);
        [from, to] = [Math.min(a, b), Math.max(a, b)];
      } catch { /* keep the state's selection */ }
    }
    let hit = false;
    v.state.doc.forEach((node, offset) => {
      if (foreign.has(node.attrs.bid) && offset < to + 1 && offset + node.nodeSize > from - 1) hit = true;
    });
    return hit;
  };

  const say = (text, error) => { status.textContent = text; status.classList.toggle("error", Boolean(error)); };

  const plugin = new Plugin({
    key,
    filterTransaction(tr, state) {
      if (!tr.docChanged || tr.getMeta(key)) return true;
      const before = byBid(state.doc);
      const after = byBid(tr.doc);
      for (const bid of foreign.keys()) {
        const a = before.get(bid);
        if (a && !(after.get(bid)?.eq(a))) return false;
      }
      return true;
    },
    // Every block has a unique ID: a split or paste makes a new block.
    appendTransaction(trs, _old, state) {
      if (!trs.some((t) => t.docChanged)) return null;
      const seen = new Set();
      let tr = null;
      state.doc.forEach((node, offset) => {
        const id = node.attrs.bid;
        if (id && !seen.has(id)) {
          seen.add(id);
          return;
        }
        tr = tr || state.tr;
        tr.setNodeMarkup(offset, null, { ...node.attrs, bid: newID() });
      });
      return tr;
    },
    props: {
      decorations(state) {
        const decos = [];
        state.doc.forEach((node, offset) => {
          const who = foreign.get(node.attrs.bid);
          if (!who) return;
          decos.push(Decoration.node(offset, offset + node.nodeSize, { class: "pm-locked", "data-holder": who }));
          // The name is a widget, not generated CSS content: generated
          // content inside an editable block misplaces the browser's caret.
          decos.push(Decoration.widget(offset, () => h("div", { class: "pm-holder", contenteditable: "false", text: t("live.holder", { name: who }) }),
            { side: -1, key: `holder-${node.attrs.bid}-${who}`, ignoreSelection: true }));
        });
        return DecorationSet.create(state.doc, decos);
      },
      // Moving blocks by drag and drop would reorder them without an
      // operation the session knows; cut and paste makes new blocks instead.
      handleDrop: () => true,
      // Input into a held block is stopped before the browser changes the
      // page; refusing the transaction afterwards would leave the cursor
      // displaced, and the next keys would land in the block after it.
      handleDOMEvents: {
        beforeinput: (v, ev) => {
          if (!touchesForeign(v)) return false;
          ev.preventDefault();
          return true;
        },
      },
      handleKeyDown: (v, ev) => touchesForeign(v) && (ev.key.length === 1 && !ev.metaKey && !ev.ctrlKey
        || ["Enter", "Backspace", "Delete"].includes(ev.key)),
    },
    view: () => ({
      update(v, prev) {
        if (closed || !editable) return;
        if (!v.state.doc.eq(prev.doc)) schedule();
        if (!v.state.selection.eq(prev.selection)) cursorMoved();
      },
    }),
  });

  // --- sending ---------------------------------------------------------------

  function schedule() {
    clearTimeout(sendTimer);
    sendTimer = setTimeout(send, SEND_AFTER);
    pending(true);
  }

  // pending shows whether something is not yet saved: unsent changes or
  // blocks we still hold. data-pending is what the browser test waits for.
  function pending(yes) {
    const now = yes || mine.size > 0 || sending || (view && ops().some((o) => o.op !== "lock"));
    el.dataset.pending = now ? "1" : "0";
    if (!status.classList.contains("error")) say(now ? t("live.saving") : t("live.allSaved"));
  }

  function cursorBlock() {
    return view && view.hasFocus() ? blockAt(view.state.doc, view.state.selection.from) : null;
  }

  function cursorMoved() {
    const at = cursorBlock();
    if (at && !mine.has(at) && !foreign.has(at) && known.has(at)) schedule(); // take the lock early
    clearTimeout(releaseTimer);
    releaseTimer = setTimeout(schedule, RELEASE_AFTER);
  }

  // ops computes the operations that bring the server to the document.
  function ops() {
    const out = [];
    const doc = view.state.doc;
    let prev = "";
    const present = new Set();
    doc.forEach((node) => {
      const bid = node.attrs.bid;
      present.add(bid);
      const block = blockFromNode(node);
      const json = JSON.stringify(block);
      if (!known.has(bid)) out.push({ op: "insert", id: bid, after: prev, block, json });
      else if (known.get(bid) !== json) out.push({ op: "set", id: bid, block, json });
      prev = bid;
    });
    for (const bid of known.keys()) if (!present.has(bid)) out.push({ op: "delete", id: bid });
    // The placeholder of an empty body is not a block until it is written.
    if (known.size === 0 && out.length === 1 && out[0].op === "insert" && out[0].json === '{"kind":"paragraph","text":""}') out.pop();
    const at = cursorBlock();
    if (at && known.has(at) && !mine.has(at) && !foreign.has(at) && asked.get(at) !== "lock" && !out.some((o) => o.id === at)) {
      out.push({ op: "lock", id: at });
    }
    // Release the blocks we hold and are no longer in.
    for (const bid of mine) {
      if (bid !== at && present.has(bid) && asked.get(bid) !== "release") out.push({ op: "release", id: bid });
    }
    for (const o of out.filter((o) => o.op === "insert" && o.id !== at)) out.push({ op: "release", id: o.id });
    return out;
  }

  async function send() {
    if (closed || sending || !view) return;
    const batch = ops();
    if (batch.length === 0) return;
    sending = true;
    try {
      await post(`/api/items/${encodeURIComponent(ref)}/live`, { client: me, ops: batch.map(({ json, ...op }) => op) });
      for (const o of batch) {
        if (o.op === "set" || o.op === "insert") known.set(o.id, o.json);
        if (o.op === "delete") known.delete(o.id);
        if (o.op === "lock" || o.op === "release") asked.set(o.id, o.op);
      }
    } catch (err) {
      // The batch was refused as a whole; the snapshot of a fresh
      // connection replaces what this one could not send.
      say(t("live.reloading", { error: err instanceof ApiError ? err.message : String(err) }), true);
      reconnect();
    } finally {
      sending = false;
    }
    if (!closed && ops().length) schedule();
    else pending(false);
  }

  // --- receiving -------------------------------------------------------------

  function remote(fn) {
    if (!view) return;
    const tr = view.state.tr;
    fn(tr);
    tr.setMeta(key, true).setMeta("addToHistory", false);
    view.dispatch(tr);
  }

  function find(doc, bid) {
    let at = null;
    doc.forEach((node, offset) => { if (node.attrs.bid === bid) at = { node, offset }; });
    return at;
  }

  async function labelEmbed(block, bid) {
    if (block.kind !== "embed") return;
    try {
      const it = await resolve(block.ref);
      remote((tr) => {
        const f = find(tr.doc, bid);
        if (f) tr.setNodeMarkup(f.offset, null, { ...f.node.attrs, label: `${it.key} ${it.title}`, href: "#/item/" + encodeURIComponent(it.key) });
      });
    } catch { /* an item the reader cannot see keeps the placeholder */ }
  }

  function snapshot(e) {
    foreign.clear();
    mine.clear();
    known.clear();
    asked.clear();
    const blocks = (e.blocks || []).map((b) => {
      known.set(b.id, JSON.stringify(normal(b.block)));
      setHolder(b.id, b.holder);
      return { ...b.block, id: b.id };
    });
    if (blocks.length === 0) blocks.push({ kind: "paragraph", text: "", id: newID() });
    if (!view) {
      const ed = createEditor({ blocks, embeds, editable: e.editable, resolve, childPages, files, extra: [plugin, ...extra], comment, actions, embedContent, tasks });
      view = ed.view;
      el.insertBefore(ed.element, status);
      // Leaving the editor releases its blocks, after a moment: a click on
      // the toolbar should not.
      view.dom.addEventListener("focusout", () => setTimeout(schedule, RELEASE_AFTER));
    } else {
      view.updateState(EditorState.create({ doc: schema.nodes.doc.create(null, blocks.map((b) => nodeFromBlock(b, embeds))), plugins: view.state.plugins }));
    }
    editable = e.editable;
    view.setProps({ editable: () => editable && !closed });
  }

  // normal is a block as blockFromNode would produce it, so JSON compares.
  function normal(b) {
    const out = { kind: b.kind, text: b.text ?? "" };
    if (b.kind === "embed") return { kind: "embed", ref: b.ref };
    if (b.kind === "toc" || b.kind === "children" || b.kind === "divider") return { kind: b.kind };
    if (b.kind === "image" || b.kind === "video") {
      const img = { kind: b.kind, text: b.text ?? "", ref: b.ref };
      if (b.width) img.width = b.width;
      if (b.align) img.align = b.align;
      return img;
    }
    if (b.kind === "heading") out.level = b.level;
    if (b.marks?.length) out.marks = b.marks;
    if (b.mentions?.length) out.mentions = b.mentions;
    if (b.kind === "task" && b.checked) out.checked = true;
    if (b.kind === "panel") out.tone = b.tone;
    if (b.align) out.align = b.align;
    return out;
  }

  function setHolder(bid, holder) {
    asked.delete(bid);
    foreign.delete(bid);
    mine.delete(bid);
    if (!holder) return;
    if (holder.client === me) mine.add(bid);
    else foreign.set(bid, holder.name);
  }

  const handlers = {
    snapshot(e) {
      if (e.client) me = e.client;
      snapshot(e);
      handlers.presence(e);
    },
    presence(e) {
      const others = (e.participants || []).map((p) => p.name);
      presence.textContent = others.length ? t("live.present", { names: others.join(", ") }) : "";
    },
    lock(e) {
      setHolder(e.id, e.holder);
      remote(() => {});
      if (editable) pending(false);
    },
    block(e) {
      if (e.holder?.client === me) return; // our own change, echoed
      known.set(e.id, JSON.stringify(normal(e.block)));
      remote((tr) => {
        const f = find(tr.doc, e.id);
        if (f) tr.replaceWith(f.offset, f.offset + f.node.nodeSize, nodeFromBlock({ ...e.block, id: e.id }, embeds));
      });
      labelEmbed(e.block, e.id);
    },
    insert(e) {
      if (e.holder?.client === me) return;
      known.set(e.id, JSON.stringify(normal(e.block)));
      setHolder(e.id, e.holder);
      remote((tr) => {
        const node = nodeFromBlock({ ...e.block, id: e.id }, embeds);
        const after = e.after ? find(tr.doc, e.after) : null;
        tr.insert(after ? after.offset + after.node.nodeSize : 0, node);
      });
      labelEmbed(e.block, e.id);
      reorder(e.order);
    },
    delete(e) {
      known.delete(e.id);
      setHolder(e.id, null);
      remote((tr) => {
        const f = find(tr.doc, e.id);
        if (!f) return;
        // A document has at least one block; the last one becomes the
        // empty placeholder.
        if (tr.doc.childCount > 1) tr.delete(f.offset, f.offset + f.node.nodeSize);
        else tr.replaceWith(f.offset, f.offset + f.node.nodeSize, schema.nodes.paragraph.create({ bid: newID() }));
      });
      reorder(e.order);
    },
    committed(e) {
      presence.title = t("live.lastSaved", { name: e.name });
    },
    closed(e) {
      say(e.reason || t("live.ended"), true);
      close();
    },
  };

  // reorder puts the blocks the server knows into its order; blocks it
  // does not know yet stay after the block they follow.
  function reorder(order) {
    if (!order || !view) return;
    const doc = view.state.doc;
    const nodes = [];
    doc.forEach((n) => nodes.push(n));
    const serverOrder = nodes.filter((n) => order.includes(n.attrs.bid)).map((n) => n.attrs.bid);
    const want = order.filter((id) => serverOrder.includes(id));
    if (want.join() === serverOrder.join()) return;
    const byId = new Map(nodes.map((n) => [n.attrs.bid, n]));
    const out = [];
    for (const id of want) {
      out.push(byId.get(id));
      // local blocks that followed this one
      let i = nodes.findIndex((n) => n.attrs.bid === id) + 1;
      while (i < nodes.length && !order.includes(nodes[i].attrs.bid)) out.push(nodes[i++]);
    }
    remote((tr) => tr.replaceWith(0, tr.doc.content.size, out));
  }

  // --- the connection ----------------------------------------------------------

  let source = null;
  function connect() {
    source = new EventSource(`/api/items/${encodeURIComponent(ref)}/live`);
    for (const kind of Object.keys(handlers)) {
      source.addEventListener(kind, (ev) => {
        if (!closed) handlers[kind](JSON.parse(ev.data));
      });
    }
    source.onerror = () => { if (!closed) say(t("live.lost"), true); };
    source.onopen = () => { if (status.classList.contains("error")) say(""); };
  }
  function reconnect() {
    if (source) source.close();
    me = null;
    connect();
  }
  connect();

  function close() {
    if (closed) return;
    closed = true;
    clearTimeout(sendTimer);
    clearTimeout(releaseTimer);
    if (source) source.close(); // the server releases our locks and commits
    if (view) view.setProps({ editable: () => false });
  }

  // finish sends what is not yet saved, gives up every block, and closes
  // the session: the body is then stored as one leaves the editor.
  async function finish() {
    if (closed) return;
    clearTimeout(sendTimer);
    clearTimeout(releaseTimer);
    if (view) view.dom.blur();
    while (sending) await new Promise((r) => setTimeout(r, 50));
    if (editable) await send();
    close();
  }

  return { element: el, close, finish, view: () => view };
}
