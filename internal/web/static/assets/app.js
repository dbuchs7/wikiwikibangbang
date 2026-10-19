// The UI shell (ADR-0009, ADR-0031): hash routes, one view per route, all
// data through the HTTP API. Plain ES modules, no build step.
import { api, get, post, put, del, download, ApiError } from "./api.js";
import { h, mount, when } from "./dom.js";
import { createEditor } from "./editor.js";
import { liveBody } from "./live.js";

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

async function action(fn, status) {
  try {
    await fn();
    route();
  } catch (err) {
    if (status) status.replaceChildren(errorBox(err));
    else alert(err.message);
  }
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

function renderNav() {
  if (!me) {
    nav.replaceChildren();
    return;
  }
  const current = location.hash.startsWith("#/search/") ? decodeURIComponent(location.hash.slice(9)) : "";
  const search = h("form", { class: "search", role: "search", onsubmit: (ev) => {
    ev.preventDefault();
    const q = new FormData(ev.target).get("q").trim();
    if (q) location.hash = "#/search/" + encodeURIComponent(q);
  } }, h("input", { name: "q", type: "search", placeholder: "Search", "aria-label": "Search", value: current }));
  mount(nav,
    link("#/", "Spaces"),
    search,
    me.admin ? link("#/admin", "Administration") : null,
    link("#/account", me.user.username),
    h("button", { class: "link", onclick: () => action(async () => { await post("/api/logout"); me = null; location.hash = "#/"; }) }, "Log out"),
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
    }
    renderNav();
    renderFooter();
    if (!me) return loginView();
    const [, section, arg, arg2] = hash.split("/").map(decodeURIComponent);
    switch (section) {
      case "space": return await spaceView(arg);
      case "board": return await boardView(arg, arg2);
      case "links": return await linksView(arg);
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

window.addEventListener("hashchange", route);
route();

// --- login -------------------------------------------------------------------

async function loginView() {
  const show = screen();
  const params = new URLSearchParams(location.search);
  const sso = await get("/api/oidc").catch(() => ({ enabled: false }));
  show(
    h("section", { class: "card narrow" },
      h("h1", { text: "Log in" }),
      params.get("login_error") ? h("p", { class: "error", text: "Single sign-on failed. Try again or use your password." }) : null,
      form({ id: "login" }, [
        field("Username", h("input", { name: "username", autocomplete: "username", required: true })),
        field("Password", h("input", { name: "password", type: "password", autocomplete: "current-password", required: true })),
        h("button", { type: "submit", text: "Log in" }),
      ], async (data) => {
        await post("/api/login", { username: data.get("username"), password: data.get("password") });
        me = null;
        history.replaceState(null, "", "/#/");
        route();
      }),
      sso.enabled ? h("p", {}, h("a", { class: "button", href: "/api/oidc/login", text: "Log in with " + sso.name })) : null,
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
        hit.snippet ? h("p", { class: "snippet", text: hit.snippet }) : null,
      ));
    }
    more.replaceChildren();
    if (page.next) more.append(h("button", { text: "More", onclick: async () => addHits(await get(url(page.next))) }));
  };
  addHits(first);
  show(
    h("h1", { text: "Search" }),
    h("p", { class: "muted", text: `${first.total.value} results for “${q}”` }),
    first.total.value === 0 ? h("p", { text: "Nothing found among the items you may read." }) : list,
    more,
  );
}

// --- spaces ------------------------------------------------------------------

async function homeView() {
  const show = screen();
  const { spaces } = await get("/api/spaces");
  show(
    h("h1", { text: "Spaces" }),
    spaces.length === 0
      ? h("p", { class: "muted", text: me.admin ? "No spaces yet. Create one under Administration." : "You have no role in any space yet. Ask an administrator." })
      : h("ul", { class: "list" }, spaces.map((s) => h("li", {}, link("#/space/" + encodeURIComponent(s.key), s.name), " ", h("span", { class: "muted", text: s.key })))),
  );
}

async function spaceView(key) {
  const show = screen();
  const [{ types }, first] = await Promise.all([get("/api/types"), get(`/api/items?space=${encodeURIComponent(key)}&limit=100`)]);
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
      more.append(h("button", { text: "More", onclick: async () => addRows(await get(`/api/items?space=${encodeURIComponent(key)}&limit=100&after=${page.next}`)) }));
    }
  };
  addRows(first);
  const boards = types.filter((t) => t.workflow)
    .map((t) => link(`#/board/${encodeURIComponent(key)}/${encodeURIComponent(t.name)}`, "Board: " + t.name));
  show(
    h("p", {}, link("#/", "Spaces"), " / " + key),
    h("h1", { text: key }),
    h("p", {}, [...boards, link(`#/links/${encodeURIComponent(key)}`, "Link check")].flatMap((b, i) => i ? [" · ", b] : [b])),
    h("p", { class: "muted", text: `${first.total.value} items` }),
    h("table", {}, h("thead", {}, h("tr", {}, ["Key", "Title", "Type", "State"].map((c) => h("th", { text: c })))), rows),
    more,
    newItemForm(key, types),
  );
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
  const type = types.find((t) => t.name === typeName);
  if (!type || !type.workflow) throw new Error(`type ${typeName} has no workflow`);
  const base = `/api/items?space=${encodeURIComponent(key)}&type=${encodeURIComponent(typeName)}&limit=100`;
  let page = await get(base);
  const items = [...page.items];
  while (page.next && items.length < boardMax) {
    page = await get(`${base}&after=${encodeURIComponent(page.next)}`);
    items.push(...page.items);
  }
  const status = h("div", { class: "status" });
  const transitionsFrom = (state) => type.workflow.transitions.filter((t) => t.from.includes(state));
  const perform = (it, name) => action(() => post(`/api/items/${it.id}/transitions`, { name }), status);

  const card = (it) => h("li", { class: "card-item", draggable: "true", "data-id": it.id,
    ondragstart: (ev) => { ev.dataTransfer.setData("text/plain", it.id); ev.dataTransfer.effectAllowed = "move"; } },
  link("#/item/" + encodeURIComponent(it.key), it.key),
  h("div", { class: "card-title", text: it.title }),
  h("div", { class: "card-actions" }, transitionsFrom(it.state).map((t) =>
    h("button", { class: "link", text: `${t.name} → ${t.to}`, onclick: () => perform(it, t.name) }))),
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
        const to = transitionsFrom(it.state).filter((t) => t.to === state);
        if (to.length === 1) return perform(it, to[0].name);
        status.replaceChildren(h("p", { class: "error", text: to.length
          ? `Several transitions lead from ${it.state} to ${state}; use the buttons on the card.`
          : `No transition leads from ${it.state} to ${state}.` }));
      } },
    h("h2", {}, state, h("span", { class: "muted", text: ` ${cards.length}` })),
    h("ul", { class: "board-cards" }, cards.map(card)));
    return col;
  };
  show(
    h("p", {}, link("#/", "Spaces"), " / ", link("#/space/" + encodeURIComponent(key), key), ` / board: ${typeName}`),
    h("h1", { text: `${key} · ${typeName}` }),
    page.next ? h("p", { class: "notice", text: `This board shows the first ${items.length} items.` }) : null,
    status,
    h("div", { class: "board" }, type.workflow.states.map(column)),
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
  const status = h("div", { class: "status" });
  const who = (id) => id === me.user.id ? "you" : id;
  let body;
  if (!report.enabled) {
    body = h("p", { class: "notice", text: "Link checking is switched off on this server. An administrator enables it with WWBB_LINKCHECK=on." });
  } else {
    const start = h("button", { text: report.running ? "Checking…" : "Check links", disabled: report.running,
      onclick: () => action(() => post(path), status) });
    const summary = !report.started ? "No check has run yet."
      : report.running ? `Checking since ${when(report.started)}…`
        : `Checked ${report.checked} addresses, finished ${when(report.finished)}${report.truncated ? " (only the first ones; the space has more)" : ""}.`;
    body = h("div", {},
      h("p", {}, start, " ", h("span", { class: "muted", text: summary })),
      status,
      report.finished && !report.running
        ? (report.broken.length
          ? h("table", { class: "links" }, h("thead", {}, h("tr", {}, ["Used in", "Link", "Problem", "Responsible"].map((c) => h("th", { text: c })))),
            h("tbody", {}, report.broken.map((b) => h("tr", {},
              // The item the link is used in, and where in its body.
              h("td", {}, link("#/item/" + encodeURIComponent(b.key), b.title || b.key),
                h("div", { class: "muted", text: `${b.key} · block ${b.block + 1}` })),
              h("td", { class: "href", text: b.href }),
              h("td", { text: b.problem }),
              h("td", { text: b.responsible ? who(b.responsible) : "—" }),
            ))))
          : h("p", { text: "No broken links found." }))
        : null,
    );
    if (report.running) {
      const timer = setTimeout(route, 2000);
      onLeave(() => clearTimeout(timer));
    }
  }
  show(
    h("p", {}, link("#/", "Spaces"), " / ", link("#/space/" + encodeURIComponent(key), key), " / link check"),
    h("h1", { text: `${key} · link check` }),
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

function newItemForm(space, types) {
  if (types.length === 0) return null;
  const fields = h("div");
  const typeSelect = h("select", { name: "type" }, types.map((t) => h("option", { value: t.name, text: t.name })));
  const showFields = () => {
    const t = types.find((x) => x.name === typeSelect.value);
    fields.replaceChildren(...t.fields.map((f) => field(f.name + (f.required ? " *" : ""), propertyInput(f))));
  };
  typeSelect.addEventListener("change", showFields);
  showFields();
  const f = form({ class: "card", id: "new-item" }, [
    h("h2", { text: "New item" }),
    field("Type", typeSelect),
    field("Title", h("input", { name: "title", required: true })),
    fields,
    h("button", { type: "submit", text: "Create" }),
  ], async () => {
    const t = types.find((x) => x.name === typeSelect.value);
    const properties = {};
    for (const fd of t.fields) {
      const v = readProperty(fd, f.querySelector(`[name="p:${CSS.escape(fd.name)}"]`));
      if (v !== null) properties[fd.name] = v;
    }
    const { item } = await post("/api/items", { space, type: t.name, title: f.querySelector("[name=title]").value, properties });
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
  const type = types.find((t) => t.name === item.type) || { fields: [], workflow: null, body: false };
  const status = h("div", { class: "status" });
  const live = item.presence === "live";

  const title = live
    ? form({ class: "inline", id: "title" }, [
      h("input", { name: "title", value: item.title, class: "title", "aria-label": "Title" }),
      h("button", { type: "submit", text: "Save title" }),
    ], async (data) => { await put(path + "/title", { title: data.get("title") }); route(); })
    : h("h1", { text: item.title || "(erased)" });

  const props = h("table", { class: "props" }, type.fields.map((f) => {
    const input = propertyInput(f, item.properties[f.name]);
    input.disabled = !live;
    return h("tr", {},
      h("th", { text: f.name }),
      h("td", {}, input),
      h("td", {}, live ? h("button", { text: "Save", onclick: () => action(() => put(`${path}/properties/${encodeURIComponent(f.name)}`, { value: readProperty(f, input) }), status) }) : null),
    );
  }));

  const transitions = type.workflow && live
    ? type.workflow.transitions.filter((t) => t.from.includes(item.state))
      .map((t) => h("button", { text: t.name, onclick: () => action(() => post(path + "/transitions", { name: t.name }), status) }))
    : [];

  const actions = item.presence === "live"
    ? [h("button", { text: "Move to trash", onclick: () => action(() => post(path + "/trash"), status) })]
    : item.presence === "trashed"
      ? [h("button", { text: "Restore", onclick: () => action(() => post(path + "/restore"), status) }),
        h("button", { class: "danger", text: "Delete for good", onclick: () => confirm("Delete for good? The content cannot be recovered.") && action(() => post(path + "/purge"), status) })]
      : [];

  show(
    h("p", {}, link("#/", "Spaces"), " / ", link("#/space/" + encodeURIComponent(item.space), item.space), " / " + item.key),
    title,
    h("p", { class: "muted" }, `${item.type} · ${item.presence}`, item.state ? ` · state: ${item.state}` : "", ` · version ${item.version}`),
    status,
    transitions.length ? h("p", {}, "Transitions: ", transitions) : null,
    type.fields.length ? h("section", {}, h("h2", { text: "Properties" }), props) : null,
    type.body ? bodyEditor(item, embeds.embeds, live) : null,
    relationsSection(out.relations, inc.relations, item.id),
    commentsSection(path, comments, live),
    h("p", {}, actions),
    historySection(history.events),
  );
}

// bodyEditor edits a live item's body in its live session (ADR-0010):
// changes reach everyone at once and are saved when one leaves a block.
// A trashed or erased item's body is only shown.
function bodyEditor(item, embeds, live) {
  const resolve = async (ref) => (await get("/api/items/" + encodeURIComponent(ref))).item;
  if (!live) {
    return h("section", { id: "body" }, h("h2", { text: "Body" }),
      createEditor({ blocks: item.body, embeds, editable: false, resolve }).element);
  }
  const session = liveBody({ ref: item.id, embeds, resolve });
  onLeave(session.close);
  return h("section", { id: "body" }, h("h2", { text: "Body" }), session.element);
}

function relationsSection(out, inc, self) {
  const row = (r, other) => h("li", {}, `${r.type}${r.derived ? " (from the body)" : ""} `, link("#/item/" + other, other.slice(0, 8)));
  if (out.length + inc.length === 0) return null;
  return h("section", {},
    h("h2", { text: "Relations" }),
    h("ul", { class: "list" }, out.map((r) => row(r, r.to)), inc.map((r) => row(r, r.from))),
  );
}

// commentsSection lists an item's comments (ADR-0035). Text is inserted as
// text (ADR-0014); one's own comments can be edited and deleted.
function commentsSection(path, page, live) {
  const author = (c) => c.author === me.user.id ? "you" : c.author;
  const row = (c) => {
    const li = h("li", { class: "comment" });
    const head = h("div", { class: "muted" }, `${author(c)} · ${when(c.created)}`, c.edited ? " · edited" : "");
    if (c.deleted) {
      li.append(head, h("p", { class: "muted", text: "(comment deleted)" }));
      return li;
    }
    const mine = live && c.author === me.user.id;
    const status = h("div", { class: "status" });
    const edit = () => li.replaceChildren(head, form({ class: "comment-form" }, [
      h("textarea", { name: "text", rows: 3, "aria-label": "Edit comment", value: c.text }),
      h("p", {}, h("button", { type: "submit", text: "Save comment" }), h("button", { type: "button", class: "link", text: "Cancel", onclick: () => route() })),
    ], async (data) => { await put(`${path}/comments/${c.id}`, { text: data.get("text") }); route(); }));
    li.append(head, h("p", { class: "comment-text", text: c.text }), status, mine
      ? h("p", {},
        h("button", { class: "link", text: "Edit", onclick: edit }), " · ",
        h("button", { class: "link", text: "Delete", onclick: () => confirm("Delete this comment?") && action(() => del(`${path}/comments/${c.id}`), status) }))
      : null);
    return li;
  };
  const add = live
    ? form({ id: "add-comment", class: "comment-form" }, [
      h("textarea", { name: "text", rows: 3, placeholder: "Write a comment", "aria-label": "Comment", required: true }),
      h("p", {}, h("button", { type: "submit", text: "Add comment" })),
    ], async (data) => { await post(path + "/comments", { text: data.get("text") }); route(); })
    : null;
  return h("section", { id: "comments" },
    h("h2", { text: `Comments (${page.total.value})` }),
    page.comments.length ? h("ul", { class: "comments" }, page.comments.map(row)) : h("p", { class: "muted", text: "No comments yet." }),
    page.next ? h("p", { class: "muted", text: `Showing the first ${page.comments.length}.` }) : null,
    add,
  );
}

function historySection(events) {
  return h("section", {},
    h("h2", { text: "History" }),
    h("table", { class: "history" }, events.slice().reverse().map((e) => h("tr", {},
      h("td", { text: when(e.at) }),
      h("td", { text: e.actor }),
      h("td", { text: e.kind }),
      h("td", { text: e.erased ? "(content erased)" : [e.title, e.field && `${e.field} = ${JSON.stringify(e.value ?? null)}`, e.to, e.comment && `comment ${e.comment.slice(0, 8)}`].filter(Boolean).join(" ") }),
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
    h("p", { class: "muted", text: `ID ${me.user.id} · signed in via ${me.via}` }),
    form({ class: "card", id: "password" }, [
      h("h2", { text: "Change password" }),
      field("Current password", h("input", { name: "current", type: "password", autocomplete: "current-password", required: true })),
      field("New password", h("input", { name: "new", type: "password", autocomplete: "new-password", minlength: 12, required: true })),
      h("button", { type: "submit", text: "Change" }),
    ], async (data) => {
      await put("/api/me/password", { current: data.get("current"), new: data.get("new") });
      me = null;
      route();
    }),
    h("section", { class: "card" },
      h("h2", { text: "API tokens" }),
      h("table", {}, tokens.map((t) => h("tr", {},
        h("td", { text: t.name }), h("td", { text: t.scope }), h("td", { text: "expires " + when(t.expires) }),
        h("td", {}, h("button", { text: "Revoke", onclick: () => action(() => del("/api/tokens/" + t.id)) })),
      ))),
      form({ id: "new-token" }, [
        field("Name", h("input", { name: "name", required: true })),
        field("Scope", h("select", { name: "scope" }, h("option", { value: "read", text: "read" }), h("option", { value: "full", text: "full" }),
          me.admin ? h("option", { value: "metrics", text: "metrics (only GET /metrics, for a scraper)" }) : null)),
        field("Days", h("input", { name: "days", type: "number", min: 1, max: 366, value: 30 })),
        h("button", { type: "submit", text: "Create token" }),
      ], async (data) => {
        const res = await post("/api/tokens", { name: data.get("name"), scope: data.get("scope"), days: Number(data.get("days")) });
        secret.replaceChildren(h("p", { class: "notice" }, "Copy this token now; it is not shown again: ", h("code", { text: res.secret })));
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
  show(
    h("h1", { text: "Administration" }),
    form({ class: "card", id: "new-space" }, [
      h("h2", { text: "New space" }),
      field("Key (upper-case letters and digits)", h("input", { name: "key", required: true, pattern: "[A-Z][A-Z0-9]*" })),
      field("Name", h("input", { name: "name", required: true })),
      h("button", { type: "submit", text: "Create space" }),
    ], async (d) => { await post("/api/spaces", { key: d.get("key"), name: d.get("name") }); route(); }),
    form({ class: "card", id: "new-type" }, [
      h("h2", { text: "Define a type" }),
      h("p", { class: "muted", text: "Existing: " + (types.map((t) => `${t.name} v${t.version}`).join(", ") || "none") }),
      field("Type as JSON", h("textarea", { name: "type", rows: 6, required: true },
        '{"name":"page","body":true,"fields":[{"name":"owner","kind":"user"}]}')),
      h("button", { type: "submit", text: "Define" }),
    ], async (d) => {
      let def;
      try {
        def = JSON.parse(d.get("type"));
      } catch (err) {
        throw new Error("not valid JSON: " + err.message);
      }
      await post("/api/types", def);
      route();
    }),
    form({ class: "card", id: "new-user" }, [
      h("h2", { text: "New user" }),
      field("Username", h("input", { name: "username", required: true })),
      field("Password", h("input", { name: "password", type: "password", minlength: 12, required: true, autocomplete: "new-password" })),
      h("button", { type: "submit", text: "Create user" }),
    ], async (d) => { await post("/api/users", { username: d.get("username"), password: d.get("password") }); route(); }),
    form({ class: "card", id: "grant" }, [
      h("h2", { text: "Grant a space role" }),
      field("Space", h("select", { name: "space" }, spaces.map((s) => h("option", { value: s.key, text: s.key })))),
      field("User", h("select", { name: "user" }, userOptions)),
      field("Role", h("select", { name: "role" }, ["viewer", "editor", "space-admin"].map((r) => h("option", { value: r, text: r })))),
      h("button", { type: "submit", text: "Grant" }),
    ], async (d, status) => {
      await put(`/api/spaces/${encodeURIComponent(d.get("space"))}/roles/${encodeURIComponent(d.get("user"))}`, { role: d.get("role") });
      status.replaceChildren(h("p", { class: "notice", text: "Granted." }));
    }),
    form({ class: "card", id: "backup" }, [
      h("h2", { text: "Backup" }),
      h("p", { class: "muted", text: "An encrypted copy of all data, including the keys to the content and the account hashes. Keep the passphrase apart from the file: without it the backup cannot be restored, with it anyone can read everything in it — also content deleted for good after the backup was made." }),
      field("Passphrase", h("input", { name: "passphrase", type: "password", minlength: 12, required: true, autocomplete: "new-password" })),
      field("Repeat", h("input", { name: "repeat", type: "password", minlength: 12, required: true, autocomplete: "new-password" })),
      h("button", { type: "submit", text: "Download backup" }),
    ], async (d, status) => {
      if (d.get("passphrase") !== d.get("repeat")) throw new Error("The passphrases differ.");
      const { blob, name } = await download("/api/backup", { passphrase: d.get("passphrase") });
      const url = URL.createObjectURL(blob);
      const a = h("a", { download: name, text: name });
      a.href = url; // a blob made here, not user content; safeHref would refuse the scheme
      a.click();
      setTimeout(() => URL.revokeObjectURL(url), 60_000);
      status.replaceChildren(h("p", { class: "notice", text: `Saved ${name}. Restore it with: wikiwikibangbang restore -from ${name}` }));
    }),
    h("section", { class: "card" },
      h("h2", { text: "Audit trail (latest 50)" }),
      h("table", { class: "history" }, auditPage.entries.map((e) => h("tr", {},
        h("td", { text: when(e.at) }), h("td", { text: e.event }), h("td", { text: e.actor || "" }),
        h("td", { text: e.addr || "" }), h("td", { text: e.subject || "" }),
        h("td", { text: Object.entries(e.detail || {}).map(([k, v]) => `${k}=${v}`).join(" ") }),
      ))),
    ),
  );
}
