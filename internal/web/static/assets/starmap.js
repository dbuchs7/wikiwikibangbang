// The starmap of a room (ADR-0157): its items, pictures and shared labels
// as a graph, drawn as SVG without a library. The server derives the
// graph per request from what the reader may read; this module lays it
// out (starmap-graph.js), filters it, and lets one pan, zoom and focus a
// node. Shapes and outlines tell the kinds apart, not colour alone; the
// colours are the look's tokens (style.css), so it reads in the light,
// dark and cockpit looks alike.
import { h, mount } from "./dom.js";
import { t } from "./i18n.js";
import { get } from "./api.js";
import { GROUPS, filter, groupOf, kinds, kindOf, layout, neighbourhood, size, toneOf } from "./starmap-graph.js";

const NS = "http://www.w3.org/2000/svg";

// s creates an SVG element with attributes; children are appended.
function s(tag, attrs = {}, ...children) {
  const el = document.createElementNS(NS, tag);
  for (const [k, v] of Object.entries(attrs)) if (v !== undefined && v !== null && v !== false) el.setAttribute(k, String(v));
  for (const c of children) if (c) el.append(c);
  return el;
}

const text = (value) => document.createTextNode(String(value));
const short = (value, n = 22) => (value.length > n ? value.slice(0, n - 1) + "…" : value);

// shape draws a node's mark: a circle for an item, a square for a
// picture, a diamond for a label.
function shape(kind) {
  if (kind === "picture") return s("rect", { x: -9, y: -9, width: 18, height: 18, rx: 3, class: "star-shape" });
  if (kind === "label") return s("polygon", { points: "0,-11 11,0 0,11 -11,0", class: "star-shape" });
  return s("circle", { r: 11, class: "star-shape" });
}

const edgeKinds = ["parent", "relation", "mentions", "embeds", "picture", "copy", "label"];
// The names of the edge groups, edge kinds and node kinds, with their keys
// written out so the catalogue's tests find them (ADR-0017).
const groupName = (g) => ({ tree: t("starmap.groupTree"), relations: t("starmap.groupRelations"), pictures: t("starmap.groupPictures"), labels: t("starmap.groupLabels") })[g];
const edgeName = (k) => ({ parent: t("starmap.edgeParent"), relation: t("starmap.edgeRelation"), mentions: t("starmap.edgeMentions"), embeds: t("starmap.edgeEmbeds"),
  picture: t("starmap.edgePicture"), copy: t("starmap.edgeCopy"), label: t("starmap.edgeLabel") })[k];
const kindName = (k) => (k.type !== undefined ? k.type : { picture: t("starmap.kindPicture"), label: t("starmap.kindLabel"), outside: t("starmap.kindOutside") }[k.key]);

function nodeName(n) {
  let name;
  if (n.kind === "picture") name = t("starmap.nodePicture", { name: n.name });
  else if (n.kind === "label") name = t("starmap.nodeLabel", { name: n.name });
  else name = t("starmap.nodeItem", { key: n.key, title: n.title, type: n.type });
  return n.outside ? name + ", " + t("starmap.outside", { space: n.space }) : name;
}

// starmapView mounts the starmap of a room into container; space is the
// room's key from the address, or empty for the first of one's rooms,
// favourites first as everywhere a room is chosen (ADR-0145).
export async function starmapView(container, space) {
  const { spaces } = await get("/api/spaces");
  const rooms = [...spaces.filter((r) => r.favourite), ...spaces.filter((r) => !r.favourite)];
  if (!rooms.length) return mount(container, h("p", { class: "empty", text: t("starmap.noSpaces") }));
  const key = rooms.some((r) => r.key === space) ? space : rooms[0].key;
  const select = h("select", { "aria-label": t("starmap.space"), id: "starmap-space" },
    rooms.map((r) => h("option", { value: r.key, text: r.name ? `${r.key} – ${r.name}` : r.key })));
  select.value = key;
  select.addEventListener("change", () => { location.hash = "#/starmap/" + encodeURIComponent(select.value); });
  const data = await get(`/api/spaces/${encodeURIComponent(key)}/starmap`);
  mount(container, drawMap(data, key, select));
}

