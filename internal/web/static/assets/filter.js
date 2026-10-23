// The filter of a list (ADR-0098): one bar above every list, the same
// everywhere. A text field narrows the list as one types, every word must
// match, case and accents ignored; choices narrow it by a value its entries
// have. The bar counts what it shows, offers a reset, and keeps its state
// in the view's address, so a reload, the back button and a shared link
// keep it.
//
// filtered() filters a list loaded whole, in the browser. serverFilter()
// is the same bar for a list the server pages: it tells the view what to
// ask for, and the view asks the API.
import { h } from "./dom.js";
import { t } from "./i18n.js";

// A list shows its bar from this many entries, or while a filter applies.
export const filterFrom = 5;

// norm makes text comparable: lower case, accents off, ß as ss.
export function norm(text) {
  return String(text ?? "").normalize("NFD").replace(/\p{M}/gu, "").toLowerCase().replace(/ß/g, "ss");
}

// hasWords reports whether text holds every word typed.
export function hasWords(text, typed) {
  const hay = norm(text);
  return norm(typed).split(/\s+/).filter(Boolean).every((w) => hay.includes(w));
}

// textOf is what an entry shows to read: its text without that of its
// controls (a select's options, a button's label), or its data-filter-text.
function textOf(row) {
  if (row.dataset.filterText !== undefined) return row.dataset.filterText;
  const out = [];
  const walk = document.createTreeWalker(row, NodeFilter.SHOW_TEXT, {
    acceptNode: (n) => (n.parentElement.closest("select, button, script, style") ? NodeFilter.FILTER_REJECT : NodeFilter.FILTER_ACCEPT) });
  while (walk.nextNode()) out.push(walk.currentNode.nodeValue);
  // Words an entry does not show but is found by, e.g. one's own username
  // where it reads "you".
  if (row.dataset.filterExtra) out.push(row.dataset.filterExtra);
  for (const input of row.querySelectorAll("input:not([type]), input[type=text]")) out.push(input.value);
  return out.join(" ");
}

// The state lives in the address's query, after the route: id holds the
// text, id.key each choice.
function query() {
  const at = location.hash.indexOf("?");
  return new URLSearchParams(at < 0 ? "" : location.hash.slice(at + 1));
}

function remember(id, state) {
  const [route, query = ""] = location.hash.split("?");
  // The address's other parts stay exactly as written (e.g. "?edit").
  const ours = (part) => {
    const k = decodeURIComponent(part.split("=")[0]);
    return k === id || k.startsWith(id + ".");
  };
  const parts = query.split("&").filter((p) => p && !ours(p));
  const enc = encodeURIComponent;
  if (state.text) parts.push(`${enc(id)}=${enc(state.text)}`);
  for (const [k, v] of Object.entries(state.choices)) if (v) parts.push(`${enc(id + "." + k)}=${enc(v)}`);
  const next = (route || "#/") + (parts.length ? "?" + parts.join("&") : "");
  if (next !== (location.hash || "#/")) history.replaceState(history.state, "", next);
}

function recalled(id, choices) {
  const q = query();
  return { text: q.get(id) || "", choices: Object.fromEntries(choices.map((c) => [c.key, q.get(`${id}.${c.key}`) || ""])) };
}

// bar draws the fields: label names the list, for screen readers.
function bar(id, label, choices, state, changed) {
  const text = h("input", { type: "search", class: "filter-text", id: id + "-filter", value: state.text,
    placeholder: t("filter.text"), "aria-label": t("filter.of", { list: label }), autocomplete: "off" });
  text.addEventListener("input", () => { state.text = text.value; changed(); });
  const selects = choices.map((c) => {
    const el = h("select", { class: "filter-choice", "data-choice": c.key, "aria-label": t("filter.of", { list: c.label }), onchange: () => { state.choices[c.key] = el.value; changed(); } },
      h("option", { value: "", text: `${c.label}: ${t("filter.all")}` }),
      c.options.map(([v, name]) => h("option", { value: v, text: name, selected: v === state.choices[c.key] })));
    return el;
  });
  const count = h("span", { class: "filter-count muted", role: "status", "aria-live": "polite" });
  const reset = h("button", { type: "button", class: "link filter-reset", text: t("filter.reset"), hidden: true });
  const el = h("div", { class: "filter-bar", id: id + "-filterbar", role: "search", "aria-label": t("filter.of", { list: label }) }, text, ...selects, count, reset);
  return { el, text, selects, count, reset };
}

