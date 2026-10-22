// The album (ADR-0054): the images and drawings of every item one may
// read, in one place. Inserting a picture into a body copies it into that
// body's item, where it takes on the item's key and access; the original
// stays where it is, unchanged. Folders (ADR-0090) are items of their own
// that hold the pictures uploaded into them; pictures carry labels of
// their own, which only narrow the album.
import { h } from "./dom.js";
import { t } from "./i18n.js";
import { get, post, put, del, upload } from "./api.js";
import { DRAWING } from "./drawing.js";

const fileURL = (p) => `/api/items/${p.item.id}/attachments/${p.attachment.id}`;

// captionOf makes a caption from a file name.
export const captionOf = (name) => (name.endsWith(DRAWING) ? name.slice(0, -DRAWING.length) : name.replace(/\.[^.]*$/, ""));

// albumBrowser shows the album with its filters and folders. With pick,
// each picture has a button that hands it to pick; without, a picture
// leads to its item, and one who may edit it labels or deletes it there.
export function albumBrowser({ pick } = {}) {
  const search = h("input", { type: "search", placeholder: t("album.search"), "aria-label": t("album.search") });
  const kind = h("select", { "aria-label": t("album.kind") },
    [["", t("album.kindAll")], ["image", t("album.kindImages")], ["drawing", t("album.kindDrawings")]].map(([v, label]) => h("option", { value: v, text: label })));
  const space = h("select", { "aria-label": t("album.space") }, h("option", { value: "", text: t("album.spaceAll") }));
  const label = h("select", { "aria-label": t("album.label"), id: "album-label" }, h("option", { value: "", text: t("album.labelAll") }));
  const options = h("datalist", { id: "album-label-options" });
  const trail = h("nav", { class: "album-trail", "aria-label": t("album.folders") });
  const tools = h("div", { class: "album-tools" });
  const folders = h("ul", { class: "album-folders" });
  const grid = h("ul", { class: "album-grid" });
  const count = h("p", { class: "muted", role: "status" });
  const more = h("button", { type: "button", text: t("album.more"), hidden: true });
  const status = h("div", { class: "status" });
  let next = 0;
  let generation = 0;
  // path is the open folder and those above it; empty at the top.
  let path = [];
  const here = () => path[path.length - 1];
  const fail = (err) => status.replaceChildren(h("p", { class: "error", role: "alert", text: err.message }));

  // Labels are suggested from those of pictures and of items alike.
  const loadLabels = async () => {
    const [pics, items] = await Promise.all([get("/api/album/labels"), get("/api/labels").catch(() => ({ labels: [] }))]);
    const keep = label.value;
    label.replaceChildren(h("option", { value: "", text: t("album.labelAll") }),
      ...pics.labels.map((l) => h("option", { value: l.label, text: `${l.label} (${l.count})` })));
    label.value = pics.labels.some((l) => l.label === keep) ? keep : "";
    const all = [...new Set([...pics.labels, ...items.labels].map((l) => l.label))];
    options.replaceChildren(...all.map((l) => h("option", { value: l })));
  };

  const labelsOf = (p) => {
    const chips = (p.attachment.labels || []).map((l) => h("button", { type: "button", class: "chip album-label", text: l,
      onclick: () => { label.value = l; load(false); } }));
    if (pick || !p.editable) return chips.length ? h("p", { class: "album-labels" }, ...chips) : null;
    const input = h("input", { class: "album-labels-input", list: "album-label-options", value: (p.attachment.labels || []).join(", "),
      placeholder: t("album.labelsPlaceholder"), "aria-label": t("album.labels") });
    input.addEventListener("change", async () => {
      const labels = input.value.split(",").map((l) => l.trim()).filter(Boolean);
      try {
        await put(`/api/items/${p.item.id}/attachments/${p.attachment.id}/labels`, { labels });
        status.replaceChildren();
        await loadLabels();
        load(false);
      } catch (err) { fail(err); }
    });
    return h("div", { class: "album-labels" }, ...chips, input);
  };

  const card = (p) => h("li", { class: "album-card", "data-attachment": p.attachment.id },
    h("figure", {},
      h("img", { src: fileURL(p), alt: captionOf(p.attachment.name), loading: "lazy" }),
      h("figcaption", {},
        h("span", { class: "album-name", text: captionOf(p.attachment.name) }),
        p.drawing ? h("span", { class: "chip", text: t("album.drawing") }) : null,
        h("a", { href: "#/item/" + encodeURIComponent(p.item.key), class: "muted album-item", text: `${p.item.key} ${p.item.title}`,
          target: pick ? "_blank" : null, rel: pick ? "noopener" : null }))),
    labelsOf(p),
    pick ? h("button", { type: "button", class: "primary album-pick", text: t("album.insert"), onclick: () => pick(p) }) : null,
    // A picture its item's text shows is deleted there, not here.
    !pick && p.editable && !p.used ? h("button", { type: "button", class: "danger album-delete", text: t("album.delete"),
      onclick: async () => {
        if (!confirm(t("album.deleteConfirm", { name: p.attachment.name }))) return;
        try {
          await del(`/api/items/${p.item.id}/attachments/${p.attachment.id}`);
          await loadLabels();
          load(false);
        } catch (err) { fail(err); }
      } }) : null,
    !pick && p.editable && p.used ? h("p", { class: "muted album-used", text: t("album.used") }) : null);

  const showTrail = () => {
    const step = (name, to) => h("button", { type: "button", class: "link", text: name, onclick: () => { path = path.slice(0, to); open(); } });
    trail.replaceChildren(step(t("album.top"), 0), ...path.flatMap((f, i) => [" › ", i === path.length - 1 ? h("strong", { text: f.name }) : step(f.name, i + 1)]));
  };

  const showTools = () => {
    if (pick) return tools.replaceChildren();
    const name = h("input", { id: "album-folder-name", placeholder: t("album.folderName"), "aria-label": t("album.folderName"), required: true });
    const make = h("form", { class: "album-new-folder", hidden: true }, name,
      h("button", { type: "submit", class: "primary", text: t("album.create") }));
    make.addEventListener("submit", async (e) => {
      e.preventDefault();
      if (!here() && !space.value) return fail(new Error(t("album.chooseSpace")));
      try {
        await post("/api/album/folders", here() ? { parent: here().id, name: name.value } : { space: space.value, name: name.value });
        status.replaceChildren();
        make.hidden = true;
        loadFolders();
      } catch (err) { fail(err); }
    });
    const files = h("input", { type: "file", accept: "image/png,image/jpeg,image/gif,image/webp", multiple: true, hidden: true, id: "album-files" });
    files.addEventListener("change", async () => {
      try {
        for (const f of files.files) await upload(`/api/items/${here().id}`, f);
        status.replaceChildren();
      } catch (err) { fail(err); }
      files.value = "";
      load(false);
    });
    // replaceChildren, unlike h, writes null as text.
    tools.replaceChildren(...[
      h("button", { type: "button", class: "secondary", id: "album-new-folder", text: t("album.newFolder"), onclick: () => { make.hidden = !make.hidden; if (!make.hidden) name.focus(); } }),
      here() ? h("button", { type: "button", class: "primary", id: "album-upload", text: t("album.upload"), onclick: () => files.click() }) : null,
      here() ? null : h("span", { class: "muted", text: t("album.uploadHint") }),
      files, make].filter(Boolean));
  };

  const loadFolders = async () => {
    const q = new URLSearchParams();
    if (here()) q.set("parent", here().id);
    else if (space.value) q.set("space", space.value);
    try {
      const { folders: list } = await get("/api/album/folders?" + q);
      folders.replaceChildren(...list.map((f) => h("li", {},
        h("button", { type: "button", class: "album-folder", "data-folder": f.id, onclick: () => { path = [...path, f]; open(); } },
          h("span", { class: "album-folder-icon", "aria-hidden": "true" }), h("span", { text: f.name })))));
    } catch (err) { fail(err); }
  };

  // load shows the first page of the filtered album, or with append the
  // next. A later filter replaces an answer still on its way.
  const load = async (append) => {
    const mine = ++generation;
    const q = new URLSearchParams({ offset: String(append ? next : 0) });
    if (search.value.trim()) q.set("q", search.value.trim());
    if (kind.value) q.set("kind", kind.value);
    if (here()) q.set("folder", here().id);
    else if (space.value) q.set("space", space.value);
    if (label.value) q.set("label", label.value);
    try {
      const page = await get("/api/album?" + q);
      if (mine !== generation) return;
      if (!append) grid.replaceChildren();
      grid.append(...page.pictures.map(card));
      next = page.next;
      more.hidden = !page.next;
      count.textContent = page.total ? t("album.count", { n: page.total }) : t("album.none");
    } catch (err) { fail(err); }
  };

  // open shows the folder path ends in, or the top.
  const open = () => {
    status.replaceChildren();
    space.disabled = !!here();
    showTrail();
    showTools();
    loadFolders();
    load(false);
  };
  let timer = null;
  search.addEventListener("input", () => { clearTimeout(timer); timer = setTimeout(() => load(false), 250); });
  kind.addEventListener("change", () => load(false));
  label.addEventListener("change", () => load(false));
  space.addEventListener("change", () => { loadFolders(); load(false); });
  more.addEventListener("click", () => load(true));
  get("/api/spaces").then(({ spaces }) => space.append(...spaces.map((s) => h("option", { value: s.key, text: `${s.key} ${s.name}` })))).catch(() => {});
  loadLabels().catch(() => {});
  open();
  return h("div", { class: "album" }, h("div", { class: "album-filters" }, search, kind, space, label), options,
    trail, tools, status, folders, count, grid, more);
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
