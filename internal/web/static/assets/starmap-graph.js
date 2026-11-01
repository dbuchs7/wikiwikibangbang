// The starmap's graph without a browser (ADR-0157): which kind and group a
// node and an edge belong to, what the filters keep, a node's
// neighbourhood, and the layout. The layout is a force simulation with a
// fixed seed and a fixed number of steps, as atlas's panorama has it: the
// same map lands in the same place on every load, so a reload never looks
// like a change. It is computed once for the whole map; the filters only
// hide, so what stays keeps its place.
//
// Plain functions over plain data, so node --test runs them
// (internal/web/jstest/starmap.test.mjs).

// GROUPS are the edge groups the filters switch, each with its kinds.
export const GROUPS = {
  tree: ["parent"],
  relations: ["relation", "mentions", "embeds"],
  pictures: ["picture", "copy"],
  labels: ["label"],
};

// groupOf names the group of an edge kind.
export function groupOf(kind) {
  return Object.keys(GROUPS).find((g) => GROUPS[g].includes(kind)) || "relations";
}

// kindOf is the filter a node falls under: an item by its type (types are
// the users' own, never named here), a picture or a label. Outside nodes
// have a switch of their own besides (outsideKind).
export function kindOf(node) {
  if (node.kind === "item") return "type:" + node.type;
  return node.kind;
}

export const outsideKind = "outside";

// kinds lists the node filters the map needs: every item type on it in
// the order of the alphabet, then pictures, labels and outside nodes when
// it has them.
export function kinds(nodes) {
  const types = [...new Set(nodes.filter((n) => n.kind === "item").map((n) => n.type))].sort();
  const out = types.map((type) => ({ key: "type:" + type, type }));
  for (const k of ["picture", "label"]) if (nodes.some((n) => n.kind === k)) out.push({ key: k });
  if (nodes.some((n) => n.outside)) out.push({ key: outsideKind });
  return out;
}

// toneOf gives each item type of the map a tone, 0 to 5, by its place
// among the types: the colour that tells types apart besides the legend.
export function toneOf(nodes) {
  const types = kinds(nodes).filter((k) => k.type !== undefined).map((k) => k.type);
  return (type) => Math.max(0, types.indexOf(type)) % 6;
}

const fold = (s) => String(s || "").normalize("NFKD").replace(/\p{M}/gu, "").toLowerCase();

// matches reports whether a node holds every word of the query in its
// title, key, labels or name.
export function matches(node, query) {
  const words = fold(query).split(/\s+/).filter(Boolean);
  if (!words.length) return false;
  const text = fold([node.title, node.key, node.name, ...(node.labels || [])].join(" "));
  return words.every((w) => text.includes(w));
}

// filter applies the filters: hidden is a set of node kinds and edge
// groups switched off. A node shows when its kind is on (and, outside, the
// outside switch); an edge when its group is on and both ends show. With
// a query, only the matches show and what one shown edge joins to them.
// It returns the ids of the nodes shown, the indexes of the edges shown
// and the ids of the matches.
export function filter(graph, { query = "", hidden = new Set() } = {}) {
  const on = new Set(graph.nodes.filter((n) => !hidden.has(kindOf(n)) && !(n.outside && hidden.has(outsideKind))).map((n) => n.id));
  const edgeOn = (e) => !hidden.has(groupOf(e.kind)) && on.has(e.from) && on.has(e.to);
  const found = new Set();
  let shown = on;
  if (fold(query).trim()) {
    for (const n of graph.nodes) if (on.has(n.id) && matches(n, query)) found.add(n.id);
    shown = new Set(found);
    for (const e of graph.edges) {
      if (!edgeOn(e)) continue;
      if (found.has(e.from)) shown.add(e.to);
      if (found.has(e.to)) shown.add(e.from);
    }
  }
  const edges = [];
  graph.edges.forEach((e, i) => { if (edgeOn(e) && shown.has(e.from) && shown.has(e.to)) edges.push(i); });
  return { nodes: shown, edges, matches: found };
}

