// The UI shell (ADR-0009, ADR-0031): hash routes, one view per route, all
// data through the HTTP API. Plain ES modules, no build step.
import { api, get, post, put, del, download, upload, ApiError } from "./api.js";
import { h, mount, when } from "./dom.js";
import { createEditor } from "./editor.js";
import { liveBody } from "./live.js";
import { pageTree, ancestors } from "./tree.js";
import { renderDiff } from "./diff.js";
import { commentPlugin } from "./anchors.js";
import { isDrawing } from "./drawing.js";
import { albumBrowser } from "./album.js";
import { t, setLanguage, suggestedLanguage, languages, language } from "./i18n.js";

const app = document.getElementById("app");
const nav = document.getElementById("nav");
let me = null; // the signed-in principal, from GET /api/me

// --- shared pieces -------------------------------------------------------

function errorBox(err) {
  return h("p", { class: "error", role: "alert", text: err instanceof ApiError ? err.message : String(err) });
}

// form wires a submit handler that shows errors next to the form and keeps
// the button from being pressed twice.
function form(attrs, children, onSubmit) {
  const status = h("div", { class: "status" });
  const f = h("form", attrs, children, status);
  f.addEventListener("submit", async (ev) => {
    ev.preventDefault();
    const button = f.querySelector("button[type=submit]");
    if (button) button.disabled = true;
    status.replaceChildren();
    try {
      await onSubmit(new FormData(f), status);
    } catch (err) {
      status.replaceChildren(errorBox(err));
    } finally {
      if (button) button.disabled = false;
    }
  });
  return f;
}

function field(label, input) {
  return h("label", { class: "field" }, h("span", { text: label }), input);
}

// link is an anchor; cls styles it, e.g. "button" for an action that
// navigates.
function link(href, text, cls, id) {
  return h("a", { href, text, class: cls || null, id: id || null });
}

// --- watching (ADR-0046) --------------------------------------------------------

// watchButton starts or stops watching; path is the item's or space's.
// It is a bell: filled while one watches, outlined while one does not; its
// name and tooltip say what a press does.
function watchButton(path, watching, status) {
  const label = watching ? t("watch.stop") : t("watch.start");
  return h("button", { type: "button", class: "watch-bell", id: "watch", "aria-pressed": String(watching), "aria-label": label, title: label,
    onclick: () => action(() => (watching ? del(path + "/watch") : put(path + "/watch")), status) }, bellIcon(watching));
}

// what says what a notification's change was.
function what(n) {
  if (n.created) return t("notif.child");
  switch (n.kind) {
    case "ItemCreated": return t("notif.created");
    case "ItemImported": return t("notif.imported");
    case "TitleSet": return t("notif.renamed");
    case "BodyCommitted": return t("notif.edited");
    case "PropertySet": return t("notif.field");
    case "Transitioned": return t("notif.state");
    case "ItemMoved": return t("notif.moved");
    case "ItemTrashed": return t("notif.trashed");
    case "ItemRestored": return t("notif.restored");
    case "CommentAdded": return t("notif.commented");
    case "LabelsSet": return t("notif.labels");
    case "AttachmentAdded": return t("notif.attached");
    case "RestrictionSet": return t("notif.restrictedSensitive");
    case "HoldSet": return t("notif.held");
    case "HoldReleased": return t("notif.released");
    default: return n.kind;
  }
}

// notificationsView lists what others changed on what one watches, and
// marks it seen.
async function notificationsView() {
  const show = screen();
  const [first, { watches }] = await Promise.all([get("/api/notifications?limit=50&findings=1"), get("/api/me/watches")]);
  const list = h("ul", { class: "notifications", id: "notifications" });
  const more = h("div");
  const add = async (page) => {
    await learnNames(page.notifications.map((n) => n.actor));
    for (const n of page.notifications) {
      const governance = n.actor === "governance";
      list.append(h("li", { class: [n.unread ? "unread" : "", governance ? "governance" : ""].join(" ").trim() },
        h("span", { text: `${governance ? t("gov.title") : person(n.actor)} ${what(n)} ` }),
        link("#/item/" + encodeURIComponent(n.key), `${n.key} ${n.title}`),
        h("span", { class: "muted", text: " · " + when(n.at) })));
    }
    more.replaceChildren();
    if (page.next) more.append(h("button", { text: t("common.more"), onclick: async () => add(await get(`/api/notifications?limit=50&before=${page.next}`)) }));
  };
  await add(first);
  if (first.notifications.length && first.notifications[0].seq > first.seen) {
    post("/api/notifications/seen", { seq: first.notifications[0].seq }).then(() => renderNav()).catch(() => {});
  }
  // The governance findings one is responsible for, until they are
  // resolved (ADR-0057).
  const f = first.findings;
  const kindName = { sensitive: t("gov.colSensitive"), stale: t("gov.colStale"), implausible: t("gov.colImplausible"), personal: t("gov.colPersonal") };
  const findings = f.items?.length ? h("section", { id: "my-findings" },
    h("h2", {}, t("notif.findings", { n: f.sensitive + f.stale + f.implausible + (f.personal || 0) }), " ", link("#/governance", t("notif.findingsAll"))),
    h("p", { class: "muted", text: t("notif.findingsNote") }),
    h("ul", { class: "notifications" }, f.items.map((x) => h("li", { class: "finding-" + x.kind },
      h("span", { class: "chip", text: kindName[x.kind] }), " ",
      link("#/item/" + encodeURIComponent(x.key), `${x.key} ${x.title}`),
      h("span", { class: "muted", text: " · " + (x.kind === "stale" ? t("notif.unchangedSince", { date: when(x.since) }) : x.detail) }),
      x.release ? h("strong", { class: "finding-release", text: " · " + t("dp.yourRelease") }) : null)))) : null;
  show(
    h("h1", { text: t("notif.title") }),
    findings,
    findings ? h("h2", { text: t("notif.changes") }) : null,
    first.notifications.length ? list : h("p", { class: "muted", text: watches.length ? t("notif.none") : t("notif.noWatches") }),
    more,
    h("h2", { text: t("watch.title") }),
    watches.length
      ? h("ul", { class: "list", id: "watches" }, watches.map((w) => h("li", {}, w.item
        ? link("#/item/" + encodeURIComponent(w.key), `${w.key} ${w.title}`)
        : [t("watch.space") + " ", link("#/space/" + encodeURIComponent(w.space), w.space)])))
      : h("p", { class: "muted", text: t("watch.none") }),
  );
}

// --- labels (ADR-0044) ----------------------------------------------------------

function labelChip(label, onRemove) {
  return h("span", { class: "label" },
    h("a", { href: "#/label/" + encodeURIComponent(label), text: label }),
    onRemove ? h("button", { type: "button", class: "label-remove", title: t("label.remove", { label }), "aria-label": t("label.remove", { label }), text: "×", onclick: onRemove }) : null);
}

// labelsLine shows an item's labels; on a live item they can be added and
// removed.
function labelsLine(path, item, live, status) {
  const set = (labels) => action(() => put(path + "/labels", { labels }), status);
  const chips = item.labels.map((l) => labelChip(l, live ? () => set(item.labels.filter((x) => x !== l)) : null));
  const add = live ? form({ class: "label-add" }, [
    h("input", { name: "label", placeholder: t("label.placeholder"), "aria-label": t("label.placeholder"), size: 14 }),
  ], async (data) => {
    const l = String(data.get("label") || "").trim();
    if (l) await set([...item.labels, l]);
  }) : null;
  if (!chips.length && !add) return null;
  return h("div", { class: "labels", id: "labels" }, chips, add);
}

// labelView lists the readable items that carry a label.
async function labelView(label) {
  const show = screen();
  const page = await get("/api/items?label=" + encodeURIComponent(label));
  show(
    h("p", {}, link("#/", t("nav.spaces")), " / " + t("label.title")),
    h("h1", {}, labelChip(label)),
    page.items.length
      ? h("table", { id: "labelled" }, h("thead", {}, h("tr", {}, [t("col.key"), t("col.title"), t("col.type")].map((c) => h("th", { text: c })))),
        h("tbody", {}, page.items.map((it) => h("tr", {},
          h("td", {}, link("#/item/" + encodeURIComponent(it.key), it.key)), h("td", { text: it.title }), h("td", { text: it.type })))))
      : h("p", { class: "muted", text: t("label.noItems") }),
    page.next ? h("p", { class: "muted", text: t("label.more", { n: page.total.value }) }) : null,
  );
}

// withTree lays a space's page tree beside a view's content.
function withTree(tree, ...content) {
  return h("div", { class: "with-tree" }, tree, h("div", { class: "with-tree-main" }, ...content));
}

// childPagesOf lists an item's child pages for a list of child pages in
// its body (ADR-0042).
const childPagesOf = (item) => async () =>
  (await get(`/api/spaces/${encodeURIComponent(item.space)}/tree?parent=${item.id}`)).items;

// filesOf serves and stores an item's attachments for its body (ADR-0045),
// and knows which of them are drawings (ADR-0053).
const filesOf = (item, attachments = []) => {
  const known = new Map(attachments.map((a) => [a.id, a]));
  return {
    item: item.id,
    url: (id) => `/api/items/${item.id}/attachments/${id}`,
    // source names the file a copy or new version comes from (ADR-0055).
    upload: async (file, source) => {
      const a = await upload("/api/items/" + item.id, file, source);
      known.set(a.id, a);
      return a;
    },
    isDrawing: (id) => isDrawing(known.get(id)?.name),
    // origin says where a copy came from, and whether its original is
    // newer: {readable, item: {id, key, title}, newer}.
    origin: (id) => known.get(id)?.origin || null,
  };
};

// readOnlyBody shows an item's body without editing it.
function readOnlyBody(item, embeds) {
  const resolve = async (ref) => (await get("/api/items/" + encodeURIComponent(ref))).item;
  return createEditor({ blocks: item.body, embeds, editable: false, resolve, childPages: childPagesOf(item), files: filesOf(item) }).element;
}

// sizeText shows a number of bytes for people.
function sizeText(n) {
  const units = ["B", "KB", "MB", "GB"];
  let i = 0;
  for (; n >= 1024 && i < units.length - 1; i++) n /= 1024;
  return `${i ? n.toFixed(1) : n} ${units[i]}`;
}

// attachmentsSection lists an item's files (ADR-0045); on a live item more
// can be uploaded and files deleted.
function attachmentsSection(path, files, live) {
  const status = h("div", { class: "status" });
  const row = (a) => h("li", { class: "attachment" },
    h("a", { href: `${path}/attachments/${a.id}`, text: a.name, download: a.inline ? null : a.name, target: a.inline ? "_blank" : null, rel: "noopener" }),
    h("span", { class: "muted", text: ` · ${sizeText(a.size)} · ${person(a.author)} · ${when(a.created)}` }),
    a.origin ? h("span", { class: "att-origin" }, " · ", a.origin.readable
      ? [t("att.copyOf"), " ", link("#/item/" + encodeURIComponent(a.origin.item.key), a.origin.item.key)]
      : t("att.copyOfUnknown"),
    a.origin.newer ? [" ", h("span", { class: "chip", text: t("att.originChanged") })] : null) : null,
    live ? h("button", { class: "link", text: t("common.delete"), onclick: () => confirm(t("att.deleteConfirm", { name: a.name }))
      && action(() => del(`${path}/attachments/${a.id}`), status) }) : null);
  const picker = live ? h("input", { type: "file", multiple: true, id: "upload", "aria-label": t("att.upload") }) : null;
  if (picker) {
    picker.addEventListener("change", () => action(async () => {
      for (const file of picker.files) await upload(path, file);
    }, status));
  }
  return h("section", { id: "attachments" },
    h("h2", { text: t("att.title", { n: files.length }) }),
    files.length ? h("ul", { class: "attachments" }, files.map(row)) : h("p", { class: "muted", text: t("att.none") }),
    picker ? h("label", { class: "upload" }, h("span", { text: t("att.upload") }), picker) : null,
    status);
}

async function action(fn, status) {
  try {
    await fn();
    route();
  } catch (err) {
    if (status) status.replaceChildren(errorBox(err));
    else alert(err.message);
  }
}

// --- people ------------------------------------------------------------------

// names caches usernames by user ID, so a view shows who did something
// instead of an ID (ADR-0031, ADR-0035); avatars the content tags of their
// profile pictures (ADR-0050).
const names = new Map();
const avatars = new Map();

// learnNames looks up the usernames of the IDs not yet known, in one
// request. IDs that are no user (such as "governance") stay as they are.
async function learnNames(ids) {
  const missing = [...new Set(ids.filter((id) => id && !names.has(id)))];
  // 100 per request stays below any sensible page budget (ADR-0015).
  for (let i = 0; i < missing.length; i += 100) {
    const chunk = missing.slice(i, i + 100);
    const res = await get("/api/users/names?" + chunk.map((id) => "id=" + encodeURIComponent(id)).join("&"));
    for (const id of chunk) {
      names.set(id, res.names[id] ?? id);
      avatars.set(id, res.avatars?.[id] || "");
    }
  }
}

// person names a user: "you", their username, or the ID as given.
function person(id) {
  if (!id) return "—";
  if (me && id === me.user.id) return t("common.you");
  return names.get(id) ?? id;
}

// face is a person's profile picture, or initials, for lists of who did
// what; learnNames must have seen the ID.
function face(id, size = 24) {
  const tag = me && id === me.user.id ? me.avatar : avatars.get(id);
  return avatar(id, names.get(id) ?? id, tag, size);
}

// --- navigation ------------------------------------------------------------

// renderFooter names the running build for signed-in users, e.g.
// "wikiwikibangbang 0.0.0-dev+858fd3a90746".
function renderFooter() {
  const footer = document.getElementById("footer");
  if (!me || !me.version) return footer.replaceChildren();
  const rev = me.revision && !me.version.includes(me.revision.slice(0, 12)) ? ` (${me.revision})` : "";
  footer.replaceChildren(`wikiwikibangbang ${me.version}${rev}`);
}

// --- module switcher ---------------------------------------------------------

// modules are offered behind the nine dots, as in the atlas console. match
// names the route sections that belong to a module, for its highlight.
// admin marks the modules only administrators see. The notifications are
// reached through the bell in the header, not here.
const modules = [
  { id: "spaces", name: "nav.spaces", href: "#/", match: ["", "space", "item", "board", "links", "search", "label"] },
  { id: "new", name: "nav.new", href: "#/new", match: ["new"] },
  { id: "tasks", name: "nav.tasks", href: "#/tasks", match: ["tasks", "kanbans", "kanban"] },
  { id: "album", name: "nav.album", href: "#/album", match: ["album"] },
  { id: "mcp", name: "nav.mcp", href: "#/mcp", match: ["mcp"] },
  { id: "governance", name: "nav.governance", href: "#/governance", match: ["governance"] },
  { id: "account", name: "nav.account", href: "#/account", match: ["account"] },
  { id: "admin", name: "nav.admin", href: "#/admin", match: ["admin"], admin: true },
];

const switcher = document.getElementById("app-switcher");
const drawer = document.getElementById("drawer");
const scrim = document.getElementById("scrim");

function openDrawer() {
  drawer.hidden = false;
  scrim.hidden = false;
  switcher.setAttribute("aria-expanded", "true");
  drawer.querySelector("#drawer-apps a")?.focus();
}

function closeDrawer() {
  if (drawer.hidden) return;
  drawer.hidden = true;
  scrim.hidden = true;
  switcher.setAttribute("aria-expanded", "false");
}

switcher.addEventListener("click", () => (drawer.hidden ? openDrawer() : closeDrawer()));
document.getElementById("drawer-close").addEventListener("click", () => { closeDrawer(); switcher.focus(); });
scrim.addEventListener("click", closeDrawer);
document.getElementById("drawer-apps").addEventListener("click", closeDrawer);
document.addEventListener("keydown", (ev) => {
  if (ev.key === "Escape" && !drawer.hidden) {
    closeDrawer();
    switcher.focus();
  }
});

// renderModules lists the modules and marks the one the route belongs to.
function renderModules(section) {
  switcher.hidden = !me;
  mount(document.getElementById("drawer-apps"), ...modules.filter((m) => !m.admin || me?.admin).map((m) =>
    h("a", { href: m.href, class: m.match.includes(section) ? "active" : "", "aria-current": m.match.includes(section) ? "page" : "false", text: t(m.name) })));
}

// labelChrome translates the fixed parts of index.html.
function labelChrome() {
  for (const el of [switcher, drawer, document.getElementById("drawer-apps")]) el.setAttribute("aria-label", t("nav.modules"));
  switcher.title = t("nav.modules");
  const close = document.getElementById("drawer-close");
  close.title = t("common.close");
  close.setAttribute("aria-label", t("common.close"));
}

// lockIcon draws a padlock, closed or open, as SVG.
function lockIcon(closed) {
  const ns = "http://www.w3.org/2000/svg";
  const svg = document.createElementNS(ns, "svg");
  for (const [k, v] of Object.entries({ viewBox: "0 0 24 24", width: "18", height: "18", "aria-hidden": "true", class: "lock" })) svg.setAttribute(k, v);
  const body = document.createElementNS(ns, "rect");
  for (const [k, v] of Object.entries({ x: "5", y: "11", width: "14", height: "10", rx: "2", fill: "currentColor" })) body.setAttribute(k, v);
  const shackle = document.createElementNS(ns, "path");
  // An open lock's shackle swings up and away on the right.
  shackle.setAttribute("d", closed ? "M8 11V8a4 4 0 0 1 8 0v3" : "M8 11V7a4 4 0 0 1 7.5-2");
  for (const [k, v] of Object.entries({ fill: "none", stroke: "currentColor", "stroke-width": "2", "stroke-linecap": "round" })) shackle.setAttribute(k, v);
  svg.append(shackle, body);
  return svg;
}

