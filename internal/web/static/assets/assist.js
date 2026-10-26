// Writing aids of the editor that work as in common word processors
// (ADR-0096): a dialog for links (Ctrl+K), web addresses that become links
// as they are typed or pasted, "@" that suggests people and items, a bubble
// on a link to open, edit or remove it, and a dialog to embed an item.
import { Plugin } from "../vendor/prosemirror.js";
import { get } from "./api.js";
import { h } from "./dom.js";
import { t } from "./i18n.js";

// hrefOf turns what was typed as an address into a link target: a bare
// www. address is taken as https, a bare e-mail address as mailto.
export function hrefOf(text) {
  const v = text.trim();
  if (/^www\.[^\s]+\.[^\s]+$/i.test(v)) return "https://" + v;
  if (/^[^\s@/:]+@[^\s@/]+\.[^\s@/]+$/.test(v)) return "mailto:" + v;
  return v;
}

// A word that reads as a web address. Trailing punctuation is not part of it.
const addressRE = /^(?:https?:\/\/[^\s]+|www\.[^\s]+\.[^\s]+)$/i;
const trailing = /[.,;:!?)\]'"»]+$/;

const people = (q) => get("/api/users/search?q=" + encodeURIComponent(q)).then((r) => r.users || []).catch(() => []);
const items = (q) => (q.trim() ? get("/api/search?limit=6&q=" + encodeURIComponent(q)).then((r) => (r.hits || []).map((x) => x.item)).catch(() => []) : Promise.resolve([]));

// linkRange is the extent of the link at pos in its text block, or null.
function linkRange(state, pos, type) {
  const $pos = state.doc.resolve(pos);
  if (!$pos.parent.isTextblock) return null;
  const start = $pos.start();
  const runs = [];
  $pos.parent.forEach((child, offset) => {
    const mark = child.marks.find((m) => m.type === type);
    const from = start + offset;
    const to = from + child.nodeSize;
    const last = runs[runs.length - 1];
    if (mark && last && last.to === from && last.mark.eq(mark)) last.to = to;
    else if (mark) runs.push({ from, to, mark });
  });
  return runs.find((r) => r.from <= pos && pos <= r.to) || null;
}

