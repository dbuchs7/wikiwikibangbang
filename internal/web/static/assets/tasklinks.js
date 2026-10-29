// The task links of a process engine in the administration (ADR-0151):
// whether they run, their rules, the credential the open tasks are listed
// with, how people connect their accounts to the engine, a try that
// changes nothing, a round now, and the last round's report.
import { h, when } from "./dom.js";
import { get, put, post } from "./api.js";
import { t } from "./i18n.js";
import { dateFields, endStates, linkTypes, nextRuleID, ruleOf } from "./taskparts.js";

const errorBox = (err) => h("p", { class: "error", role: "alert", text: err instanceof Error ? err.message : String(err) });
const field = (label, input) => h("label", { class: "field" }, h("span", { text: label }), input);

// reportOf shows what a round did, or, dry, would do.
function reportOf(rep) {
  if (!rep) return "";
  const n = (k) => String(rep[k] || 0);
  const counts = { when: when(rep.at), open: n("open"), linked: n("linked"), updated: n("updated"), ended: n("ended"), unmatched: n("unmatched"), facts: n("facts") };
  // A round that only followed the feed read no list of open tasks.
  const key = rep.dry ? "tasklinks.reportDry" : rep.compared === false ? "tasklinks.reportFollow" : "tasklinks.report";
  const lines = [h("p", { class: rep.error ? "error" : "notice", text: rep.error
    ? t("tasklinks.failed", { when: when(rep.at), error: rep.error })
    : t(key, counts) })];
  if (!rep.dry && rep.compared && rep.facts) lines.push(h("p", { class: "muted", text: t("tasklinks.alsoFollowed", { facts: n("facts") }) }));
  // Who sees each task, from the connected people's own lists.
  if (rep.visible) lines.push(h("p", { class: "muted", id: "task-links-visible", text: t("tasklinks.visible", { people: n("people"), unread: n("unread"), readers: n("readers") }) }));
  const problems = rep.problems || [];
  if (problems.length) {
    lines.push(h("p", { class: "muted", text: t("tasklinks.problems") }),
      h("ul", { class: "task-link-problems" }, problems.map((p) => h("li", { text: p }))));
    if (rep.more) lines.push(h("p", { class: "muted", text: t("tasklinks.more", { n: String(rep.more) }) }));
  }
  return h("div", { class: "task-link-report" }, lines);
}

// ruleBox edits one rule: what it matches, where its tasks go and how
// they look. Choosing a type offers that type's date fields and finished
// states.
function ruleBox(rule, spaces, types, remove) {
  const typeSelect = h("select", { name: "type", required: true },
    types.map((ty) => h("option", { value: ty.name, text: ty.name, selected: ty.name === rule.type })));
  const typed = h("div", { class: "task-rule-typed" });
  const fill = () => {
    const ty = types.find((x) => x.name === typeSelect.value);
    const ends = endStates(ty);
    const pick = (name, chosen, fallback) => h("select", { name, required: true },
      ends.map((s) => h("option", { value: s, text: s, selected: s === (chosen || fallback) })));
    typed.replaceChildren(
      field(t("tasklinks.dueField"), h("select", { name: "dueField" }, h("option", { value: "", text: t("tasklinks.dueNone") }),
        dateFields(ty).map((f) => h("option", { value: f, text: f, selected: f === rule.dueField })))),
      field(t("tasklinks.completed"), pick("completed", rule.completed, ends[0])),
      field(t("tasklinks.cancelled"), pick("cancelled", rule.cancelled, ends[ends.length - 1])));
  };
  typeSelect.addEventListener("change", fill);
  fill();
  return h("fieldset", { class: "task-rule connector-target", "data-rule": rule.id },
    h("legend", { text: t("tasklinks.rule", { id: rule.id }) }),
    field(t("tasklinks.ruleId"), h("input", { name: "id", value: rule.id, required: true, pattern: "[a-z0-9][a-z0-9.\\-]{0,39}", maxlength: 40 })),
    field(t("tasklinks.processes"), h("input", { name: "processes", value: (rule.processes || []).join(", ") })),
    field(t("tasklinks.elements"), h("input", { name: "elements", value: (rule.elements || []).join(", ") })),
    field(t("tasklinks.groups"), h("input", { name: "groups", value: (rule.groups || []).join(", ") })),
    field(t("tasklinks.space"), h("select", { name: "space", required: true },
      spaces.map((s) => h("option", { value: s.key, text: `${s.name} (${s.key})`, selected: s.key === rule.space })))),
    field(t("tasklinks.type"), typeSelect),
    typed,
    field(t("tasklinks.labels"), h("input", { name: "labels", value: (rule.labels || []).join(", ") })),
    h("p", {}, h("button", { type: "button", class: "link danger", text: t("tasklinks.removeRule"), onclick: remove })));
}

