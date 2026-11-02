// The starmap of a room (ADR-0157, ADR-0158): its items, pictures and
// shared labels as a graph, drawn as SVG without a library. The server
// derives the graph per request from what the reader may read; this
// module seeds a layout (starmap-graph.js), lets it move under a live
// force simulation (starmap-sim.js) that cools to rest, filters it, and
// lets one pan, zoom, drag a node with its neighbours, light a node's
// neighbourhood by pointing at it, focus it, and separate it with its
// neighbourhood by a double-click. From a node's panel, an impact view
// (ADR-0159) shows what depends on it, or what it depends on, across
// rooms to a chosen depth, as the server walks it. Shapes and outlines tell the kinds
// apart, not colour alone; the colours are the look's tokens (style.css),
// so it reads in the light, dark and cockpit looks alike. A focused node's
// panel shows the page itself, read-only and as the reader may read it,
// clipped with "Read more", so one stays on the map (ADR-0157).
import { h, mount } from "./dom.js";
import { t } from "./i18n.js";
import { get } from "./api.js";
import { DEPTHS, GROUPS, IMPACT_DEPTHS, distanceClass, drillBack, drillInto, fitView, groupOf, impactAddress, impactGraph, impactList, impactParams, impactPath,
  impactSummary, kinds, layout, neighbourhood, size, toneOf, visible, zoomBand } from "./starmap-graph.js";
import { simulation } from "./starmap-sim.js";

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

// crumbName is how a station of the drill path is called: an item by its
// key, a picture or a label by its name.
const crumbName = (n) => (n.kind === "item" ? n.key : n.name);

// station is how a node stands in the address: an item by its key,
// another node by its id.
const station = (n) => (n.kind === "item" ? n.key : n.id);

const distanceName = (d) => (d === 0 ? t("starmap.impactStart") : t("starmap.impactDistance", { n: d }));

// impactReturn is the address of the map an impact view was opened from,
// so leaving it returns there; without one (after a reload), leaving
// goes to the drill path the impact view's address holds.
let impactReturn = null;

// starmapView mounts the starmap of a room into container; space is the
// room's key from the address, or empty for the first of one's rooms,
// favourites first as everywhere a room is chosen (ADR-0145). path is the
// drill path from the address (keys of items, ids of other nodes) and
// depth the reach of a separated neighbourhood. params are the address's
// parameters: depth, and impact with depth for an impact view of the
// last station of the path (ADR-0159).
// preview, given by the page that shows the map, draws an item's page
// read-only for the panel (the renderer embeds use, so protected content
// stays veiled); without it the panel shows the facts only.
export async function starmapView(container, space, path = [], params = new URLSearchParams(), { preview = null } = {}) {
  const depth = Number(params.get("depth")) || 1;
  const impact = path.length ? impactParams(params) : null;
  const { spaces } = await get("/api/spaces");
  const rooms = [...spaces.filter((r) => r.favourite), ...spaces.filter((r) => !r.favourite)];
  if (!rooms.length) return mount(container, h("p", { class: "empty", text: t("starmap.noSpaces") }));
  const key = rooms.some((r) => r.key === space) ? space : rooms[0].key;
  const select = h("select", { "aria-label": t("starmap.space"), id: "starmap-space" },
    rooms.map((r) => h("option", { value: r.key, text: r.name ? `${r.key} – ${r.name}` : r.key })));
  select.value = key;
  select.addEventListener("change", () => { location.hash = "#/starmap/" + encodeURIComponent(select.value); });
  if (impact && key === space) {
    const start = path[path.length - 1];
    const result = await get(impactPath(start, impact));
    const map = drawMap({ ...impactGraph(result), truncated: false }, key, select,
      { path: [], depth: 1, preview, impact: { ...impact, result, stations: path.slice(0, -1), start: result.start } });
    mount(container, map.root);
    map.start();
    return;
  }
  impactReturn = null;
  const data = await get(`/api/spaces/${encodeURIComponent(key)}/starmap`);
  const map = drawMap(data, key, select, { path: key === space ? path : [], depth, preview });
  mount(container, map.root);
  map.start();
}

