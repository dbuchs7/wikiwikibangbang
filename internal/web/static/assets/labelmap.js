// The labels' starmap (ADR-0165): every label of the items one may read as
// a star, as big as the number of items that carry it, with lines between
// labels that items carry together, as thick as the number of such items.
// The canvas grows with the number of labels shown: a few make a small
// map, many a big one. Filters choose labels (with or without the labels
// connected to them), find labels by a word and hide rarely used ones;
// the whole state is in the address. The counts come from GET /api/labels
// with pairs, which counts only what the reader may read (D10); nothing is
// stored. The layout is labelmap-graph.js; this module draws it as SVG
// without a library, in the look's tokens (style.css).
import { h, mount } from "./dom.js";
import { t } from "./i18n.js";
import { get } from "./api.js";
import { address, canvasHeight, frame, layout, lineWidth, neighbours, readState, shown, starRadius } from "./labelmap-graph.js";

const NS = "http://www.w3.org/2000/svg";

// s creates an SVG element with attributes; children are appended.
function s(tag, attrs = {}, ...children) {
  const el = document.createElementNS(NS, tag);
  for (const [k, v] of Object.entries(attrs)) if (v !== undefined && v !== null && v !== false) el.setAttribute(k, String(v));
  for (const c of children) if (c) el.append(c);
  return el;
}
const text = (value) => document.createTextNode(String(value));
const short = (value, n = 24) => (value.length > n ? value.slice(0, n - 1) + "…" : value);

// Above this many stars only the biggest, the chosen and the lit ones are
// captioned, so the map stays legible.
const CAPTIONED = 60;

// labelmapView mounts the labels' starmap into container, in the state
// the address's parameters hold. favouritesFirst orders the rooms as
// everywhere a room is chosen (ADR-0145).
export async function labelmapView(container, params, { favouritesFirst = (r) => r } = {}) {
  const state = readState(params);
  const { spaces } = await get("/api/spaces");
  const rooms = favouritesFirst(spaces);
  if (state.space && !rooms.some((r) => r.key === state.space)) state.space = "";
  const query = new URLSearchParams({ pairs: "on" });
  if (state.space) query.set("space", state.space);
  const data = await get("/api/labels?" + query);
  mount(container, drawMap(data, rooms, state));
}