const active = (state) => Boolean(state.text.trim() || Object.values(state.choices).some(Boolean));

// filtered puts a filter bar above a list loaded whole and returns both,
// with the message shown when nothing matches. rows() are the list's
// entries (by default its items, or a table's body rows); an entry's text
// is its data-filter-text or its text, and its value for a choice is its
// data-f-<key>, several separated by "|". A choice without options offers
// the values its entries have. onApply runs after each filtering, e.g. to
// hide the headings of groups left empty.
export function filtered(list, { id, label, choices = [], rows, min = filterFrom, onApply }) {
  // A table's entries are its rows below the heading.
  const entries = rows || (() => list.tagName === "TABLE" ? [...list.rows].filter((r) => !r.querySelector("th") && !r.closest("thead")) : [...list.children]);
  const valuesOf = (row, key) => (row.dataset["f" + key[0].toUpperCase() + key.slice(1)] || "").split("|").filter(Boolean);
  const all = entries();
  const opts = choices.map((c) => ({ ...c, options: c.options || [...new Set(all.flatMap((r) => valuesOf(r, c.key)))]
    .sort((a, b) => a.localeCompare(b)).map((v) => [v, c.name ? c.name(v) : v]) }));
  const state = recalled(id, opts);
  const empty = h("p", { class: "empty filter-empty", id: id + "-filter-empty", hidden: true },
    t("filter.none"), " ", h("button", { type: "button", class: "link", text: t("filter.reset"), onclick: () => clear() }));
  let b = null;
  const apply = () => {
    const rowsNow = entries();
    let n = 0;
    for (const row of rowsNow) {
      const show = hasWords(textOf(row), state.text)
        && opts.every((c) => !state.choices[c.key] || valuesOf(row, c.key).includes(state.choices[c.key]));
      row.hidden = !show;
      if (show) n++;
    }
    const on = active(state);
    b.el.hidden = !on && rowsNow.length < min;
    b.count.textContent = on ? t("filter.count", { n, total: rowsNow.length }) : "";
    b.reset.hidden = !on;
    empty.hidden = !on || n > 0;
    list.hidden = on && n === 0;
    onApply?.();
    remember(id, state);
  };
  b = bar(id, label, opts, state, apply);
  const clear = () => {
    state.text = "";
    b.text.value = "";
    for (const s of b.selects) { s.value = ""; state.choices[s.dataset.choice] = ""; }
    apply();
    b.text.focus();
  };
  b.reset.addEventListener("click", clear);
  apply();
  const box = h("div", { class: "filtered", id: id + "-filtered" }, b.el, list, empty);
  box.applyFilter = apply;
  return box;
}

// serverFilter is the bar of a list the server pages. load(state) asks
// the API anew and answers how many entries it shows; state.text and
// state.choices name what to ask for. The bar shows from the start: what a
// page holds says nothing of what the whole list holds.
export function serverFilter({ id, label, choices = [], load }) {
  const state = recalled(id, choices);
  let timer = 0;
  let asked = 0;
  let b = null;
  const run = async () => {
    const mine = ++asked;
    remember(id, state);
    const n = await load(state);
    if (mine !== asked) return;
    const on = active(state);
    b.count.textContent = on ? t("filter.shown", { n }) : "";
    b.reset.hidden = !on;
  };
  b = bar(id, label, choices, state, () => {
    clearTimeout(timer);
    timer = setTimeout(run, 250);
  });
  b.reset.addEventListener("click", () => {
    state.text = "";
    b.text.value = "";
    for (const s of b.selects) { s.value = ""; state.choices[s.dataset.choice] = ""; }
    run();
  });
  return { el: b.el, state, run, active: () => active(state) };
}

// filterParams adds a server filter's state to an API query: the text as
// q, each choice under its own name.
export function filterParams(params, state) {
  if (state.text.trim()) params.set("q", state.text.trim());
  for (const [k, v] of Object.entries(state.choices)) if (v) params.set(k, v);
  return params;
}