// markdownFileRE is the address of a Markdown file as a repository's host
// shows it (ADR-0138).
const markdownFileRE = /^https?:\/\/[^\s]+\/(blob|raw|src|browse|-\/blob|-\/raw)\/[^\s]*\.(md|markdown)([?#][^\s]*)?$|^https:\/\/raw\.githubusercontent\.com\/[^\s]+\.(md|markdown)$/i;

// assist makes the aids for a schema; isSafe decides which link targets
// the model keeps (ADR-0014). showFile(href), if given, shows a Markdown
// file a link leads to instead of the link (ADR-0138).
export function assist(schema, isSafe, { showFile } = {}) {
  const link = schema.marks.link;

  // autolink marks the address before pos as a link, as typing a space or
  // Enter after it does in a word processor.
  const autolink = (state, pos) => {
    const $pos = state.doc.resolve(pos);
    if (!$pos.parent.isTextblock || $pos.parent.type.spec.code) return null;
    const before = $pos.parent.textBetween(0, $pos.parentOffset, null, "￼");
    const word = before.match(/(\S+)$/)?.[1];
    if (!word) return null;
    const bare = word.replace(trailing, "");
    if (!addressRE.test(bare)) return null;
    const href = hrefOf(bare);
    if (!isSafe(href)) return null;
    const from = pos - word.length;
    const to = from + bare.length;
    if (state.doc.rangeHasMark(from, to, link)) return null;
    return state.tr.addMark(from, to, link.create({ href }));
  };

  const autolinkPlugin = new Plugin({
    props: {
      handleTextInput(view, from, to, text) {
        if (/^\s$/.test(text)) {
          const tr = autolink(view.state, from);
          if (tr) view.dispatch(tr);
        }
        return false;
      },
      handleKeyDown(view, ev) {
        if (ev.key === "Enter" && view.state.selection.empty) {
          const tr = autolink(view.state, view.state.selection.from);
          if (tr) view.dispatch(tr);
        }
        return false;
      },
      // An address pasted on its own becomes a link; pasted on selected
      // text, it links that text.
      handlePaste(view, ev) {
        const text = ev.clipboardData?.getData("text/plain")?.trim() || "";
        if (!text || /\s/.test(text) || !(addressRE.test(text) || /^mailto:/i.test(text))) return false;
        const href = hrefOf(text);
        if (!isSafe(href)) return false;
        const { from, to, empty } = view.state.selection;
        const tr = empty
          ? view.state.tr.replaceSelectionWith(schema.text(text, [link.create({ href })]), false)
          : view.state.tr.addMark(from, to, link.create({ href }));
        view.dispatch(tr.scrollIntoView());
        return true;
      },
      // Ctrl+click (Cmd+click) opens a link while editing.
      handleClick(view, pos, ev) {
        if (!(ev.ctrlKey || ev.metaKey)) return false;
        const r = linkRange(view.state, pos, link);
        if (!r) return false;
        window.open(r.mark.attrs.href, "_blank", "noopener,noreferrer");
        return true;
      },
    },
  });

  // dialog shows a modal dialog and resolves with what submit returns, or
  // null when it is cancelled; ok labels its button.
  const dialog = (heading, body, { submit, extra = [], ok = t("common.ok") }) => new Promise((resolve) => {
    const error = h("p", { class: "error", role: "alert" });
    const form = h("form", { method: "dialog" },
      h("h2", { text: heading }), ...body, error,
      h("div", { class: "pm-dialog-actions" }, ...extra,
        h("button", { type: "button", class: "link", text: t("common.cancel"), onclick: () => close(null) }),
        h("button", { type: "submit", class: "primary", text: ok })));
    const el = h("dialog", { class: "pm-dialog" }, form);
    let done = false;
    const close = (v) => {
      if (done) return;
      done = true;
      el.close();
      el.remove();
      resolve(v);
    };
    form.addEventListener("submit", (ev) => {
      ev.preventDefault();
      error.textContent = "";
      try {
        const v = submit();
        if (v !== undefined) close(v);
      } catch (err) {
        error.textContent = err.message;
      }
    });
    el.addEventListener("cancel", (ev) => { ev.preventDefault(); close(null); });
    el.closeWith = close;
    document.body.append(el);
    el.showModal();
  });

  // results lists items found for what is typed in input; a choice calls
  // pick(item).
  const results = (input, pick) => {
    const list = h("ul", { class: "pm-results", role: "listbox", "aria-label": t("assist.pages") });
    let timer = 0;
    let asked = 0;
    const find = () => {
      clearTimeout(timer);
      const q = input.value.trim();
      if (!q || addressRE.test(q) || /^mailto:/i.test(q)) {
        list.replaceChildren();
        return;
      }
      timer = setTimeout(async () => {
        const mine = ++asked;
        const found = await items(q);
        if (mine !== asked) return;
        list.replaceChildren(...found.map((it) => h("li", { role: "option" },
          h("button", { type: "button", class: "pm-result", "data-key": it.key, onclick: () => pick(it) },
            h("strong", { text: it.title || it.key }), " ", h("span", { class: "muted", text: `${it.key} · ${it.type}` })))));
        if (!found.length) list.append(h("li", { class: "muted", text: t("assist.noPages") }));
      }, 150);
    };
    input.addEventListener("input", find);
    return { list, find };
  };

  // openLink inserts or edits a link at the selection, as Ctrl+K does in a
  // word processor: the text shown and the address, a web address or a
  // page of the wiki found by typing. A page is linked as a mention of
  // the item, which follows it when it moves (ADR-0001).
  const openLink = async (view) => {
    const { state } = view;
    let { from, to } = state.selection;
    // Within a link, the whole link is edited.
    const found = linkRange(state, from, link);
    const existing = found && to <= found.to ? found : null;
    if (existing) ({ from, to } = existing);
    const shown = state.doc.textBetween(from, to, " ", " ");
    const text = h("input", { name: "text", value: shown, autocomplete: "off" });
    const address = h("input", { name: "address", value: existing?.mark.attrs.href || "", autocomplete: "off",
      placeholder: t("assist.addressHint"), required: true });
    let chosen = null;
    const { list } = results(address, (it) => {
      chosen = it;
      address.value = `${it.key} · ${it.title}`;
      if (!text.value.trim()) text.value = it.title || it.key;
      list.replaceChildren();
      address.focus();
    });
    address.addEventListener("input", () => { chosen = null; });
    const remove = existing ? h("button", { type: "button", class: "link danger", id: "pm-link-remove", text: t("assist.removeLink"), onclick: () => {
      view.dispatch(view.state.tr.removeMark(existing.from, existing.to, link));
      address.closest("dialog").closeWith(false);
    } }) : null;
    const result = dialog(existing ? t("assist.editLink") : t("assist.insertLink"), [
      h("label", { class: "field" }, h("span", { text: t("assist.textToShow") }), text),
      h("label", { class: "field" }, h("span", { text: t("assist.address") }), address),
      list,
    ], {
      extra: remove ? [remove] : [],
      submit: () => {
        const shownText = text.value;
        if (chosen) return { item: chosen, text: shownText.trim() || chosen.title || chosen.key };
        const href = hrefOf(address.value);
        if (!isSafe(href)) throw new Error(t("editor.linkNeedsURL"));
        return { href, text: shownText || address.value.trim() };
      },
    });
    setTimeout(() => (shown ? address : text).focus(), 0);
    const v = await result;
    view.focus();
    if (!v) return;
    const tr = view.state.tr;
    if (v.item) {
      tr.replaceWith(from, to, schema.nodes.mention.create({ ref: v.item.id, label: v.text }));
    } else if (v.text === shown && from < to) {
      tr.removeMark(from, to, link).addMark(from, to, link.create({ href: v.href }));
    } else {
      const marks = view.state.doc.resolve(from).marks().filter((m) => m.type !== link);
      tr.replaceWith(from, to, schema.text(v.text, [...marks, link.create({ href: v.href })]));
    }
    view.dispatch(tr.scrollIntoView());
  };

  // pickItem asks for an item found by typing, e.g. to embed it.
  const pickItem = async (heading) => {
    const input = h("input", { name: "q", autocomplete: "off", placeholder: t("assist.searchPages"), "aria-label": t("assist.searchPages") });
    let chosen = null;
    let el = null;
    const { list } = results(input, (it) => {
      chosen = it;
      el.closeWith(it);
    });
    const p = dialog(heading, [input, list], { submit: () => {
      if (!chosen) throw new Error(t("assist.choosePage"));
      return chosen;
    } });
    el = input.closest("dialog");
    setTimeout(() => input.focus(), 0);
    return p;
  };

  // The bubble under a link while editing: its address, to open it, to
  // edit or to remove it.
  const bubblePlugin = new Plugin({
    view(view) {
      const href = h("a", { class: "pm-bubble-href", target: "_blank", rel: "noopener noreferrer" });
      const file = h("button", { type: "button", class: "link", id: "pm-link-show-file", hidden: true, text: t("assist.showFile"), onmousedown: (ev) => {
        ev.preventDefault();
        const r = linkRange(view.state, view.state.selection.from, link);
        if (r) showFile(r.mark.attrs.href);
      } });
      const bubble = h("div", { class: "pm-bubble", id: "pm-link-bubble", hidden: true },
        href, file,
        h("button", { type: "button", class: "link", text: t("assist.edit"), onmousedown: (ev) => { ev.preventDefault(); openLink(view); } }),
        h("button", { type: "button", class: "link danger", text: t("assist.remove"), onmousedown: (ev) => {
          ev.preventDefault();
          const r = linkRange(view.state, view.state.selection.from, link);
          if (r) view.dispatch(view.state.tr.removeMark(r.from, r.to, link));
        } }));
      document.body.append(bubble);
      const update = (v) => {
        const sel = v.state.selection;
        const r = v.editable && v.hasFocus() && sel.empty ? linkRange(v.state, sel.from, link) : null;
        if (!r) {
          bubble.hidden = true;
          return;
        }
        href.href = r.mark.attrs.href;
        href.textContent = r.mark.attrs.href.replace(/^mailto:/, "");
        file.hidden = !showFile || !markdownFileRE.test(r.mark.attrs.href);
        const at = v.coordsAtPos(r.from);
        bubble.hidden = false;
        bubble.style.left = Math.max(8, Math.min(at.left, window.innerWidth - bubble.offsetWidth - 8)) + "px";
        bubble.style.top = at.bottom + 6 + "px";
      };
      const blur = () => setTimeout(() => update(view), 150);
      view.dom.addEventListener("blur", blur);
      return { update, destroy: () => { view.dom.removeEventListener("blur", blur); bubble.remove(); } };
    },
  });

  // "@" followed by part of a name suggests people and items. A person is
  // written as @username, which tells them (ADR-0084, ADR-0096); an item
  // becomes a mention of it.
  let atKeys = () => false;
  const typedAt = (state) => {
    const sel = state.selection;
    if (!sel.empty || !sel.$from.parent.isTextblock || sel.$from.parent.type.spec.code) return null;
    const before = sel.$from.parent.textBetween(Math.max(0, sel.$from.parentOffset - 64), sel.$from.parentOffset, null, "￼");
    const m = before.match(/(?:^|[\s(])@([^\s@￼]{0,40})$/u);
    return m ? { query: m[1], from: sel.from - m[1].length - 1, to: sel.from } : null;
  };
  const atPlugin = new Plugin({
    view(view) {
      const list = h("ul", { class: "pm-at", id: "pm-at", role: "listbox", hidden: true, "aria-label": t("assist.mention") });
      document.body.append(list);
      let options = [];
      let active = 0;
      let timer = 0;
      let asked = 0;
      let at = null;
      let dismissed = -1;
      const mark = (i) => {
        active = options.length ? (i + options.length) % options.length : 0;
        options.forEach((o, j) => o.el.setAttribute("aria-selected", String(j === active)));
      };
      const choose = (o) => {
        const cur = typedAt(view.state);
        if (!cur) return;
        const tr = view.state.tr;
        if (o.person) tr.insertText("@" + o.person.username + " ", cur.from, cur.to);
        else tr.replaceWith(cur.from, cur.to, [schema.nodes.mention.create({ ref: o.item.id, label: "@" + (o.item.title || o.item.key) }), schema.text(" ")]);
        view.dispatch(tr.scrollIntoView());
        hide();
        view.focus();
      };
      const hide = () => {
        list.hidden = true;
        options = [];
        at = null;
      };
      const show = (found) => {
        const group = (text) => h("li", { role: "presentation", class: "pm-at-group", text });
        options = [];
        const option = (o, ...children) => {
          o.el = h("li", { role: "option", class: "pm-at-option", "aria-selected": "false", onmousedown: (ev) => { ev.preventDefault(); choose(o); } }, ...children);
          options.push(o);
          return o.el;
        };
        list.replaceChildren(
          ...(found.people.length ? [group(t("assist.people")), ...found.people.map((p) => option({ person: p }, h("strong", { text: "@" + p.username })))] : []),
          ...(found.items.length ? [group(t("assist.pages")), ...found.items.map((it) => option({ item: it },
            h("strong", { text: it.title || it.key }), " ", h("span", { class: "muted", text: it.key })))] : []));
        if (!options.length) list.append(h("li", { class: "muted pm-at-none", text: at.query ? t("assist.noneFound") : t("assist.typeName") }));
        mark(0);
        const c = view.coordsAtPos(at.from);
        list.hidden = false;
        list.style.left = Math.max(8, Math.min(c.left, window.innerWidth - list.offsetWidth - 8)) + "px";
        list.style.top = c.bottom + 4 + "px";
      };
      const update = (v) => {
        const cur = v.editable && v.hasFocus() ? typedAt(v.state) : null;
        if (!cur || cur.from === dismissed) {
          if (!cur) dismissed = -1;
          hide();
          return;
        }
        if (at && at.from === cur.from && at.query === cur.query) return;
        at = cur;
        clearTimeout(timer);
        timer = setTimeout(async () => {
          const mine = ++asked;
          const q = cur.query;
          const found = q ? { people: (await people(q)).slice(0, 5), items: await items(q) } : { people: [], items: [] };
          if (mine !== asked || !at || at.query !== q) return;
          show(found);
        }, cur.query ? 150 : 0);
      };
      atKeys = (ev) => {
        if (list.hidden) return false;
        if (ev.key === "Escape") { dismissed = at?.from ?? -1; hide(); return true; }
        if (!options.length) return false;
        if (ev.key === "ArrowDown") { mark(active + 1); return true; }
        if (ev.key === "ArrowUp") { mark(active - 1); return true; }
        if (ev.key === "Enter" || ev.key === "Tab") { choose(options[active]); return true; }
        return false;
      };
      const blur = () => setTimeout(() => { if (!view.hasFocus()) hide(); }, 150);
      view.dom.addEventListener("blur", blur);
      return { update, destroy: () => { view.dom.removeEventListener("blur", blur); list.remove(); } };
    },
    props: {
      handleKeyDown(view, ev) {
        return atKeys(ev);
      },
    },
  });

  return { plugins: [atPlugin, autolinkPlugin, bubblePlugin], openLink, pickItem, dialog };
}
