// The page tree of a space (ADR-0041): one level at a time from
// GET /api/spaces/{space}/tree, opened down to the page shown. Dragging a
// page onto the upper edge of another places it before that page; onto the
// rest of it, as its last child. The server decides who may move what.
import { get, post } from "./api.js";
import { h } from "./dom.js";
import { t } from "./i18n.js";

// pageTree returns the tree as a <details>: a sidebar on wide screens, a
// block that folds away on narrow ones. current is the ID of the page
// shown, open the IDs of its ancestors; onMoved runs after a page moved,
// onError with a failed request. A new page is made with the buttons of
// the page head, the bell last among them (ADR-0112).
export function pageTree({ space, current = "", open = [], onMoved, onError }) {
  const opened = new Set(open);
  // A list of links with disclosure buttons, not an ARIA tree: that role
  // promises arrow-key navigation this tree does not have.
  const root = h("ul", { class: "tree" });
  // Folded on narrow screens, where it would push the page down.
  const wide = !window.matchMedia || window.matchMedia("(min-width: 801px)").matches;
  const box = h("details", { class: "tree-box", open: wide }, h("summary", { text: t("tree.title") }),
    h("nav", { "aria-label": t("tree.title") }, root));
  let dragged = null;

  const base = `/api/spaces/${encodeURIComponent(space)}/tree`;

  // level fills ul with the children of parent ("" for the top), page by page.
  async function level(parent, ul, after = "") {
    const q = new URLSearchParams();
    if (parent) q.set("parent", parent);
    if (after) q.set("after", after);
    const page = await get(base + (q.size ? "?" + q : ""));
    if (!after && page.items.length === 0 && !parent) {
      ul.append(h("li", { class: "muted", text: t("tree.empty") }));
    }
    for (const n of page.items) ul.append(node(n, parent));
    if (page.next) {
      const more = h("li", {}, h("button", { class: "link", text: t("common.more"), onclick: () => {
        more.remove();
        level(parent, ul, page.next).catch(onError);
      } }));
      ul.append(more);
    }
  }

  function node(n, parent) {
    const kids = h("ul", { hidden: true });
    const li = h("li", { "data-id": n.id });
    let loaded = false;
    const toggle = n.hasChildren
      ? h("button", { class: "tree-toggle", type: "button", "aria-label": t("tree.expand", { title: n.title }), text: "▸" })
      : h("span", { class: "tree-toggle" });
    const setOpen = async (on) => {
      if (on && !loaded) {
        loaded = true;
        await level(n.id, kids);
      }
      kids.hidden = !on;
      li.classList.toggle("open", on);
      toggle.setAttribute("aria-expanded", String(on));
      toggle.textContent = on ? "▾" : "▸";
      toggle.setAttribute("aria-label", on ? t("tree.collapse", { title: n.title }) : t("tree.expand", { title: n.title }));
    };
    if (n.hasChildren) {
      toggle.setAttribute("aria-expanded", "false");
      toggle.addEventListener("click", () => setOpen(kids.hidden).catch(onError));
    }
    const label = h("a", { href: "#/item/" + encodeURIComponent(n.key), text: n.title || n.key, title: n.key,
      class: n.id === current ? "current" : "", "aria-current": n.id === current ? "page" : "false" });
    const row = h("div", { class: "tree-row", draggable: "true" }, toggle, label);
    wireDrag(row, n, parent);
    li.append(row, kids);
    if (opened.has(n.id) && n.hasChildren) setOpen(true).catch(onError);
    return li;
  }

  function wireDrag(row, n, parent) {
    const where = (ev) => (ev.offsetY < row.offsetHeight * 0.3 ? "before" : "into");
    const clear = () => row.classList.remove("drop-before", "drop-into");
    row.addEventListener("dragstart", (ev) => {
      dragged = n;
      ev.dataTransfer.effectAllowed = "move";
      ev.dataTransfer.setData("text/plain", n.key);
    });
    row.addEventListener("dragend", () => { dragged = null; });
    row.addEventListener("dragover", (ev) => {
      if (!dragged || dragged.id === n.id) return;
      ev.preventDefault();
      clear();
      row.classList.add("drop-" + where(ev));
    });
    row.addEventListener("dragleave", clear);
    row.addEventListener("drop", async (ev) => {
      ev.preventDefault();
      clear();
      if (!dragged || dragged.id === n.id) return;
      const body = where(ev) === "before"
        ? { space, parent, before: n.id }
        : { space, parent: n.id, before: "" };
      const moving = dragged;
      dragged = null;
      try {
        await post(`/api/items/${moving.id}/move`, body);
        if (onMoved) onMoved();
      } catch (err) {
        if (onError) onError(err);
      }
    });
  }

  level("", root).catch(onError);
  return box;
}

// ancestors returns the IDs above an item, nearest first, up to the first
// one the user may not read.
export async function ancestors(item) {
  const out = [];
  for (let p = item.parent; p && out.length < 50; ) {
    out.push(p);
    try {
      p = (await get("/api/items/" + encodeURIComponent(p))).item.parent;
    } catch {
      break;
    }
  }
  return out;
}