// readRule reads a rule from its box.
function readRule(box) {
  const v = {};
  for (const el of box.querySelectorAll("input, select")) v[el.name] = el.value;
  return ruleOf(v);
}

// connectInfo says how people connect their accounts to the engine: this
// server's registration there, the address the engine sends them back
// to, and how many did.
function connectInfo(c) {
  return h("div", { id: "task-links-connect" },
    h("p", { class: c.configured ? "muted" : "error", text: c.configured
      ? t("tasklinks.connectOn", { n: String(c.connected || 0) })
      : t("tasklinks.connectOff", { id: c.clientIdVar, secret: c.clientSecretVar }) }),
    c.callback
      ? h("p", { class: "muted" }, t("tasklinks.callback"), " ", h("code", { text: c.callback }))
      : h("p", { class: "error", text: t("tasklinks.noPublicURL") }));
}

// taskLinksCard is the task links of an engine.
export function taskLinksCard(engine) {
  const path = "/api/engines/" + encodeURIComponent(engine.id) + "/task-links";
  const card = h("div", { id: "task-links", class: "connector-editor" }, h("p", { class: "muted", text: t("autobackup.loading") }));
  const load = async (report) => {
    let links, spaces, types;
    try {
      [links, { spaces }, { types }] = await Promise.all([get(path), get("/api/spaces"), get("/api/types")]);
    } catch (err) {
      card.replaceChildren(errorBox(err));
      return;
    }
    types = linkTypes(types).filter((ty) => endStates(ty).length);
    const status = h("div", { class: "status", id: "task-links-status" }, reportOf(report || links.last));
    const rules = h("div", { id: "task-rules" });
    const add = (rule) => {
      const box = ruleBox(rule, spaces, types, () => box.remove());
      rules.append(box);
    };
    (links.rules || []).forEach(add);
    const enabled = h("input", { type: "checkbox", name: "enabled", checked: links.enabled });
    const save = async () => {
      const body = { enabled: enabled.checked, rules: [...rules.querySelectorAll(".task-rule")].map(readRule) };
      await put(path, body);
    };
    const act = (fn) => async (ev) => {
      const button = ev.currentTarget;
      button.disabled = true;
      status.replaceChildren(h("p", { class: "muted", text: t("autobackup.loading") }));
      try {
        await fn();
      } catch (err) {
        status.replaceChildren(errorBox(err));
      } finally {
        button.disabled = false;
      }
    };
    card.replaceChildren(
      h("h3", { text: t("tasklinks.title") }),
      h("p", { class: "muted", text: t("tasklinks.note") }),
      h("p", { class: links.credential ? "muted" : "error", id: "task-links-credential",
        text: links.credential ? t("tasklinks.credentialIs", { name: links.credentialVar }) : t("tasklinks.credentialMissing", { name: links.credentialVar }) }),
      h("p", { class: "muted", id: "task-links-events", text: links.events ? t("tasklinks.eventsIs", { name: links.eventsVar }) : t("tasklinks.eventsMissing", { name: links.eventsVar }) }),
      links.readAt ? h("p", { class: links.stale ? "notice warning" : "muted", id: "task-links-fresh", text: t(links.stale ? "enginetask.stale" : "enginetask.fresh", { engine: engine.name, when: when(links.readAt) }) }) : "",
      connectInfo(links.connect || {}),
      types.length ? "" : h("p", { class: "error", text: t("tasklinks.noTypes") }),
      h("label", { class: "check" }, enabled, " ", t("tasklinks.enabled")),
      rules,
      h("p", {}, h("button", { type: "button", class: "secondary", id: "task-rule-add", text: t("tasklinks.addRule"), disabled: !types.length || !spaces.length,
        onclick: () => add({ id: nextRuleID([...rules.querySelectorAll(".task-rule")].map(readRule)), space: spaces[0]?.key, type: types[0]?.name }) })),
      h("div", { class: "actions-row" },
        h("button", { type: "button", class: "primary", id: "task-links-save", text: t("tasklinks.save"), onclick: act(async () => {
          await save();
          await load();
          card.querySelector("#task-links-status").prepend(h("p", { class: "notice", text: t("tasklinks.saved") }));
        }) }),
        h("button", { type: "button", class: "secondary", id: "task-links-dry", text: t("tasklinks.dry"), onclick: act(async () => {
          status.replaceChildren(reportOf(await post(path + "/run?dry=true", {})));
        }) }),
        h("button", { type: "button", class: "secondary", id: "task-links-run", text: t("tasklinks.run"), onclick: act(async () => {
          await load(await post(path + "/run", {}));
        }) })),
      status);
  };
  load();
  return card;
}