function drawMap(data, space, select) {
  const graph = { nodes: data.nodes, edges: data.edges };
  const at = layout(graph);
  // The canvas shows what the layout filled, with room for the captions.
  const xs = [...at.values()].map((p) => p.x), ys = [...at.values()].map((p) => p.y);
  const pad = 60;
  const box = xs.length ? { x: Math.min(...xs) - pad, y: Math.min(...ys) - pad, w: Math.max(...xs) - Math.min(...xs) + 2 * pad, h: Math.max(...ys) - Math.min(...ys) + 2 * pad }
    : { x: 0, y: 0, w: size(0), h: size(0) };
  const tone = toneOf(graph.nodes);
  const byID = new Map(graph.nodes.map((n) => [n.id, n]));
  const hidden = new Set();
  let focused = null;

  // --- the canvas ---
  const markers = s("defs", {}, ...["arrow", "arrow-copy"].map((id) =>
    s("marker", { id: "starmap-" + id, viewBox: "0 0 10 10", refX: 22, refY: 5, markerWidth: 7, markerHeight: 7, orient: "auto-start-reverse", class: "star-marker " + id },
      s("path", { d: "M0,0L10,5L0,10z" }))));
  const edgeEls = graph.edges.map((e) => {
    const a = at.get(e.from), b = at.get(e.to);
    const directed = e.kind !== "label" && e.kind !== "picture";
    const line = s("line", { x1: a.x, y1: a.y, x2: b.x, y2: b.y, class: "star-edge edge-" + e.kind,
      "marker-end": directed ? `url(#starmap-${e.kind === "copy" ? "arrow-copy" : "arrow"})` : null },
      s("title", {}, text(e.type || edgeName(e.kind))));
    const g = s("g", { class: "star-link", "data-kind": e.kind }, line);
    if (e.kind === "relation" && e.type) {
      g.append(s("text", { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 - 3, class: "star-edge-label", "text-anchor": "middle" }, text(short(e.type, 18))));
    }
    return g;
  });
  const nodeEls = graph.nodes.map((n) => {
    const p = at.get(n.id);
    const cls = ["star-node", "kind-" + n.kind, n.kind === "item" ? "tone-" + tone(n.type) : "", n.outside ? "outside" : ""].join(" ");
    const caption = n.kind === "item" ? short(n.title || n.key) : short(n.name || "");
    const g = s("g", { class: cls, transform: `translate(${p.x} ${p.y})`, tabindex: 0, role: "button", "aria-label": nodeName(n), "data-id": n.id, "data-key": n.key || null },
      s("title", {}, text(nodeName(n))), shape(n.kind), s("text", { y: 24, "text-anchor": "middle", class: "star-caption" }, text(caption)));
    g.addEventListener("click", (ev) => { ev.stopPropagation(); focus(n.id); });
    g.addEventListener("keydown", (ev) => {
      if (ev.key === "Enter" || ev.key === " ") { ev.preventDefault(); focus(n.id); }
    });
    return g;
  });
  const view = s("g", { class: "star-view" }, s("g", { class: "star-edges" }, ...edgeEls), s("g", { class: "star-nodes" }, ...nodeEls));
  const svg = s("svg", { viewBox: `${box.x} ${box.y} ${box.w} ${box.h}`, class: "starmap-canvas", id: "starmap-canvas", role: "group",
    "aria-label": t("starmap.canvas", { space }), "aria-describedby": "starmap-legend" }, markers, view);

  // --- pan and zoom: one transform on the view, in the canvas's units ---
  let zoom = 1, panX = 0, panY = 0;
  const apply = () => view.setAttribute("transform", `translate(${panX} ${panY}) scale(${zoom})`);
  const toCanvas = (ev) => {
    const m = svg.getScreenCTM();
    if (!m) return { x: 0, y: 0 };
    const pt = new DOMPoint(ev.clientX, ev.clientY).matrixTransform(m.inverse());
    return { x: pt.x, y: pt.y };
  };
  const zoomAt = (factor, cx = box.x + box.w / 2, cy = box.y + box.h / 2) => {
    const next = Math.min(6, Math.max(0.2, zoom * factor));
    panX = cx - ((cx - panX) * next) / zoom;
    panY = cy - ((cy - panY) * next) / zoom;
    zoom = next;
    apply();
  };
  svg.addEventListener("wheel", (ev) => {
    ev.preventDefault();
    const p = toCanvas(ev);
    zoomAt(ev.deltaY < 0 ? 1.15 : 1 / 1.15, p.x, p.y);
  }, { passive: false });
  let drag = null;
  svg.addEventListener("pointerdown", (ev) => {
    if (ev.target.closest(".star-node")) return;
    drag = { start: toCanvas(ev), panX, panY, moved: false };
    svg.setPointerCapture(ev.pointerId);
  });
  svg.addEventListener("pointermove", (ev) => {
    if (!drag) return;
    const p = toCanvas(ev);
    panX = drag.panX + p.x - drag.start.x;
    panY = drag.panY + p.y - drag.start.y;
    drag.moved = drag.moved || Math.abs(p.x - drag.start.x) + Math.abs(p.y - drag.start.y) > 3;
    apply();
  });
  let dragged = false; // the click that ends a drag does not clear the focus
  const endDrag = () => { dragged = Boolean(drag?.moved); drag = null; };
  svg.addEventListener("pointerup", endDrag);
  svg.addEventListener("pointercancel", endDrag);
  const zoomButton = (label, cls, act) => h("button", { type: "button", class: "secondary " + cls, title: label, "aria-label": label, onclick: act }, cls === "zoom-in" ? "+" : cls === "zoom-out" ? "−" : "⤢");
  const zoomIn = zoomButton(t("starmap.zoomIn"), "zoom-in", () => zoomAt(1.25));
  const zoomOut = zoomButton(t("starmap.zoomOut"), "zoom-out", () => zoomAt(1 / 1.25));
  const zoomFit = zoomButton(t("starmap.zoomReset"), "zoom-fit", () => { zoom = 1; panX = panY = 0; apply(); });

  // --- the side panel of the focused node ---
  const panel = h("aside", { class: "starmap-panel card", id: "starmap-panel", hidden: true, "aria-live": "polite" });
  function showPanel(n) {
    const row = (label, value) => (value ? [h("dt", { text: label }), h("dd", { text: value })] : null);
    let link = null;
    if (n.kind === "item") link = h("a", { href: "#/item/" + encodeURIComponent(n.key), class: "button primary", id: "starmap-open", text: t("starmap.open") });
    else if (n.kind === "picture" && byID.get(n.item)?.key) link = h("a", { href: "#/item/" + encodeURIComponent(byID.get(n.item).key), class: "button primary", id: "starmap-open", text: t("starmap.openItem") });
    else if (n.kind === "label") link = h("a", { href: "#/label/" + encodeURIComponent(n.name), class: "button primary", id: "starmap-open", text: t("starmap.openLabel") });
    const owner = n.kind === "picture" ? byID.get(n.item) : null;
    mount(panel,
      h("div", { class: "starmap-panel-head" }, h("h2", { text: n.kind === "item" ? n.title || n.key : n.name }),
        h("button", { type: "button", class: "link", "aria-label": t("common.close"), title: t("common.close"), onclick: () => focus(null) }, "×")),
      h("dl", { class: "starmap-facts" },
        row(t("starmap.panelKind"), n.kind === "item" ? n.type : n.kind === "picture" ? t("starmap.kindPicture") : t("starmap.kindLabel")),
        row(t("starmap.panelKey"), n.key || owner?.key),
        row(t("starmap.panelTitle"), owner ? owner.title : null),
        row(t("starmap.panelState"), n.state),
        row(t("starmap.panelSpace"), n.space)),
      link);
    panel.hidden = false;
  }

  // --- filters ---
  const search = h("input", { type: "search", id: "starmap-search", class: "filter-text", placeholder: t("starmap.search"), "aria-label": t("starmap.search") });
  const count = h("span", { class: "muted filter-count", id: "starmap-count", "aria-live": "polite" });
  const check = (key, label) => {
    const input = h("input", { type: "checkbox", checked: true, value: key });
    input.addEventListener("change", () => { if (input.checked) hidden.delete(key); else hidden.add(key); render(); });
    return h("label", { class: "starmap-check", "data-filter": key }, input, h("span", { text: label }));
  };
  const kindBoxes = h("fieldset", { class: "starmap-filter", id: "starmap-kinds" }, h("legend", { text: t("starmap.kinds") }),
    kinds(graph.nodes).map((k) => check(k.key, kindName(k))));
  const groupBoxes = h("fieldset", { class: "starmap-filter", id: "starmap-groups" }, h("legend", { text: t("starmap.edges") }),
    Object.keys(GROUPS).filter((g) => graph.edges.some((e) => groupOf(e.kind) === g)).map((g) => check(g, groupName(g))));
  search.addEventListener("input", render);

  function render() {
    const shown = filter(graph, { query: search.value, hidden });
    const near = focused && shown.nodes.has(focused) ? neighbourhood(graph, focused, shown.edges) : null;
    const edgeOn = new Set(shown.edges);
    graph.nodes.forEach((n, i) => {
      const el = nodeEls[i];
      if (shown.nodes.has(n.id)) el.removeAttribute("hidden"); else el.setAttribute("hidden", "");
      el.classList.toggle("match", shown.matches.has(n.id));
      el.classList.toggle("focused", n.id === focused);
      el.classList.toggle("dim", Boolean(near) && !near.has(n.id));
    });
    graph.edges.forEach((e, i) => {
      const el = edgeEls[i];
      if (edgeOn.has(i)) el.removeAttribute("hidden"); else el.setAttribute("hidden", "");
      el.classList.toggle("dim", Boolean(near) && e.from !== focused && e.to !== focused);
    });
    count.textContent = t("starmap.count", { n: shown.nodes.size });
  }

  function focus(id) {
    focused = id;
    if (id) showPanel(byID.get(id)); else panel.hidden = true;
    render();
  }

  svg.addEventListener("click", () => { if (focused && !dragged) focus(null); dragged = false; });
  const root = h("div", { class: "starmap" });
  const onKey = (ev) => {
    if (!root.isConnected) return document.removeEventListener("keydown", onKey);
    if (ev.key === "Escape" && focused) focus(null);
  };
  document.addEventListener("keydown", onKey);

  // --- the legend, in words, each with its mark ---
  const swatch = (...marks) => s("svg", { viewBox: "-14 -14 28 28", class: "star-swatch", "aria-hidden": "true" }, ...marks);
  const lineSwatch = (kind) => s("svg", { viewBox: "0 0 40 10", class: "star-swatch wide", "aria-hidden": "true" },
    s("line", { x1: 2, y1: 5, x2: 38, y2: 5, class: "star-edge edge-" + kind }));
  const legend = h("section", { class: "starmap-legend", id: "starmap-legend", "aria-label": t("starmap.legend") },
    h("h2", { text: t("starmap.legend") }),
    h("ul", {},
      h("li", {}, swatch(s("g", { class: "star-node kind-item tone-0" }, shape("item"))), h("span", { text: t("starmap.legendItem") })),
      h("li", {}, swatch(s("g", { class: "star-node kind-picture" }, shape("picture"))), h("span", { text: t("starmap.legendPicture") })),
      h("li", {}, swatch(s("g", { class: "star-node kind-label" }, shape("label"))), h("span", { text: t("starmap.legendLabel") })),
      h("li", {}, swatch(s("g", { class: "star-node kind-item outside" }, shape("item"))), h("span", { text: t("starmap.legendOutside") })),
      edgeKinds.map((k) => h("li", {}, lineSwatch(k), h("span", { text: edgeName(k) })))),
    h("p", { class: "muted", text: t("starmap.help") }));

  const truncated = data.truncated ? h("p", { class: "notice", id: "starmap-truncated", role: "status", text: t("starmap.truncated") }) : null;
  const body = graph.nodes.length
    ? h("div", { class: "starmap-stage" }, svg, panel)
    : h("p", { class: "empty", id: "starmap-empty", text: t("starmap.empty") });
  mount(root,
    h("div", { class: "filter-bar starmap-tools", role: "search", "aria-label": t("starmap.title") }, search, select, zoomIn, zoomOut, zoomFit, count),
    h("div", { class: "starmap-filters" }, kindBoxes, groupBoxes),
    truncated, body, legend);
  render();
  return root;
}