// neighbourhood is a node and every node one shown edge joins to it.
export function neighbourhood(graph, id, edges = graph.edges.map((_, i) => i)) {
  const out = new Set([id]);
  for (const i of edges) {
    const e = graph.edges[i];
    if (e.from === id) out.add(e.to);
    if (e.to === id) out.add(e.from);
  }
  return out;
}

// mulberry32 is a small seeded random number generator.
function mulberry32(seed) {
  return () => {
    seed |= 0; seed = (seed + 0x6D2B79F5) | 0;
    let t = Math.imul(seed ^ (seed >>> 15), 1 | seed);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

// size is the side of the square the layout fills: room for every node.
export function size(n) {
  return Math.round(Math.max(800, Math.sqrt(Math.max(n, 1)) * 110));
}

// layout places the nodes: they repel each other, edges pull their ends
// together, and a weak pull holds everything near the middle. The start is
// a spiral in the map's order with a seeded jitter, and the steps are
// fixed, so the result depends on the map alone. It returns a Map of id to
// {x, y}, rounded to tenths.
export function layout(graph, { iterations = 300, seed = 0x5EED } = {}) {
  const n = graph.nodes.length;
  const side = size(n);
  const mid = side / 2;
  const random = mulberry32(seed);
  const index = new Map(graph.nodes.map((node, i) => [node.id, i]));
  const x = new Float64Array(n), y = new Float64Array(n);
  for (let i = 0; i < n; i++) {
    const angle = i * 2.399963; // the golden angle
    const r = 18 * Math.sqrt(i + 1);
    x[i] = mid + r * Math.cos(angle) + (random() - 0.5) * 4;
    y[i] = mid + r * Math.sin(angle) + (random() - 0.5) * 4;
  }
  const links = [];
  for (const e of graph.edges) {
    const a = index.get(e.from), b = index.get(e.to);
    if (a !== undefined && b !== undefined && a !== b) links.push([a, b]);
  }
  const rest = 90;
  const repulsion = 4200;
  const steps = n > 400 ? Math.min(iterations, 160) : iterations;
  const dx = new Float64Array(n), dy = new Float64Array(n);
  for (let step = 0; step < steps; step++) {
    const heat = 1 - step / steps; // cooling: big moves first, small ones last
    dx.fill(0); dy.fill(0);
    for (let i = 0; i < n; i++) {
      for (let j = i + 1; j < n; j++) {
        let ux = x[i] - x[j], uy = y[i] - y[j];
        let d2 = ux * ux + uy * uy;
        if (d2 < 0.01) { ux = (i % 2 ? 1 : -1) * 0.1; uy = 0.1; d2 = 0.02; }
        if (d2 > 250000) continue; // beyond 500, too far to matter
        const f = repulsion / d2;
        const d = Math.sqrt(d2);
        dx[i] += (ux / d) * f; dy[i] += (uy / d) * f;
        dx[j] -= (ux / d) * f; dy[j] -= (uy / d) * f;
      }
    }
    for (const [a, b] of links) {
      const ux = x[b] - x[a], uy = y[b] - y[a];
      const d = Math.sqrt(ux * ux + uy * uy) || 0.1;
      const f = (d - rest) * 0.04;
      dx[a] += (ux / d) * f; dy[a] += (uy / d) * f;
      dx[b] -= (ux / d) * f; dy[b] -= (uy / d) * f;
    }
    const limit = 2 + 28 * heat;
    for (let i = 0; i < n; i++) {
      dx[i] += (mid - x[i]) * 0.012; dy[i] += (mid - y[i]) * 0.012;
      const m = Math.sqrt(dx[i] * dx[i] + dy[i] * dy[i]);
      const k = m > limit ? limit / m : 1;
      x[i] = Math.min(side - 20, Math.max(20, x[i] + dx[i] * k));
      y[i] = Math.min(side - 20, Math.max(20, y[i] + dy[i] * k));
    }
  }
  const out = new Map();
  graph.nodes.forEach((node, i) => out.set(node.id, { x: Math.round(x[i] * 10) / 10, y: Math.round(y[i] * 10) / 10 }));
  return out;
}