function drawMap(data, rooms, state) {
  const labels = data.labels || [];
  const pairs = data.pairs || [];
  const known = new Map(labels.map((l) => [l.label, l.count]));
  const keep = () => history.replaceState(null, "", address(state));

  // --- the filters ---
  const room = h("select", { id: "labelmap-space", "aria-label": t("labelmap.space") },
    h("option", { value: "", text: t("labelmap.allSpaces") }),
    rooms.map((r) => h("option", { value: r.key, text: r.name ? `${r.key} – ${r.name}` : r.key })));
  room.value = state.space;
  room.addEventListener("change", () => { location.hash = address({ ...state, space: room.value }); });
  const search = h("input", { type: "search", id: "labelmap-search", class: "filter-text", value: state.q, placeholder: t("labelmap.search"), "aria-label": t("labelmap.search") });
  search.addEventListener("input", () => { state.q = search.value; keep(); render(); });
  const options = h("datalist", { id: "labelmap-options" }, labels.map((l) => h("option", { value: l.label })));
  const pick = h("input", { type: "text", id: "labelmap-pick", list: "labelmap-options", autocomplete: "off", placeholder: t("labelmap.pick"), "aria-label": t("labelmap.pick") });
  const choose = (label) => {
    if (!known.has(label) || state.labels.includes(label)) return false;
    state.labels = [...state.labels, label];
    keep(); render();
    return true;
  };
  const unchoose = (label) => {
    state.labels = state.labels.filter((l) => l !== label);
    keep(); render();
  };
  pick.addEventListener("change", () => { if (choose(pick.value.trim())) pick.value = ""; });
  pick.addEventListener("keydown", (ev) => {
    if (ev.key !== "Enter") return;
    ev.preventDefault();
    if (choose(pick.value.trim())) pick.value = "";
  });
  const chips = h("div", { class: "labelmap-chosen", id: "labelmap-chosen", "aria-live": "polite" });
  const near = h("input", { type: "checkbox", id: "labelmap-near", checked: state.near });
  near.addEventListener("change", () => { state.near = near.checked; keep(); render(); });
  const min = h("input", { type: "number", id: "labelmap-min", min: "1", step: "1", value: String(state.min), class: "labelmap-min" });
  min.addEventListener("change", () => {
    const v = Math.floor(Number(min.value));
    state.min = Number.isFinite(v) && v > 1 ? v : 1;
    min.value = String(state.min);
    keep(); render();
  });
  const reset = h("button", { type: "button", class: "link", id: "labelmap-reset", text: t("labelmap.reset"), onclick: () => {
    Object.assign(state, { labels: [], near: true, min: 1, q: "" });
    search.value = ""; near.checked = true; min.value = "1";
    keep(); render();
  } });
  const count = h("span", { class: "muted filter-count", id: "labelmap-count", "aria-live": "polite" });

  // --- the canvas, its view box moved by panning and zooming ---
  const svg = s("svg", { class: "labelmap-canvas", id: "labelmap-canvas", role: "group", "aria-label": t("labelmap.canvas"),
    "aria-describedby": "labelmap-legend", preserveAspectRatio: "xMidYMid meet" });
  let base = { x: 0, y: 0, width: 360, height: 240 };
  let vb = { ...base };
  const r1 = (v) => Math.round(v * 10) / 10;
  const applyBox = () => svg.setAttribute("viewBox", `${r1(vb.x)} ${r1(vb.y)} ${r1(vb.width)} ${r1(vb.height)}`);
  const zoomAt = (factor, cx = vb.x + vb.width / 2, cy = vb.y + vb.height / 2) => {
    const w = Math.min(base.width * 4, Math.max(base.width / 8, vb.width / factor));
    const k = w / vb.width;
    vb = { x: cx - (cx - vb.x) * k, y: cy - (cy - vb.y) * k, width: w, height: vb.height * k };
    applyBox();
  };
  const toWorld = (ev) => {
    const m = svg.getScreenCTM();
    if (!m) return { x: 0, y: 0 };
    const p = new DOMPoint(ev.clientX, ev.clientY).matrixTransform(m.inverse());
    return { x: p.x, y: p.y };
  };
  svg.addEventListener("wheel", (ev) => {
    ev.preventDefault();
    const p = toWorld(ev);
    zoomAt(ev.deltaY < 0 ? 1.15 : 1 / 1.15, p.x, p.y);
  }, { passive: false });
  let pan = null, panned = false;
  svg.addEventListener("pointerdown", (ev) => {
    if (ev.button !== 0 || ev.target.closest(".lm-star")) return;
    pan = { x: ev.clientX, y: ev.clientY, vb: { ...vb }, moved: false };
    svg.setPointerCapture(ev.pointerId);
  });
  svg.addEventListener("pointermove", (ev) => {
    if (!pan) return;
    const box = svg.getBoundingClientRect();
    const scale = Math.max(pan.vb.width / Math.max(box.width, 1), pan.vb.height / Math.max(box.height, 1));
    const dx = ev.clientX - pan.x, dy = ev.clientY - pan.y;
    if (Math.abs(dx) + Math.abs(dy) > 3) pan.moved = true;
    vb = { ...pan.vb, x: pan.vb.x - dx * scale, y: pan.vb.y - dy * scale };
    applyBox();
  });
  const endPan = () => { panned = Boolean(pan?.moved); pan = null; };
  svg.addEventListener("pointerup", endPan);
  svg.addEventListener("pointercancel", endPan);
  svg.addEventListener("click", (ev) => {
    if (!ev.target.closest(".lm-star") && !panned) choosePanel(null);
    panned = false;
  });
  const zoomButton = (label, cls, glyph, act) => h("button", { type: "button", class: "secondary " + cls, title: label, "aria-label": label, onclick: act }, glyph);
  const zoomIn = zoomButton(t("labelmap.zoomIn"), "zoom-in", "+", () => zoomAt(1.25));
  const zoomOut = zoomButton(t("labelmap.zoomOut"), "zoom-out", "−", () => zoomAt(1 / 1.25));
  const zoomFit = zoomButton(t("labelmap.zoomReset"), "zoom-fit", "⤢", () => { vb = { ...base }; applyBox(); });

  // --- the panel of a chosen star ---
  const panel = h("aside", { class: "starmap-panel card labelmap-panel", id: "labelmap-panel", hidden: true, tabindex: "-1", "aria-labelledby": "labelmap-panel-title" });
  let selected = null;
  function choosePanel(label) {
    const was = selected;
    selected = label && known.has(label) ? label : null;
    if (!selected) {
      const inPanel = panel.contains(document.activeElement);
      panel.hidden = true;
      markSelected();
      if (was && inPanel) svg.querySelector(`.lm-star[data-label="${CSS.escape(was)}"]`)?.focus({ preventScroll: true });
      return;
    }
    const n = known.get(selected);
    const chosen = state.labels.includes(selected);
    const linked = neighbours(pairs, selected);
    const enc = encodeURIComponent(selected);
    mount(panel,
      h("div", { class: "starmap-panel-head" }, h("h2", { id: "labelmap-panel-title", text: selected }),
        h("button", { type: "button", class: "link", "aria-label": t("common.close"), title: t("common.close"), onclick: () => choosePanel(null) }, "×")),
      h("p", { class: "muted", id: "labelmap-panel-count", text: t("labelmap.items", { n }) }),
      h("div", { class: "starmap-panel-actions" },
        h("a", { href: "#/label/" + enc, class: "button primary", id: "labelmap-open", text: t("labelmap.open") }),
        h("button", { type: "button", class: "secondary", id: "labelmap-filter", "aria-pressed": String(chosen),
          text: chosen ? t("labelmap.unfilter") : t("labelmap.filter"), onclick: () => { if (chosen) unchoose(selected); else choose(selected); choosePanel(selected); } }),
        h("a", { href: "#/labelboard/" + enc, class: "button secondary", text: t("labelboard.open") }),
        h("a", { href: "#/roadmap?labels=" + enc, class: "button secondary", text: t("roadmap.open") })),
      h("h3", { class: "labelmap-near-head", text: linked.length ? t("labelmap.connected", { n: linked.length }) : t("labelmap.noneConnected") }),
      linked.length ? h("ul", { class: "list labelmap-near", id: "labelmap-near-list" }, linked.map((m) => h("li", {},
        h("button", { type: "button", class: "link", "data-label": m.label, text: m.label, onclick: () => {
          if (!svg.querySelector(`.lm-star[data-label="${CSS.escape(m.label)}"]`)) choose(m.label);
          choosePanel(m.label);
        } }),
        h("span", { class: "muted", text: " " + t("labelmap.shared", { n: m.count }) })))) : null);
    panel.hidden = false;
    panel.scrollTop = 0;
    panel.focus({ preventScroll: true });
    markSelected();
  }
  function markSelected() {
    for (const el of svg.querySelectorAll(".lm-star")) el.classList.toggle("focused", el.dataset.label === selected);
  }

  // --- lighting a star's neighbourhood while it is pointed at ---
  let lines = [];
  function light(label) {
    for (const el of svg.querySelectorAll(".lit")) el.classList.remove("lit");
    svg.classList.toggle("hovering", Boolean(label));
    if (!label) return;
    const lit = new Set([label]);
    for (const { el, a, b } of lines) {
      if (a !== label && b !== label) continue;
      el.classList.add("lit");
      lit.add(a); lit.add(b);
    }
    for (const el of svg.querySelectorAll(".lm-star")) if (lit.has(el.dataset.label)) el.classList.add("lit");
  }

  // --- drawing what the filters leave ---
  const list = h("tbody");
  const listBox = h("details", { class: "labelmap-list", id: "labelmap-list" }, h("summary", { id: "labelmap-list-summary" }),
    h("table", {}, h("thead", {}, h("tr", {}, h("th", { text: t("labelmap.colLabel") }), h("th", { text: t("labelmap.colCount") }))), list));
  const none = h("p", { class: "empty", id: "labelmap-none", hidden: true, text: t("labelmap.none") });
  const stage = h("div", { class: "starmap-stage labelmap-stage" }, svg, panel);
  let laid = { key: null, at: null };
  let width = 0;
  let points = [];
  // reframe fits the view box to the stars in the canvas as it stands,
  // narrower while the panel stands beside it.
  function reframe() {
    const box = svg.getBoundingClientRect();
    const height = Number(svg.getAttribute("height")) || box.height || 240;
    const w = box.width || width || 800;
    base = frame(points, { aspect: Math.max(0.6, w / height) });
    vb = { ...base };
    applyBox();
  }
  function render() {
    const { labels: on, focus } = shown(labels, pairs, state);
    // The stars, the most carried first; the lines between shown labels.
    const stars = labels.filter((l) => on.has(l.label));
    const most = stars.reduce((m, l) => Math.max(m, l.count), 0);
    const links = pairs.filter((p) => on.has(p.labels[0]) && on.has(p.labels[1])).map((p) => ({ a: p.labels[0], b: p.labels[1], count: p.count }));
    const strongest = links.reduce((m, l) => Math.max(m, l.count), 0);
    const sized = stars.map((l) => ({ label: l.label, count: l.count, r: starRadius(l.count, most) }));
    // The canvas is as tall as the number of stars asks for; the stars
    // spread over its width.
    const height = canvasHeight(sized.length);
    svg.setAttribute("height", String(height));
    width = Math.round(stage.getBoundingClientRect().width) || width || 800;
    const aspect = Math.round((width / height) * 10) / 10;
    const key = aspect + "|" + sized.map((st) => st.label + ":" + st.r).join(",") + "|" + links.map((l) => `${l.a}+${l.b}:${l.count}`).join(",");
    if (key !== laid.key) laid = { key, at: layout(sized, links, { aspect }) };
    const at = laid.at;
    points = sized.map((st) => ({ ...at.get(st.label), r: st.r }));
    reframe();

    const captionAll = sized.length <= CAPTIONED;
    lines = links.map((l) => {
      const a = at.get(l.a), b = at.get(l.b);
      const el = s("line", { class: "lm-line", x1: a.x, y1: a.y, x2: b.x, y2: b.y, "stroke-width": lineWidth(l.count, strongest) },
        s("title", {}, text(t("labelmap.pair", { a: l.a, b: l.b, n: l.count }))));
      return { el, a: l.a, b: l.b };
    });
    const starEls = sized.map((st, i) => {
      const p = at.get(st.label);
      const name = t("labelmap.star", { label: st.label, n: st.count });
      const cls = ["lm-star", focus.has(st.label) ? "match" : "", captionAll || i < CAPTIONED || focus.has(st.label) ? "" : "minor"].filter(Boolean).join(" ");
      const g = s("g", { class: cls, transform: `translate(${p.x} ${p.y})`, tabindex: 0, role: "button", "aria-label": name, "data-label": st.label, "data-count": st.count },
        s("title", {}, text(name)),
        s("circle", { class: "lm-halo", r: r1(st.r * 1.7) }),
        s("circle", { class: "lm-core", r: st.r }),
        s("text", { class: "lm-caption", y: r1(st.r + 13), "text-anchor": "middle" }, text(`${short(st.label)} · ${st.count}`)));
      g.addEventListener("click", (ev) => { ev.stopPropagation(); choosePanel(st.label); });
      g.addEventListener("keydown", (ev) => { if (ev.key === "Enter" || ev.key === " ") { ev.preventDefault(); choosePanel(st.label); } });
      g.addEventListener("pointerenter", () => light(st.label));
      g.addEventListener("pointerleave", () => light(null));
      g.addEventListener("focus", () => light(st.label));
      g.addEventListener("blur", () => light(null));
      return g;
    });
    mount(svg, s("g", { class: "lm-lines" }, ...lines.map((l) => l.el)), s("g", { class: "lm-stars" }, ...starEls));
    svg.setAttribute("data-stars", String(sized.length));
    svg.classList.remove("hovering");
    markSelected();
    if (selected && !on.has(selected)) choosePanel(null);

    stage.hidden = !sized.length;
    none.hidden = Boolean(sized.length);
    count.textContent = t("labelmap.count", { n: sized.length, total: labels.length });
    mount(chips, state.labels.map((l) => h("span", { class: "chip labelmap-chip", "data-label": l }, l,
      h("button", { type: "button", class: "link", "aria-label": t("labelmap.remove", { label: l }), title: t("labelmap.remove", { label: l }), onclick: () => unchoose(l) }, "×"))));
    reset.hidden = !state.labels.length && !state.q && state.min === 1 && state.near;
    listBox.querySelector("summary").textContent = t("labelmap.list", { n: sized.length });
    mount(list, stars.map((l) => h("tr", {}, h("td", {}, h("a", { href: "#/label/" + encodeURIComponent(l.label), text: l.label })), h("td", { text: String(l.count) }))));
  }

  const onKey = (ev) => {
    if (!root.isConnected) return document.removeEventListener("keydown", onKey);
    if (ev.key === "Escape" && selected) choosePanel(null);
  };
  document.addEventListener("keydown", onKey);
  if (typeof ResizeObserver === "function") {
    // A new width of the page lays the stars out again; the panel
    // opening beside the canvas only fits the view box to it.
    const observer = new ResizeObserver(() => {
      if (!root.isConnected) return observer.disconnect();
      const w = Math.round(stage.getBoundingClientRect().width);
      if (w && Math.abs(w - width) > 8) render(); else reframe();
    });
    requestAnimationFrame(() => { if (root.isConnected) { observer.observe(stage); observer.observe(svg); } });
  }

  const legend = h("section", { class: "starmap-legend", id: "labelmap-legend", "aria-label": t("labelmap.legend") },
    h("h2", { text: t("labelmap.legend") }),
    h("p", { class: "muted", text: t("labelmap.help") }));
  const truncated = data.total && data.total.value > labels.length
    ? h("p", { class: "notice", id: "labelmap-truncated", role: "status", text: t("labelmap.truncated", { n: labels.length, total: data.total.value }) }) : null;
  const pairsCut = data.pairsTotal && data.pairsTotal.value > pairs.length
    ? h("p", { class: "muted", id: "labelmap-pairs-truncated", role: "status", text: t("labelmap.pairsTruncated", { n: pairs.length }) }) : null;
  const root = h("div", { class: "labelmap" });
  if (!labels.length) {
    mount(root, h("div", { class: "filter-bar starmap-tools" }, room), h("p", { class: "empty", id: "labelmap-empty", text: t("labelmap.empty") }));
    return root;
  }
  mount(root,
    h("div", { class: "filter-bar starmap-tools", role: "search", "aria-label": t("labelmap.title") }, search, room, zoomIn, zoomOut, zoomFit, count),
    h("div", { class: "labelmap-filters" },
      h("label", { class: "labelmap-field" }, h("span", { text: t("labelmap.pickLabel") }), pick), options,
      h("label", { class: "starmap-check" }, near, h("span", { text: t("labelmap.near") })),
      h("label", { class: "labelmap-field" }, h("span", { text: t("labelmap.min") }), min),
      reset),
    chips, truncated, pairsCut, none, stage, listBox, legend);
  // The first drawing waits until the view is on the page, so the
  // canvas's width is known.
  queueMicrotask(render);
  render();
  return root;
}
