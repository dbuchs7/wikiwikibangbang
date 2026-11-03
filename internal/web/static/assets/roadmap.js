// The roadmap and the Gantt chart (ADR-0164): readable items with date
// fields on a time axis. The roadmap lays them out in lanes by label, the
// Gantt chart gives each item a row of its own. Nothing is kept: the view
// is computed in the browser from the item listing, as the board of a
// label is (ADR-0087), and its whole state is in the address, so a link
// shows the same view to anyone who may read the same items (D10). The
// layout is timeline.js; this module only draws it. Bars and milestones
// are links to their items; colours are the look's tokens (style.css), and
// shape and text, not colour alone, tell a milestone and a finished item.
import { h } from "./dom.js";
import { t, language } from "./i18n.js";
import { get } from "./api.js";
import { finished } from "./taskparts.js";
import { MODES, NO_LABEL, SCALES, address, autoScale, dateFields, frame, ganttRows, guessFields, lanes, normalLabel, place, readState,
  schedule, span, suggest, todayDay } from "./timeline.js";

// unitWidth is how many pixels a unit of each scale takes at least, so a
// long frame scrolls rather than squeezing its bars.
const unitWidth = { week: 72, month: 96, quarter: 132 };

// The names of the modes and scales, with their keys written out so the
// catalogue's tests find them (ADR-0017).
const modeName = (m) => ({ roadmap: t("roadmap.modeRoadmap"), gantt: t("roadmap.modeGantt") })[m];
const scaleName = (s) => ({ "": t("roadmap.scaleAuto"), week: t("roadmap.scaleWeek"), month: t("roadmap.scaleMonth"), quarter: t("roadmap.scaleQuarter") })[s];

// dateText writes a day number as a date in the UI language.
const dateText = (day) => new Date(day * 86400000).toLocaleDateString(language() || undefined, { timeZone: "UTC", day: "numeric", month: "short", year: "numeric" });

// tickText is the caption of a unit of the axis.
function tickText(tick, scale, first) {
  if (scale === "week") return t("roadmap.week", { n: tick.week });
  if (scale === "quarter") return t("roadmap.quarter", { q: tick.quarter, year: tick.year });
  const opts = { timeZone: "UTC", month: "short" };
  if (first || tick.month === 0) opts.year = "numeric";
  return new Date(tick.day * 86400000).toLocaleDateString(language() || undefined, opts);
}

const field = (label, input) => h("label", { class: "field" }, h("span", { text: label }), input);

// at sets where an element stands in its track, in percent.
function at(el, left, width) {
  el.style.left = left + "%";
  if (width !== undefined) el.style.width = width + "%";
  return el;
}

// barTitle tells everything about a bar in words: its item, its days, its
// state and whether it waits on others; it is the bar's accessible name.
function barTitle(bar, fields) {
  const it = bar.item;
  const when = bar.milestone ? dateText(bar.from) : t("roadmap.fromTo", { from: dateText(bar.from), to: dateText(bar.to) });
  const parts = [`${it.key} ${it.title}`, when];
  if (it.state) parts.push(it.state);
  if (bar.inverted) parts.push(t("roadmap.inverted", { start: fields.start, end: fields.end }));
  if (it.waitingOn) parts.push(t("roadmap.waitingOn", { n: it.waitingOn }));
  return parts.join(" · ");
}

// barEl draws a bar or a milestone as a link to its item.
function barEl(bar, fr, fields, done, withText) {
  const it = bar.item;
  const cls = ["tl-bar", bar.milestone ? "tl-milestone" : "", done ? "tl-done" : "", bar.inverted ? "tl-inverted" : "", it.waitingOn ? "tl-waiting" : ""].filter(Boolean).join(" ");
  const title = barTitle(bar, fields);
  const el = h("a", { class: cls, href: "#/item/" + encodeURIComponent(it.key), title, "aria-label": title, "data-key": it.key },
    withText ? h("span", { class: "tl-bar-text", text: bar.milestone ? it.key : `${it.key} ${it.title}` }) : null);
  const p = place(fr, bar.from, bar.to);
  if (bar.milestone) return at(el, p.left + p.width / 2);
  return at(el, p.left, p.width);
}

// grid draws the lines of the units and of today over the tracks.
function grid(fr) {
  const layer = h("div", { class: "tl-grid", "aria-hidden": "true" },
    fr.ticks.map((tick) => at(h("span", { class: "tl-gridline" }), place(fr, tick.day, tick.day).left)));
  if (fr.today !== null) layer.append(at(h("span", { class: "tl-today", title: t("roadmap.today") }), place(fr, fr.today, fr.today).left + place(fr, fr.today, fr.today).width / 2));
  return layer;
}

function axis(fr) {
  return h("div", { class: "tl-row tl-axis-row" },
    h("div", { class: "tl-head tl-corner" }),
    h("div", { class: "tl-track tl-axis" }, fr.ticks.map((tick, i) => {
      const p = place(fr, tick.day, tick.end - 1);
      return at(h("span", { class: "tl-tick", text: tickText(tick, fr.scale, i === 0) }), p.left, p.width);
    })));
}

