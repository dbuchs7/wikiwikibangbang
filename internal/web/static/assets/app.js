// The UI shell (ADR-0009, ADR-0031): hash routes, one view per route, all
// data through the HTTP API. Plain ES modules, no build step.
import { api, get, post, put, del, download, ApiError } from "./api.js";
import { h, mount, when } from "./dom.js";
import { createEditor } from "./editor.js";
import { liveBody } from "./live.js";
import { pageTree, ancestors } from "./tree.js";
import { renderDiff } from "./diff.js";
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

function link(href, text) {
  return h("a", { href, text });
}

// withTree lays a space's page tree beside a view's content.
function withTree(tree, ...content) {
  return h("div", { class: "with-tree" }, tree, h("div", { class: "with-tree-main" }, ...content));
}

// childPagesOf lists an item's child pages for a list of child pages in
// its body (ADR-0042).
const childPagesOf = (item) => async () =>
  (await get(`/api/spaces/${encodeURIComponent(item.space)}/tree?parent=${item.id}`)).items;

// readOnlyBody shows an item's body without editing it.
function readOnlyBody(item, embeds) {
  const resolve = async (ref) => (await get("/api/items/" + encodeURIComponent(ref))).item;
  return createEditor({ blocks: item.body, embeds, editable: false, resolve, childPages: childPagesOf(item) }).element;
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
// instead of an ID (ADR-0031, ADR-0035).
const names = new Map();

// learnNames looks up the usernames of the IDs not yet known, in one
// request. IDs that are no user (such as "governance") stay as they are.
async function learnNames(ids) {
  const missing = [...new Set(ids.filter((id) => id && !names.has(id)))];
  // 100 per request stays below any sensible page budget (ADR-0015).
  for (let i = 0; i < missing.length; i += 100) {
    const chunk = missing.slice(i, i + 100);
    const res = await get("/api/users/names?" + chunk.map((id) => "id=" + encodeURIComponent(id)).join("&"));
    for (const id of chunk) names.set(id, res.names[id] ?? id);
  }
}

// person names a user: "you", their username, or the ID as given.
function person(id) {
  if (!id) return "—";
  if (me && id === me.user.id) return t("common.you");
  return names.get(id) ?? id;
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
const modules = [
  { id: "spaces", name: "nav.spaces", href: "#/", match: ["", "space", "item", "board", "links", "search"] },
  { id: "tasks", name: "nav.tasks", href: "#/tasks", match: ["tasks"] },
  { id: "governance", name: "nav.governance", href: "#/governance", match: ["governance"] },
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
  mount(document.getElementById("drawer-apps"), ...modules.map((m) =>
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
  mount(nav,
    link("#/", t("nav.spaces")),
    search,
    me.admin ? link("#/admin", t("nav.admin")) : null,
    link("#/account", me.user.username),
    h("button", { class: "link", onclick: () => action(async () => { await post("/api/logout"); me = null; location.hash = "#/"; }) }, t("nav.logout")),
  );
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
    renderModules(section || "");
    if (!me) return loginView();
    switch (section) {
      case "tasks": return await tasksView();
      case "space": return await spaceView(arg);
      case "board": return await boardView(arg, arg2);
      case "links": return await linksView(arg);
      case "governance": return arg ? await governanceView(arg) : await governanceOverview();
      case "item": return await itemView(arg);
      case "search": return await searchView(arg || "");
      case "account": return await accountView();
      case "admin": return await adminView();
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

window.addEventListener("hashchange", route);
route();

// --- login -------------------------------------------------------------------

async function loginView() {
  const show = screen();
  const params = new URLSearchParams(location.search);
  const sso = await get("/api/oidc").catch(() => ({ enabled: false }));
  show(
    h("section", { class: "card narrow" },
      h("h1", { text: t("login.title") }),
      params.get("login_error") ? h("p", { class: "error", text: t("login.failed") }) : null,
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
      sso.enabled ? h("p", {}, h("a", { class: "button", href: "/api/oidc/login", text: t("login.with", { name: sso.name }) })) : null,
    ));
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

async function homeView() {
  const show = screen();
  const { spaces } = await get("/api/spaces");
  show(
    h("h1", { text: t("spaces.title") }),
    spaces.length === 0
      ? h("p", { class: "muted", text: me.admin ? t("spaces.noneAdmin") : t("spaces.none") })
      : h("ul", { class: "list" }, spaces.map((s) => h("li", {}, link("#/space/" + encodeURIComponent(s.key), s.name), " ", h("span", { class: "muted", text: s.key })))),
  );
}

async function spaceView(key) {
  const show = screen();
  const [{ types }, first, top] = await Promise.all([
    get("/api/types"),
    get(`/api/items?space=${encodeURIComponent(key)}&limit=100`),
    get(`/api/spaces/${encodeURIComponent(key)}/tree?limit=1`),
  ]);
  const status = h("div", { class: "status" });
  // The space's home is its first page at the top of the tree.
  let home = h("p", { class: "muted", text: t("space.noPages") });
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
  const tree = pageTree({ space: key, onMoved: () => route(), onError: (err) => status.replaceChildren(errorBox(err)) });
  show(withTree(tree,
    h("p", {}, link("#/", t("nav.spaces")), " / " + key),
    h("h1", { text: key }),
    h("p", {}, [...boards, link(`#/links/${encodeURIComponent(key)}`, t("space.linkCheck")), link(`#/governance/${encodeURIComponent(key)}`, t("nav.governance"))].flatMap((b, i) => i ? [" · ", b] : [b])),
    status,
    home,
    h("h2", { text: t("space.allItems") }),
    h("p", { class: "muted", text: t("space.items", { n: first.total.value }) }),
    h("table", {}, h("thead", {}, h("tr", {}, [t("col.key"), t("col.title"), t("col.type"), t("col.state")].map((c) => h("th", { text: c })))), rows),
    more,
    newItemForm(key, types),
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
  const transitionsFrom = (state) => type.workflow.transitions.filter((tr) => tr.from.includes(state));
  const perform = (it, name) => action(() => post(`/api/items/${it.id}/transitions`, { name }), status);

  const card = (it) => h("li", { class: "card-item", draggable: "true", "data-id": it.id,
    ondragstart: (ev) => { ev.dataTransfer.setData("text/plain", it.id); ev.dataTransfer.effectAllowed = "move"; } },
  link("#/item/" + encodeURIComponent(it.key), it.key),
  h("div", { class: "card-title", text: it.title }),
  h("div", { class: "card-actions" }, transitionsFrom(it.state).map((tr) =>
    h("button", { class: "link", text: `${tr.name} → ${tr.to}`, onclick: () => perform(it, tr.name) }))),
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
  show(
    h("p", {}, link("#/", t("nav.spaces")), " / ", link("#/space/" + encodeURIComponent(key), key), " / " + t("board.crumb", { type: typeName })),
    h("h1", { text: `${key} · ${typeName}` }),
    page.next ? h("p", { class: "notice", text: t("board.truncated", { n: items.length }) }) : null,
    status,
    h("div", { class: "board" }, type.workflow.states.map(column)),
  );
}

// --- tasks -------------------------------------------------------------------

// tasksView lists open work across every space one may read: items of a
// type with a workflow that are not in its last state. It reads the paged
// item listing, at most boardMax items, like the board.
async function tasksView() {
  const show = screen();
  const { types } = await get("/api/types");
  const flows = new Map(types.filter((ty) => ty.workflow).map((ty) => [ty.name, ty.workflow]));
  let page = await get("/api/items?limit=100");
  const items = [...page.items];
  while (page.next && items.length < boardMax) {
    page = await get(`/api/items?limit=100&after=${encodeURIComponent(page.next)}`);
    items.push(...page.items);
  }
  const open = items.filter((it) => {
    const wf = flows.get(it.type);
    return wf && it.state !== wf.states[wf.states.length - 1];
  });
  open.sort((a, b) => a.space.localeCompare(b.space) || a.key.localeCompare(b.key, undefined, { numeric: true }));
  show(
    h("h1", { text: t("tasks.title") }),
    h("p", { class: "muted", text: t("tasks.open", { n: open.length }) }),
    h("section", { id: "tasks" }, open.length
      ? h("table", {}, h("thead", {}, h("tr", {}, [t("col.item"), t("col.title"), t("col.type"), t("col.state"), t("col.space")].map((c) => h("th", { text: c })))),
        h("tbody", {}, open.map((it) => h("tr", {},
          h("td", {}, link("#/item/" + encodeURIComponent(it.key), it.key)),
          h("td", { text: it.title }),
          h("td", { text: it.type }),
          h("td", { text: it.state }),
          h("td", {}, link(`#/board/${encodeURIComponent(it.space)}/${encodeURIComponent(it.type)}`, it.space)),
        ))))
      : h("p", { class: "muted", text: t("tasks.none") })),
    page.next ? h("p", { class: "notice", text: t("tasks.truncated", { n: items.length }) }) : null,
  );
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
      ? h("table", {}, h("thead", {}, h("tr", {}, [t("col.space"), t("gov.colSensitive"), t("gov.colStale"), t("gov.colImplausible")].map((c) => h("th", { text: c })))),
        h("tbody", {}, spaces.map((s, i) => h("tr", {},
          h("td", {}, link("#/governance/" + encodeURIComponent(s.key), s.key), " ", h("span", { class: "muted", text: s.name })),
          h("td", { text: String(reports[i].sensitive.length) }),
          h("td", { text: String(reports[i].stale.length) }),
          h("td", { text: String(reports[i].implausible.length) }),
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
  await learnNames([...rep.sensitive, ...rep.stale, ...rep.implausible].map((f) => f.responsible));
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
      h("h2", { text: t("gov.sensitive", { n: rep.sensitive.length }) }),
      h("p", { class: "muted", text: t("gov.sensitiveNote") }),
      table([t("col.item"), t("gov.where"), t("gov.what"), t("col.responsible"), t("gov.access")], rep.sensitive.map((f) => h("tr", {},
        itemCell(f), h("td", { text: f.where }), h("td", {}, h("div", { text: f.kind }), h("code", { text: f.masked })),
        h("td", { text: who(f.responsible) }),
        h("td", {}, f.restricted
          ? [h("span", { text: t("gov.restricted") + " " }), h("button", { class: "link", text: t("gov.lift"), onclick: () => action(() => lift(f), status) })]
          : t("gov.open"))))),
      h("h2", { text: t("gov.stale", { days: rep.staleDays, n: rep.stale.length }) }),
      table([t("col.item"), t("gov.lastChange"), t("col.responsible")], rep.stale.map((f) => h("tr", {},
        itemCell(f), h("td", { text: when(f.lastChange) }), h("td", { text: who(f.responsible) })))),
      h("h2", { text: t("gov.implausible", { n: rep.implausible.length }) }),
      table([t("col.item"), t("col.problem"), t("col.responsible")], rep.implausible.map((f) => h("tr", {},
        itemCell(f), h("td", { text: f.problem }), h("td", { text: who(f.responsible) })))),
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

// newItemForm creates an item in a space, below parent if one is given;
// "page" is preselected when the type exists.
function newItemForm(space, types, parent = "", heading = t("item.new"), id = "new-item") {
  if (types.length === 0) return null;
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
    const { item } = await post("/api/items", { space, type: ty.name, parent, title: f.querySelector("[name=title]").value, properties });
    location.hash = "#/item/" + encodeURIComponent(item.key);
  });
  return f;
}

// --- one item ---------------------------------------------------------------

async function itemView(ref) {
  const show = screen();
  const { item } = await get("/api/items/" + encodeURIComponent(ref));
  const path = "/api/items/" + item.id;
  const [{ types }, history, out, inc, embeds, comments] = await Promise.all([
    get("/api/types"),
    get(path + "/history"),
    get(path + "/relations?direction=outgoing"),
    get(path + "/relations?direction=incoming"),
    get(path + "/embeds"),
    get(path + "/comments"),
  ]);
  const type = types.find((ty) => ty.name === item.type) || { fields: [], workflow: null, body: false };
  const level = `/api/spaces/${encodeURIComponent(item.space)}/tree` + (item.parent ? "?parent=" + item.parent : "");
  const [, open, siblings] = await Promise.all([
    learnNames([...history.events.map((e) => e.actor), ...comments.comments.map((c) => c.author)]),
    ancestors(item),
    item.presence === "live" ? get(level) : { items: [] },
  ]);
  const status = h("div", { class: "status" });
  const live = item.presence === "live";

  // Moving up or down among the siblings: the keyboard's way to reorder,
  // besides dragging in the tree (ADR-0041).
  const at = siblings.items.findIndex((n) => n.id === item.id);
  const place = (before) => action(() => post(path + "/move", { space: item.space, parent: item.parent || "", before }), status);
  const reorder = [];
  if (live && at > 0) reorder.push(h("button", { text: t("item.moveUp"), onclick: () => place(siblings.items[at - 1].id) }));
  if (live && at >= 0 && (at + 1 < siblings.items.length)) {
    const after = siblings.items[at + 2];
    if (after || !siblings.next) reorder.push(h("button", { text: t("item.moveDown"), onclick: () => place(after ? after.id : "") }));
  }

  const title = live
    ? form({ class: "inline", id: "title" }, [
      h("input", { name: "title", value: item.title, class: "title", "aria-label": t("col.title") }),
      h("button", { type: "submit", text: t("item.saveTitle") }),
    ], async (data) => { await put(path + "/title", { title: data.get("title") }); route(); })
    : h("h1", { text: item.title || t("item.erased") });

  const props = h("table", { class: "props" }, type.fields.map((f) => {
    const input = propertyInput(f, item.properties[f.name]);
    input.disabled = !live;
    return h("tr", {},
      h("th", { text: f.name }),
      h("td", {}, input),
      h("td", {}, live ? h("button", { text: t("common.save"), onclick: () => action(() => put(`${path}/properties/${encodeURIComponent(f.name)}`, { value: readProperty(f, input) }), status) }) : null),
    );
  }));

  const transitions = type.workflow && live
    ? type.workflow.transitions.filter((tr) => tr.from.includes(item.state))
      .map((tr) => h("button", { text: tr.name, onclick: () => action(() => post(path + "/transitions", { name: tr.name }), status) }))
    : [];

  const actions = item.presence === "live"
    ? [h("button", { text: t("item.trash"), onclick: () => action(() => post(path + "/trash"), status) })]
    : item.presence === "trashed"
      ? [h("button", { text: t("item.restore"), onclick: () => action(() => post(path + "/restore"), status) }),
        h("button", { class: "danger", text: t("item.purge"), onclick: () => confirm(t("item.purgeConfirm")) && action(() => post(path + "/purge"), status) })]
      : [];

  const lastRestriction = history.events.filter((e) => e.kind === "RestrictionSet").pop();
  const guarded = item.restriction?.read?.length && lastRestriction?.actor === "governance";
  // The tree opens down to the page and shows the page's own children.
  const tree = pageTree({ space: item.space, current: item.id, open: [...open, item.id], onMoved: () => route(), onError: (err) => status.replaceChildren(errorBox(err)) });
  show(withTree(tree,
    h("p", {}, link("#/", t("nav.spaces")), " / ", link("#/space/" + encodeURIComponent(item.space), item.space), " / " + item.key),
    title,
    guarded ? h("p", { class: "notice", text: t("item.guarded") }) : null,
    h("p", { class: "muted" }, t("item.meta", { type: item.type, presence: item.presence }), item.state ? t("item.state", { state: item.state }) : "", t("item.version", { n: item.version })),
    status,
    transitions.length ? h("p", {}, t("item.transitions"), transitions) : null,
    type.fields.length ? h("section", {}, h("h2", { text: t("item.properties") }), props) : null,
    type.body ? bodyEditor(item, embeds.embeds, live) : null,
    relationsSection(out.relations, inc.relations, item.id),
    commentsSection(path, comments, live),
    h("p", {}, reorder, actions),
    live ? newItemForm(item.space, types, item.id, t("item.newChild"), "new-child") : null,
    historySection(path, history.events, live),
  ));
}

// bodyEditor edits a live item's body in its live session (ADR-0010):
// changes reach everyone at once and are saved when one leaves a block.
// A trashed or erased item's body is only shown.
function bodyEditor(item, embeds, live) {
  const resolve = async (ref) => (await get("/api/items/" + encodeURIComponent(ref))).item;
  if (!live) {
    return h("section", { id: "body" }, h("h2", { text: t("item.body") }), readOnlyBody(item, embeds));
  }
  const session = liveBody({ ref: item.id, embeds, resolve, childPages: childPagesOf(item) });
  onLeave(session.close);
  return h("section", { id: "body" }, h("h2", { text: t("item.body") }), session.element);
}

function relationsSection(out, inc, self) {
  const row = (r, other) => h("li", {}, `${r.type}${r.derived ? t("rel.fromBody") : ""} `, link("#/item/" + other, other.slice(0, 8)));
  if (out.length + inc.length === 0) return null;
  return h("section", {},
    h("h2", { text: t("rel.title") }),
    h("ul", { class: "list" }, out.map((r) => row(r, r.to)), inc.map((r) => row(r, r.from))),
  );
}

// commentsSection lists an item's comments (ADR-0035). Text is inserted as
// text (ADR-0014); one's own comments can be edited and deleted.
function commentsSection(path, page, live) {
  const author = (c) => person(c.author);
  const row = (c) => {
    const li = h("li", { class: "comment" });
    const head = h("div", { class: "muted" }, `${author(c)} · ${when(c.created)}`, c.edited ? t("comment.edited") : "");
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
      : null);
    return li;
  };
  const add = live
    ? form({ id: "add-comment", class: "comment-form" }, [
      h("textarea", { name: "text", rows: 3, placeholder: t("comment.placeholder"), "aria-label": t("comment.label"), required: true }),
      h("p", {}, h("button", { type: "submit", text: t("comment.add") })),
    ], async (data) => { await post(path + "/comments", { text: data.get("text") }); route(); })
    : null;
  return h("section", { id: "comments" },
    h("h2", { text: t("comment.title", { n: page.total.value }) }),
    page.comments.length ? h("ul", { class: "comments" }, page.comments.map(row)) : h("p", { class: "muted", text: t("comment.none") }),
    page.next ? h("p", { class: "muted", text: t("comment.truncated", { n: page.comments.length }) }) : null,
    add,
  );
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
      h("td", { text: person(e.actor) }),
      h("td", { text: e.kind }),
      h("td", {}, details(e)),
    ))),
  );
}

// --- account -----------------------------------------------------------------

async function accountView() {
  const show = screen();
  const { tokens } = await get("/api/tokens");
  const secret = h("div");
  show(
    h("h1", { text: me.user.username }),
    h("p", { class: "muted", text: t("account.id", { id: me.user.id, via: me.via }) }),
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
    ),
  );
}

// --- administration ----------------------------------------------------------

async function adminView() {
  const show = screen();
  const [{ users }, { spaces }, { types }, auditPage] = await Promise.all([
    get("/api/users"), get("/api/spaces"), get("/api/types"), get("/api/audit?limit=50"),
  ]);
  const userOptions = users.map((u) => h("option", { value: u.id, text: u.username }));
  for (const u of users) names.set(u.id, u.username);
  show(
    h("h1", { text: t("admin.title") }),
    form({ class: "card", id: "new-space" }, [
      h("h2", { text: t("admin.newSpace") }),
      field(t("admin.key"), h("input", { name: "key", required: true, pattern: "[A-Z][A-Z0-9]*" })),
      field(t("field.name"), h("input", { name: "name", required: true })),
      h("button", { type: "submit", text: t("admin.createSpace") }),
    ], async (d) => { await post("/api/spaces", { key: d.get("key"), name: d.get("name") }); route(); }),
    form({ class: "card", id: "new-type" }, [
      h("h2", { text: t("admin.defineType") }),
      h("p", { class: "muted", text: t("admin.existing", { list: types.map((ty) => `${ty.name} v${ty.version}`).join(", ") || t("admin.none") }) }),
      field(t("admin.typeJson"), h("textarea", { name: "type", rows: 6, required: true },
        '{"name":"page","body":true,"fields":[{"name":"owner","kind":"user"}]}')),
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
    }),
    form({ class: "card", id: "new-user" }, [
      h("h2", { text: t("admin.newUser") }),
      field(t("field.username"), h("input", { name: "username", required: true })),
      field(t("field.password"), h("input", { name: "password", type: "password", minlength: 12, required: true, autocomplete: "new-password" })),
      h("button", { type: "submit", text: t("admin.createUser") }),
    ], async (d) => { await post("/api/users", { username: d.get("username"), password: d.get("password") }); route(); }),
    form({ class: "card", id: "grant" }, [
      h("h2", { text: t("admin.grant") }),
      field(t("col.space"), h("select", { name: "space" }, spaces.map((s) => h("option", { value: s.key, text: s.key })))),
      field(t("admin.user"), h("select", { name: "user" }, userOptions)),
      field(t("admin.role"), h("select", { name: "role" }, [["viewer", t("role.viewer")], ["editor", t("role.editor")], ["space-admin", t("role.space-admin")]].map(([r, label]) => h("option", { value: r, text: label })))),
      h("button", { type: "submit", text: t("admin.grantButton") }),
    ], async (d, status) => {
      await put(`/api/spaces/${encodeURIComponent(d.get("space"))}/roles/${encodeURIComponent(d.get("user"))}`, { role: d.get("role") });
      status.replaceChildren(h("p", { class: "notice", text: t("admin.granted") }));
    }),
    form({ class: "card", id: "backup" }, [
      h("h2", { text: t("admin.backup") }),
      h("p", { class: "muted", text: t("admin.backupNote") }),
      field(t("admin.passphrase"), h("input", { name: "passphrase", type: "password", minlength: 12, required: true, autocomplete: "new-password" })),
      field(t("admin.repeat"), h("input", { name: "repeat", type: "password", minlength: 12, required: true, autocomplete: "new-password" })),
      h("button", { type: "submit", text: t("admin.download") }),
    ], async (d, status) => {
      if (d.get("passphrase") !== d.get("repeat")) throw new Error(t("admin.differ"));
      const { blob, name } = await download("/api/backup", { passphrase: d.get("passphrase") });
      const url = URL.createObjectURL(blob);
      const a = h("a", { download: name, text: name });
      a.href = url; // a blob made here, not user content; safeHref would refuse the scheme
      a.click();
      setTimeout(() => URL.revokeObjectURL(url), 60_000);
      status.replaceChildren(h("p", { class: "notice", text: t("admin.saved", { name }) }));
    }),
    h("section", { class: "card" },
      h("h2", { text: t("admin.audit", { n: 50 }) }),
      h("table", { class: "history" }, auditPage.entries.map((e) => h("tr", {},
        h("td", { text: when(e.at) }), h("td", { text: e.event }), h("td", { text: e.actor ? person(e.actor) : "" }),
        h("td", { text: e.addr || "" }), h("td", { text: e.subject || "" }),
        h("td", { text: Object.entries(e.detail || {}).map(([k, v]) => `${k}=${v}`).join(" ") }),
      ))),
    ),
  );
}