function drawMap(data, space, select, { path, depth: askedDepth, impact = null, preview = null }) {
  const graph = { nodes: data.nodes, edges: data.edges };
  const n = graph.nodes.length;
  const tone = toneOf(graph.nodes);
  const byID = new Map(graph.nodes.map((node) => [node.id, node]));
  const index = new Map(graph.nodes.map((node, i) => [node.id, i]));
  const hidden = new Set();
  let focused = null;
  let hovered = null;
  let started = false;
  let depth = DEPTHS.includes(askedDepth) ? askedDepth : 1;
  // The drill path from the address, each station a key or an id; what
  // the map does not hold (any more) is left out.
  let trail = [];
  for (const seg of path) {
    const node = byID.get(seg) || graph.nodes.find((m) => m.kind === "item" && m.key === seg);
    if (node) trail = drillInto(trail, node.id);
  }
  const reduced = typeof matchMedia === "function" && matchMedia("(prefers-reduced-motion: reduce)").matches;

  // --- the simulation: it starts from the seeded layout and moves on ---
  const at = layout(graph);
  const mid = size(n) / 2;
  const sim = simulation({
    x0: graph.nodes.map((node) => at.get(node.id).x), y0: graph.nodes.map((node) => at.get(node.id).y),
    links: graph.edges.map((e) => [index.get(e.from), index.get(e.to)]), center: { x: mid, y: mid }, alpha: 0.6,
  });

  // --- the canvas ---
  const markers = s("defs", {}, ...["arrow", "arrow-copy"].map((id) =>
    s("marker", { id: "starmap-" + id, viewBox: "0 0 10 10", refX: 22, refY: 5, markerWidth: 7, markerHeight: 7, orient: "auto-start-reverse", class: "star-marker " + id },
      s("path", { d: "M0,0L10,5L0,10z" }))));
  const edgeLines = [], edgeLabels = [];
  const edgeEls = graph.edges.map((e, k) => {
    const directed = e.kind !== "label" && e.kind !== "picture";
    const line = s("line", { class: "star-edge edge-" + e.kind,
      "marker-end": directed ? `url(#starmap-${e.kind === "copy" ? "arrow-copy" : "arrow"})` : null },
      s("title", {}, text(e.type || edgeName(e.kind))));
    edgeLines[k] = line;
    const g = s("g", { class: "star-link" + (e.walk ? " walk" : ""), "data-kind": e.kind }, line);
    if (e.kind === "relation" && e.type) {
      edgeLabels[k] = s("text", { class: "star-edge-label", "text-anchor": "middle" }, text(short(e.type, 18)));
      g.append(edgeLabels[k]);
    }
    return g;
  });
  const nodeEls = graph.nodes.map((node, i) => {
    const cls = ["star-node", "kind-" + node.kind, node.kind === "item" ? "tone-" + tone(node.type) : "", node.outside ? "outside" : "",
      impact ? distanceClass(node.distance) : "", impact && node.id === impact.start ? "root impact-start" : ""].join(" ");
    const caption = node.kind === "item" ? short(node.title || node.key) : short(node.name || "");
    // In an impact view a ring grades the node by its distance, and its
    // name says the distance too.
    const name = impact ? nodeName(node) + ", " + distanceName(node.distance) : nodeName(node);
    const g = s("g", { class: cls, tabindex: 0, role: "button", "aria-label": name, "data-id": node.id, "data-key": node.key || null,
      "data-distance": impact ? node.distance : null },
    s("title", {}, text(name)), impact ? s("circle", { r: 17, class: "star-ring" }) : null, shape(node.kind),
    s("text", { y: 24, "text-anchor": "middle", class: "star-caption" }, text(caption)));
    g.addEventListener("click", (ev) => {
      ev.stopPropagation();
      if (suppressClick) { suppressClick = false; return; }
      focus(node.id);
    });
    g.addEventListener("dblclick", (ev) => { ev.preventDefault(); ev.stopPropagation(); if (!impact) separate(node.id); });
    g.addEventListener("keydown", (ev) => {
      if (ev.key === "Enter" && ev.shiftKey) { ev.preventDefault(); if (!impact) separate(node.id); }
      else if (ev.key === "Enter" || ev.key === " ") { ev.preventDefault(); focus(node.id); }
    });
    g.addEventListener("pointerenter", () => { if (!nodeDrag) light(node.id); });
    g.addEventListener("pointerleave", () => { if (!nodeDrag && hovered === node.id) light(document.activeElement?.closest?.(".star-node")?.dataset.id || null); });
    g.addEventListener("focus", () => light(node.id));
    g.addEventListener("blur", () => { if (hovered === node.id) light(null); });
    g.addEventListener("pointerdown", (ev) => grab(ev, i, g));
    return g;
  });
  const view = s("g", { class: "star-view" }, s("g", { class: "star-edges" }, ...edgeEls), s("g", { class: "star-nodes" }, ...nodeEls));
  const svg = s("svg", { viewBox: "0 0 800 560", class: "starmap-canvas zoom-mid", id: "starmap-canvas", role: "group",
    "aria-label": t("starmap.canvas", { space }), "aria-describedby": "starmap-legend", "data-settled": "false" }, markers, view);

  // draw writes the simulation's positions into the shown nodes and
  // edges: attributes of the elements there are, never new elements.
  const r1 = (v) => Math.round(v * 10) / 10;
  let shownEdges = [];
  function draw() {
    const { x, y } = sim;
    for (const i of sim.active) nodeEls[i].setAttribute("transform", `translate(${r1(x[i])} ${r1(y[i])})`);
    for (const k of shownEdges) {
      const e = graph.edges[k], a = index.get(e.from), b = index.get(e.to);
      const line = edgeLines[k];
      line.setAttribute("x1", r1(x[a])); line.setAttribute("y1", r1(y[a]));
      line.setAttribute("x2", r1(x[b])); line.setAttribute("y2", r1(y[b]));
      if (edgeLabels[k]) { edgeLabels[k].setAttribute("x", r1((x[a] + x[b]) / 2)); edgeLabels[k].setAttribute("y", r1((y[a] + y[b]) / 2 - 3)); }
    }
  }

  // --- the view: world to canvas by one transform, the canvas in pixels ---
  let W = 800, H = 560;
  let zoom = 1, panX = 0, panY = 0;
  let band = "mid";
  // follow: the view eases to frame what is shown until the reader takes
  // the view in hand (pans, zooms or drags); a drill, the fit button and
  // the first load set it again.
  let follow = true;
  const apply = () => {
    view.setAttribute("transform", `translate(${r1(panX)} ${r1(panY)}) scale(${Math.round(zoom * 1000) / 1000})`);
    const next = zoomBand(zoom);
    if (next !== band) { svg.classList.replace("zoom-" + band, "zoom-" + next); band = next; }
  };
  const frame = () => fitView(sim.active.map((i) => ({ x: sim.x[i], y: sim.y[i] })), W, H, { pad: 56, max: 1.6 });
  const toCanvas = (ev) => {
    const m = svg.getScreenCTM();
    if (!m) return { x: 0, y: 0 };
    const pt = new DOMPoint(ev.clientX, ev.clientY).matrixTransform(m.inverse());
    return { x: pt.x, y: pt.y };
  };
  const toWorld = (p) => ({ x: (p.x - panX) / zoom, y: (p.y - panY) / zoom });
  const zoomAt = (factor, cx = W / 2, cy = H / 2) => {
    const next = Math.min(6, Math.max(0.1, zoom * factor));
    panX = cx - ((cx - panX) * next) / zoom;
    panY = cy - ((cy - panY) * next) / zoom;
    zoom = next;
    follow = false;
    apply();
  };

  // --- the loop: a tick or two per frame while the simulation moves or
  // the view eases; it stops at rest and when the view is left ---
  let raf = 0, left = false;
  const alive = () => !left && root.isConnected;
  function wake() {
    if (left) return;
    svg.setAttribute("data-settled", "false");
    if (!raf) raf = requestAnimationFrame(step);
  }
  function step() {
    raf = 0;
    if (!alive()) return leave();
    let moving = false;
    if (!sim.settled) {
      sim.tick(); sim.tick();
      draw();
      moving = !sim.settled;
    }
    let easing = false;
    if (follow) {
      const want = frame();
      const dz = want.zoom - zoom, dx = want.panX - panX, dy = want.panY - panY;
      if (Math.abs(dz) > 0.002 || Math.abs(dx) > 0.5 || Math.abs(dy) > 0.5) {
        zoom += dz * 0.14; panX += dx * 0.14; panY += dy * 0.14;
        easing = true;
      } else {
        zoom = want.zoom; panX = want.panX; panY = want.panY;
      }
      apply();
    }
    if (moving || easing) raf = requestAnimationFrame(step);
    else svg.setAttribute("data-settled", "true");
  }
  // settle brings everything to rest at once, for reduced motion.
  function settle() {
    sim.run();
    draw();
    if (follow) ({ zoom, panX, panY } = frame());
    apply();
    svg.setAttribute("data-settled", "true");
  }
  // reheat sets the simulation going again after a change of what it
  // holds; with reduced motion it lands at once.
  function reheat(a = 0.3) {
    sim.reheat(a);
    if (reduced) settle(); else wake();
  }

  // --- pan, zoom and dragging a node ---
  svg.addEventListener("wheel", (ev) => {
    ev.preventDefault();
    const p = toCanvas(ev);
    zoomAt(ev.deltaY < 0 ? 1.15 : 1 / 1.15, p.x, p.y);
  }, { passive: false });
  let pan = null;
  svg.addEventListener("pointerdown", (ev) => {
    if (ev.target.closest(".star-node")) return;
    pan = { start: toCanvas(ev), panX, panY, moved: false };
    svg.setPointerCapture(ev.pointerId);
  });
  svg.addEventListener("pointermove", (ev) => {
    if (!pan) return;
    const p = toCanvas(ev);
    panX = pan.panX + p.x - pan.start.x;
    panY = pan.panY + p.y - pan.start.y;
    if (!pan.moved && Math.abs(p.x - pan.start.x) + Math.abs(p.y - pan.start.y) > 3) { pan.moved = true; follow = false; }
    apply();
  });
  let panned = false; // the click that ends a pan does not clear the focus
  const endPan = () => { panned = Boolean(pan?.moved); pan = null; };
  svg.addEventListener("pointerup", endPan);
  svg.addEventListener("pointercancel", endPan);

  // A node follows the pointer that holds it, pinned while it is held;
  // its neighbours follow through the springs. Let go, it is free again.
  let nodeDrag = null;
  let suppressClick = false; // the click that ends a drag does not focus
  function grab(ev, i, g) {
    if (ev.button !== 0) return;
    ev.stopPropagation();
    nodeDrag = { i, g, pointer: ev.pointerId, start: toCanvas(ev), moved: false };
    g.setPointerCapture(ev.pointerId);
    g.addEventListener("pointermove", drag);
    g.addEventListener("pointerup", drop);
    g.addEventListener("pointercancel", drop);
  }
  function drag(ev) {
    if (!nodeDrag || ev.pointerId !== nodeDrag.pointer) return;
    const p = toCanvas(ev);
    if (!nodeDrag.moved) {
      if (Math.abs(p.x - nodeDrag.start.x) + Math.abs(p.y - nodeDrag.start.y) <= 3) return;
      nodeDrag.moved = true;
      follow = false;
      nodeDrag.g.classList.add("dragging");
      if (!reduced) sim.alphaTarget = 0.3;
    }
    sim.pin(nodeDrag.i, toWorld(p));
    if (reduced) draw(); else { sim.reheat(0.3); wake(); }
  }
  function drop(ev) {
    if (!nodeDrag || ev.pointerId !== nodeDrag.pointer) return;
    const { i, g, moved } = nodeDrag;
    nodeDrag = null;
    g.removeEventListener("pointermove", drag);
    g.removeEventListener("pointerup", drop);
    g.removeEventListener("pointercancel", drop);
    if (!moved) return;
    g.classList.remove("dragging");
    suppressClick = ev.type === "pointerup";
    sim.alphaTarget = 0;
    if (reduced) { sim.reheat(0.3); sim.run(); sim.unpin(i); draw(); } else { sim.unpin(i); wake(); }
  }

  const zoomButton = (label, cls, act) => h("button", { type: "button", class: "secondary " + cls, title: label, "aria-label": label, onclick: act }, cls === "zoom-in" ? "+" : cls === "zoom-out" ? "−" : "⤢");
  const zoomIn = zoomButton(t("starmap.zoomIn"), "zoom-in", () => zoomAt(1.25));
  const zoomOut = zoomButton(t("starmap.zoomOut"), "zoom-out", () => zoomAt(1 / 1.25));
  const zoomFit = zoomButton(t("starmap.zoomReset"), "zoom-fit", () => { follow = true; if (reduced) settle(); else wake(); });

  // --- lighting a neighbourhood: the node under the pointer or the
  // keyboard's focus, its neighbours and their edges; the rest dims ---
  let litNodes = [], litEdges = [];
  function light(id) {
    hovered = id && shownNow.nodes.has(id) ? id : null;
    for (const i of litNodes) nodeEls[i].classList.remove("lit", "hover");
    for (const k of litEdges) edgeEls[k].classList.remove("lit");
    litNodes = []; litEdges = [];
    if (hovered) {
      for (const m of neighbourhood(graph, hovered, shownNow.edges)) litNodes.push(index.get(m));
      for (const k of shownNow.edges) if (graph.edges[k].from === hovered || graph.edges[k].to === hovered) litEdges.push(k);
      for (const i of litNodes) nodeEls[i].classList.add("lit");
      for (const k of litEdges) edgeEls[k].classList.add("lit");
      nodeEls[index.get(hovered)].classList.add("hover");
    }
    svg.classList.toggle("hovering", Boolean(hovered));
  }

  // --- the side panel of the focused node ---
  const panel = h("aside", { class: "starmap-panel card", id: "starmap-panel", hidden: true, tabindex: "-1", "aria-labelledby": "starmap-panel-title" });
  // What the panel shows of a node is read once while the map is open,
  // when the node is chosen; pointing reads nothing.
  const previews = new Map();
  function contentOf(node) {
    if (!previews.has(node.id)) previews.set(node.id, previewOf(node));
    return previews.get(node.id);
  }
  async function previewOf(node) {
    if (node.kind === "picture") {
      const [, file] = node.id.slice("picture:".length).split("/");
      const owner = byID.get(node.item);
      return h("div", { class: "starmap-picture" },
        h("figure", {}, h("img", { src: `/api/items/${encodeURIComponent(node.item)}/attachments/${encodeURIComponent(file)}`, alt: node.name, id: "starmap-picture", loading: "lazy" }),
          h("figcaption", { class: "muted", text: node.name })),
        owner ? h("p", {}, h("button", { type: "button", class: "link", id: "starmap-picture-item", text: owner.title || owner.key, onclick: () => focus(owner.id) })) : null);
    }
    if (node.kind === "label") {
      const carriers = graph.edges.filter((e) => e.kind === "label" && (e.from === node.id || e.to === node.id))
        .map((e) => byID.get(e.from === node.id ? e.to : e.from)).filter((m) => m && m.kind === "item");
      return h("div", { class: "starmap-label-items" },
        h("h3", { text: t("starmap.labelItems", { n: carriers.length }) }),
        h("ul", { class: "list", id: "starmap-label-list" }, carriers.map((m) => h("li", {},
          h("button", { type: "button", class: "link", "data-id": m.id, text: `${m.key} ${m.title}`, onclick: () => focus(m.id) })))));
    }
    if (!preview) return null;
    const content = await preview(node.key || node.id);
    // Long pages are clipped; "Read more" opens them in the panel.
    const body = h("div", { class: "starmap-preview-body clipped", id: "starmap-preview-body" }, content);
    const more = h("button", { type: "button", class: "link starmap-more", id: "starmap-more", hidden: true, "aria-expanded": "false", text: t("starmap.readMore"),
      onclick: () => {
        const open = body.classList.toggle("clipped");
        more.textContent = open ? t("starmap.readMore") : t("starmap.readLess");
        more.setAttribute("aria-expanded", String(!open));
      } });
    const measure = () => {
      if (body.classList.contains("clipped")) more.hidden = body.scrollHeight <= body.clientHeight + 4;
    };
    if (typeof ResizeObserver === "function") new ResizeObserver(measure).observe(content);
    requestAnimationFrame(measure);
    return h("div", { class: "starmap-preview" }, body, more);
  }
  function showPanel(node) {
    const row = (label, value) => (value ? [h("dt", { text: label }), h("dd", { text: value })] : null);
    let link = null;
    if (node.kind === "item") link = h("a", { href: "#/item/" + encodeURIComponent(node.key), class: "button primary", id: "starmap-open", text: t("starmap.open") });
    else if (node.kind === "picture") link = h("a", { href: "#/item/" + encodeURIComponent(byID.get(node.item)?.key || node.item), class: "button primary", id: "starmap-open", text: t("starmap.openItem") });
    else if (node.kind === "label") link = h("a", { href: "#/label/" + encodeURIComponent(node.name), class: "button primary", id: "starmap-open", text: t("starmap.openLabel") });
    const owner = node.kind === "picture" ? byID.get(node.item) : null;
    const drill = impact || node.id === trail[trail.length - 1] ? null
      : h("button", { type: "button", class: "secondary", id: "starmap-drill", title: t("starmap.drillHelp"), text: t("starmap.drill"), onclick: () => separate(node.id) });
    // The impact of an item or a picture: from the map, the drill path
    // and the node; from an impact view, the same path, the node the new
    // start, the direction and depth kept.
    let impactLink = null;
    if (node.kind !== "label" && !(impact && node.id === impact.start)) {
      const stations = impact ? impact.stations : trail.map((id) => station(byID.get(id))).filter((st) => st !== station(node));
      impactLink = h("a", { href: impactAddress(space, [...stations, station(node)], impact || { direction: "affected", depth: 1 }), class: "button secondary",
        id: "starmap-impact-open", title: t("starmap.impactHelp"), text: t("starmap.impact"),
        onclick: () => { if (!impact) impactReturn = location.hash; } });
    }
    const box = h("div", { class: "starmap-preview-box", id: "starmap-preview", "aria-live": "polite" },
      h("p", { class: "muted", id: "starmap-preview-loading", text: t("starmap.loading") }));
    contentOf(node).then((el) => { if (el) mount(box, el); else box.replaceChildren(); },
      () => { previews.delete(node.id); mount(box, h("p", { class: "muted", id: "starmap-preview-none", text: t("starmap.unavailable") })); });
    mount(panel,
      h("div", { class: "starmap-panel-head" }, h("h2", { id: "starmap-panel-title", text: node.kind === "item" ? node.title || node.key : node.name }),
        h("button", { type: "button", class: "link", "aria-label": t("common.close"), title: t("common.close"), onclick: () => focus(null) }, "×")),
      // An item says its kind, key, room and state in one line.
      node.kind === "item" ? h("p", { class: "muted starmap-facts-line", id: "starmap-facts" },
        [node.type, node.key, node.space, node.state, impact ? distanceName(node.distance) : null].filter(Boolean).join(" · ")) : null,
      node.kind === "item" ? null : h("dl", { class: "starmap-facts" },
        row(t("starmap.panelKind"), node.kind === "item" ? node.type : node.kind === "picture" ? t("starmap.kindPicture") : t("starmap.kindLabel")),
        row(t("starmap.panelKey"), node.key || owner?.key),
        row(t("starmap.panelTitle"), owner ? owner.title : null),
        row(t("starmap.panelState"), node.state),
        row(t("starmap.panelSpace"), node.space),
        impact ? row(t("starmap.impactDistanceLabel"), distanceName(node.distance)) : null),
      h("div", { class: "starmap-panel-actions" }, link, drill, impactLink),
      box);
    panel.hidden = false;
    panel.scrollTop = 0;
    // The keyboard follows into the panel; Escape closes it as before.
    panel.focus({ preventScroll: true });
  }

  // --- filters and the depth of a separated neighbourhood ---
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
  const depthSelect = h("select", { id: "starmap-depth", "aria-label": t("starmap.depth"), title: t("starmap.depthHelp") },
    DEPTHS.map((d) => h("option", { value: String(d), text: String(d) })));
  depthSelect.value = String(depth);
  depthSelect.addEventListener("change", () => {
    depth = Number(depthSelect.value);
    if (trail.length) { follow = true; address(); }
    render();
  });
  const depthBox = h("label", { class: "starmap-depth" }, h("span", { text: t("starmap.depth") }), depthSelect);

  // --- the drill path: the whole room, then each separated node ---
  const crumbs = h("nav", { class: "starmap-trail", id: "starmap-trail", "aria-label": t("starmap.trail"), hidden: true });
  function paintTrail() {
    if (impact) return paintImpactTrail();
    crumbs.hidden = !trail.length;
    if (!trail.length) return mount(crumbs);
    const station = (label, to, here) => h("li", {}, here
      ? h("span", { class: "starmap-crumb here", "aria-current": "location", text: label })
      : h("button", { type: "button", class: "link starmap-crumb", "data-crumb": String(to), text: label, onclick: () => stepBack(to) }));
    mount(crumbs, h("ol", {}, station(t("starmap.trailAll"), -1, false),
      trail.map((id, i) => station(crumbName(byID.get(id)), i, i === trail.length - 1))));
  }
  // In an impact view the trail is the drill path it was opened from and
  // the impact last; a station of it leaves the impact view for there.
  function paintImpactTrail() {
    crumbs.hidden = false;
    const go = (label, to) => h("li", {}, h("button", { type: "button", class: "link starmap-crumb", "data-crumb": String(to), text: label,
      onclick: () => leaveImpact(to) }));
    const startNode = byID.get(impact.start);
    mount(crumbs, h("ol", {}, go(t("starmap.trailAll"), -1), impact.stations.map((st, i) => go(st.startsWith("picture:") ? t("starmap.kindPicture") : st, i)),
      h("li", {}, h("span", { class: "starmap-crumb here", "aria-current": "location", text: t("starmap.impactTrail", { name: crumbName(startNode) }) }))));
  }
  // leaveImpact returns to the map the impact view was opened from, or
  // to the station at index to of its drill path (-1 for the whole room).
  function leaveImpact(to) {
    const back = impactReturn;
    impactReturn = null;
    if (to === undefined && back) { location.hash = back; return; }
    const stations = to === undefined ? impact.stations : impact.stations.slice(0, to + 1);
    location.hash = "#/starmap/" + [space, ...stations].map(encodeURIComponent).join("/");
  }

  // address keeps the drill path and its depth in the address, so a
  // reload and the browser's back button find it again.
  function address() {
    const want = "#/starmap/" + [space, ...trail.map((id) => (byID.get(id).kind === "item" ? byID.get(id).key : id))].map(encodeURIComponent).join("/")
      + (trail.length && depth !== 1 ? "?depth=" + depth : "");
    if (location.hash !== want) history.pushState(null, "", want);
  }

  // separate narrows the map to a node and its neighbourhood, one station
  // further down the drill path; the view eases to frame it.
  function separate(id) {
    trail = drillInto(trail, id);
    focused = null;
    panel.hidden = true;
    follow = true;
    address();
    render();
  }
  // stepBack goes back one station of the drill path, or to station to.
  function stepBack(to) {
    trail = drillBack(trail, to);
    follow = true;
    address();
    render();
  }

  let shownNow = { nodes: new Set(), edges: [], matches: new Set() };
  let signature = null;
  function render() {
    const shown = visible(graph, { query: search.value, hidden, trail, depth });
    shownNow = shown;
    const at = trail[trail.length - 1];
    const near = focused && shown.nodes.has(focused) ? neighbourhood(graph, focused, shown.edges) : null;
    const edgeOn = new Set(shown.edges);
    graph.nodes.forEach((node, i) => {
      const el = nodeEls[i];
      if (shown.nodes.has(node.id)) el.removeAttribute("hidden"); else el.setAttribute("hidden", "");
      el.classList.toggle("match", shown.matches.has(node.id));
      el.classList.toggle("focused", node.id === focused);
      el.classList.toggle("root", impact ? node.id === impact.start : node.id === at);
      el.classList.toggle("dim", Boolean(near) && !near.has(node.id));
    });
    graph.edges.forEach((e, k) => {
      const el = edgeEls[k];
      if (edgeOn.has(k)) el.removeAttribute("hidden"); else el.setAttribute("hidden", "");
      el.classList.toggle("dim", Boolean(near) && e.from !== focused && e.to !== focused);
    });
    shownEdges = shown.edges;
    count.textContent = t("starmap.count", { n: shown.nodes.size });
    paintTrail();
    light(hovered);
    // What the simulation holds changed: it takes the shown nodes and
    // edges only, and moves again.
    const next = shown.edges.join(",") + "|" + [...shown.nodes].join(",");
    if (next !== signature) {
      signature = next;
      sim.setActive((i) => shown.nodes.has(graph.nodes[i].id), (k) => edgeOn.has(k));
      draw();
      if (started) reheat(trail.length ? 0.5 : 0.3);
    } else if (follow && started && !reduced) wake();
  }

  function focus(id) {
    const was = focused;
    const inPanel = panel.contains(document.activeElement);
    focused = id;
    if (id) showPanel(byID.get(id)); else panel.hidden = true;
    // Closed from within, the keyboard goes back to the node it came from.
    if (!id && was && inPanel && index.has(was)) nodeEls[index.get(was)].focus?.({ preventScroll: true });
    render();
  }

  svg.addEventListener("click", () => { if (focused && !panned) focus(null); panned = false; });
  const root = h("div", { class: "starmap" });
  // Escape ends the focus first, then steps back along the drill path,
  // wherever the keyboard is (but in a field, where it is the field's).
  const onKey = (ev) => {
    if (!alive()) return leave();
    if (ev.key !== "Escape") return;
    if (focused) { focus(null); return; }
    if (impact && !ev.target.closest?.("input, select, textarea")) { ev.preventDefault(); leaveImpact(); return; }
    if (trail.length && !ev.target.closest?.("input, select, textarea")) { ev.preventDefault(); stepBack(); }
  };
  // leave stops the loop and lets go of what the view listens to beyond
  // itself, once the view is left.
  let observer = null;
  function leave() {
    left = true;
    if (raf) cancelAnimationFrame(raf);
    raf = 0;
    document.removeEventListener("keydown", onKey);
    window.removeEventListener("hashchange", leave);
    observer?.disconnect();
  }

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
    impact ? h("p", { class: "muted", id: "starmap-impact-legend", text: t("starmap.legendImpact") }) : null,
    h("p", { class: "muted", text: t("starmap.help") }));

  // --- the impact view's controls and its list (ADR-0159) ---
  let impactTools = null, impactPanel = null;
  if (impact) {
    const go = (next) => { location.hash = impactAddress(space, [...impact.stations, station(byID.get(impact.start))], { ...impact, ...next }); };
    const dirButton = (dir, label) => h("button", { type: "button", class: "secondary" + (impact.direction === dir ? " active" : ""), "data-direction": dir,
      "aria-pressed": String(impact.direction === dir), text: label, onclick: () => { if (impact.direction !== dir) go({ direction: dir }); } });
    const impactDepth = h("select", { id: "starmap-impact-depth", "aria-label": t("starmap.impactDepth") },
      IMPACT_DEPTHS.map((d) => h("option", { value: String(d), text: d === "all" ? t("starmap.impactAll") : String(d) })));
    impactDepth.value = String(impact.depth);
    impactDepth.addEventListener("change", () => go({ depth: impactDepth.value === "all" ? "all" : Number(impactDepth.value) }));
    impactTools = [
      h("div", { class: "starmap-direction", id: "starmap-impact-direction", role: "group", "aria-label": t("starmap.impactDirection") },
        dirButton("affected", t("starmap.impactAffected")), dirButton("dependson", t("starmap.impactDependsOn"))),
      h("label", { class: "starmap-depth" }, h("span", { text: t("starmap.impactDepth") }), impactDepth),
      h("button", { type: "button", class: "secondary", id: "starmap-impact-leave", text: t("starmap.impactLeave"), onclick: () => leaveImpact() }),
    ];
    const { result } = impact;
    const sum = impactSummary(result);
    const all = result.depth === "all";
    const affected = impact.direction === "affected";
    const head = affected
      ? (all ? t("starmap.impactCountAffectedAll", { n: sum.items }) : t("starmap.impactCountAffected", { n: sum.items, depth: result.depth }))
      : (all ? t("starmap.impactCountDependsOnAll", { n: sum.items }) : t("starmap.impactCountDependsOn", { n: sum.items, depth: result.depth }));
    const reached = impactList(result);
    const owner = (n) => (n.kind === "picture" ? byID.get(n.item) : n);
    impactPanel = h("section", { class: "starmap-impact card", id: "starmap-impact", "aria-labelledby": "starmap-impact-count" },
      h("h2", { id: "starmap-impact-count", text: head }),
      sum.pictures ? h("p", { class: "muted", id: "starmap-impact-pictures", text: t("starmap.impactPictures", { n: sum.pictures }) }) : null,
      result.truncated ? h("p", { class: "notice", id: "starmap-impact-truncated", role: "status", text: t("starmap.impactTruncated") }) : null,
      sum.endedEarly ? h("p", { class: "muted", id: "starmap-impact-complete", role: "status", text: t("starmap.impactComplete", { n: sum.farthest }) }) : null,
      reached.length
        ? h("ol", { class: "starmap-impact-list", id: "starmap-impact-list", "aria-label": t("starmap.impactList") }, reached.map((n) => {
          const it = owner(n);
          const label = n.kind === "item" ? `${n.key} ${n.title}` : t("starmap.nodePicture", { name: n.name });
          return h("li", { class: distanceClass(n.distance), "data-distance": String(n.distance), "data-id": n.id },
            // A picture whose item the walk did not reach still leads to
            // it, by its ID: the server sent it, so the reader may read it.
            h("a", { href: "#/item/" + encodeURIComponent(it?.key || n.item), text: label }),
            h("span", { class: "muted", text: [n.kind === "item" ? n.type : t("starmap.kindPicture"), t("starmap.impactRoom", { space: n.space }), distanceName(n.distance)].join(" · ") }));
        }))
        : h("p", { class: "empty", id: "starmap-impact-none", text: affected ? t("starmap.impactNoneAffected") : t("starmap.impactNoneDependsOn") }));
  }

  const truncated = data.truncated ? h("p", { class: "notice", id: "starmap-truncated", role: "status", text: t("starmap.truncated") }) : null;
  const body = n
    ? h("div", { class: "starmap-stage" }, svg, panel)
    : h("p", { class: "empty", id: "starmap-empty", text: t("starmap.empty") });
  mount(root,
    h("div", { class: "filter-bar starmap-tools", role: "search", "aria-label": t("starmap.title") }, search, select, impactTools || depthBox, zoomIn, zoomOut, zoomFit, count),
    h("div", { class: "starmap-filters" }, kindBoxes, groupBoxes),
    crumbs, truncated, body, impactPanel, legend);
  if (impact) root.classList.add("impact-mode");
  render();

  // start runs once the map is on the page: it measures the canvas, keeps
  // the measure as the canvas changes size, and sets the map moving.
  function start() {
    started = true;
    if (!n) return;
    document.addEventListener("keydown", onKey);
    window.addEventListener("hashchange", leave);
    const measure = () => {
      const box = svg.getBoundingClientRect();
      W = Math.max(1, Math.round(box.width)) || 800;
      H = Math.max(1, Math.round(box.height)) || 560;
      svg.setAttribute("viewBox", `0 0 ${W} ${H}`);
    };
    measure();
    if (typeof ResizeObserver === "function") {
      observer = new ResizeObserver(() => { if (!alive()) return leave(); measure(); if (follow) { if (reduced) settle(); else wake(); } else apply(); });
      observer.observe(svg);
    }
    ({ zoom, panX, panY } = frame());
    apply();
    if (reduced) settle(); else wake();
  }
  return { root, start };
}
