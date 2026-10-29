// One's connections to process engines (ADR-0151): connecting takes one
// to the engine, where one signs in and approves this server for one's
// tasks, and back here; a connection shows who one is at the engine, and
// whether the engine knows one by another mail address than one's own.
import { h, when } from "./dom.js";
import { get, del } from "./api.js";
import { t } from "./i18n.js";

const errorBox = (err) => h("p", { class: "error", role: "alert", text: err instanceof Error ? err.message : String(err) });

// returned is what the engine's redirect back said, once: the address
// is cleaned so a reload does not say it again.
function returned() {
  const q = new URLSearchParams(location.search);
  const out = { connected: q.get("engine_connected"), error: q.get("engine_error") };
  if (out.connected || out.error) history.replaceState(null, "", location.pathname + location.hash);
  return out;
}

// errorText says why a connection did not come about.
const errorText = (code) => ({ config: t("engineconn.errConfig"), state: t("engineconn.errState"), denied: t("engineconn.errDenied") })[code] ||
  t("engineconn.errUnavailable");

// engineRow is one engine, connected or not.
function engineRow(e, reload) {
  const c = e.connection;
  if (!c) {
    return h("li", { "data-engine": e.id },
      h("strong", { text: e.name }), " ",
      h("a", { class: "button secondary", href: "/api/engines/" + encodeURIComponent(e.id) + "/connect", text: t("engineconn.connect") }));
  }
  const who = [c.name, c.email].filter(Boolean).join(", ");
  return h("li", { "data-engine": e.id, class: "connected" },
    h("strong", { text: e.name }), " ",
    h("span", { text: t("engineconn.as", { username: c.username }) + (who ? " (" + who + ")" : "") }),
    c.groups?.length ? h("p", { class: "muted", text: t("engineconn.groups", { groups: c.groups.join(", ") }) }) : "",
    c.emailDiffers ? h("p", { class: "notice", text: t("engineconn.emailDiffers", { email: c.email }) }) : "",
    h("p", { class: "muted", text: t("engineconn.since", { when: when(c.connected) }) }),
    h("p", {},
      h("button", { type: "button", class: "link danger", text: t("engineconn.disconnect"), onclick: async (ev) => {
        if (!confirm(t("engineconn.disconnectConfirm", { name: e.name }))) return;
        ev.currentTarget.disabled = true;
        try {
          await del("/api/me/engines/" + encodeURIComponent(e.id));
          reload();
        } catch (err) {
          ev.target.closest("li").append(errorBox(err));
        }
      } }), " · ",
      h("a", { href: e.taskApp, target: "_blank", rel: "noopener", text: t("engineconn.taskApp") })));
}

// connectionsCard lists the engines one may connect to; it is left out
// when there is none.
export function connectionsCard() {
  const card = h("section", { class: "card", id: "engines", hidden: true });
  const back = returned();
  const load = async () => {
    let engines;
    try {
      ({ engines } = await get("/api/me/engines"));
    } catch (err) {
      card.hidden = false;
      card.replaceChildren(h("h2", { text: t("engineconn.title") }), errorBox(err));
      return;
    }
    if (!engines.length && !back.error) return;
    card.hidden = false;
    const named = engines.find((e) => e.id === back.connected);
    card.replaceChildren(
      h("h2", { text: t("engineconn.title") }),
      h("p", { class: "muted", text: t("engineconn.note") }),
      named ? h("p", { class: "notice success", id: "engine-connected", text: t("engineconn.connected", { name: named.name }) }) : "",
      back.error ? h("p", { class: "error", role: "alert", id: "engine-error", text: errorText(back.error) }) : "",
      h("ul", { class: "engine-connections" }, engines.map((e) => engineRow(e, () => { back.connected = null; back.error = null; load(); }))));
  };
  load();
  return card;
}
