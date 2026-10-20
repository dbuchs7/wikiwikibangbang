// The album (ADR-0054): the images and drawings of every item one may
// read, in one place. Inserting a picture into a body copies it into that
// body's item, where it takes on the item's key and access; the original
// stays where it is, unchanged.
import { h } from "./dom.js";
import { t } from "./i18n.js";
import { get } from "./api.js";
import { DRAWING } from "./drawing.js";

const fileURL = (p) => `/api/items/${p.item.id}/attachments/${p.attachment.id}`;

// captionOf makes a caption from a file name.
export const captionOf = (name) => (name.endsWith(DRAWING) ? name.slice(0, -DRAWING.length) : name.replace(/\.[^.]*$/, ""));

// albumBrowser shows the album with its filters. With pick, each picture
// has a button that hands it to pick; without, a picture leads to its item.
export function albumBrowser({ pick } = {}) {
  const search = h("input", { type: "search", placeholder: t("album.search"), "aria-label": t("album.search") });
  const kind = h("select", { "aria-label": t("album.kind") },
    [["", t("album.kindAll")], ["image", t("album.kindImages")], ["drawing", t("album.kindDrawings")]].map(([v, label]) => h("option", { value: v, text: label })));
  const space = h("select", { "aria-label": t("album.space") }, h("option", { value: "", text: t("album.spaceAll") }));
  const grid = h("ul", { class: "album-grid" });
  const count = h("p", { class: "muted", role: "status" });
  const more = h("button", { type: "button", text: t("album.more"), hidden: true });
  const status = h("div", { class: "status" });
  let next = 0;
  let generation = 0;

  const card = (p) => h("li", { class: "album-card" },
    h("figure", {},
      h("img", { src: fileURL(p), alt: captionOf(p.attachment.name), loading: "lazy" }),
      h("figcaption", {},
        h("span", { class: "album-name", text: captionOf(p.attachment.name) }),
        p.drawing ? h("span", { class: "chip", text: t("album.drawing") }) : null,
        h("a", { href: "#/item/" + encodeURIComponent(p.item.key), class: "muted album-item", text: `${p.item.key} ${p.item.title}`,
          target: pick ? "_blank" : null, rel: pick ? "noopener" : null }))),
    pick ? h("button", { type: "button", class: "primary album-pick", text: t("album.insert"), onclick: () => pick(p) }) : null);

  // load shows the first page of the filtered album, or with append the
  // next. A later filter replaces an answer still on its way.
  const load = async (append) => {
    const mine = ++generation;
    const q = new URLSearchParams({ offset: String(append ? next : 0) });
    if (search.value.trim()) q.set("q", search.value.trim());
    if (kind.value) q.set("kind", kind.value);
    if (space.value) q.set("space", space.value);
    status.replaceChildren();
    try {
      const page = await get("/api/album?" + q);
      if (mine !== generation) return;
      if (!append) grid.replaceChildren();
      grid.append(...page.pictures.map(card));
      next = page.next;
      more.hidden = !page.next;
      count.textContent = page.total ? t("album.count", { n: page.total }) : t("album.none");
    } catch (err) {
      status.replaceChildren(h("p", { class: "error", role: "alert", text: err.message }));
    }
  };
  let timer = null;
  search.addEventListener("input", () => { clearTimeout(timer); timer = setTimeout(() => load(false), 250); });
  kind.addEventListener("change", () => load(false));
  space.addEventListener("change", () => load(false));
  more.addEventListener("click", () => load(true));
  get("/api/spaces").then(({ spaces }) => space.append(...spaces.map((s) => h("option", { value: s.key, text: `${s.key} ${s.name}` })))).catch(() => {});
  load(false);
  return h("div", { class: "album" }, h("div", { class: "album-filters" }, search, kind, space), count, status, grid, more);
}

// pickFromAlbum shows the album in a dialog and resolves to the chosen
// picture, or to null when it is closed.
export function pickFromAlbum() {
  return new Promise((resolve) => {
    let done = false;
    const finish = (p) => {
      if (done) return;
      done = true;
      dialog.close();
      dialog.remove();
      resolve(p);
    };
    const dialog = h("dialog", { class: "album-dialog", "aria-label": t("album.title") },
      h("div", { class: "drawing-bar" }, h("strong", { text: t("album.title") }), h("button", { type: "button", text: t("draw.cancel"), onclick: () => finish(null) })),
      h("div", { class: "album-dialog-body" }, albumBrowser({ pick: finish })));
    dialog.addEventListener("cancel", () => finish(null));
    document.body.append(dialog);
    dialog.showModal();
  });
}

// pictureFile reads a picture as a file, to be stored in another item:
// one copies only what one may read, into what one may edit.
export async function pictureFile(p) {
  const res = await fetch(fileURL(p), { credentials: "same-origin" });
  if (!res.ok) throw new Error(res.statusText);
  return new File([await res.blob()], p.attachment.name, { type: p.attachment.type });
}