// bellIcon draws a bell, as SVG: h() makes HTML elements only; filled
// fills it.
function bellIcon(filled = false) {
  const ns = "http://www.w3.org/2000/svg";
  const svg = document.createElementNS(ns, "svg");
  svg.setAttribute("viewBox", "0 0 24 24");
  svg.setAttribute("width", "20");
  svg.setAttribute("height", "20");
  svg.setAttribute("aria-hidden", "true");
  const path = document.createElementNS(ns, "path");
  path.setAttribute("d", "M12 3a6 6 0 0 0-6 6v3.6L4.3 15.4A1 1 0 0 0 5.2 17h13.6a1 1 0 0 0 .9-1.6L18 12.6V9a6 6 0 0 0-6-6zm-2 15a2 2 0 0 0 4 0");
  path.setAttribute("fill", filled ? "currentColor" : "none");
  path.setAttribute("stroke", "currentColor");
  path.setAttribute("stroke-width", "2");
  path.setAttribute("stroke-linejoin", "round");
  svg.append(path);
  return svg;
}

function renderNav() {
  labelChrome();
  if (!me) {
    nav.replaceChildren();
    return;
  }
  const current = location.hash.startsWith("#/search/") ? decodeURIComponent(location.hash.slice(9)) : "";
  const search = h("form", { class: "search", role: "search", onsubmit: (ev) => {
    ev.preventDefault();
    const q = new FormData(ev.target).get("q").trim();
    if (q) location.hash = "#/search/" + encodeURIComponent(q);
  } }, h("input", { name: "q", type: "search", placeholder: t("nav.search"), "aria-label": t("nav.search"), value: current }));
  // The unread count arrives after the navigation is drawn (ADR-0046).
  // A bell with the number of unread notifications; its name says it in
  // words, for screen readers and as a tooltip.
  const count = h("span", { class: "count", hidden: true });
  const bell = h("a", { href: "#/notifications", id: "nav-notifications", class: "bell", title: t("nav.notifications"), "aria-label": t("nav.notifications") },
    bellIcon(), count);
  get("/api/notifications?limit=1").then((n) => {
    const shown = n.unread >= 100 ? "99+" : String(n.unread);
    count.textContent = shown;
    count.hidden = !n.unread;
    // Open governance findings one is responsible for show as a dot until
    // they are resolved (ADR-0057); the number counts unread changes.
    const open = n.findings ? n.findings.sensitive + n.findings.stale + n.findings.implausible + (n.findings.personal || 0) : 0;
    bell.classList.toggle("findings", open > 0);
    const name = (n.unread ? t("nav.notificationsUnread", { n: shown }) : t("nav.notifications"))
      + (open ? " · " + t("nav.findingsOpen", { n: open }) : "");
    bell.title = name;
    bell.setAttribute("aria-label", name);
    bell.classList.toggle("unread", n.unread > 0);
  }).catch(() => {});
  // The spaces are reached through the logo and the module switcher; the
  // page tree navigates within a space (ADR-0041).
  mount(nav,
    search,
    h("button", { id: "nav-new", class: "primary", type: "button", text: t("nav.newButton"), onclick: () => { location.hash = newHref(); } }),
    bell,
    accountMenu(),
  );
}

// avatar shows a user's profile picture, or their initials when they have
// none (ADR-0050). tag names the picture's content, so a new one is
// fetched when it changes.
function avatar(id, name, tag, size = 32) {
  const initials = (name || "?").split(/[\s._@-]+/).filter(Boolean).slice(0, 2).map((w) => w[0]).join("").toUpperCase() || "?";
  // The initials are drawn by CSS from an attribute, so they are no part
  // of the text beside them, which names the person.
  const box = h("span", { class: `avatar avatar-${size}`, "aria-hidden": "true" });
  if (tag) {
    const img = h("img", { src: `/api/users/${encodeURIComponent(id)}/avatar?v=${encodeURIComponent(tag)}`, alt: "", width: size, height: size });
    img.addEventListener("error", () => { img.remove(); box.dataset.initials = initials; });
    box.append(img);
  } else {
    box.dataset.initials = initials;
  }
  return box;
}

// accountMenu is the account's button in the header: the profile picture
// or initials, opening a menu with the account, the AI assistants, the
// administration for administrators, and signing out.
function accountMenu() {
  const button = h("button", { type: "button", id: "account-button", class: "account-button", "aria-haspopup": "menu", "aria-expanded": "false",
    "aria-controls": "account-menu", title: t("nav.accountOf", { name: me.user.username }), "aria-label": t("nav.accountOf", { name: me.user.username }) },
  avatar(me.user.id, me.user.username, me.avatar));
  // Account requests waiting for an administrator show as a dot
  // (ADR-0061).
  if (me.admin && me.requests) {
    button.classList.add("pending");
    button.title += " · " + t("nav.requestsWaiting", { n: me.requests });
    button.setAttribute("aria-label", button.title);
  }
  const item = (href, text) => h("a", { href, role: "menuitem", text, tabindex: "-1" });
  const roles = [me.admin ? t("admin.roleAdmin") : null, me.erasure ? t("admin.roleErasure") : null].filter(Boolean);
  const menu = h("div", { id: "account-menu", class: "menu", role: "menu", hidden: true, "aria-label": t("nav.account") },
    h("div", { class: "menu-head" }, avatar(me.user.id, me.user.username, me.avatar, 40),
      h("div", {}, h("strong", { text: me.user.username }), roles.length ? h("div", { class: "muted", text: roles.join(", ") }) : null)),
    item("#/account", t("nav.accountPage")),
    item("#/mcp", t("nav.mcp")),
    me.admin ? item("#/admin", me.requests ? t("nav.adminRequests", { n: me.requests }) : t("nav.admin")) : null,
    h("hr"),
    h("button", { type: "button", role: "menuitem", tabindex: "-1", class: "link", id: "logout", text: t("nav.logout"),
      onclick: () => action(async () => { await post("/api/logout"); me = null; location.hash = "#/"; }) }));
  const entries = () => [...menu.querySelectorAll("[role=menuitem]")];
  const close = (focus) => {
    if (menu.hidden) return;
    menu.hidden = true;
    button.setAttribute("aria-expanded", "false");
    document.removeEventListener("click", outside, true);
    if (focus) button.focus();
  };
  const outside = (ev) => { if (!menu.contains(ev.target) && !button.contains(ev.target)) close(false); };
  const open = () => {
    menu.hidden = false;
    button.setAttribute("aria-expanded", "true");
    document.addEventListener("click", outside, true);
    entries()[0]?.focus();
  };
  button.addEventListener("click", () => (menu.hidden ? open() : close(true)));
  menu.addEventListener("click", (ev) => { if (ev.target.closest("[role=menuitem]")) close(false); });
  menu.addEventListener("keydown", (ev) => {
    const list = entries();
    const at = list.indexOf(document.activeElement);
    if (ev.key === "Escape") { ev.preventDefault(); close(true); }
    else if (ev.key === "ArrowDown") { ev.preventDefault(); list[(at + 1) % list.length].focus(); }
    else if (ev.key === "ArrowUp") { ev.preventDefault(); list[(at - 1 + list.length) % list.length].focus(); }
    else if (ev.key === "Home") { ev.preventDefault(); list[0].focus(); }
    else if (ev.key === "End") { ev.preventDefault(); list[list.length - 1].focus(); }
    else if (ev.key === "Tab") close(false);
  });
  onLeave(() => close(false));
  return h("div", { class: "account" }, button, menu);
}

// here is where the user is: the space and item shown, so that "New"
// creates in the right place. Views set it; route() clears it.
const here = { space: "", parent: "" };

// newHref is the address of the creation view for the place shown.
function newHref(space = here.space, parent = here.parent) {
  const q = new URLSearchParams();
  if (space) q.set("space", space);
  if (parent) q.set("parent", parent);
  return "#/new" + (q.size ? "?" + q : "");
}

// navigation counts route() calls. A view shows its content only if no
// later navigation started while it was loading; otherwise a slow view
// would replace the one the user chose after it.
let navigation = 0;

// screen captures the current navigation; call it before a view's first
// await and show the view through the function it returns.
function screen() {
  const n = navigation;
  return (...children) => { if (n === navigation) mount(app, ...children); };
}

// leaving holds what the current view must end when another replaces it,
// such as a live session.
let leaving = [];
function onLeave(fn) { leaving.push(fn); }

