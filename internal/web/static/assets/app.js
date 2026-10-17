// The UI shell (ADR-0009, ADR-0031): hash routes, one view per route, all
// data through the HTTP API. Plain ES modules, no build step.
import { api, get, post, put, del, ApiError } from "./api.js";
import { h, mount, when } from "./dom.js";
import { createEditor } from "./editor.js";

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

function renderNav() {
  if (!me) {
    nav.replaceChildren();
    return;
  }
  mount(nav,
    link("#/", "Spaces"),
    me.admin ? link("#/admin", "Administration") : null,
    link("#/account", me.user.username),
    h("button", { class: "link", onclick: () => action(async () => { await post("/api/logout"); me = null; location.hash = "#/"; }) }, "Log out"),
  );
}

async function route() {
  const hash = location.hash.replace(/^#/, "") || "/";
  try {
    if (!me) {
      try {
        me = await get("/api/me");
      } catch (err) {
        if (!(err instanceof ApiError) || err.status !== 401) throw err;
      }
    }
    renderNav();
    if (!me) return loginView();
    const [, section, arg] = hash.split("/").map(decodeURIComponent);
    switch (section) {
      case "space": return await spaceView(arg);
      case "item": return await itemView(arg);
      case "account": return await accountView();
      case "admin": return await adminView();
      default: return await homeView();
    }
  } catch (err) {
    mount(app, errorBox(err));
  }
}

window.addEventListener("hashchange", route);
route();

// --- login -------------------------------------------------------------------

async function loginView() {
  const params = new URLSearchParams(location.search);
  const sso = await get("/api/oidc").catch(() => ({ enabled: false }));
  mount(app,
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

// --- spaces ------------------------------------------------------------------

async function homeView() {
  const { spaces } = await get("/api/spaces");
  mount(app,
    h("h1", { text: "Spaces" }),
    spaces.length === 0
      ? h("p", { class: "muted", text: me.admin ? "No spaces yet. Create one under Administration." : "You have no role in any space yet. Ask an administrator." })
      : h("ul", { class: "list" }, spaces.map((s) => h("li", {}, link("#/space/" + encodeURIComponent(s.key), s.name), " ", h("span", { class: "muted", text: s.key })))),
  );
}

async function spaceView(key) {
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
  mount(app,
    h("p", {}, link("#/", "Spaces"), " / " + key),
    h("h1", { text: key }),
    h("p", { class: "muted", text: `${first.total.value} items` }),
    h("table", {}, h("thead", {}, h("tr", {}, ["Key", "Title", "Type", "State"].map((c) => h("th", { text: c })))), rows),
    more,
    newItemForm(key, types),
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
  const { item } = await get("/api/items/" + encodeURIComponent(ref));
  const path = "/api/items/" + item.id;
  const [{ types }, history, out, inc, embeds] = await Promise.all([
    get("/api/types"),
    get(path + "/history"),
    get(path + "/relations?direction=outgoing"),
    get(path + "/relations?direction=incoming"),
    get(path + "/embeds"),
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

  mount(app,
    h("p", {}, link("#/", "Spaces"), " / ", link("#/space/" + encodeURIComponent(item.space), item.space), " / " + item.key),
    title,
    h("p", { class: "muted" }, `${item.type} · ${item.presence}`, item.state ? ` · state: ${item.state}` : "", ` · version ${item.version}`),
    status,
    transitions.length ? h("p", {}, "Transitions: ", transitions) : null,
    type.fields.length ? h("section", {}, h("h2", { text: "Properties" }), props) : null,
    type.body ? bodyEditor(item, path, embeds.embeds, live, status) : null,
    relationsSection(out.relations, inc.relations, item.id),
    h("p", {}, actions),
    historySection(history.events),
  );
}

function bodyEditor(item, path, embeds, live, status) {
  const ed = createEditor({
    blocks: item.body,
    embeds,
    editable: live,
    resolve: async (ref) => (await get("/api/items/" + encodeURIComponent(ref))).item,
  });
  return h("section", { id: "body" },
    h("h2", { text: "Body" }),
    ed.element,
    live ? h("p", {}, h("button", { text: "Save body", onclick: () => action(() => put(path + "/body", { blocks: ed.blocks() }), status) })) : null,
  );
}

function relationsSection(out, inc, self) {
  const row = (r, other) => h("li", {}, `${r.type}${r.derived ? " (from the body)" : ""} `, link("#/item/" + other, other.slice(0, 8)));
  if (out.length + inc.length === 0) return null;
  return h("section", {},
    h("h2", { text: "Relations" }),
    h("ul", { class: "list" }, out.map((r) => row(r, r.to)), inc.map((r) => row(r, r.from))),
  );
}

function historySection(events) {
  return h("section", {},
    h("h2", { text: "History" }),
    h("table", { class: "history" }, events.slice().reverse().map((e) => h("tr", {},
      h("td", { text: when(e.at) }),
      h("td", { text: e.actor }),
      h("td", { text: e.kind }),
      h("td", { text: e.erased ? "(content erased)" : [e.title, e.field && `${e.field} = ${JSON.stringify(e.value ?? null)}`, e.to].filter(Boolean).join(" ") }),
    ))),
  );
}

// --- account -----------------------------------------------------------------

async function accountView() {
  const { tokens } = await get("/api/tokens");
  const secret = h("div");
  mount(app,
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
        field("Scope", h("select", { name: "scope" }, h("option", { value: "read", text: "read" }), h("option", { value: "full", text: "full" }))),
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
  const [{ users }, { spaces }, { types }, auditPage] = await Promise.all([
    get("/api/users"), get("/api/spaces"), get("/api/types"), get("/api/audit?limit=50"),
  ]);
  const userOptions = users.map((u) => h("option", { value: u.id, text: u.username }));
  mount(app,
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
