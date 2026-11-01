// The starmap of a room (ADR-0157, ADR-0158): its items, pictures and
// shared labels as a graph, drawn as SVG without a library. The server
// derives the graph per request from what the reader may read; this
// module seeds a layout (starmap-graph.js), lets it move under a live
// force simulation (starmap-sim.js) that cools to rest, filters it, and
// lets one pan, zoom, drag a node with its neighbours, light a node's
// neighbourhood by pointing at it, focus it, and separate it with its
// neighbourhood by a double-click. Shapes and outlines tell the kinds
// apart, not colour alone; the colours are the look's tokens (style.css),
// so it reads in the light, dark and cockpit looks alike.
import { h, mount } from "./dom.js";
import { t } from "./i18n.js";
import { get } from "./api.js";
import { DEPTHS, GROUPS, drillBack, drillInto, fitView, groupOf, kinds, layout, neighbourhood, size, toneOf, visible, zoomBand } from "./starmap-graph.js";
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

// starmapView mounts the starmap of a room into container; space is the
// room's key from the address, or empty for the first of one's rooms,
// favourites first as everywhere a room is chosen (ADR-0145). path is the
// drill path from the address (keys of items, ids of other nodes) and
// depth the reach of a separated neighbourhood.
export async function starmapView(container, space, path = [], depth = 1) {
  const { spaces } = await get("/api/spaces");
  const rooms = [...spaces.filter((r) => r.favourite), ...spaces.filter((r) => !r.favourite)];
  if (!rooms.length) return mount(container, h("p", { class: "empty", text: t("starmap.noSpaces") }));
  const key = rooms.some((r) => r.key === space) ? space : rooms[0].key;
  const select = h("select", { "aria-label": t("starmap.space"), id: "starmap-space" },
    rooms.map((r) => h("option", { value: r.key, text: r.name ? `${r.key} – ${r.name}` : r.key })));
  select.value = key;
  select.addEventListener("change", () => { location.hash = "#/starmap/" + encodeURIComponent(select.value); });
  const data = await get(`/api/spaces/${encodeURIComponent(key)}/starmap`);
  const map = drawMap(data, key, select, { path: key === space ? path : [], depth });
  mount(container, map.root);
  map.start();
}

function drawMap(data, space, select, { path, depth: askedDepth }) {
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
    const g = s("g", { class: "star-link", "data-kind": e.kind }, line);
    if (e.kind === "relation" && e.type) {
      edgeLabels[k] = s("text", { class: "star-edge-label", "text-anchor": "middle" }, text(short(e.type, 18)));
      g.append(edgeLabels[k]);
    }
    return g;
  });
  const nodeEls = graph.nodes.map((node, i) => {
    const cls = ["star-node", "kind-" + node.kind, node.kind === "item" ? "tone-" + tone(node.type) : "", node.outside ? "outside" : ""].join(" ");
    const caption = node.kind === "item" ? short(node.title || node.key) : short(node.name || "");
    const g = s("g", { class: cls, tabindex: 0, role: "button", "aria-label": nodeName(node), "data-id": node.id, "data-key": node.key || null },
      s("title", {}, text(nodeName(node))), shape(node.kind), s("text", { y: 24, "text-anchor": "middle", class: "star-caption" }, text(caption)));
    g.addEventListener("click", (ev) => {
      ev.stopPropagation();
      if (suppressClick) { suppressClick = false; return; }
      focus(node.id);
    });
    g.addEventListener("dblclick", (ev) => { ev.preventDefault(); ev.stopPropagation(); separate(node.id); });
    g.addEventListener("keydown", (ev) => {
      if (ev.key === "Enter" && ev.shiftKey) { ev.preventDefault(); separate(node.id); }
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
  const panel = h("aside", { class: "starmap-panel card", id: "starmap-panel", hidden: true, "aria-live": "polite" });
  function showPanel(node) {
    const row = (label, value) => (value ? [h("dt", { text: label }), h("dd", { text: value })] : null);
    let link = null;
    if (node.kind === "item") link = h("a", { href: "#/item/" + encodeURIComponent(node.key), class: "button primary", id: "starmap-open", text: t("starmap.open") });
    else if (node.kind === "picture" && byID.get(node.item)?.key) link = h("a", { href: "#/item/" + encodeURIComponent(byID.get(node.item).key), class: "button primary", id: "starmap-open", text: t("starmap.openItem") });
    else if (node.kind === "label") link = h("a", { href: "#/label/" + encodeURIComponent(node.name), class: "button primary", id: "starmap-open", text: t("starmap.openLabel") });
    const owner = node.kind === "picture" ? byID.get(node.item) : null;
    const drill = node.id === trail[trail.length - 1] ? null
      : h("button", { type: "button", class: "secondary", id: "starmap-drill", title: t("starmap.drillHelp"), text: t("starmap.drill"), onclick: () => separate(node.id) });
    mount(panel,
      h("div", { class: "starmap-panel-head" }, h("h2", { text: node.kind === "item" ? node.title || node.key : node.name }),
        h("button", { type: "button", class: "link", "aria-label": t("common.close"), title: t("common.close"), onclick: () => focus(null) }, "×")),
      h("dl", { class: "starmap-facts" },
        row(t("starmap.panelKind"), node.kind === "item" ? node.type : node.kind === "picture" ? t("starmap.kindPicture") : t("starmap.kindLabel")),
        row(t("starmap.panelKey"), node.key || owner?.key),
        row(t("starmap.panelTitle"), owner ? owner.title : null),
        row(t("starmap.panelState"), node.state),
        row(t("starmap.panelSpace"), node.space)),
      h("div", { class: "starmap-panel-actions" }, link, drill));
    panel.hidden = false;
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
    crumbs.hidden = !trail.length;
    if (!trail.length) return mount(crumbs);
    const station = (label, to, here) => h("li", {}, here
      ? h("span", { class: "starmap-crumb here", "aria-current": "location", text: label })
      : h("button", { type: "button", class: "link starmap-crumb", "data-crumb": String(to), text: label, onclick: () => stepBack(to) }));
    mount(crumbs, h("ol", {}, station(t("starmap.trailAll"), -1, false),
      trail.map((id, i) => station(crumbName(byID.get(id)), i, i === trail.length - 1))));
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
    const station = trail[trail.length - 1];
    const near = focused && shown.nodes.has(focused) ? neighbourhood(graph, focused, shown.edges) : null;
    const edgeOn = new Set(shown.edges);
    graph.nodes.forEach((node, i) => {
      const el = nodeEls[i];
      if (shown.nodes.has(node.id)) el.removeAttribute("hidden"); else el.setAttribute("hidden", "");
      el.classList.toggle("match", shown.matches.has(node.id));
      el.classList.toggle("focused", node.id === focused);
      el.classList.toggle("root", node.id === station);
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
    focused = id;
    if (id) showPanel(byID.get(id)); else panel.hidden = true;
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
    h("p", { class: "muted", text: t("starmap.help") }));

  const truncated = data.truncated ? h("p", { class: "notice", id: "starmap-truncated", role: "status", text: t("starmap.truncated") }) : null;
  const body = n
    ? h("div", { class: "starmap-stage" }, svg, panel)
    : h("p", { class: "empty", id: "starmap-empty", text: t("starmap.empty") });
  mount(root,
    h("div", { class: "filter-bar starmap-tools", role: "search", "aria-label": t("starmap.title") }, search, select, depthBox, zoomIn, zoomOut, zoomFit, count),
    h("div", { class: "starmap-filters" }, kindBoxes, groupBoxes),
    crumbs, truncated, body, legend);
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