// chart draws the frame with its rows: lanes of packed bars for a roadmap,
// a row per item for a Gantt chart.
function chart(state, bars, fields, typeOf) {
  const s = span(bars);
  const scale = state.scale || autoScale(s);
  const fr = frame(s, scale, todayDay());
  const done = (it) => finished(typeOf(it)?.workflow, it.state);
  const body = h("div", { class: "tl-body" });
  if (state.mode === "gantt") {
    for (const bar of ganttRows(bars)) {
      body.append(h("div", { class: "tl-row" },
        h("div", { class: "tl-head" }, h("a", { href: "#/item/" + encodeURIComponent(bar.item.key), class: "key", text: bar.item.key }), " ",
          h("span", { class: "tl-head-title", text: bar.item.title })),
        h("div", { class: "tl-track" }, barEl(bar, fr, fields, done(bar.item), false))));
    }
  } else {
    for (const lane of lanes(bars, state.labels)) {
      const rows = lane.rows.length ? lane.rows : [[]];
      const group = h("div", { class: "tl-lane", "data-label": lane.label });
      rows.forEach((row, i) => group.append(h("div", { class: "tl-row" },
        h("div", { class: "tl-head" }, i === 0 ? (lane.label === NO_LABEL ? h("em", { text: t("roadmap.noLabel") }) : h("span", { class: "label", text: lane.label })) : null),
        h("div", { class: "tl-track" }, row.map((bar) => barEl(bar, fr, fields, done(bar.item), true))))));
      body.append(group);
    }
  }
  body.append(grid(fr));
  const el = h("div", { class: "tl " + (state.mode === "gantt" ? "tl-gantt" : "tl-roadmap"), role: "region", "aria-label": modeName(state.mode) }, axis(fr), body);
  el.style.minWidth = `calc(var(--tl-head) + ${fr.ticks.length * unitWidth[scale]}px)`;
  return h("div", { class: "tl-scroll", id: "roadmap-chart" }, el);
}

// readItems reads the items the view shows: those with any of its labels,
// or all it narrows to without a label; each item once.
async function readItems(state, readAll) {
  const base = "/api/items?limit=100" + (state.space ? "&space=" + encodeURIComponent(state.space) : "") + (state.type ? "&type=" + encodeURIComponent(state.type) : "");
  const reads = state.labels.length ? state.labels.map((l) => base + "&label=" + encodeURIComponent(l)) : [base];
  const found = await Promise.all(reads.map((url) => readAll(url)));
  const seen = new Map();
  for (const f of found) for (const it of f.items) seen.set(it.id, it);
  return { items: [...seen.values()], truncated: found.some((f) => f.truncated) };
}

