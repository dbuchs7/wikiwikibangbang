// Personal notes (ADR-0107): a note on a passage of the text, seen by its
// maker alone. The passage is marked while reading; a click on it opens
// the note to read, change or delete it. The side column lists one's
// notes on the item, also those whose passage the text no longer holds.
import { Plugin, PluginKey, Decoration, DecorationSet } from "../vendor/prosemirror.js";
import { h, when } from "./dom.js";
import { t } from "./i18n.js";
import { get, post, put, del } from "./api.js";
import { locate } from "./anchors.js";

const key = new PluginKey("notes");

// personalNotes(path, initial) gives the plugin that marks the notes'
// passages, start(anchor) to write a note on a selected passage, and the
// section that lists them.
export function personalNotes(path, initial) {
  let notes = initial;
  let found = new Set();
  const views = new Set();
  const list = h("ul", { class: "notes-list", id: "notes-list" });
  const count = h("span", { class: "muted" });
  const section = h("section", { class: "side-card", id: "notes" },
    h("h2", {}, t("note.title"), " ", count),
    h("p", { class: "muted notes-private", text: t("note.private") }),
    list);

  const refresh = async () => {
    notes = (await get(path + "/notes")).notes;
    for (const v of views) v.dispatch(v.state.tr.setMeta(key, true));
    show();
  };

  // open shows a note to write or change: the passage, the text, and who
  // may see it, which is the reader alone.
  const open = (note, anchor) => {
    const text = h("textarea", { id: "note-text", rows: 5, maxlength: 4000, "aria-label": t("note.text"), value: note ? note.text : "" });
    const status = h("p", { class: "error", role: "alert", hidden: true });
    const fail = (err) => { status.textContent = err.message; status.hidden = false; };
    const done = () => dialog.close();
    const save = h("button", { type: "submit", class: "primary", id: "note-save", text: t("note.save") });
    const remove = note ? h("button", { type: "button", class: "danger", id: "note-delete", text: t("common.delete"), onclick: async () => {
      if (!confirm(t("note.deleteConfirm"))) return;
      try { await del(`${path}/notes/${note.id}`); done(); await refresh(); } catch (err) { fail(err); }
    } }) : null;
    const f = h("form", { method: "dialog", class: "note-form" },
      h("blockquote", { class: "note-quote", text: (note ? note.anchor : anchor).quote }),
      text,
      h("p", { class: "muted", text: t("note.private") }),
      note ? h("p", { class: "muted", text: t("note.when", { created: when(note.created), updated: when(note.updated) }) }) : null,
      status,
      h("div", { class: "actions-row" }, save, remove,
        h("button", { type: "button", class: "link", id: "note-cancel", text: t("common.cancel"), onclick: done })));
    f.addEventListener("submit", async (ev) => {
      ev.preventDefault();
      try {
        if (note) await put(`${path}/notes/${note.id}`, { text: text.value });
        else await post(path + "/notes", { anchor, text: text.value });
        done();
        await refresh();
      } catch (err) { fail(err); }
    });
    const dialog = h("dialog", { class: "note-dialog", id: "note-dialog", "aria-label": t(note ? "note.edit" : "note.new") },
      h("div", { class: "drawing-bar" }, h("strong", { text: t(note ? "note.edit" : "note.new") })), f);
    dialog.addEventListener("close", () => dialog.remove());
    document.body.append(dialog);
    dialog.showModal();
    text.focus();
  };

  const reveal = (id) => {
    const marks = document.querySelectorAll(`.note-anchor[data-note="${CSS.escape(id)}"]`);
    if (!marks.length) return;
    marks[0].scrollIntoView({ block: "center" });
    marks.forEach((m) => m.classList.add("note-focus"));
    setTimeout(() => marks.forEach((m) => m.classList.remove("note-focus")), 1500);
  };

  const show = () => {
    count.textContent = `(${notes.length})`;
    list.replaceChildren(...(notes.length ? notes.map((n) => h("li", { class: "note", "data-note": n.id },
      h("blockquote", { class: "note-quote" }, found.has(n.id)
        ? h("button", { type: "button", class: "link", text: n.anchor.quote, title: t("note.showPassage"), onclick: () => reveal(n.id) })
        : h("span", { text: n.anchor.quote })),
      found.has(n.id) ? null : h("p", { class: "muted note-gone", text: t("note.passageGone") }),
      h("p", { class: "note-text", text: n.text }),
      h("p", {}, h("button", { type: "button", class: "link note-open", text: t("common.edit"), onclick: () => open(n) }))))
      : [h("li", { class: "muted", text: t("note.none") })]));
  };

  const decorate = (doc) => {
    const decos = [];
    const ids = new Set();
    for (const n of notes) {
      const r = locate(doc, n.anchor);
      if (!r || r.from >= r.to) continue;
      ids.add(n.id);
      decos.push(Decoration.inline(r.from, r.to, { class: "note-anchor", "data-note": n.id, title: t("note.mark") }));
    }
    return { set: DecorationSet.create(doc, decos), ids };
  };
  const plugin = new Plugin({
    key,
    state: {
      init: (_, state) => decorate(state.doc),
      apply: (tr, old) => (tr.docChanged || tr.getMeta(key) ? decorate(tr.doc) : old),
    },
    props: {
      decorations: (state) => key.getState(state).set,
      handleClick: (view, pos, ev) => {
        const hit = ev.target.closest?.(".note-anchor");
        if (!hit) return false;
        const n = notes.find((x) => x.id === hit.dataset.note);
        if (n) open(n);
        return false;
      },
    },
    view: (view) => {
      views.add(view);
      const report = () => {
        const { ids } = key.getState(view.state);
        if (ids.size !== found.size || [...ids].some((id) => !found.has(id))) { found = ids; show(); }
      };
      report();
      return { update: report, destroy: () => views.delete(view) };
    },
  });
  show();
  return { plugin, section, start: (anchor) => open(null, anchor) };
}