async function route() {
  const hash = location.hash.replace(/^#/, "") || "/";
  for (const fn of leaving) fn();
  leaving = [];
  here.space = here.parent = "";
  const show = (navigation++, screen());
  try {
    if (!me) {
      try {
        me = await get("/api/me");
      } catch (err) {
        if (!(err instanceof ApiError) || err.status !== 401) throw err;
      }
      await chooseLanguage();
    }
    renderNav();
    renderFooter();
    closeDrawer();
    const [, section, arg, arg2] = hash.split("?")[0].split("/").map(decodeURIComponent);
    const params = new URLSearchParams(hash.split("?")[1] || "");
    renderModules(section || "");
    if (!me) return loginView();
    switch (section) {
      case "new": return await newView(params);
      case "tasks": return await tasksView(arg, params);
      case "kanbans": return await kanbansView();
      case "kanban": return await kanbanView(arg, arg2);
      case "space": return await spaceView(arg);
      case "board": return await boardView(arg, arg2);
      case "links": return await linksView(arg);
      case "governance": return arg ? await governanceView(arg) : await governanceOverview();
      case "item": return await itemView(arg, params);
      case "label": return await labelView(arg);
      case "notifications": return await notificationsView();
      case "album": return albumView();
      case "search": return await searchView(arg || "");
      case "account": return await accountView();
      case "mcp": return await mcpView();
      case "admin": return await adminView();
      case "members": return await membersView(arg);
      default: return await homeView();
    }
  } catch (err) {
    show(errorBox(err));
  }
}

// chooseLanguage applies the signed-in user's language (ADR-0017). The
// browser's language is only a suggestion, taken on the first sign-in and
// saved, so it does not change behind the user's back later.
async function chooseLanguage() {
  if (!me) return setLanguage(suggestedLanguage());
  if (!me.language) {
    me.language = suggestedLanguage();
    await put("/api/me/language", { language: me.language }).catch(() => {});
  }
  setLanguage(me.language);
}

// --- corporate design (ADR-0052) -----------------------------------------------

// The product's own logo, kept to come back to when a design's logo goes.
const productLogo = document.querySelector(".brand picture")?.cloneNode(true);

// applyBranding shows the installation's logo and name; the colours, fonts
// and background come with /branding.css. reload asks for the stylesheet
// again after an administrator changed the design.
async function applyBranding(reload = false) {
  const b = await get("/branding.json").catch(() => null);
  if (!b) return;
  const brand = document.querySelector(".brand");
  if (b.assets?.logo) {
    brand.replaceChildren(h("img", { src: "/branding/logo?v=" + encodeURIComponent(b.assets.logo), alt: b.name || "wikiwikibangbang", class: "brand-logo" }));
  } else if (productLogo) {
    brand.replaceChildren(productLogo.cloneNode(true));
  }
  document.title = b.name ? `${b.name} · wikiwikibangbang` : "wikiwikibangbang";
  if (reload) document.getElementById("branding-css").href = "/branding.css?v=" + Date.now();
}

window.addEventListener("hashchange", route);
applyBranding();
route();

// --- login -------------------------------------------------------------------

async function loginView() {
  const show = screen();
  const params = new URLSearchParams(location.search);
  const [sso, signup] = await Promise.all([get("/api/oidc").catch(() => ({ enabled: false })), get("/api/signup").catch(() => ({ open: false }))]);
  const card = h("section", { class: "card narrow", id: "login-card" });
  const signIn = () => card.replaceChildren(
    h("h1", { text: t("login.title") }),
    params.get("login_error") ? h("p", { class: "error", text: t("login.failed") }) : "",
    form({ id: "login" }, [
      field(t("field.username"), h("input", { name: "username", autocomplete: "username", required: true })),
      field(t("field.password"), h("input", { name: "password", type: "password", autocomplete: "current-password", required: true })),
      h("button", { type: "submit", text: t("login.submit") }),
    ], async (data) => {
      await post("/api/login", { username: data.get("username"), password: data.get("password") });
      me = null;
      history.replaceState(null, "", "/#/");
      route();
    }),
    sso.enabled ? h("p", {}, h("a", { class: "button", href: "/api/oidc/login", text: t("login.with", { name: sso.name }) })) : "",
    signup.open ? h("p", { class: "login-more" }, t("signup.question"), " ",
      h("button", { type: "button", class: "link", id: "signup-open", text: t("signup.open"), onclick: requestForm })) : "",
  );
  // requestForm asks for an account (ADR-0061): the password is chosen
  // now, an administrator approves or rejects the request.
  const requestForm = () => card.replaceChildren(
    h("h1", { text: t("signup.title") }),
    h("p", { class: "muted", text: t("signup.help") }),
    form({ id: "signup" }, [
      field(t("field.username"), h("input", { name: "username", autocomplete: "username", required: true, maxlength: 64 })),
      field(t("contacts.email"), h("input", { name: "email", type: "email", autocomplete: "email", required: true })),
      field(t("field.password"), h("input", { name: "password", type: "password", autocomplete: "new-password", minlength: 12, required: true })),
      field(t("signup.reason"), h("textarea", { name: "reason", rows: 3, maxlength: 500, placeholder: t("signup.reasonHint") })),
      h("button", { type: "submit", text: t("signup.submit") }),
    ], async (data) => {
      await post("/api/signup", { username: data.get("username"), email: data.get("email"), password: data.get("password"), reason: data.get("reason") });
      card.replaceChildren(
        h("h1", { text: t("signup.title") }),
        h("p", { class: "notice", id: "signup-sent", text: t("signup.sent") }),
        h("p", {}, h("button", { type: "button", class: "link", text: t("signup.back"), onclick: signIn })));
    }),
    h("p", { class: "login-more" }, h("button", { type: "button", class: "link", text: t("signup.back"), onclick: signIn })),
  );
  signIn();
  show(card);
}

// --- search (ADR-0033) -------------------------------------------------------

async function searchView(q) {
  const show = screen();
  const url = (after) => `/api/search?q=${encodeURIComponent(q)}&limit=50${after ? "&after=" + encodeURIComponent(after) : ""}`;
  const first = await get(url());
  const list = h("ol", { class: "list hits" });
  const more = h("div");
  const addHits = (page) => {
    for (const hit of page.hits) {
      const it = hit.item;
      list.append(h("li", {},
        link("#/item/" + encodeURIComponent(it.key), it.key), " ", h("strong", { text: it.title }),
        " ", h("span", { class: "muted", text: `${it.space} · ${it.type}${it.state ? " · " + it.state : ""}` }),
        hit.snippet ? h("p", { class: "snippet", text: hit.comment ? t("search.inComment", { text: hit.snippet }) : hit.snippet }) : null,
      ));
    }
    more.replaceChildren();
    if (page.next) more.append(h("button", { text: t("common.more"), onclick: async () => addHits(await get(url(page.next))) }));
  };
  addHits(first);
  show(
    h("h1", { text: t("search.title") }),
    h("p", { class: "muted", text: t("search.results", { n: first.total.value, q }) }),
    first.total.value === 0 ? h("p", { text: t("search.none") }) : list,
    more,
  );
}

// --- spaces ------------------------------------------------------------------

// roleText names a space role for people.
function roleText(role) {
  switch (role) {
    case "viewer": return t("role.viewer");
    case "editor": return t("role.editor");
    case "space-admin": return t("role.space-admin");
    default: return role || "";
  }
}

// mayEdit says whether a role lets one create and change pages.
const mayEdit = (role) => role === "editor" || role === "space-admin";

async function homeView() {
  const show = screen();
  const [{ spaces }, { types }, notes, brand] = await Promise.all([get("/api/spaces"), get("/api/types"),
    get("/api/notifications?limit=1").catch(() => ({ unread: 0 })), get("/branding.json").catch(() => ({}))]);
  const anyEditable = spaces.some((s) => mayEdit(s.role));
  // A greeting by the time of day, the reader's own clock.
  const hour = new Date().getHours();
  const greeting = hour < 11 ? t("home.morning") : hour < 18 ? t("home.day") : t("home.evening");
  const name = brand.name || "wikiwikibangbang";
  const quick = (href, title, text, id, extra) => h("a", { class: "home-tile", href, id }, h("strong", { text: title }), h("span", { class: "muted", text }), extra || null);
  const findings = notes.findings ? notes.findings.sensitive + notes.findings.stale + notes.findings.implausible + (notes.findings.personal || 0) : 0;
  show(
    h("section", { class: "welcome", id: "welcome" },
      h("div", { class: "welcome-text" },
        h("h1", { text: `${greeting}, ${me.user.username}` }),
        h("p", { class: "welcome-lead", text: t("home.lead", { name }) }),
        h("p", { class: "actions-row" },
          anyEditable ? link("#/new", t("home.newPage"), "button primary") : null,
          link("#/tasks", t("home.myTasks"), "button"),
          link("#/search/", t("home.search"), "button"))),
      h("img", { class: "welcome-art", src: "/assets/brand/welcome.svg", alt: "", width: 360, height: 240 })),
    h("section", { class: "home-tiles", "aria-label": t("home.quick") },
      quick("#/tasks", t("nav.tasks"), t("home.tasksText"), "home-tasks"),
      quick("#/notifications", t("nav.notifications"), notes.unread ? t("home.notesUnread", { n: notes.unread }) : t("home.notesNone"), "home-notes",
        findings ? h("span", { class: "chip", text: t("nav.findingsOpen", { n: findings }) }) : null),
      quick("#/album", t("nav.album"), t("home.albumText"), "home-album"),
      quick("#/mcp", t("nav.mcp"), t("home.mcpText"), "home-mcp")),
    h("h2", { text: t("spaces.title") }),
    spaces.length === 0
      ? h("div", { class: "card first-steps" },
        h("p", { text: me.admin ? t("spaces.noneAdmin") : t("spaces.none") }),
        me.admin ? h("ol", {}, h("li", { text: t("home.step1") }), h("li", { text: t("home.step2") }), h("li", { text: t("home.step3") })) : null)
      : h("ul", { class: "space-tiles spaces" }, spaces.map((s) => h("li", {},
        h("a", { href: "#/space/" + encodeURIComponent(s.key), class: "space-tile" },
          h("span", { class: "space-key", text: s.key }),
          h("strong", { text: s.name }),
          h("span", { class: "badge", text: roleText(s.role) }))))),
    me.admin ? setupCard(types) : null,
    me.admin ? newSpaceForm(spaces.length === 0) : null,
  );
}

// newSpaceForm creates a space and opens it (administrators). It starts
// folded unless open is set.
function newSpaceForm(open) {
  const f = form({ id: "create-space" }, [
    field(t("admin.key"), h("input", { name: "key", required: true, pattern: "[A-Z][A-Z0-9]*", placeholder: "DOCS", title: t("admin.keyHint") })),
    field(t("field.name"), h("input", { name: "name", required: true })),
    h("button", { type: "submit", class: "primary", text: t("admin.createSpace") }),
  ], async (d) => {
    await post("/api/spaces", { key: d.get("key"), name: d.get("name") });
    location.hash = "#/space/" + encodeURIComponent(d.get("key"));
  });
  return h("details", { class: "card", id: "new-space-card", open: open || null }, h("summary", { text: t("admin.newSpace") }), f);
}

// typeTemplates are ready-made types for a new installation: types are
// data (ADR-0001), so an empty server has none until someone defines them.
const typeTemplates = [
  { name: "page", body: true, fields: [{ name: "owner", kind: "user" }] },
  { name: "task", body: true,
    fields: [{ name: "assignee", kind: "user" }, { name: "due", kind: "date" }, { name: "priority", kind: "enum", values: ["low", "normal", "high"] }],
    workflow: { initial: "open", states: ["open", "in progress", "done"],
      transitions: [{ name: "start", from: ["open"], to: "in progress" }, { name: "finish", from: ["open", "in progress"], to: "done" },
        { name: "reopen", from: ["done"], to: "open" }] } },
  { name: "decision", body: true, fields: [{ name: "decided", kind: "date" }],
    workflow: { initial: "proposed", states: ["proposed", "accepted", "rejected"],
      transitions: [{ name: "accept", from: ["proposed"], to: "accepted" }, { name: "reject", from: ["proposed"], to: "rejected" }] } },
];

// setupCard offers administrators the ready-made types that are missing.
function setupCard(types) {
  const missing = typeTemplates.filter((tt) => !types.some((ty) => ty.name === tt.name));
  if (missing.length === 0) return null;
  const status = h("div", { class: "status" });
  return h("section", { class: "card notice-card", id: "setup-types" },
    h("h2", { text: t("setup.title") }),
    h("p", { text: types.length ? t("setup.someTypes", { list: missing.map((m) => m.name).join(", ") }) : t("setup.noTypes") }),
    h("button", { class: "primary", text: t("setup.create", { list: missing.map((m) => m.name).join(", ") }),
      onclick: () => action(async () => { for (const tt of missing) await post("/api/types", tt); }, status) }),
    status);
}

// newView creates a page or another item, and for administrators a space:
// one place that lists what can be created (#/new?space=X&parent=Y).
async function newView(params) {
  const show = screen();
  const [{ spaces }, { types }] = await Promise.all([get("/api/spaces"), get("/api/types")]);
  const editable = spaces.filter((s) => mayEdit(s.role));
  let parent = null;
  if (params.get("parent")) parent = (await get("/api/items/" + encodeURIComponent(params.get("parent")))).item;
  const space = editable.some((s) => s.key === params.get("space")) ? params.get("space") : editable[0]?.key;
  here.space = space || "";
  let pageCard;
  if (types.length === 0) {
    pageCard = h("p", { class: "muted", text: me.admin ? t("new.noTypesAdmin") : t("new.noTypes") });
  } else if (editable.length === 0) {
    pageCard = h("p", { class: "muted", text: me.admin ? t("new.noSpaceAdmin") : t("new.noSpace") });
  } else {
    pageCard = newItemForm({ space, spaces: editable, types, parent: parent && parent.space === space ? parent : null, heading: t("new.page") });
  }
  show(
    h("h1", { text: t("new.title") }),
    pageCard,
    h("section", { class: "card" }, h("h2", { text: t("new.withAssistant") }), h("p", { text: t("new.withAssistantText") }),
      link("#/mcp", t("new.withAssistantLink"), "button secondary")),
    me.admin ? setupCard(types) : null,
    me.admin ? newSpaceForm(spaces.length === 0) : null,
    me.admin ? h("p", { class: "muted" }, t("new.moreAdmin"), " ", link("#/admin", t("nav.admin"))) : null,
  );
}

async function spaceView(key) {
  const show = screen();
  const [{ types }, first, top, { labels }, { watches }, { spaces }] = await Promise.all([
    get("/api/types"),
    get(`/api/items?space=${encodeURIComponent(key)}&limit=100`),
    get(`/api/spaces/${encodeURIComponent(key)}/tree?limit=1`),
    get(`/api/labels?space=${encodeURIComponent(key)}`),
    get("/api/me/watches"),
    get("/api/spaces"),
  ]);
  here.space = key;
  const sp = spaces.find((s) => s.key === key) || { key, name: key, role: "" };
  const canEdit = mayEdit(sp.role);
  const status = h("div", { class: "status" });
  const create = canEdit ? newItemForm({ space: key, types }) : null;
  // "New page" leads to the form at the end of the view.
  const toForm = () => { create.scrollIntoView({ block: "start" }); create.querySelector("[name=title]").focus(); };
  // The space's home is its first page at the top of the tree.
  let home = h("section", { class: "card empty" }, h("p", { class: "muted", text: t("space.noPages") }),
    create ? h("button", { class: "primary", text: t("space.firstPage"), onclick: toForm }) : null);
  if (top.items.length) {
    const [{ item }, { embeds }] = await Promise.all([get("/api/items/" + top.items[0].id), get(`/api/items/${top.items[0].id}/embeds`)]);
    home = h("section", { id: "home", class: "card" },
      h("h2", {}, link("#/item/" + encodeURIComponent(item.key), item.title || item.key)),
      item.body.length ? readOnlyBody(item, embeds) : h("p", { class: "muted", text: t("space.homeEmpty") }));
  }
  const rows = h("tbody");
  const more = h("div");
  const addRows = (page) => {
    for (const it of page.items) {
      rows.append(h("tr", {},
        h("td", {}, link("#/item/" + encodeURIComponent(it.key), it.key)),
        h("td", { text: it.title }),
        h("td", { text: it.type }),
        h("td", { text: it.state || "" }),
      ));
    }
    more.replaceChildren();
    if (page.next) {
      more.append(h("button", { text: t("common.more"), onclick: async () => addRows(await get(`/api/items?space=${encodeURIComponent(key)}&limit=100&after=${page.next}`)) }));
    }
  };
  addRows(first);
  const boards = types.filter((ty) => ty.workflow)
    .map((ty) => link(`#/board/${encodeURIComponent(key)}/${encodeURIComponent(ty.name)}`, t("space.board", { type: ty.name })));
  const tree = pageTree({ space: key, newPage: canEdit ? newHref(key, "") : "", onMoved: () => route(), onError: (err) => status.replaceChildren(errorBox(err)) });
  show(withTree(tree,
    h("p", {}, link("#/", t("nav.spaces")), " / " + key),
    h("div", { class: "page-head" },
      h("h1", { text: sp.name }),
      h("p", { class: "actions" },
        create && types.length ? h("button", { class: "primary", id: "space-new-page", text: t("space.newPage"), onclick: toForm }) : null,
        watchButton("/api/spaces/" + encodeURIComponent(key), watches.some((w) => !w.item && w.space === key), status))),
    h("p", {}, [...boards, link(`#/links/${encodeURIComponent(key)}`, t("space.linkCheck")), link(`#/governance/${encodeURIComponent(key)}`, t("nav.governance")),
      sp.role === "space-admin" ? link(`#/members/${encodeURIComponent(key)}`, t("members.title")) : null].filter(Boolean).flatMap((b, i) => i ? [" · ", b] : [b])),
    status,
    home,
    labels.length ? h("section", { id: "space-labels" }, h("h2", { text: t("label.title") }),
      h("div", { class: "labels" }, labels.map((l) => h("span", { class: "label-count" }, labelChip(l.label), " " + l.count)))) : null,
    h("h2", { text: t("space.allItems") }),
    h("p", { class: "muted", text: t("space.items", { n: first.total.value }) }),
    h("table", {}, h("thead", {}, h("tr", {}, [t("col.key"), t("col.title"), t("col.type"), t("col.state")].map((c) => h("th", { text: c })))), rows),
    more,
    create,
  ));
}

// --- board -------------------------------------------------------------------

// boardMax bounds the cards one board loads; a larger board says so.
const boardMax = 2000;

// boardView shows the items of one type in a space as columns, one per
// workflow state (ADR-0036). A card moves only by a workflow transition:
// with the buttons on the card, or by dragging it to a column that exactly
// one of its transitions leads to.
async function boardView(key, typeName) {
  const show = screen();
  const { types } = await get("/api/types");
  const type = types.find((ty) => ty.name === typeName);
  if (!type || !type.workflow) throw new Error(t("board.noWorkflow", { type: typeName }));
  const base = `/api/items?space=${encodeURIComponent(key)}&type=${encodeURIComponent(typeName)}&limit=100`;
  let page = await get(base);
  const items = [...page.items];
  while (page.next && items.length < boardMax) {
    page = await get(`${base}&after=${encodeURIComponent(page.next)}`);
    items.push(...page.items);
  }
  const status = h("div", { class: "status" });
  show(
    h("p", {}, link("#/", t("nav.spaces")), " / ", link("#/space/" + encodeURIComponent(key), key), " / " + t("board.crumb", { type: typeName })),
    h("h1", { text: `${key} · ${typeName}` }),
    page.next ? h("p", { class: "notice", text: t("board.truncated", { n: items.length }) }) : null,
    status,
    boardOf(items, type, status),
  );
}

// boardOf lays items of a type with a workflow out in columns of its
// states (ADR-0036); a card moves by a transition, by button or by
// dragging it to another column. cardTail adds to each card.
function boardOf(items, type, status, cardTail) {
  const transitionsFrom = (state) => type.workflow.transitions.filter((tr) => tr.from.includes(state));
  const perform = (it, name) => action(() => post(`/api/items/${it.id}/transitions`, { name }), status);

  const card = (it) => h("li", { class: "card-item", draggable: "true", "data-id": it.id,
    ondragstart: (ev) => { ev.dataTransfer.setData("text/plain", it.id); ev.dataTransfer.effectAllowed = "move"; } },
  link("#/item/" + encodeURIComponent(it.key), it.key),
  h("div", { class: "card-title", text: it.title }),
  h("div", { class: "card-actions" }, transitionsFrom(it.state).map((tr) =>
    h("button", { class: "link", text: `${tr.name} → ${tr.to}`, onclick: () => perform(it, tr.name) }))),
  cardTail ? cardTail(it) : null,
  );

  const byID = new Map(items.map((it) => [it.id, it]));
  const column = (state) => {
    const cards = items.filter((it) => it.state === state);
    const col = h("section", { class: "board-column", "data-state": state,
      ondragover: (ev) => { ev.preventDefault(); col.classList.add("drop"); },
      ondragleave: () => col.classList.remove("drop"),
      ondrop: (ev) => {
        ev.preventDefault();
        col.classList.remove("drop");
        const it = byID.get(ev.dataTransfer.getData("text/plain"));
        if (!it || it.state === state) return;
        const to = transitionsFrom(it.state).filter((tr) => tr.to === state);
        if (to.length === 1) return perform(it, to[0].name);
        status.replaceChildren(h("p", { class: "error", text: t(to.length ? "board.several" : "board.none", { from: it.state, to: state }) }));
      } },
    h("h2", {}, state, h("span", { class: "muted", text: ` ${cards.length}` })),
    h("ul", { class: "board-cards" }, cards.map(card)));
    return col;
  };
  return h("div", { class: "board" }, type.workflow.states.map(column));
}

// --- tasks -------------------------------------------------------------------

// tasksView lists open work across every space one may read: items of a
// type with a workflow that are not in its last state. It reads the paged
// item listing, at most boardMax items, like the board.
// taskFields finds the fields of a type that make it a task in the inbox
// (ADR-0049), by convention: the assignee is the user field named assignee
// or, failing that, the first user field; the due date the first date
// field named like a deadline (as governance reads it, ADR-0040); the
// priority an enum named priority, its values ordered from low to high.
function taskFields(type) {
  const users = type.fields.filter((f) => f.kind === "user");
  return {
    assignee: users.find((f) => /^(assignee|zugewiesen|bearbeiter)/i.test(f.name)) || users[0] || null,
    due: type.fields.find((f) => f.kind === "date" && /due|deadline|f(ä|ae)llig/i.test(f.name)) || null,
    priority: type.fields.find((f) => f.kind === "enum" && /^(priority|priorität|prioritaet)$/i.test(f.name)) || null,
  };
}

// tasksView is the inbox of open work across spaces (ADR-0049): the items
// whose type has a workflow and that are not in its last state, in three
// lists — assigned to me, unassigned, all — overdue and urgent first. One
// takes a task over or gives it back, and moves it on by its transitions.
async function tasksView(tab, params) {
  const show = screen();
  const [{ types }, { spaces }] = await Promise.all([get("/api/types"), get("/api/spaces")]);
  const typeOf = new Map(types.filter((ty) => ty.workflow).map((ty) => [ty.name, { ty, f: taskFields(ty) }]));
  let page = await get("/api/items?limit=100");
  const items = [...page.items];
  while (page.next && items.length < boardMax) {
    page = await get(`/api/items?limit=100&after=${encodeURIComponent(page.next)}`);
    items.push(...page.items);
  }
  const open = items.filter((it) => {
    const wf = typeOf.get(it.type)?.ty.workflow;
    return wf && it.state !== wf.states[wf.states.length - 1];
  });
  const assigneeOf = (it) => {
    const f = typeOf.get(it.type).f.assignee;
    return f ? it.properties[f.name] || "" : null;
  };
  await learnNames(open.map(assigneeOf).filter(Boolean));
  const today = new Date().toISOString().slice(0, 10);
  const dueOf = (it) => {
    const f = typeOf.get(it.type).f.due;
    return f && it.properties[f.name] ? String(it.properties[f.name]).slice(0, 10) : "";
  };
  const rankOf = (it) => {
    const f = typeOf.get(it.type).f.priority;
    return f ? f.values.indexOf(it.properties[f.name]) : -1;
  };
  const space = params.get("space") || "";
  const lists = {
    mine: open.filter((it) => assigneeOf(it) === me.user.id),
    unassigned: open.filter((it) => assigneeOf(it) === ""),
    all: open,
  };
  const current = lists[tab] ? tab : (lists.mine.length ? "mine" : "all");
  const shown = lists[current].filter((it) => !space || it.space === space);
  // Overdue first, then by due date, then the most urgent, then by key.
  shown.sort((a, b) => {
    const da = dueOf(a) || "9999", db = dueOf(b) || "9999";
    return da.localeCompare(db) || rankOf(b) - rankOf(a) || a.space.localeCompare(b.space) || a.key.localeCompare(b.key, undefined, { numeric: true });
  });
  const mayEditIn = new Set(spaces.filter((s) => mayEdit(s.role)).map((s) => s.key));
  const status = h("div", { class: "status" });
  const setAssignee = (it, value) => action(() =>
    put(`/api/items/${it.id}/properties/${encodeURIComponent(typeOf.get(it.type).f.assignee.name)}`, { value }), status);

  const row = (it) => {
    const { ty, f } = typeOf.get(it.type);
    const who = assigneeOf(it);
    const due = dueOf(it);
    const canAct = mayEditIn.has(it.space);
    const claim = canAct && f.assignee
      ? (who === me.user.id
        ? h("button", { class: "link", text: t("tasks.unclaim"), onclick: () => setAssignee(it, null) })
        : h("button", { class: "link", text: t("tasks.claim"), onclick: () => setAssignee(it, me.user.id) }))
      : null;
    const moves = canAct ? ty.workflow.transitions.filter((tr) => tr.from.includes(it.state))
      .map((tr) => h("button", { class: "link", text: tr.name, onclick: () => action(() => post(`/api/items/${it.id}/transitions`, { name: tr.name }), status) })) : [];
    return h("tr", { class: due && due < today ? "overdue" : "" },
      h("td", {}, link("#/item/" + encodeURIComponent(it.key), it.key)),
      h("td", { text: it.title }),
      h("td", { text: it.state }),
      h("td", { text: who === null ? "" : who ? person(who) : t("tasks.nobody") }),
      h("td", {}, due ? h("span", { class: due < today ? "badge danger" : due === today ? "badge" : "", text: due < today ? t("tasks.overdue", { date: due }) : due }) : ""),
      h("td", { text: f.priority ? it.properties[f.priority.name] || "" : "" }),
      h("td", {}, link(`#/board/${encodeURIComponent(it.space)}/${encodeURIComponent(it.type)}`, it.space)),
      h("td", { class: "task-actions" }, [claim, ...moves].filter(Boolean).flatMap((b, k) => k ? [" · ", b] : [b])));
  };
  // Every column can be filtered. The filters apply at once, without
  // reading the tasks again, and are kept in the address, so a reload or a
  // shared link shows the same; changing a tab keeps them.
  const filterNames = ["key", "title", "state", "who", "due", "prio"];
  const filters = Object.fromEntries(filterNames.map((k) => [k, params.get(k) || ""]));
  const query = (extra = {}) => {
    const q = new URLSearchParams();
    for (const [k, v] of Object.entries({ space, ...filters, ...extra })) if (v) q.set(k, v);
    return q.size ? "?" + q : "";
  };
  const weekEnd = new Date(Date.now() + 7 * 864e5).toISOString().slice(0, 10);
  const contains = (text, part) => String(text).toLowerCase().includes(part.trim().toLowerCase());
  const matches = (it) => {
    const due = dueOf(it), who = assigneeOf(it);
    const f = typeOf.get(it.type).f;
    return contains(it.key, filters.key) && contains(it.title, filters.title)
      && (!filters.state || it.state === filters.state)
      && (!filters.who || (filters.who === "-" ? who === "" : who === filters.who))
      && (!filters.prio || (f.priority && it.properties[f.priority.name] === filters.prio))
      && ({ "": true, overdue: due && due < today, today: due === today, week: due && due >= today && due <= weekEnd, none: !due }[filters.due] ?? true);
  };
  const rows = shown.map((it) => [it, row(it)]);
  const count = h("p", { class: "muted", id: "tasks-count", role: "status" });
  const empty = h("p", { class: "muted", text: t("tasks.noneFiltered"), hidden: true });
  const apply = () => {
    let n = 0;
    for (const [it, tr] of rows) n += (tr.hidden = !matches(it)) ? 0 : 1;
    count.textContent = n === rows.length ? "" : t("tasks.filtered", { n, total: rows.length });
    empty.hidden = n > 0;
    reset.hidden = !filterNames.some((k) => filters[k]);
    history.replaceState(null, "", `#/tasks/${current}${query()}`);
    relink();
  };
  const set = (k) => (ev) => { filters[k] = ev.target.value; apply(); };
  const distinct = (values) => [...new Set(values.filter((v) => v !== "" && v !== null && v !== undefined))].sort((a, b) => String(a).localeCompare(String(b)));
  const choose = (k, label, options) => h("select", { "aria-label": t("tasks.filterOf", { column: label }), onchange: set(k) },
    h("option", { value: "", text: t("tasks.filterAll") }),
    options.map(([v, text]) => h("option", { value: v, text, selected: v === filters[k] })));
  const typed = (k, label) => {
    const input = h("input", { type: "search", value: filters[k], placeholder: t("tasks.filterText"), "aria-label": t("tasks.filterOf", { column: label }) });
    input.addEventListener("input", set(k));
    return input;
  };
  const whoValues = shown.map(assigneeOf);
  const filterRow = h("tr", { class: "filters" },
    h("td", {}, typed("key", t("col.item"))),
    h("td", {}, typed("title", t("col.title"))),
    h("td", {}, choose("state", t("col.state"), distinct(shown.map((it) => it.state)).map((v) => [v, v]))),
    h("td", {}, choose("who", t("tasks.assignee"), [
      ...(whoValues.includes("") ? [["-", t("tasks.nobody")]] : []),
      ...distinct(whoValues).map((v) => [v, person(v)])])),
    h("td", {}, choose("due", t("tasks.due"), [["overdue", t("tasks.dueOverdue")], ["today", t("tasks.dueToday")], ["week", t("tasks.dueWeek")], ["none", t("tasks.dueNone")]])),
    h("td", {}, choose("prio", t("tasks.priority"), distinct(shown.map((it) => { const f = typeOf.get(it.type).f.priority; return f ? it.properties[f.name] : ""; })).map((v) => [v, v]))),
    h("td", {}, h("select", { "aria-label": t("tasks.filterOf", { column: t("col.space") }), onchange: (ev) => {
      location.hash = `#/tasks/${current}${query({ space: ev.target.value })}`;
    } }, h("option", { value: "", text: t("tasks.allSpaces") }),
    [...new Set(open.map((it) => it.space))].sort().map((k) => h("option", { value: k, text: k, selected: k === space })))),
    h("td", {}));
  const reset = h("button", { type: "button", class: "link", text: t("tasks.filterReset"), hidden: true, onclick: () => {
    location.hash = `#/tasks/${current}${space ? "?space=" + encodeURIComponent(space) : ""}`;
  } });
  const tabLink = (id, label) => link(`#/tasks/${id}${query()}`, `${label} (${lists[id].length})`, id === current ? "tab active" : "tab");
  const tabs = h("nav", { class: "tabs", "aria-label": t("tasks.title") },
    tabLink("mine", t("tasks.mine")), tabLink("unassigned", t("tasks.unassigned")), tabLink("all", t("tasks.all")),
    link("#/kanbans", t("kanban.tab"), "tab", "tab-kanbans"));
  // The tab links carry the filters as they change.
  const relink = () => tabs.querySelectorAll("a").forEach((a) => { a.href = a.getAttribute("href").split("?")[0] + query(); });
  show(
    h("div", { class: "page-head" }, h("h1", { text: t("tasks.title") }),
      h("p", { class: "actions" }, link(newHref(space || here.space), t("tasks.new"), "button"))),
    tabs,
    status,
    h("section", { id: "tasks" }, shown.length
      ? [h("table", {}, h("thead", {}, h("tr", {}, [t("col.item"), t("col.title"), t("col.state"), t("tasks.assignee"), t("tasks.due"), t("tasks.priority"), t("col.space"), ""]
        .map((c) => h("th", { text: c }))), filterRow),
      h("tbody", {}, rows.map(([, tr]) => tr))), h("p", { class: "tasks-foot" }, count, reset), empty]
      : h("p", { class: "muted", text: current === "mine" ? t("tasks.noneMine") : t("tasks.none") })),
    h("p", { class: "muted", text: t("tasks.conventions") }),
    page.next ? h("p", { class: "notice", text: t("tasks.truncated", { n: items.length }) }) : null,
  );
  if (rows.length) apply();
}

// --- kanbans (ADR-0060) -------------------------------------------------------

// readAll reads every page of an item listing, at most boardMax items.
async function readAll(base) {
  let page = await get(base);
  const items = [...page.items];
  while (page.next && items.length < boardMax) {
    page = await get(`${base}&after=${encodeURIComponent(page.next)}`);
    items.push(...page.items);
  }
  return { items, truncated: Boolean(page.next) };
}

// kanbansView lists the kanbans of one's spaces and makes new ones: a
// name, a type with a workflow; their cards are the items with its label.
async function kanbansView() {
  const show = screen();
  const [{ kanbans }, { spaces }, { types }] = await Promise.all([get("/api/kanbans"), get("/api/spaces"), get("/api/types")]);
  const flows = types.filter((ty) => ty.workflow);
  const editable = spaces.filter((sp) => mayEdit(sp.role));
  const counts = await Promise.all(kanbans.map((k) =>
    get(`/api/items?space=${encodeURIComponent(k.space)}&type=${encodeURIComponent(k.type)}&label=${encodeURIComponent(k.label)}&limit=100`)
      .then((p) => (p.next ? p.items.length + "+" : p.items.length)).catch(() => "?")));
  const create = editable.length && flows.length ? form({ id: "new-kanban", class: "card" }, [
    h("h2", { text: t("kanban.new") }),
    h("div", { class: "form-row" },
      field(t("col.space"), h("select", { name: "space" }, editable.map((sp) => h("option", { value: sp.key, text: `${sp.key} ${sp.name}`, selected: sp.key === here.space })))),
      field(t("kanban.name"), h("input", { name: "name", required: true, maxlength: 80, placeholder: t("kanban.namePlaceholder") })),
      field(t("kanban.type"), h("select", { name: "type" }, flows.map((ty) => h("option", { value: ty.name, text: ty.name, selected: ty.name === "task" }))))),
    h("details", {}, h("summary", { text: t("kanban.labelMore") }),
      field(t("kanban.label"), h("input", { name: "label", placeholder: t("kanban.labelPlaceholder") }))),
    h("button", { type: "submit", class: "primary", text: t("kanban.create") }),
  ], async (d) => {
    const { kanban } = await post(`/api/spaces/${encodeURIComponent(d.get("space"))}/kanbans`, { name: d.get("name"), type: d.get("type"), label: (d.get("label") || "").trim() });
    location.hash = `#/kanban/${encodeURIComponent(kanban.space)}/${kanban.id}`;
  }) : null;
  const bySpace = new Map();
  kanbans.forEach((k, i) => bySpace.set(k.space, [...(bySpace.get(k.space) || []), { ...k, count: counts[i] }]));
  show(
    h("div", { class: "page-head" }, h("h1", { text: t("kanban.title") })),
    h("nav", { class: "tabs", "aria-label": t("tasks.title") },
      link("#/tasks/mine", t("tasks.mine"), "tab"), link("#/tasks/unassigned", t("tasks.unassigned"), "tab"), link("#/tasks/all", t("tasks.all"), "tab"),
      link("#/kanbans", t("kanban.tab"), "tab active", "tab-kanbans")),
    h("p", { class: "muted", text: t("kanban.intro") }),
    kanbans.length ? h("div", { id: "kanbans" }, [...bySpace].map(([sp, list]) => h("section", {},
      h("h2", { text: `${sp} · ${spaces.find((x) => x.key === sp)?.name || ""}` }),
      h("ul", { class: "space-tiles" }, list.map((k) => h("li", {},
        h("a", { class: "space-tile", href: `#/kanban/${encodeURIComponent(k.space)}/${k.id}` },
          h("strong", { text: k.name }),
          h("span", { class: "muted", text: t("kanban.meta", { type: k.type, label: k.label }) }),
          h("span", { class: "badge", text: t("kanban.cards", { n: k.count }) }))))))))
      : h("p", { class: "muted", text: t("kanban.none") }),
    create,
  );
}

// kanbanView shows one kanban: its cards in the columns of the type's
// workflow. Editors add a new card, put an existing item on it, take a
// card off (the item keeps its other labels and kanbans), rename the
// kanban or remove it.
async function kanbanView(space, id) {
  const show = screen();
  const [{ kanbans }, { types }, { spaces }] = await Promise.all([get(`/api/kanbans?space=${encodeURIComponent(space)}`), get("/api/types"), get("/api/spaces")]);
  const k = kanbans.find((x) => x.id === id);
  if (!k) throw new Error(t("kanban.missing"));
  here.space = space;
  const type = types.find((ty) => ty.name === k.type);
  if (!type?.workflow) throw new Error(t("board.noWorkflow", { type: k.type }));
  const canEdit = mayEdit(spaces.find((sp) => sp.key === space)?.role);
  const base = `/api/items?space=${encodeURIComponent(space)}&type=${encodeURIComponent(k.type)}&limit=100`;
  const [cards, all] = await Promise.all([readAll(`${base}&label=${encodeURIComponent(k.label)}`), canEdit ? readAll(base) : { items: [] }]);
  const status = h("div", { class: "status" });
  const setLabels = (it, labels) => put(`/api/items/${it.id}/labels`, { labels });
  const off = (it) => canEdit ? h("button", { type: "button", class: "link kanban-off", text: t("kanban.takeOff"),
    onclick: () => action(() => setLabels(it, it.labels.filter((l) => l !== k.label)), status) }) : null;
  const elsewhere = all.items.filter((it) => !it.labels.includes(k.label));
  const tools = canEdit ? h("div", { class: "kanban-tools" },
    form({ id: "kanban-add", class: "inline-form" }, [
      h("input", { name: "title", required: true, placeholder: t("kanban.newCard"), "aria-label": t("kanban.newCard") }),
      h("button", { type: "submit", class: "primary", text: t("kanban.add") }),
    ], async (d) => {
      const { item } = await post("/api/items", { space, type: k.type, title: d.get("title").trim() });
      await setLabels(item, [k.label]);
      route();
    }),
    elsewhere.length ? form({ id: "kanban-assign", class: "inline-form" }, [
      h("select", { name: "item", "aria-label": t("kanban.assign") }, elsewhere.map((it) => h("option", { value: it.id, text: `${it.key} ${it.title}` }))),
      h("button", { type: "submit", text: t("kanban.assign") }),
    ], async (d) => {
      const it = elsewhere.find((x) => x.id === d.get("item"));
      await setLabels(it, [...it.labels, k.label]);
      route();
    }) : null,
    h("span", { class: "kanban-admin" },
      h("button", { type: "button", class: "link", text: t("kanban.rename"), onclick: () => {
        const name = prompt(t("kanban.name"), k.name);
        if (name && name.trim() !== k.name) action(() => put(`/api/spaces/${encodeURIComponent(space)}/kanbans/${k.id}`, { name }), status);
      } }), " · ",
      h("button", { type: "button", class: "link", text: t("kanban.remove"), onclick: () => confirm(t("kanban.removeConfirm", { name: k.name }))
        && action(async () => { await del(`/api/spaces/${encodeURIComponent(space)}/kanbans/${k.id}`); location.hash = "#/kanbans"; }, status) }))) : null;
  show(
    h("p", {}, link("#/tasks", t("nav.tasks")), " / ", link("#/kanbans", t("kanban.tab")), " / " + k.name),
    h("div", { class: "page-head" }, h("h1", { text: k.name }), h("p", { class: "muted", text: t("kanban.metaLong", { space, type: k.type, label: k.label }) })),
    tools,
    cards.truncated ? h("p", { class: "notice", text: t("board.truncated", { n: cards.items.length }) }) : null,
    status,
    h("div", { id: "kanban" }, boardOf(cards.items, type, status, off)),
  );
}

// albumView shows the images and drawings of every item one may read
// (ADR-0054); each leads to its item.
function albumView() {
  screen()(h("h1", { text: t("album.title") }), h("p", { class: "muted", text: t("album.intro") }), h("section", { id: "album" }, albumBrowser()));
}

// infoTip is an "i" that explains what it stands beside. text holds a
// first line and, after it, one line per point. It opens on a click (so it
// works on touch screens and from the keyboard) and closes on a second
// click, Escape or a click elsewhere. note, if given, closes it as a
// paragraph of its own.
let infoTips = 0;
function infoTip(label, text, note) {
  const [intro, ...points] = text.split("\n");
  const id = "info-tip-" + ++infoTips;
  const pop = h("div", { class: "info-pop", id, role: "note", hidden: true },
    h("p", { text: intro }), points.length ? h("ul", {}, points.map((p) => h("li", { text: p }))) : null,
    note ? h("p", { class: "muted", text: note }) : null);
  const button = h("button", { type: "button", class: "info-btn", "aria-label": t("common.infoAbout", { what: label }), "aria-expanded": "false", "aria-controls": id, text: "i" });
  const wrap = h("span", { class: "info-tip" }, button, pop);
  const close = () => { pop.hidden = true; button.setAttribute("aria-expanded", "false"); document.removeEventListener("click", outside, true); document.removeEventListener("keydown", esc); };
  const outside = (e) => { if (!wrap.contains(e.target)) close(); };
  const esc = (e) => { if (e.key === "Escape") { close(); button.focus(); } };
  button.addEventListener("click", () => {
    if (!pop.hidden) return close();
    pop.hidden = false;
    button.setAttribute("aria-expanded", "true");
    document.addEventListener("click", outside, true);
    document.addEventListener("keydown", esc);
  });
  return wrap;
}

// governanceOverview shows the governance check of every space one holds
// a role in, as counts that lead to each space's check (ADR-0040).
async function governanceOverview() {
  const show = screen();
  const { spaces } = await get("/api/spaces");
  const reports = await Promise.all(spaces.map((s) => get(`/api/spaces/${encodeURIComponent(s.key)}/governance`)));
  show(
    h("h1", { text: t("gov.title") }),
    h("p", { class: "muted", text: t("gov.intro") }),
    h("section", { id: "governance-overview" }, spaces.length
      ? h("table", {}, h("thead", {}, h("tr", {}, h("th", { text: t("col.space") }),
        h("th", {}, t("gov.colSensitive"), " ", infoTip(t("gov.colSensitive"), t("gov.infoSensitive"), t("gov.infoSensitiveNote"))),
        h("th", {}, t("gov.colStale"), " ", infoTip(t("gov.colStale"), t("gov.infoStale", { days: 365 }))),
        h("th", {}, t("gov.colImplausible"), " ", infoTip(t("gov.colImplausible"), t("gov.infoImplausible"))),
        h("th", {}, t("gov.colPersonal"), " ", infoTip(t("gov.colPersonal"), t("gov.infoPersonal"))))),
        h("tbody", {}, spaces.map((s, i) => h("tr", {},
          h("td", {}, link("#/governance/" + encodeURIComponent(s.key), s.key), " ", h("span", { class: "muted", text: s.name })),
          h("td", { text: String(reports[i].sensitive.length) }),
          h("td", { text: String(reports[i].stale.length) }),
          h("td", { text: String(reports[i].implausible.length) }),
          h("td", { text: String((reports[i].personal || []).length) }),
        ))))
      : h("p", { class: "muted", text: t("gov.noSpaces") })),
  );
}

// --- governance --------------------------------------------------------------

// governanceView checks a space (ADR-0040): sensitive data, shown masked;
// items not changed for a while; formal contradictions. Each finding names
// who is to act. Saving sensitive data has already restricted the item to
// its author; an administrator lifts that here after checking.
async function governanceView(key) {
  const show = screen();
  const days = Number(new URLSearchParams(location.hash.split("?")[1] || "").get("staleDays")) || 365;
  const rep = await get(`/api/spaces/${encodeURIComponent(key)}/governance?staleDays=${days}`);
  await learnNames([...rep.sensitive, ...rep.stale, ...rep.implausible, ...rep.personal].map((f) => f.responsible));
  const { regulations } = await get("/api/regulations").catch(() => ({ regulations: [] }));
  const regName = new Map(regulations.map((r) => [r.id, r.name.split(" – ")[0]]));
  const status = h("div", { class: "status" });
  const who = person;
  const itemCell = (f) => h("td", {}, link("#/item/" + encodeURIComponent(f.key), f.title || f.key), h("div", { class: "muted", text: f.key }));
  const lift = async (f) => {
    const { item } = await get("/api/items/" + f.item);
    await put(`/api/items/${f.item}/restriction`, { read: [], edit: item.restriction?.edit || [] });
  };
  const table = (cols, rows) => rows.length
    ? h("table", {}, h("thead", {}, h("tr", {}, cols.map((c) => h("th", { text: c })))), h("tbody", {}, rows))
    : h("p", { class: "muted", text: t("common.nothing") });
  const daysInput = h("input", { type: "number", min: 1, max: 3650, value: days, "aria-label": t("gov.days"), class: "days" });
  show(
    h("p", {}, link("#/", t("nav.spaces")), " / ", link("#/space/" + encodeURIComponent(key), key), " / " + t("gov.crumb")),
    h("h1", { text: t("gov.heading", { key }) }),
    h("section", { id: "governance" },
      h("p", {}, h("button", { text: t("gov.checkAgain"), onclick: () => {
        location.hash = `#/governance/${encodeURIComponent(key)}?staleDays=${Number(daysInput.value) || 365}`;
        route();
      } }), t("gov.staleBefore"), daysInput, t("gov.staleAfter")),
      status,
      h("h2", {}, t("gov.sensitive", { n: rep.sensitive.length }), " ", infoTip(t("gov.colSensitive"), t("gov.infoSensitive"), t("gov.infoSensitiveNote"))),
      h("p", { class: "muted", text: t("gov.sensitiveNote") }),
      table([t("col.item"), t("gov.where"), t("gov.what"), t("col.responsible"), t("gov.access")], rep.sensitive.map((f) => h("tr", {},
        itemCell(f), h("td", { text: f.where }), h("td", {}, h("div", { text: f.kind }), h("code", { text: f.masked })),
        h("td", { text: who(f.responsible) }),
        h("td", {}, f.restricted
          ? [h("span", { text: t("gov.restricted") + " " }), h("button", { class: "link", text: t("gov.lift"), onclick: () => action(() => lift(f), status) })]
          : t("gov.open"))))),
      h("h2", {}, t("gov.stale", { days: rep.staleDays, n: rep.stale.length }), " ", infoTip(t("gov.colStale"), t("gov.infoStale", { days: rep.staleDays }))),
      table([t("col.item"), t("gov.lastChange"), t("col.responsible")], rep.stale.map((f) => h("tr", {},
        itemCell(f), h("td", { text: when(f.lastChange) }), h("td", { text: who(f.responsible) })))),
      h("h2", {}, t("gov.implausible", { n: rep.implausible.length }), " ", infoTip(t("gov.colImplausible"), t("gov.infoImplausible"))),
      table([t("col.item"), t("col.problem"), t("col.responsible")], rep.implausible.map((f) => h("tr", {},
        itemCell(f), h("td", { text: f.problem }), h("td", { text: who(f.responsible) })))),
      h("h2", { id: "personal-data" }, t("gov.personal", { n: rep.personal.length }), " ", infoTip(t("gov.colPersonal"), t("gov.infoPersonal"))),
      table([t("col.item"), t("gov.what"), t("dp.status"), t("col.responsible")], rep.personal.map((f) => h("tr", { class: f.held ? "held" : "" },
        itemCell(f),
        h("td", {}, f.findings.map((x) => h("div", {}, h("span", { class: "chip", text: x.indicator }), ` ${x.category} (${regName.get(x.regulation) || x.regulation})`))),
        h("td", {}, f.held ? [h("strong", { text: t("dp.statusHeld") }), f.mayRelease ? [" · ", link("#/item/" + encodeURIComponent(f.key), t("dp.toRelease"))] : null] : t("dp.statusRestricted")),
        h("td", { text: who(f.responsible) })))),
    ),
  );
}

// --- link check --------------------------------------------------------------

// linksView shows the latest link check of a space (ADR-0039): the broken
// web links of items one may read, with who is to fix each. A space
// administrator starts a run; while it runs the view refreshes itself.
async function linksView(key) {
  const show = screen();
  const path = `/api/spaces/${encodeURIComponent(key)}/linkcheck`;
  const report = await get(path);
  await learnNames((report.broken || []).map((b) => b.responsible));
  const status = h("div", { class: "status" });
  const who = person;
  let body;
  if (!report.enabled) {
    body = h("p", { class: "notice", text: t("links.off") });
  } else {
    const start = h("button", { text: report.running ? t("links.checking") : t("links.check"), disabled: report.running,
      onclick: () => action(() => post(path), status) });
    const summary = !report.started ? t("links.never")
      : report.running ? t("links.since", { when: when(report.started) })
        : t(report.truncated ? "links.doneTruncated" : "links.done", { n: report.checked, when: when(report.finished) });
    body = h("div", {},
      h("p", {}, start, " ", h("span", { class: "muted", text: summary })),
      status,
      report.finished && !report.running
        ? (report.broken.length
          ? h("table", { class: "links" }, h("thead", {}, h("tr", {}, [t("links.usedIn"), t("links.link"), t("col.problem"), t("col.responsible")].map((c) => h("th", { text: c })))),
            h("tbody", {}, report.broken.map((b) => h("tr", {},
              // The item the link is used in, and where in its body.
              h("td", {}, link("#/item/" + encodeURIComponent(b.key), b.title || b.key),
                h("div", { class: "muted", text: t("links.block", { key: b.key, n: b.block + 1 }) })),
              h("td", { class: "href", text: b.href }),
              h("td", { text: b.problem }),
              h("td", { text: b.responsible ? who(b.responsible) : "—" }),
            ))))
          : h("p", { text: t("links.none") }))
        : null,
    );
    if (report.running) {
      const timer = setTimeout(route, 2000);
      onLeave(() => clearTimeout(timer));
    }
  }
  show(
    h("p", {}, link("#/", t("nav.spaces")), " / ", link("#/space/" + encodeURIComponent(key), key), " / " + t("links.crumb")),
    h("h1", { text: t("links.heading", { key }) }),
    h("section", { id: "linkcheck" }, body),
  );
}

// propertyInput is the input for a field of a type; it reads back with
// readProperty.
function propertyInput(f, value) {
  const name = "p:" + f.name;
  switch (f.kind) {
    case "bool":
      return h("input", { name, type: "checkbox", checked: value === true });
    case "number":
      return h("input", { name, type: "number", step: "any", value: value ?? "" });
    case "date":
      return h("input", { name, type: "date", value: value ? String(value).slice(0, 10) : "" });
    case "enum":
      return h("select", { name },
        h("option", { value: "", text: "—" }),
        f.values.map((v) => h("option", { value: v, text: v, selected: v === value })));
    default:
      return h("input", { name, value: value ?? "" });
  }
}

function readProperty(f, el) {
  if (f.kind === "bool") return el.checked;
  const v = el.value;
  if (v === "") return null;
  if (f.kind === "number") return Number(v);
  return v;
}

// newItemForm creates an item in a space, below parent (an item) if one is
// given; "page" is preselected when the type exists. With spaces, the
// space is chosen in the form.
function newItemForm({ space, spaces = null, types, parent = null, heading = t("item.new"), id = "new-item" }) {
  if (types.length === 0) return null;
  const spaceSelect = spaces && spaces.length > 1
    ? h("select", { name: "space" }, spaces.map((s) => h("option", { value: s.key, text: `${s.name} (${s.key})`, selected: s.key === space })))
    : null;
  const under = parent ? h("input", { type: "checkbox", name: "under", checked: true }) : null;
  // A page lies below its parent only in the parent's space.
  if (spaceSelect && under) {
    spaceSelect.addEventListener("change", () => {
      under.disabled = spaceSelect.value !== parent.space;
      if (under.disabled) under.checked = false;
    });
  }
  const fields = h("div");
  const typeSelect = h("select", { name: "type" }, types.map((ty) => h("option", { value: ty.name, text: ty.name, selected: ty.name === "page" })));
  const showFields = () => {
    const ty = types.find((x) => x.name === typeSelect.value);
    fields.replaceChildren(...ty.fields.map((f) => field(f.name + (f.required ? " *" : ""), propertyInput(f))));
  };
  typeSelect.addEventListener("change", showFields);
  showFields();
  const f = form({ class: "card", id }, [
    h("h2", { text: heading }),
    spaceSelect ? field(t("col.space"), spaceSelect) : null,
    under ? h("label", { class: "check" }, under, " ", t("new.under", { title: parent.title || parent.key })) : null,
    field(t("col.type"), typeSelect),
    field(t("col.title"), h("input", { name: "title", required: true })),
    fields,
    h("button", { type: "submit", text: t("common.create") }),
  ], async () => {
    const ty = types.find((x) => x.name === typeSelect.value);
    const properties = {};
    for (const fd of ty.fields) {
      const v = readProperty(fd, f.querySelector(`[name="p:${CSS.escape(fd.name)}"]`));
      if (v !== null) properties[fd.name] = v;
    }
    const target = spaceSelect ? spaceSelect.value : space;
    const below = under && under.checked && !under.disabled ? parent.id : "";
    const { item } = await post("/api/items", { space: target, type: ty.name, parent: below, title: f.querySelector("[name=title]").value, properties });
    // A new item with a body opens for writing (ADR-0051).
    location.hash = "#/item/" + encodeURIComponent(item.key) + (ty.body ? "?edit" : "");
  });
  return f;
}

// --- one item ---------------------------------------------------------------

// itemView shows an item for reading; with ?edit its title and body are
// edited in place, in the live session (ADR-0051). The column on the right
// holds what belongs to the item besides its text: owner and details,
// comments, files, relations, access and history.
async function itemView(ref, params = new URLSearchParams()) {
  const show = screen();
  const { item, editable } = await get("/api/items/" + encodeURIComponent(ref));
  const path = "/api/items/" + item.id;
  const [{ types }, history, out, inc, embeds, comments, { attachments }, { watches }, { spaces }] = await Promise.all([
    get("/api/types"),
    get(path + "/history"),
    get(path + "/relations?direction=outgoing"),
    get(path + "/relations?direction=incoming"),
    get(path + "/embeds"),
    get(path + "/comments"),
    get(path + "/attachments"),
    get("/api/me/watches"),
    get("/api/spaces"),
  ]);
  const spaceAdmin = spaces.find((sp) => sp.key === item.space)?.role === "space-admin";
  const type = types.find((ty) => ty.name === item.type) || { fields: [], workflow: null, body: false };
  const level = `/api/spaces/${encodeURIComponent(item.space)}/tree` + (item.parent ? "?parent=" + item.parent : "");
  const [, open, siblings] = await Promise.all([
    learnNames([...history.events.map((e) => e.actor), ...type.fields.filter((f) => f.kind === "user").map((f) => item.properties[f.name]), ...comments.comments.flatMap((c) => c.resolvedBy ? [c.author, c.resolvedBy] : [c.author]), ...attachments.map((a) => a.author)]),
    ancestors(item),
    item.presence === "live" ? get(level) : { items: [] },
  ]);
  const status = h("div", { class: "status" });
  const live = item.presence === "live";
  // Changes are offered only to who may make them (role, restriction,
  // import hold); the server decides again on every request.
  const canEdit = live && editable;
  const editing = canEdit && params.has("edit");
  const itemHref = "#/item/" + encodeURIComponent(item.key);
  here.space = item.space;
  here.parent = item.id;
  const threads = commentThreads(path, comments, canEdit);

  // Moving up or down among the siblings: the keyboard's way to reorder,
  // besides dragging in the tree (ADR-0041).
  const at = siblings.items.findIndex((n) => n.id === item.id);
  const place = (before) => action(() => post(path + "/move", { space: item.space, parent: item.parent || "", before }), status);
  const reorder = [];
  if (canEdit && at > 0) reorder.push(h("button", { text: t("item.moveUp"), onclick: () => place(siblings.items[at - 1].id) }));
  if (canEdit && at >= 0 && (at + 1 < siblings.items.length)) {
    const after = siblings.items[at + 2];
    if (after || !siblings.next) reorder.push(h("button", { text: t("item.moveDown"), onclick: () => place(after ? after.id : "") }));
  }

  const restricted = (item.restriction?.read?.length || 0) + (item.restriction?.edit?.length || 0) > 0;
  const title = editing
    ? form({ class: "inline", id: "title" }, [
      h("input", { name: "title", value: item.title, class: "title", "aria-label": t("col.title") }),
      h("button", { type: "submit", text: t("item.saveTitle") }),
    ], async (data) => { await put(path + "/title", { title: data.get("title") }); route(); })
    : h("h1", { class: "item-title" }, restricted ? lockIcon(true) : null, item.title || t("item.erased"));

  const props = h("table", { class: "props" }, type.fields.map((f) => {
    const input = propertyInput(f, item.properties[f.name]);
    input.disabled = !canEdit;
    return h("tr", {},
      h("th", { text: f.name }),
      h("td", {}, input),
      h("td", {}, canEdit ? h("button", { text: t("common.save"), onclick: () => action(() => put(`${path}/properties/${encodeURIComponent(f.name)}`, { value: readProperty(f, input) }), status) }) : null,
        // A user field names a person by ID; show who it is, and let one
        // put oneself in (ADR-0049).
        f.kind === "user" && item.properties[f.name] ? h("span", { class: "muted", text: " " + person(item.properties[f.name]) }) : null,
        f.kind === "user" && canEdit && item.properties[f.name] !== me.user.id
          ? h("button", { class: "link", text: t("tasks.assignMe"), onclick: () => action(() => put(`${path}/properties/${encodeURIComponent(f.name)}`, { value: me.user.id }), status) }) : null),
    );
  }));

  const transitions = type.workflow && canEdit
    ? type.workflow.transitions.filter((tr) => tr.from.includes(item.state))
      .map((tr) => h("button", { text: tr.name, onclick: () => action(() => post(path + "/transitions", { name: tr.name }), status) }))
    : [];

  const actions = canEdit
    ? [h("button", { text: t("item.trash"), onclick: () => action(() => post(path + "/trash"), status) })]
    : item.presence === "trashed" && editable
      ? [h("button", { text: t("item.restore"), onclick: () => action(() => post(path + "/restore"), status) }),
        h("button", { class: "danger", text: t("item.purge"), onclick: () => confirm(t("item.purgeConfirm")) && action(() => post(path + "/purge"), status) })]
      : [];

  const lastRestriction = history.events.filter((e) => e.kind === "RestrictionSet").pop();
  const guarded = item.restriction?.read?.length && lastRestriction?.actor === "governance";
  // The tree opens down to the page and shows the page's own children.
  const tree = pageTree({ space: item.space, current: item.id, newPage: canEdit ? newHref(item.space, item.id) : "", open: [...open, item.id], onMoved: () => route(), onError: (err) => status.replaceChildren(errorBox(err)) });
  // Leaving the editor stores what is not yet saved, then shows the page.
  let session = null;
  const done = h("button", { type: "button", id: "edit-done", class: "primary", text: t("item.done"), onclick: async () => {
    done.disabled = true;
    try { if (session) await session.finish(); } finally { location.hash = itemHref; }
  } });
  const headActions = h("p", { class: "actions" },
    live ? watchButton(path, watches.some((w) => w.item === item.id), status) : null,
    editing ? done : canEdit ? link(itemHref + "?edit", t("item.edit"), "button primary", "edit") : null,
    canEdit && !editing ? link(newHref(item.space, item.id), t("item.newChild"), "button", "item-new-child") : null);
  let body = null;
  if (type.body) {
    const b = bodySection(item, embeds.embeds, editing, threads, attachments);
    body = b.element;
    session = b.session;
  }
  const card = (id, ...content) => h("section", { class: "side-card", id }, ...content);
  const details = card("details",
    h("h2", { text: t("item.properties") }),
    type.fields.length ? props : null,
    transitions.length ? h("p", { class: "actions-row" }, transitions) : null,
    labelsLine(path, item, canEdit, status));
  const more = reorder.length || actions.length ? card("item-actions", h("p", { class: "actions-row" }, reorder, actions)) : null;
  show(withTree(tree,
    h("p", {}, link("#/", t("nav.spaces")), " / ", link("#/space/" + encodeURIComponent(item.space), item.space), " / " + item.key),
    h("div", { class: "page-head" }, title, headActions),
    guarded ? h("p", { class: "notice", text: t("item.guarded") }) : null,
    item.hold ? await holdBanner(item, path, canEdit) : null,
    h("p", { class: "muted" }, t("item.meta", { type: item.type, presence: item.presence }), item.state ? t("item.state", { state: item.state }) : "", t("item.version", { n: item.version })),
    status,
    h("div", { class: "item-layout" },
      h("div", { class: "item-main" }, body || h("p", { class: "muted", text: t("item.noBody") })),
      h("aside", { class: "item-side", "aria-label": t("item.side") },
        details,
        threads.section,
        attachmentsSection(path, attachments, canEdit),
        await relationsSection(path, out.relations, inc.relations, item.id, canEdit),
        await accessSection(path, item, canEdit, spaceAdmin),
        historySection(path, history.events, canEdit),
        more)),
  ));
}

// bodySection shows an item's body: for reading, with the passages
// commented on highlighted and commenting on a selection offered; or, when
// editing, in its live session (ADR-0010), where changes reach everyone at
// once and are saved as one leaves a block.
function bodySection(item, embeds, editing, threads, attachments) {
  const resolve = async (ref) => (await get("/api/items/" + encodeURIComponent(ref))).item;
  if (!editing) {
    const view = createEditor({ blocks: item.body, embeds, editable: false, resolve, childPages: childPagesOf(item), files: filesOf(item),
      extra: item.presence === "live" ? [threads.plugin] : [], comment: item.presence === "live" ? threads.start : undefined });
    return { element: h("section", { id: "body", class: "reading" }, view.element), session: null };
  }
  const session = liveBody({ ref: item.id, embeds, resolve, childPages: childPagesOf(item), files: filesOf(item, attachments),
    extra: [threads.plugin], comment: threads.start });
  onLeave(session.close);
  return { element: h("section", { id: "body", class: "editing" }, session.element), session };
}

// relationsSection lists an item's relations both ways, with the related
// items' keys and titles. One who may edit the item relates it to another
// by key and removes relations made by hand; those derived from a body
// change with the body.
async function relationsSection(path, out, inc, self, canEdit) {
  const others = [...new Set([...out.map((r) => r.to), ...inc.map((r) => r.from)])].slice(0, 50);
  const known = new Map(await Promise.all(others.map(async (id) => [id, await get("/api/items/" + id).then((x) => x.item).catch(() => null)])));
  const label = (id) => { const it = known.get(id); return it ? `${it.key} ${it.title}` : id.slice(0, 8); };
  const status = h("div", { class: "status" });
  const row = (r, other, outgoing) => h("li", {},
    h("span", { class: "muted", text: (outgoing ? r.type : t("rel.incoming", { type: r.type })) + (r.derived ? t("rel.fromBody") : "") + " " }),
    link("#/item/" + encodeURIComponent(known.get(other)?.key || other), label(other)),
    canEdit && outgoing && !r.derived ? h("button", { type: "button", class: "link", text: "×", title: t("rel.remove"), "aria-label": t("rel.remove"),
      onclick: () => action(() => del("/api/relations/" + encodeURIComponent(r.id)), status) }) : null);
  const add = canEdit ? form({ id: "add-relation", class: "inline" }, [
    h("input", { name: "type", required: true, list: "relation-types", placeholder: t("rel.type"), "aria-label": t("rel.type"), size: 12 }),
    h("datalist", { id: "relation-types" }, ["relates-to", "blocks", "implements", "duplicates", "depends-on"].map((v) => h("option", { value: v }))),
    h("input", { name: "target", required: true, placeholder: t("rel.target"), "aria-label": t("rel.target"), size: 12 }),
    h("button", { type: "submit", text: t("rel.add") }),
  ], async (d) => {
    const target = (await get("/api/items/" + encodeURIComponent(d.get("target").trim()))).item;
    await post("/api/relations", { type: d.get("type").trim(), from: self, to: target.id });
    route();
  }) : null;
  if (out.length + inc.length === 0 && !add) return null;
  return h("section", { id: "relations" },
    h("h2", { text: t("rel.title") }),
    out.length + inc.length ? h("ul", { class: "list" }, out.map((r) => row(r, r.to, true)), inc.map((r) => row(r, r.from, false)))
      : h("p", { class: "muted", text: t("rel.none") }),
    add, status);
}

// accessSection shows who may read and edit an item and lets one who may
// edit it narrow that, without locking themselves out; a space
// administrator may name anyone (ADR-0022). Holders of the erasure role
// redact an item at once (ADR-0026).
async function accessSection(path, item, canEdit, spaceAdmin) {
  const r = item.restriction || { read: [], edit: [] };
  const read = r.read || [], edit = r.edit || [];
  await learnNames([...read, ...edit].filter((p) => !p.startsWith("@")));
  const status = h("div", { class: "status" });
  const listed = (list) => list.length ? list.map((p) => p.startsWith("@") ? p : person(p)).join(", ") : t("access.everyone");
  const set = (next) => action(() => put(path + "/restriction", next), status);
  const restricted = read.length + edit.length > 0;
  // Who else may read or edit a restricted item is chosen among the
  // accounts, which space administrators may list.
  const users = canEdit && restricted && spaceAdmin ? (await get("/api/users")).users : null;
  if (users) for (const u of users) names.set(u.id, u.username);
  const pick = users ? form({ id: "restrict", class: "inline" }, [
    h("select", { name: "who", "aria-label": t("access.who") }, users.map((u) => h("option", { value: u.id, text: u.username }))),
    h("select", { name: "what", "aria-label": t("access.what") }, h("option", { value: "read", text: t("access.mayRead") }), h("option", { value: "edit", text: t("access.mayEdit") })),
    h("button", { type: "submit", text: t("access.add") }),
  ], async (d) => {
    const who = d.get("who");
    const next = { read: [...read], edit: [...edit] };
    // Whoever may edit a restricted item must also read it.
    if (next.read.length && !next.read.includes(who)) next.read.push(who);
    if (d.get("what") === "edit" && next.edit.length && !next.edit.includes(who)) next.edit.push(who);
    await put(path + "/restriction", next);
    route();
  }) : null;
  // Open is the normal case; a restriction is for sensitive content, and
  // shows as a closed lock on a coloured bar (ADR-0051).
  return h("section", { id: "access", class: "side-card " + (restricted ? "access-restricted" : "access-open") },
    h("h2", { class: "access-bar" }, lockIcon(restricted), restricted ? t("access.restricted") : t("access.open")),
    h("p", {}, t("access.read", { who: listed(read) }), h("br"), t("access.edit", { who: listed(edit) })),
    canEdit ? h("p", { class: "actions-row" },
      h("button", { type: "button", class: "secondary", text: t("access.onlyMe"), onclick: () => set({ read: [me.user.id], edit: [me.user.id] }) }),
      h("button", { type: "button", class: "secondary", text: t("access.onlyMeEdits"), onclick: () => set({ read: read, edit: [me.user.id] }) }),
      restricted ? h("button", { type: "button", class: "secondary", text: t("access.lift"), onclick: () => set({ read: [], edit: [] }) }) : null) : null,
    pick,
    me.erasure && item.presence !== "purged" ? h("p", {}, h("button", { type: "button", class: "danger", text: t("access.redact"), onclick: () => {
      const reason = prompt(t("access.redactReason"));
      if (reason && reason.trim() && confirm(t("access.redactConfirm", { key: item.key }))) action(() => post(path + "/redact", { reason: reason.trim() }), status);
    } })) : null,
    status);
}

// commentThreads shows an item's comments (ADR-0035) as threads: a first
// comment, on the whole item or on a passage of its body, and the replies
// to it (ADR-0047). Text is inserted as text (ADR-0014); one's own
// comments can be edited and deleted, and a thread resolved and reopened.
// It returns the section, a plugin highlighting the passages of the open
// threads in the body, and start(anchor), which begins a comment on a
// passage.
function commentThreads(path, page, live) {
  const author = (c) => person(c.author);
  const roots = page.comments.filter((c) => !c.reply);
  const replies = new Map();
  for (const c of page.comments.filter((c) => c.reply)) replies.set(c.reply, [...(replies.get(c.reply) || []), c]);
  const anchored = roots.filter((c) => c.anchor && !c.deleted && !c.resolved);
  const threadEls = new Map(); // root id -> its element

  const row = (c) => {
    const li = h("li", { class: "comment", id: "comment-" + c.id });
    const head = h("div", { class: "muted comment-head" }, face(c.author), `${author(c)} · ${when(c.created)}`, c.edited ? t("comment.edited") : "");
    if (c.deleted) {
      li.append(head, h("p", { class: "muted", text: t("comment.deleted") }));
      return li;
    }
    const mine = live && c.author === me.user.id;
    const status = h("div", { class: "status" });
    const edit = () => li.replaceChildren(head, form({ class: "comment-form" }, [
      h("textarea", { name: "text", rows: 3, "aria-label": t("comment.editLabel"), value: c.text }),
      h("p", {}, h("button", { type: "submit", text: t("comment.save") }), h("button", { type: "button", class: "link", text: t("common.cancel"), onclick: () => route() })),
    ], async (data) => { await put(`${path}/comments/${c.id}`, { text: data.get("text") }); route(); }));
    li.append(head, h("p", { class: "comment-text", text: c.text }), status, mine
      ? h("p", {},
        h("button", { class: "link", text: t("common.edit"), onclick: edit }), " · ",
        h("button", { class: "link", text: t("common.delete"), onclick: () => confirm(t("comment.deleteConfirm")) && action(() => del(`${path}/comments/${c.id}`), status) }))
      : ""); // append() would write a null out as the word "null"
    return li;
  };

  // reveal scrolls to a passage in the body and marks it for a moment.
  const reveal = (id) => {
    const marks = document.querySelectorAll(`.comment-anchor[data-comment="${CSS.escape(id)}"]`);
    if (!marks.length) return;
    marks[0].scrollIntoView({ block: "center" });
    marks.forEach((m) => m.classList.add("comment-focus"));
    setTimeout(() => marks.forEach((m) => m.classList.remove("comment-focus")), 1500);
  };

  const thread = (c) => {
    const status = h("div", { class: "status" });
    const quote = c.anchor && !c.deleted
      ? h("blockquote", { class: "comment-quote" },
        h("button", { type: "button", class: "link", text: c.anchor.quote, title: t("comment.showPassage"), onclick: () => reveal(c.id) }))
      : null;
    const gone = h("p", { class: "muted comment-gone", hidden: true, text: t("comment.passageGone") });
    const answers = h("ul", { class: "comments replies" }, (replies.get(c.id) || []).map(row));
    const replyForm = h("div");
    const reply = () => {
      replyForm.replaceChildren(form({ class: "comment-form" }, [
        h("textarea", { name: "text", rows: 2, "aria-label": t("comment.replyLabel"), required: true }),
        h("p", {}, h("button", { type: "submit", text: t("comment.reply") }),
          h("button", { type: "button", class: "link", text: t("common.cancel"), onclick: () => replyForm.replaceChildren() })),
      ], async (data) => { await post(path + "/comments", { text: data.get("text"), reply: c.id }); route(); }));
      replyForm.querySelector("textarea").focus();
    };
    const tools = live && !c.deleted
      ? h("p", { class: "thread-tools" },
        h("button", { class: "link", text: t("comment.reply"), onclick: reply }), " · ",
        c.resolved
          ? h("button", { class: "link", text: t("comment.reopen"), onclick: () => action(() => del(`${path}/comments/${c.id}/resolved`), status) })
          : h("button", { class: "link", text: t("comment.resolve"), onclick: () => action(() => put(`${path}/comments/${c.id}/resolved`), status) }))
      : null;
    const el = h("li", { class: "thread" + (c.anchor ? " inline" : "") },
      quote, gone, h("ul", { class: "comments" }, row(c)), answers,
      c.resolved ? h("p", { class: "muted", text: t("comment.resolvedBy", { name: person(c.resolvedBy), when: when(c.resolvedAt) }) }) : null,
      tools, replyForm, status);
    threadEls.set(c.id, el);
    return el;
  };

  const pending = h("div", { id: "inline-comment" });
  const start = (anchor) => {
    pending.replaceChildren(form({ class: "comment-form card" }, [
      h("blockquote", { class: "comment-quote", text: anchor.quote }),
      h("textarea", { name: "text", rows: 3, placeholder: t("comment.placeholder"), "aria-label": t("comment.inlineLabel"), required: true }),
      h("p", {}, h("button", { type: "submit", text: t("comment.add") }),
        h("button", { type: "button", class: "link", text: t("common.cancel"), onclick: () => pending.replaceChildren() })),
    ], async (data) => { await post(path + "/comments", { text: data.get("text"), anchor }); route(); }));
    pending.scrollIntoView({ block: "nearest" });
    pending.querySelector("textarea").focus();
  };

  const open = roots.filter((c) => !c.resolved), done = roots.filter((c) => c.resolved);
  const add = live
    ? form({ id: "add-comment", class: "comment-form" }, [
      h("textarea", { name: "text", rows: 3, placeholder: t("comment.placeholder"), "aria-label": t("comment.label"), required: true }),
      h("p", {}, h("button", { type: "submit", text: t("comment.add") })),
    ], async (data) => { await post(path + "/comments", { text: data.get("text") }); route(); })
    : null;
  const section = h("section", { id: "comments" },
    h("h2", { text: t("comment.title", { n: page.total.value }) }),
    pending,
    open.length ? h("ul", { class: "threads" }, open.map(thread)) : h("p", { class: "muted", text: t("comment.none") }),
    done.length ? h("details", { class: "resolved-threads" }, h("summary", { text: t("comment.resolvedThreads", { n: done.length }) }),
      h("ul", { class: "threads" }, done.map(thread))) : null,
    page.next ? h("p", { class: "muted", text: t("comment.truncated", { n: page.comments.length }) }) : null,
    add,
  );

  const plugin = commentPlugin({
    threads: () => anchored,
    // A thread whose passage is gone says so.
    found: (ids) => anchored.forEach((c) => { const g = threadEls.get(c.id)?.querySelector(".comment-gone"); if (g) g.hidden = ids.has(c.id); }),
    open: (id) => {
      const el = threadEls.get(id);
      if (!el) return;
      el.scrollIntoView({ block: "nearest" });
      el.classList.add("comment-focus");
      setTimeout(() => el.classList.remove("comment-focus"), 1500);
    },
  });
  return { section, plugin, start };
}

// historySection lists an item's history. A change of the body can be
// compared with the body before it and, on a live item, restored
// (ADR-0043).
function historySection(path, events, live) {
  const shown = h("div", { id: "version" });
  const bodies = events.filter((e) => e.kind === "BodyCommitted" && !e.erased);
  const version = async (seq) => (await get(`${path}/versions/${seq}`)).body;
  const compare = async (e) => {
    const i = bodies.indexOf(e);
    const [before, after] = await Promise.all([i > 0 ? version(bodies[i - 1].seq) : [], version(e.seq)]);
    mount(shown, h("section", { class: "card" },
      h("h3", { text: t("version.title", { when: when(e.at), who: person(e.actor) }) }),
      renderDiff(before, after),
      h("p", {},
        // No confirmation: restoring is undone by restoring the version before.
        live ? h("button", { text: t("version.restore"), onclick: () => action(() => post(`${path}/versions/${e.seq}/restore`), shown) }) : null,
        h("button", { class: "link", text: t("common.close"), onclick: () => shown.replaceChildren() })),
    ));
    shown.scrollIntoView({ block: "nearest" });
  };
  const details = (e) => {
    if (e.erased) return t("history.erased");
    if (e.kind === "BodyCommitted") {
      return h("button", { class: "link", text: t("version.changes"), onclick: () => compare(e).catch((err) => mount(shown, errorBox(err))) });
    }
    return [e.title, e.field && `${e.field} = ${JSON.stringify(e.value ?? null)}`, e.to, e.comment && t("history.comment", { id: e.comment.slice(0, 8) })].filter(Boolean).join(" ");
  };
  return h("section", {},
    h("h2", { text: t("history.title") }),
    shown,
    h("table", { class: "history" }, events.slice().reverse().map((e) => h("tr", {},
      h("td", { text: when(e.at) }),
      h("td", {}, h("span", { class: "who" }, face(e.actor, 20), person(e.actor))),
      h("td", { text: e.kind }),
      h("td", {}, details(e)),
    ))),
  );
}

// --- account -----------------------------------------------------------------

async function accountView() {
  const show = screen();
  const [{ tokens }, contacts] = await Promise.all([get("/api/tokens"), get("/api/me/contacts")]);
  const secret = h("div");
  show(
    h("h1", { text: me.user.username }),
    h("p", { class: "muted", text: t("account.id", { id: me.user.id, via: me.via }) }),
    avatarCard(),
    contactsCard(contacts),
    form({ class: "card", id: "language" }, [
      field(t("account.language"), h("select", { name: "language", onchange: (ev) => ev.target.form.requestSubmit() },
        languages.filter((l) => !me.languages || me.languages.includes(l.code))
          .map((l) => h("option", { value: l.code, text: l.name, selected: l.code === language() })))),
    ], async (data) => {
      me.language = data.get("language");
      await put("/api/me/language", { language: me.language });
      setLanguage(me.language);
      route();
    }),
    form({ class: "card", id: "password" }, [
      h("h2", { text: t("account.password") }),
      field(t("account.current"), h("input", { name: "current", type: "password", autocomplete: "current-password", required: true })),
      field(t("account.new"), h("input", { name: "new", type: "password", autocomplete: "new-password", minlength: 12, required: true })),
      h("button", { type: "submit", text: t("account.change") }),
    ], async (data) => {
      await put("/api/me/password", { current: data.get("current"), new: data.get("new") });
      me = null;
      route();
    }),
    h("section", { class: "card" },
      h("h2", { text: t("account.tokens") }),
      h("table", {}, tokens.map((tok) => h("tr", {},
        h("td", { text: tok.name }), h("td", { text: tok.scope }), h("td", { text: t("account.expires", { when: when(tok.expires) }) }),
        h("td", {}, h("button", { text: t("account.revoke"), onclick: () => action(() => del("/api/tokens/" + tok.id)) })),
      ))),
      form({ id: "new-token" }, [
        field(t("field.name"), h("input", { name: "name", required: true })),
        field(t("account.scope"), h("select", { name: "scope" }, h("option", { value: "read", text: "read" }), h("option", { value: "full", text: "full" }),
          me.admin ? h("option", { value: "metrics", text: t("account.scopeMetrics") }) : null)),
        field(t("account.days"), h("input", { name: "days", type: "number", min: 1, max: 366, value: 30 })),
        h("button", { type: "submit", text: t("account.createToken") }),
      ], async (data) => {
        const res = await post("/api/tokens", { name: data.get("name"), scope: data.get("scope"), days: Number(data.get("days")) });
        secret.replaceChildren(h("p", { class: "notice" }, t("account.copy"), h("code", { text: res.secret })));
      }),
      secret,
      h("p", { class: "muted" }, t("account.mcpHint"), " ", link("#/mcp", t("nav.mcp"))),
    ),
  );
}

// avatarCard sets or removes one's profile picture (ADR-0050). The browser
// cuts the picture to a square and scales it down to 256 pixels before
// sending it, so a photo from a phone fits the server's budget.
function avatarCard() {
  const status = h("div", { class: "status" });
  const picker = h("input", { type: "file", id: "avatar-file", accept: "image/png,image/jpeg,image/gif,image/webp", "aria-label": t("account.avatarChoose") });
  picker.addEventListener("change", () => action(async () => {
    const file = picker.files[0];
    if (!file) return;
    const blob = await squareImage(file, 256);
    const res = await fetch("/api/me/avatar", { method: "PUT", headers: { "Content-Type": "application/octet-stream" }, body: blob });
    if (!res.ok) throw new ApiError(res.status, (await res.json().catch(() => ({}))).error || res.statusText);
    me.avatar = (await res.json()).avatar;
  }, status));
  return h("section", { class: "card", id: "avatar" },
    h("h2", { text: t("account.avatar") }),
    h("div", { class: "avatar-row" }, avatar(me.user.id, me.user.username, me.avatar, 64),
      h("div", {},
        h("p", { class: "muted", text: me.avatar ? t("account.avatarSet") : t("account.avatarNone") }),
        h("label", { class: "upload" }, h("span", { text: t("account.avatarChoose") }), picker),
        me.avatar ? h("button", { type: "button", class: "link", text: t("account.avatarRemove"),
          onclick: () => action(async () => { await del("/api/me/avatar"); me.avatar = ""; }, status) }) : null)),
    status);
}

// contactsCard sets the channels messages reach one on (ADR-0061): a mail
// address and the webhook addresses of Slack, Microsoft Teams or another
// service. A field left empty is a channel one does not have.
function contactsCard(contacts) {
  const channel = (name, label, type, hint) => field(label, h("input", { name, type, value: contacts[name] || "", placeholder: hint, autocomplete: name === "email" ? "email" : "off" }));
  const state = h("p", { class: "muted", id: "channels" });
  const summary = (c) => {
    const on = [["email", t("contacts.email")], ["slack", "Slack"], ["teams", "Microsoft Teams"], ["webhook", t("contacts.webhook")]].filter(([k]) => c[k]).map(([, l]) => l);
    state.textContent = on.length ? t("contacts.available", { list: on.join(", ") }) : t("contacts.none");
  };
  summary(contacts);
  return form({ class: "card", id: "contacts" }, [
    h("h2", { text: t("contacts.title") }),
    h("p", { class: "muted", text: t("contacts.help") }),
    channel("email", t("contacts.email"), "email", "name@example.org"),
    channel("slack", "Slack", "url", "https://hooks.slack.com/services/…"),
    channel("teams", "Microsoft Teams", "url", "https://…"),
    channel("webhook", t("contacts.webhook"), "url", "https://…"),
    state,
    h("button", { type: "submit", text: t("common.save") }),
  ], async (data, st) => {
    const saved = await put("/api/me/contacts", { email: data.get("email"), slack: data.get("slack"), teams: data.get("teams"), webhook: data.get("webhook") });
    summary(saved);
    st.replaceChildren(h("p", { class: "notice", text: t("contacts.saved") }));
  });
}

// squareImage cuts the middle square out of an image file and scales it to
// size pixels, as a PNG.
async function squareImage(file, size) {
  const bitmap = await createImageBitmap(file).catch(() => { throw new Error(t("account.avatarNotImage")); });
  const side = Math.min(bitmap.width, bitmap.height);
  const canvas = document.createElement("canvas");
  canvas.width = canvas.height = Math.min(size, side);
  canvas.getContext("2d").drawImage(bitmap, (bitmap.width - side) / 2, (bitmap.height - side) / 2, side, side, 0, 0, canvas.width, canvas.height);
  bitmap.close();
  return new Promise((resolve, reject) => canvas.toBlob((b) => (b ? resolve(b) : reject(new Error(t("account.avatarNotImage")))), "image/png"));
}

// --- AI assistants (MCP) --------------------------------------------------------

// mcpView explains how to let an AI assistant read and write through the
// Model Context Protocol (ADR-0012): the endpoint, a token made here, the
// settings for common clients and the tools the assistant gets. The
// assistant acts with the token's rights, and its changes carry the
// token's owner as author.
async function mcpView() {
  const show = screen();
  const endpoint = location.origin + "/mcp";
  const listed = await api("POST", "/mcp", { jsonrpc: "2.0", id: 1, method: "tools/list" }).catch(() => null);
  const tools = listed?.result?.tools || [];
  const token = "<TOKEN>";
  const snippets = (tok) => [
    [t("mcp.claudeCode"), `claude mcp add --transport http wikiwikibangbang ${endpoint} --header "Authorization: Bearer ${tok}"`],
    [t("mcp.json"), JSON.stringify({ mcpServers: { wikiwikibangbang: { type: "http", url: endpoint, headers: { Authorization: `Bearer ${tok}` } } } }, null, 2)],
    [t("mcp.stdio"), `WWBB_TOKEN=${tok} wikiwikibangbang mcp -url ${location.origin}`],
  ];
  const copyable = (text) => {
    const pre = h("pre", { class: "snippet", text });
    const button = h("button", { type: "button", class: "secondary", text: t("mcp.copy"), onclick: async () => {
      try {
        await navigator.clipboard.writeText(pre.textContent);
        button.textContent = t("mcp.copied");
      } catch {
        button.textContent = t("mcp.copyFailed");
      }
    } });
    return h("div", { class: "copyable" }, pre, button);
  };
  const setup = h("div", { id: "mcp-setup" });
  const fill = (tok) => setup.replaceChildren(...snippets(tok).map(([label, text]) => h("section", {}, h("h3", { text: label }), copyable(text))));
  fill(token);
  show(
    h("h1", { text: t("mcp.title") }),
    h("p", { text: t("mcp.intro") }),
    h("section", { class: "card" },
      h("h2", { text: t("mcp.step1") }),
      h("p", { class: "muted", text: t("mcp.endpoint") }),
      copyable(endpoint)),
    h("section", { class: "card" },
      h("h2", { text: t("mcp.step2") }),
      h("p", { class: "muted", text: t("mcp.tokenNote") }),
      form({ id: "mcp-token" }, [
        field(t("field.name"), h("input", { name: "name", required: true, value: t("mcp.tokenName") })),
        field(t("account.scope"), h("select", { name: "scope" },
          h("option", { value: "full", text: t("mcp.scopeFull") }), h("option", { value: "read", text: t("mcp.scopeRead") }))),
        field(t("account.days"), h("input", { name: "days", type: "number", min: 1, max: 366, value: 30 })),
        h("button", { type: "submit", class: "primary", text: t("account.createToken") }),
      ], async (data, status) => {
        const res = await post("/api/tokens", { name: data.get("name"), scope: data.get("scope"), days: Number(data.get("days")) });
        fill(res.secret);
        status.replaceChildren(h("p", { class: "notice", text: t("mcp.tokenMade") }));
      }),
      h("p", { class: "muted" }, t("mcp.tokensAt"), " ", link("#/account", t("nav.account")))),
    h("section", { class: "card" },
      h("h2", { text: t("mcp.step3") }),
      setup),
    h("section", { class: "card" },
      h("h2", { text: t("mcp.tools", { n: tools.length }) }),
      h("p", { class: "muted", text: t("mcp.toolsNote") }),
      h("ul", { class: "list tools" }, tools.map((tool) => h("li", {}, h("code", { text: tool.name }), " — ", (tool.description || "").split(". ")[0])))),
  );
}

// --- administration ----------------------------------------------------------

async function adminView() {
  const show = screen();
  const [{ users }, { spaces }, { types }, auditPage, { groups }, { holders }, { requests }, access] = await Promise.all([
    get("/api/users"), get("/api/spaces"), get("/api/types"), get("/api/audit?limit=50"), get("/api/groups"), get("/api/system-roles"),
    get("/api/account-requests"), get("/api/settings/access"),
  ]);
  for (const u of users) names.set(u.id, u.username);
  const status = h("div", { class: "status" });
  const userOptions = () => users.map((u) => h("option", { value: u.id, text: u.username }));
  const rolesOf = (id) => holders.find((x) => x.principal === id)?.roles || [];

  // The table of contents scrolls to a section; the hash stays the route.
  const sections = [];
  const section = (id, title, ...content) => {
    const el = h("section", { class: "card", id: "admin-" + id }, h("h2", { text: title }), ...content);
    sections.push([el, title]);
    return el;
  };

  const spacesSection = section("spaces", t("admin.spaces"),
    spaces.length ? h("table", {}, spaces.map((sp) => h("tr", {},
      h("td", {}, link("#/space/" + encodeURIComponent(sp.key), sp.name)), h("td", { class: "muted", text: sp.key }),
      h("td", {}, link("#/members/" + encodeURIComponent(sp.key), t("admin.members")))))) : h("p", { class: "muted", text: t("admin.none") }),
    form({ id: "new-space" }, [
      h("h3", { text: t("admin.newSpace") }),
      field(t("admin.key"), h("input", { name: "key", required: true, pattern: "[A-Z][A-Z0-9]*", title: t("admin.keyHint") })),
      field(t("field.name"), h("input", { name: "name", required: true })),
      h("button", { type: "submit", text: t("admin.createSpace") }),
    ], async (d) => { await post("/api/spaces", { key: d.get("key"), name: d.get("name") }); route(); }));

  const typeText = h("textarea", { name: "type", rows: 8, required: true },
    '{"name":"page","body":true,"fields":[{"name":"owner","kind":"user"}]}');
  const describe = (ty) => [
    ty.body ? t("admin.typeBody") : null,
    ty.fields.length ? t("admin.typeFields", { list: ty.fields.map((f) => `${f.name} (${f.kind}${f.required ? ", *" : ""})`).join(", ") }) : null,
    ty.workflow ? t("admin.typeStates", { list: ty.workflow.states.join(" → ") }) : null,
  ].filter(Boolean).join(" · ");
  const typesSection = section("types", t("admin.types"),
    setupCard(types),
    types.length ? h("table", { id: "types" }, types.map((ty) => h("tr", {},
      h("td", {}, h("strong", { text: ty.name }), ` v${ty.version}`), h("td", { class: "muted", text: describe(ty) }),
      h("td", {}, h("button", { type: "button", class: "link", text: t("common.edit"), onclick: () => {
        const { version, ...def } = ty;
        typeText.value = JSON.stringify(def, null, 2);
        typeText.scrollIntoView({ block: "center" });
        typeText.focus();
      } }))))) : null,
    form({ id: "new-type" }, [
      h("h3", { text: t("admin.defineType") }),
      h("p", { class: "muted", text: t("admin.typeHelp") }),
      field(t("admin.typeJson"), typeText),
      h("button", { type: "submit", text: t("admin.define") }),
    ], async (d) => {
      let def;
      try {
        def = JSON.parse(d.get("type"));
      } catch (err) {
        throw new Error(t("admin.badJson", { error: err.message }));
      }
      await post("/api/types", def);
      route();
    }));

  const sysToggle = (u, role, label) => {
    const box = h("input", { type: "checkbox", checked: rolesOf(u.id).includes(role), "aria-label": `${label}: ${u.username}` });
    box.addEventListener("change", () => action(() => (box.checked ? put : del)(`/api/system-roles/${role}/${encodeURIComponent(u.id)}`), status));
    return h("label", { class: "check" }, box, " ", label);
  };
  // Account requests made at the sign-in page (ADR-0061): whether they
  // are accepted, and the ones waiting for a decision.
  const openBox = h("input", { type: "checkbox", id: "requests-open", checked: access.accountRequests });
  openBox.addEventListener("change", () => action(() => put("/api/settings/access", { accountRequests: openBox.checked }), status));
  const requestsSection = section("requests", t("requests.title"),
    h("p", { class: "muted", text: t("requests.help") }),
    h("label", { class: "check" }, openBox, " ", t("requests.open")),
    requests.length ? h("table", { id: "account-requests" },
      h("tr", {}, h("th", { text: t("field.username") }), h("th", { text: t("contacts.email") }), h("th", { text: t("signup.reason") }), h("th", { text: t("requests.when") }), h("th")),
      requests.map((rq) => h("tr", { "data-username": rq.username },
        h("td", { text: rq.username }), h("td", { text: rq.email }), h("td", { class: "muted", text: rq.reason }), h("td", { class: "muted", text: when(rq.created) }),
        h("td", {},
          h("button", { type: "button", class: "approve", text: t("requests.approve"), onclick: () => action(() => post(`/api/account-requests/${rq.id}/approve`), status) }), " ",
          h("button", { type: "button", class: "link", text: t("requests.reject"),
            onclick: () => confirm(t("requests.rejectConfirm", { name: rq.username })) && action(() => del(`/api/account-requests/${rq.id}`), status) }))))) :
      h("p", { class: "muted", id: "no-requests", text: t("requests.none") }));

  const emailCell = (u) => h("td", {},
    h("span", { class: "muted", text: u.email || t("contacts.noEmail") }), " ",
    h("button", { type: "button", class: "link", text: t("common.edit"), "aria-label": t("contacts.editEmail", { name: u.username }), onclick: () => {
      const email = prompt(t("contacts.editEmail", { name: u.username }), u.email || "");
      if (email !== null) action(() => put(`/api/users/${encodeURIComponent(u.id)}/email`, { email: email.trim() }), status);
    } }));
  const usersSection = section("users", t("admin.users"),
    h("p", { class: "muted", text: t("admin.usersHelp") }),
    h("table", { id: "users" }, users.map((u) => h("tr", {},
      h("td", { text: u.username }),
      emailCell(u),
      h("td", {}, sysToggle(u, "admin", t("admin.roleAdmin"))),
      h("td", {}, sysToggle(u, "erasure", t("admin.roleErasure")))))),
    form({ id: "new-user" }, [
      h("h3", { text: t("admin.newUser") }),
      field(t("field.username"), h("input", { name: "username", required: true })),
      field(t("field.password"), h("input", { name: "password", type: "password", minlength: 12, required: true, autocomplete: "new-password" })),
      h("button", { type: "submit", text: t("admin.createUser") }),
    ], async (d) => { await post("/api/users", { username: d.get("username"), password: d.get("password") }); route(); }));

  const groupsSection = section("groups", t("admin.groups"),
    h("p", { class: "muted", text: t("admin.groupsHelp") }),
    groups.length ? h("table", { id: "groups" }, groups.map((g) => h("tr", {},
      h("th", { text: "@" + g.name }),
      h("td", {}, g.members.map((m) => h("span", { class: "member" }, person(m),
        h("button", { type: "button", class: "link", text: "×", title: t("admin.removeMember", { name: person(m), group: g.name }),
          "aria-label": t("admin.removeMember", { name: person(m), group: g.name }),
          onclick: () => action(() => del(`/api/groups/${encodeURIComponent(g.name)}/members/${encodeURIComponent(m)}`), status) }))))))) : h("p", { class: "muted", text: t("admin.noGroups") }),
    form({ id: "add-member" }, [
      field(t("admin.group"), h("input", { name: "group", required: true, pattern: "[A-Za-z0-9][A-Za-z0-9._\\-]*", list: "group-names" })),
      h("datalist", { id: "group-names" }, groups.map((g) => h("option", { value: g.name }))),
      field(t("admin.user"), h("select", { name: "user" }, userOptions())),
      h("button", { type: "submit", text: t("admin.addMember") }),
    ], async (d) => { await put(`/api/groups/${encodeURIComponent(d.get("group"))}/members/${encodeURIComponent(d.get("user"))}`); route(); }));

  const grantSection = section("roles", t("admin.grant"),
    h("p", { class: "muted", text: t("admin.grantHelp") }),
    form({ id: "grant" }, [
      field(t("col.space"), h("select", { name: "space" }, spaces.map((s) => h("option", { value: s.key, text: `${s.name} (${s.key})` })))),
      field(t("admin.user"), h("select", { name: "user" }, userOptions(), groups.map((g) => h("option", { value: "@" + g.name, text: "@" + g.name })))),
      field(t("admin.role"), h("select", { name: "role" }, [["viewer", t("role.viewer")], ["editor", t("role.editor")], ["space-admin", t("role.space-admin")]].map(([r, label]) => h("option", { value: r, text: label })))),
      h("button", { type: "submit", text: t("admin.grantButton") }),
    ], async (d, st) => {
      await put(`/api/spaces/${encodeURIComponent(d.get("space"))}/roles/${encodeURIComponent(d.get("user"))}`, { role: d.get("role") });
      st.replaceChildren(h("p", { class: "notice", text: t("admin.granted") }));
    }));

  const backupSection = section("backup", t("admin.backup"),
    form({ id: "backup" }, [
      h("p", { class: "muted", text: t("admin.backupNote") }),
      field(t("admin.passphrase"), h("input", { name: "passphrase", type: "password", minlength: 12, required: true, autocomplete: "new-password" })),
      field(t("admin.repeat"), h("input", { name: "repeat", type: "password", minlength: 12, required: true, autocomplete: "new-password" })),
      h("button", { type: "submit", text: t("admin.download") }),
    ], async (d, st) => {
      if (d.get("passphrase") !== d.get("repeat")) throw new Error(t("admin.differ"));
      const { blob, name } = await download("/api/backup", { passphrase: d.get("passphrase") });
      const url = URL.createObjectURL(blob);
      const a = h("a", { download: name, text: name });
      a.href = url; // a blob made here, not user content; safeHref would refuse the scheme
      a.click();
      setTimeout(() => URL.revokeObjectURL(url), 60_000);
      st.replaceChildren(h("p", { class: "notice", text: t("admin.saved", { name }) }));
    }));

  const designSection = section("design", t("design.title"), await designCard());
  const regulationsSection = section("regulations", t("dp.adminTitle"), await regulationsCard());

  const importSection = section("import", t("admin.import"),
    h("p", { text: t("admin.importText") }),
    h("pre", { class: "snippet", text: "wikiwikibangbang import -help" }));

  const auditSection = section("audit", t("admin.audit", { n: 50 }),
    h("table", { class: "history" }, auditPage.entries.map((e) => h("tr", {},
      h("td", { text: when(e.at) }), h("td", { text: e.event }), h("td", { text: e.actor ? person(e.actor) : "" }),
      h("td", { text: e.addr || "" }), h("td", { text: e.subject ? person(e.subject) : "" }),
      h("td", { text: Object.entries(e.detail || {}).map(([k, v]) => `${k}=${v}`).join(" ") }),
    ))));

  show(
    h("h1", { text: t("admin.title") }),
    h("nav", { class: "admin-toc", "aria-label": t("admin.contents") }, sections.map(([el, title]) =>
      h("button", { type: "button", class: "link", text: title, onclick: () => el.scrollIntoView({ block: "start" }) }))),
    status,
    spacesSection, typesSection, usersSection, requestsSection, groupsSection, grantSection, backupSection, designSection, regulationsSection, importSection, auditSection,
  );
}

// holdBanner shows an item's open data protection review (ADR-0058): what
// was found under which regulation, who saved it, and, for a second person
// who may edit the item, the release.
async function holdBanner(item, path, canEdit) {
  const { regulations } = await get("/api/regulations").catch(() => ({ regulations: [] }));
  const reg = new Map(regulations.map((r) => [r.id, r]));
  await learnNames(item.hold.savers);
  const status = h("div", { class: "status" });
  const savedByMe = item.hold.savers.includes(me.user.id);
  const restricted = Boolean(item.restriction?.read?.length);
  const law = (id) => {
    const r = reg.get(id);
    if (!r) return id;
    return [r.url ? link(r.url, r.name) : r.name, r.document ? [" (", link(`/api/regulations/${id}/document`, t("dp.documentShort")), ")"] : null];
  };
  const release = canEdit && !savedByMe ? form({ class: "hold-release" }, [
    h("label", { class: "check" }, h("input", { type: "radio", name: "outcome", value: "restricted", checked: restricted, disabled: !restricted }), t("dp.outcomeRestricted")),
    restricted ? null : h("p", { class: "muted", text: t("dp.restrictFirst") }),
    h("label", { class: "check" }, h("input", { type: "radio", name: "outcome", value: "false-alarm", checked: !restricted }), t("dp.outcomeFalseAlarm")),
    field(t("dp.reason"), h("textarea", { name: "reason", rows: 2 })),
    h("button", { type: "submit", class: "primary", text: t("dp.release") }),
  ], async (d) => {
    await post(path + "/hold/release", { outcome: d.get("outcome"), reason: d.get("reason") || "" });
    route();
  }) : h("p", { class: "muted", text: savedByMe ? t("dp.fourEyes") : t("dp.editorsRelease") });
  return h("section", { class: "notice warning hold", id: "hold" },
    h("strong", { text: t("dp.heldTitle") }),
    h("p", { text: t("dp.heldText", { who: item.hold.savers.map(person).join(", ") }) }),
    h("ul", {}, item.hold.findings.map((f) => h("li", {}, h("span", { class: "chip", text: f.indicator }), " ", f.category, " – ", law(f.regulation)))),
    release, status);
}

// regulationsCard keeps the data protection regulations (ADR-0058): adopted
// from a template or written by hand, with a link to the law, its text as
// a PDF, and per category the indicators the content is assessed for.
async function regulationsCard() {
  const [{ regulations }, { templates }] = await Promise.all([get("/api/regulations"), get("/api/regulations/templates")]);
  const status = h("div", { class: "status" });
  const toText = (cats) => cats.map((c) => `${c.name}: ${c.indicators.join(", ")}`).join("\n");
  const fromText = (text) => text.split("\n").map((l) => l.trim()).filter(Boolean).map((l) => {
    const i = l.indexOf(":");
    if (i < 1) throw new Error(t("dp.categoryLine", { line: l }));
    return { name: l.slice(0, i).trim(), indicators: l.slice(i + 1).split(",").map((x) => x.trim()).filter(Boolean) };
  });
  const putFile = async (path, file) => {
    const res = await fetch(path, { method: "PUT", credentials: "same-origin", headers: { "Content-Type": "application/octet-stream" }, body: file });
    if (!res.ok) throw new Error((await res.json().catch(() => ({}))).error || res.statusText);
  };
  const editor = (r, isNew) => {
    const cats = h("textarea", { name: "categories", rows: 8, class: "code", spellcheck: "false", value: toText(r.categories || []), "aria-label": t("dp.categories"),
      placeholder: t("dp.categoriesExample") });
    const doc = h("input", { type: "file", accept: "application/pdf,.pdf", "aria-label": t("dp.document") });
    doc.addEventListener("change", () => doc.files[0] && action(() => putFile(`/api/regulations/${r.id}/document`, doc.files[0]), status));
    return h("div", { class: "regulation card-like", id: "regulation-" + (r.id || "new") },
      form({ class: "regulation-form" }, [
        isNew ? field(t("dp.id"), h("input", { name: "id", required: true, pattern: "[a-z0-9][a-z0-9\\-]{0,39}", placeholder: "intern" })) : h("h3", { text: r.name }),
        field(t("dp.name"), h("input", { name: "name", required: true, value: r.name || "", maxlength: 120 })),
        field(t("dp.url"), h("input", { name: "url", type: "url", value: r.url || "", placeholder: "https://" })),
        h("label", { class: "check" }, h("input", { type: "checkbox", name: "active", checked: Boolean(r.active) }), t("dp.active")),
        // Not a label: the info button inside would take the label's clicks.
        h("div", { class: "field" }, h("span", {}, t("dp.categories"), " ", infoTip(t("dp.categories"), t("dp.categoriesInfo"), t("dp.categoriesExample"))), cats),
        h("p", { class: "muted", text: t("dp.categoriesHint") }),
        h("p", { class: "actions-row" },
          h("button", { type: "submit", class: "primary", text: t("common.save") }),
          isNew ? null : h("button", { type: "button", class: "danger", text: t("common.delete"), onclick: () => confirm(t("dp.deleteConfirm", { name: r.name })) && action(() => del("/api/regulations/" + r.id), status) })),
      ], async (d) => {
        const id = isNew ? d.get("id").trim() : r.id;
        const categories = fromText(cats.value);
        if (!categories.length) throw new Error(t("dp.categoriesEmpty"));
        const bare = categories.find((c) => !c.indicators.length);
        if (bare) throw new Error(t("dp.indicatorsEmpty", { name: bare.name }));
        await put("/api/regulations/" + encodeURIComponent(id), { name: d.get("name").trim(), url: d.get("url").trim(), active: Boolean(d.get("active")), categories });
        route();
      }),
      isNew ? null : h("p", { class: "regulation-doc" },
        r.document ? [link(`/api/regulations/${r.id}/document`, t("dp.documentOpen")), " ", h("button", { type: "button", class: "link", text: t("common.delete"), onclick: () => action(() => del(`/api/regulations/${r.id}/document`), status) }), " · "] : null,
        h("label", { class: "upload" }, h("span", { text: r.document ? t("dp.documentReplace") : t("dp.documentAttach") }), doc)));
  };
  const have = new Set(regulations.map((r) => r.id));
  const missing = templates.filter((tpl) => !have.has(tpl.id));
  return h("div", {},
    h("p", { class: "muted", text: t("dp.adminIntro") }),
    status,
    missing.length ? h("p", { class: "actions-row" }, missing.map((tpl) => h("button", { type: "button", class: "secondary", text: t("dp.adopt", { name: tpl.name.split(" – ")[0] }),
      onclick: () => action(() => put("/api/regulations/" + tpl.id, { ...tpl, active: false }), status) }))) : null,
    regulations.map((r) => editor(r, false)),
    h("details", {}, h("summary", { text: t("dp.addOwn") }), editor({ categories: [] }, true)));
}

// designCard sets the corporate design (ADR-0052): name, logo, colours,
// background, font and an own stylesheet. Files are sent as they are
// chosen; the rest with Save.
async function designCard() {
  const b = await get("/branding.json");
  const status = h("div", { class: "status" });
  // contrast warns about pairs of colours below WCAG AA (ADR-0052); the
  // design is saved all the same.
  const contrast = h("div", { id: "design-contrast", role: "status" });
  const pairText = (pair, ratio) => ({
    headerText: () => t("design.contrast.headerText", { ratio }),
    textOnBackground: () => t("design.contrast.textOnBackground", { ratio }),
    accentInk: () => t("design.contrast.accentInk", { ratio }),
    accentOnSurface: () => t("design.contrast.accentOnSurface", { ratio }),
    accentOnDarkSurface: () => t("design.contrast.accentOnDarkSurface", { ratio }),
    accentOnBackground: () => t("design.contrast.accentOnBackground", { ratio }),
  }[pair] || (() => pair))();
  // swatch shows a pair as it reads; colours set through the style
  // object, which the content security policy allows (unlike attributes).
  const swatch = (fg, bg) => {
    const el = h("span", { class: "design-pair", text: "Aa", "aria-hidden": "true" });
    el.style.color = fg;
    el.style.background = bg;
    return el;
  };
  const showContrast = (findings) => contrast.replaceChildren(findings.length ? h("div", { class: "notice warning" },
    h("strong", { text: t("design.contrastTitle") }),
    h("ul", {}, findings.map((f) => h("li", {},
      swatch(f.foreground, f.background),
      " ", pairText(f.pair, f.ratio.toFixed(1)), " ", h("span", { class: "muted", text: `${f.foreground} / ${f.background}` })))),
    h("p", { class: "muted", text: t("design.contrastHint") })) : "");
  showContrast(b.contrast || []);
  const colour = (name, label) => {
    const on = h("input", { type: "checkbox", name: name + "-on", checked: Boolean(b[name]), "aria-label": t("design.use", { what: label }) });
    const pick = h("input", { type: "color", name, value: b[name] || "#0b5cff", "aria-label": label });
    return h("div", { class: "field design-colour" }, h("span", { text: label }), h("label", { class: "check" }, on, pick, t("design.useIt")));
  };
  const asset = (name, label, accept) => {
    const picker = h("input", { type: "file", accept, id: "design-" + name, "aria-label": label });
    picker.addEventListener("change", () => action(async () => {
      const file = picker.files[0];
      if (!file) return;
      const res = await fetch("/api/branding/" + name, { method: "PUT", headers: { "Content-Type": "application/octet-stream" }, body: file });
      if (!res.ok) throw new ApiError(res.status, (await res.json().catch(() => ({}))).error || res.statusText);
      await applyBranding(true);
    }, status));
    const has = b.assets?.[name];
    return h("div", { class: "design-asset" },
      h("strong", { text: label }),
      has && name !== "font" ? h("img", { src: `/branding/${name}?v=${encodeURIComponent(has)}`, alt: "", class: "design-preview" }) : null,
      h("span", { class: "muted", text: has ? t("design.fileSet") : t("design.fileNone") }),
      h("label", { class: "upload" }, h("span", { text: t("design.choose") }), picker),
      has ? h("button", { type: "button", class: "link", text: t("common.delete"), onclick: () => action(async () => { await del("/api/branding/" + name); await applyBranding(true); }, status) }) : null);
  };
  const fontNames = { system: t("design.fontSystem"), humanist: t("design.fontHumanist"), serif: t("design.fontSerif"), rounded: t("design.fontRounded"), mono: t("design.fontMono") };
  const cssPicker = h("input", { type: "file", accept: ".css,text/css", "aria-label": t("design.cssFile") });
  const cssText = h("textarea", { name: "css", rows: 8, class: "code", spellcheck: "false", "aria-label": t("design.css"), value: b.css || "" });
  cssPicker.addEventListener("change", async () => { if (cssPicker.files[0]) cssText.value = await cssPicker.files[0].text(); });
  return h("div", {},
    h("p", { class: "muted", text: t("design.help") }),
    h("div", { class: "design-assets" },
      asset("logo", t("design.logo"), "image/png,image/jpeg,image/gif,image/webp,image/svg+xml"),
      asset("background", t("design.background"), "image/png,image/jpeg,image/gif,image/webp"),
      asset("font", t("design.fontFile"), ".woff2,.woff,.ttf,.otf")),
    form({ id: "design" }, [
      field(t("design.name"), h("input", { name: "name", value: b.name || "", maxlength: 80 })),
      h("div", { class: "design-colours" },
        colour("accent", t("design.accent")), colour("background", t("design.backgroundColour")),
        colour("headerBackground", t("design.headerBackground")), colour("headerText", t("design.headerText"))),
      contrast,
      field(t("design.font"), h("select", { name: "font" }, h("option", { value: "", text: t("design.fontDefault") }),
        Object.entries(fontNames).map(([v, label]) => h("option", { value: v, text: label, selected: b.font === v })),
        h("option", { value: "upload", text: t("design.fontUpload"), selected: b.font === "upload" }))),
      field(t("design.fontFamily"), h("input", { name: "fontFamily", value: b.fontFamily || "", placeholder: "Frutiger" })),
      field(t("design.css"), cssText),
      h("label", { class: "upload" }, h("span", { text: t("design.cssFile") }), cssPicker),
      h("button", { type: "submit", class: "primary", text: t("common.save") }),
    ], async (d, st) => {
      const pick = (name) => (d.get(name + "-on") ? d.get(name) : "");
      const saved = await put("/api/branding", { name: d.get("name"), accent: pick("accent"), background: pick("background"),
        headerBackground: pick("headerBackground"), headerText: pick("headerText"), font: d.get("font"), fontFamily: d.get("fontFamily").trim(), css: cssText.value });
      showContrast(saved.contrast || []);
      await applyBranding(true);
      st.replaceChildren(h("p", { class: "notice", text: t("design.saved") }));
    }),
    status);
}

// membersView lists who holds which role in a space, for its
// administrators (ADR-0048): change a role, take it back, grant one to a
// person or a group.
async function membersView(key) {
  const show = screen();
  const [{ roles }, { users }, { spaces }] = await Promise.all([
    get(`/api/spaces/${encodeURIComponent(key)}/roles`), get("/api/users"), get("/api/spaces"),
  ]);
  for (const u of users) names.set(u.id, u.username);
  await learnNames(roles.map((r) => r.principal).filter((p) => !p.startsWith("@")));
  here.space = key;
  const sp = spaces.find((s) => s.key === key) || { name: key };
  const status = h("div", { class: "status" });
  const base = `/api/spaces/${encodeURIComponent(key)}/roles/`;
  const roleSelect = (value) => h("select", { name: "role" }, [["viewer", t("role.viewer")], ["editor", t("role.editor")], ["space-admin", t("role.space-admin")]]
    .map(([r, label]) => h("option", { value: r, text: label, selected: r === value })));
  const row = (g) => {
    const sel = roleSelect(g.role);
    sel.setAttribute("aria-label", t("admin.role"));
    sel.addEventListener("change", () => action(() => put(base + encodeURIComponent(g.principal), { role: sel.value }), status));
    return h("tr", {},
      h("td", { text: g.principal.startsWith("@") ? g.principal : person(g.principal) }),
      h("td", {}, sel),
      h("td", {}, h("button", { type: "button", class: "link", text: t("members.revoke"),
        onclick: () => confirm(t("members.revokeConfirm", { name: g.principal.startsWith("@") ? g.principal : person(g.principal) }))
          && action(() => del(base + encodeURIComponent(g.principal)), status) })));
  };
  const known = new Set(roles.map((r) => r.principal));
  show(
    h("p", {}, link("#/", t("nav.spaces")), " / ", link("#/space/" + encodeURIComponent(key), key), " / " + t("members.title")),
    h("h1", { text: t("members.heading", { space: sp.name }) }),
    h("p", { class: "muted", text: t("members.help") }),
    status,
    roles.length ? h("table", { id: "members" }, roles.map(row)) : h("p", { class: "muted", text: t("members.none") }),
    form({ class: "card", id: "add-role" }, [
      h("h2", { text: t("members.add") }),
      field(t("admin.user"), h("select", { name: "principal" }, users.filter((u) => !known.has(u.id)).map((u) => h("option", { value: u.id, text: u.username })))),
      field(t("members.orGroup"), h("input", { name: "group", placeholder: "@ops", pattern: "@?[A-Za-z0-9][A-Za-z0-9._\\-]*" })),
      field(t("admin.role"), roleSelect("viewer")),
      h("button", { type: "submit", text: t("admin.grantButton") }),
    ], async (d) => {
      const group = String(d.get("group") || "").trim();
      const principal = group ? "@" + group.replace(/^@/, "") : d.get("principal");
      if (!principal) throw new Error(t("members.nobody"));
      await put(base + encodeURIComponent(principal), { role: d.get("role") });
      route();
    }),
  );
}