// roadmapView mounts the view into container. helpers are what app.js
// shares: readAll reads a listing up to the boards' limit, and
// favouritesFirst orders the spaces as every choice of a space does.
export async function roadmapView(container, params, { readAll, favouritesFirst }) {
  const state = readState(params);
  const [{ types }, { spaces }, { labels }] = await Promise.all([get("/api/types"), get("/api/spaces"),
    get("/api/labels" + (state.space ? "?space=" + encodeURIComponent(state.space) : "")).catch(() => ({ labels: [] }))]);
  const dated = types.filter((ty) => dateFields([ty]).length);
  const typeOf = (it) => types.find((ty) => ty.name === it.type);
  const go = (change) => { location.hash = address({ ...state, ...change }); };

  const { items, truncated } = await readItems(state, readAll);
  // Only items whose type has a date field belong on a time axis; others
  // (pages without dates) are not even counted as unscheduled.
  const shown = items.filter((it) => dated.some((ty) => ty.name === it.type));
  const fields = dateFields(state.type ? dated.filter((ty) => ty.name === state.type) : dated.filter((ty) => shown.some((it) => it.type === ty.name)));
  const guessed = guessFields(fields);
  const chosen = { start: fields.includes(state.start) ? state.start : guessed.start, end: fields.includes(state.end) ? state.end : guessed.end };
  const { bars, unscheduled } = schedule(shown.filter((it) => {
    const ty = typeOf(it);
    return (ty.fields || []).some((f) => f.kind === "date" && (f.name === chosen.start || f.name === chosen.end));
  }), chosen);

  const select = (id, label, options, value, key) => field(label, h("select", { id, onchange: (ev) => go({ [key]: ev.target.value }) },
    options.map(([v, text]) => h("option", { value: v, text, selected: v === value }))));
  // Labels are chosen by typing: the labels in use are suggested as one
  // types, the most used first, from the server so every label is found,
  // not only the most used page; the chosen ones stand as chips in their
  // order, and the most used are offered to pick at a click.
  const addLabel = (value) => {
    const l = normalLabel(value);
    if (l && !state.labels.includes(l)) go({ labels: [...state.labels, l] });
  };
  const options = h("datalist", { id: "roadmap-label-options" });
  const input = h("input", { id: "roadmap-label", type: "search", autocomplete: "off", list: "roadmap-label-options", placeholder: t("roadmap.labelSearch"), "aria-label": t("roadmap.labelSearch") });
  const known = new Map(labels.map((l) => [l.label, l.count]));
  const offer = (list) => options.replaceChildren(...list.map((l) => h("option", { value: l, label: known.has(l) ? t("label.suggestion", { n: known.get(l) }) : null })));
  let timer = 0;
  input.addEventListener("input", (ev) => {
    clearTimeout(timer);
    // Picking a suggestion replaces the text at once: that is a choice.
    if (!ev.inputType || ev.inputType === "insertReplacementText") {
      if (known.has(input.value) || [...options.options].some((o) => o.value === input.value)) return addLabel(input.value);
    }
    const typed = input.value;
    offer(suggest(labels, typed, state.labels));
    if (!normalLabel(typed)) return;
    timer = setTimeout(async () => {
      const found = await get("/api/labels?q=" + encodeURIComponent(normalLabel(typed)) + (state.space ? "&space=" + encodeURIComponent(state.space) : "")).catch(() => ({ labels: [] }));
      for (const l of found.labels) known.set(l.label, l.count);
      if (input.value === typed) offer(suggest([...found.labels, ...labels], typed, state.labels));
    }, 150);
  });
  input.addEventListener("keydown", (ev) => {
    if (ev.key !== "Enter") return;
    ev.preventDefault();
    addLabel(input.value);
  });
  offer(suggest(labels, "", state.labels));
  const chipsOf = state.labels.map((l) => h("span", { class: "label tl-chosen", "data-label": l }, l,
    h("button", { type: "button", class: "label-remove", title: t("label.remove", { label: l }), "aria-label": t("label.remove", { label: l }), text: "\u00d7",
      onclick: () => go({ labels: state.labels.filter((x) => x !== l) }) })));
  const popular = suggest(labels, "", state.labels, 8);
  const labelChoice = h("div", { class: "tl-label-choice", id: "roadmap-labels" },
    field(t("roadmap.labels"), h("span", { class: "tl-label-input" }, input, options,
      h("button", { type: "button", class: "secondary", id: "roadmap-label-add", text: t("roadmap.labelAdd"), onclick: () => addLabel(input.value) }))),
    state.labels.length ? h("p", { class: "tl-chosen-list" }, chipsOf,
      h("button", { type: "button", class: "link", id: "roadmap-labels-clear", text: t("roadmap.labelsClear"), onclick: () => go({ labels: [] }) })) : null,
    popular.length ? h("p", { class: "muted tl-popular" }, t("roadmap.labelsPopular") + " ",
      popular.map((l) => h("button", { type: "button", class: "chip tl-pick", "data-label": l, text: l, onclick: () => addLabel(l) }))) : null,
    h("p", { class: "muted", text: t("roadmap.labelsHint") }));
  const controls = h("div", { class: "card tl-controls", id: "roadmap-controls" },
    h("nav", { class: "tabs", "aria-label": t("roadmap.mode") }, MODES.map((m) =>
      h("a", { href: address({ ...state, mode: m }), class: m === state.mode ? "tab active" : "tab", id: "roadmap-mode-" + m, text: modeName(m) }))),
    h("div", { class: "form-row" },
      select("roadmap-space", t("col.space"), [["", t("roadmap.allSpaces")], ...favouritesFirst(spaces).map((sp) => [sp.key, `${sp.key} ${sp.name}`])], state.space, "space"),
      select("roadmap-type", t("col.type"), [["", t("roadmap.allTypes")], ...dated.map((ty) => [ty.name, ty.name])], state.type, "type"),
      select("roadmap-start", t("roadmap.startField"), [["", t("roadmap.noField")], ...fields.map((f) => [f, f])], chosen.start, "start"),
      select("roadmap-end", t("roadmap.endField"), [["", t("roadmap.noField")], ...fields.map((f) => [f, f])], chosen.end, "end"),
      select("roadmap-scale", t("roadmap.scale"), SCALES.reduce((o, s) => [...o, [s, scaleName(s)]], [["", scaleName("")]]), state.scale, "scale")),
    labelChoice);

  const unscheduledList = unscheduled.length ? h("details", { class: "tl-unscheduled", id: "roadmap-unscheduled" },
    h("summary", { text: t("roadmap.unscheduled", { n: unscheduled.length }) }),
    h("ul", {}, unscheduled.map((it) => h("li", {}, h("a", { href: "#/item/" + encodeURIComponent(it.key), class: "key", text: it.key }), " " + it.title)))) : null;

  let content;
  if (!fields.length) content = h("p", { class: "empty", id: "roadmap-empty", text: t("roadmap.noDateFields") });
  else if (!bars.length) content = h("p", { class: "empty", id: "roadmap-empty", text: t("roadmap.none") });
  else content = chart(state, bars, chosen, typeOf);

  container.replaceChildren(...[
    controls,
    truncated ? h("p", { class: "notice warning", text: t("board.truncated", { n: items.length }) }) : null,
    content,
    bars.length ? h("p", { class: "muted tl-legend", text: t("roadmap.legend") }) : null,
    unscheduledList,
  ].filter(Boolean));
}
