// The labels' starmap as plain functions (ADR-0165): what the address
// asks for, which labels are shown, how big a star and the canvas are, and
// where the stars stand. Nothing here touches the page, so node --test
// runs it (internal/web/jstest/labelmap.test.mjs); labelmap.js draws it.
//
// The layout is plain arithmetic over typed arrays from a fixed start, so
// the same labels come to rest in the same place on every load; nothing
// here draws a random number.

// The defaults of the address: shown connected labels, every count.
export const DEFAULT_MIN = 1;

// readState reads the view's state from the address's parameters: the
// room (empty for every room), the chosen labels, whether their connected
// labels are shown, the least count shown and the search.
export function readState(params) {
  const labels = (params.get("labels") || "").split(",").map((l) => l.trim()).filter(Boolean);
  const min = Math.floor(Number(params.get("min")));
  return {
    space: params.get("space") || "",
    labels: [...new Set(labels)],
    near: params.get("near") !== "off",
    min: Number.isFinite(min) && min > DEFAULT_MIN ? min : DEFAULT_MIN,
    q: params.get("q") || "",
  };
}

// address writes a state as the view's address; defaults are left out, so
// the plain view is #/labelmap.
export function address(state) {
  const p = new URLSearchParams();
  if (state.space) p.set("space", state.space);
  if (state.labels.length) p.set("labels", state.labels.join(","));
  if (!state.near) p.set("near", "off");
  if (state.min > DEFAULT_MIN) p.set("min", String(state.min));
  if (state.q) p.set("q", state.q);
  const query = p.toString();
  return "#/labelmap" + (query ? "?" + query : "");
}

// neighbours lists the labels that share items with label, with how many
// they share, most shared first, then by name.
export function neighbours(pairs, label) {
  const out = [];
  for (const p of pairs) {
    if (p.labels[0] === label) out.push({ label: p.labels[1], count: p.count });
    else if (p.labels[1] === label) out.push({ label: p.labels[0], count: p.count });
  }
  return out.sort((a, b) => b.count - a.count || a.label.localeCompare(b.label));
}

// shown decides which labels the map shows. Without a choice and a
// search, every label carried at least min times. A choice and the
// labels a search finds are the focus: they are shown whatever their
// count, and with near their connected labels too, if carried at least min
// times. It answers the shown labels and the focus, both as sets.
export function shown(labels, pairs, { labels: chosen = [], near = true, min = DEFAULT_MIN, q = "" } = {}) {
  const known = new Map(labels.map((l) => [l.label, l.count]));
  const words = q.trim().toLowerCase();
  const focus = new Set(chosen.filter((l) => known.has(l)));
  if (words) for (const l of labels) if (l.label.includes(words)) focus.add(l.label);
  if (!chosen.length && !words) {
    return { labels: new Set(labels.filter((l) => l.count >= min).map((l) => l.label)), focus };
  }
  const out = new Set(focus);
  if (near) {
    for (const p of pairs) {
      const [a, b] = p.labels;
      if (focus.has(a) && known.has(b) && known.get(b) >= min) out.add(b);
      if (focus.has(b) && known.has(a) && known.get(a) >= min) out.add(a);
    }
  }
  return { labels: out, focus };
}

// starRadius is how big a label's star is: its area grows with how many
// items carry it, from a small star for one item to the biggest for the
// most carried label shown.
export const RADIUS = { min: 6, max: 30 };
export function starRadius(count, max) {
  if (!(max > 0) || !(count > 0)) return RADIUS.min;
  return Math.round((RADIUS.min + (RADIUS.max - RADIUS.min) * Math.sqrt(Math.min(count, max) / max)) * 10) / 10;
}

// canvasHeight is how tall the map is drawn, in pixels: small for a few
// labels, growing with the square root of their number, so the area grows
// with the number of stars, up to a ceiling beyond which the map is
// zoomed and panned instead.
export const HEIGHT = { min: 240, max: 960 };
export function canvasHeight(n) {
  if (n <= 0) return HEIGHT.min;
  return Math.round(Math.min(HEIGHT.max, Math.max(HEIGHT.min, 120 + 70 * Math.sqrt(n))));
}

// lineWidth is how thick the line between two labels is, by how many
// items carry both, relative to the most shared pair shown: 1 to 5.
export function lineWidth(count, max) {
  if (!(max > 0)) return 1;
  return Math.round((1 + 4 * Math.sqrt(Math.min(count, max) / max)) * 10) / 10;
}

// layout places the stars: the most carried in the middle, the others on a
// spiral around it in the order of their counts; lines pull the labels
// they join together, stars push apart until their discs, with room for
// a caption, no longer overlap, and a weak pull holds everything near the
// middle — harder up and down than sideways for a wide canvas (aspect,
// its width over its height, between 0.6 and 2.5), so the stars spread
// across it. stars are {label, r}, links {a, b, count} by label. It
// answers a Map of label to {x, y}, rounded to tenths, around (0, 0).
export function layout(stars, links = [], { iterations = 240, aspect = 1 } = {}) {
  const n = stars.length;
  const out = new Map();
  if (!n) return out;
  const index = new Map(stars.map((s, i) => [s.label, i]));
  const x = new Float64Array(n), y = new Float64Array(n), r = new Float64Array(n);
  // A star claims its disc and room for its caption below it.
  const gap = 18;
  let mean = 0;
  for (let i = 0; i < n; i++) { r[i] = stars[i].r + gap; mean += r[i]; }
  mean /= n;
  const wide = Math.min(2.5, Math.max(0.6, Number(aspect) || 1));
  const sx = Math.sqrt(wide), sy = 1 / sx;
  for (let i = 0; i < n; i++) {
    const angle = i * 2.399963; // the golden angle
    const d = mean * 1.6 * Math.sqrt(i);
    x[i] = d * Math.cos(angle) * sx;
    y[i] = d * Math.sin(angle) * sy;
  }
  let strongest = 0;
  const springs = [];
  for (const l of links) {
    const a = index.get(l.a), b = index.get(l.b);
    if (a === undefined || b === undefined || a === b) continue;
    springs.push([a, b, l.count]);
    if (l.count > strongest) strongest = l.count;
  }
  const steps = n > 200 ? Math.min(iterations, 140) : iterations;
  for (let step = 0; step < steps; step++) {
    const heat = 1 - step / steps;
    // Lines pull the labels they join towards touching, the more shared
    // the stronger.
    for (const [a, b, count] of springs) {
      const ux = x[b] - x[a], uy = y[b] - y[a];
      const d = Math.sqrt(ux * ux + uy * uy) || 0.1;
      const rest = r[a] + r[b] + 12;
      if (d <= rest) continue;
      const f = ((d - rest) / d) * 0.06 * (0.3 + 0.7 * (count / strongest)) * heat;
      x[a] += ux * f; y[a] += uy * f;
      x[b] -= ux * f; y[b] -= uy * f;
    }
    // A weak pull to the middle keeps lone labels near.
    for (let i = 0; i < n; i++) {
      x[i] -= x[i] * (0.012 / wide) * heat;
      y[i] -= y[i] * 0.012 * heat;
    }
    // Discs never overlap: each overlapping pair moves apart by half the
    // overlap each.
    for (let i = 0; i < n; i++) {
      for (let j = i + 1; j < n; j++) {
        let ux = x[j] - x[i], uy = y[j] - y[i];
        let d2 = ux * ux + uy * uy;
        const want = r[i] + r[j];
        if (d2 >= want * want) continue;
        if (d2 < 1e-6) { ux = (j % 2 ? 1 : -1) * 0.1; uy = 0.1; d2 = 0.02; }
        const d = Math.sqrt(d2);
        const push = (want - d) / d / 2;
        x[i] -= ux * push; y[i] -= uy * push;
        x[j] += ux * push; y[j] += uy * push;
      }
    }
  }
  for (let i = 0; i < n; i++) out.set(stars[i].label, { x: Math.round(x[i] * 10) / 10, y: Math.round(y[i] * 10) / 10 });
  return out;
}

// frame is the view box around the stars, with room for their discs and
// captions, never smaller than a few stars need, so one label alone is
// not blown up to fill the canvas. It keeps the canvas's proportions
// (width over height) so the stars are spread across it.
export function frame(points, { aspect = 1.6, pad = 36, minWidth = 360 } = {}) {
  let x0 = Infinity, y0 = Infinity, x1 = -Infinity, y1 = -Infinity;
  for (const p of points) {
    x0 = Math.min(x0, p.x - p.r); x1 = Math.max(x1, p.x + p.r);
    y0 = Math.min(y0, p.y - p.r); y1 = Math.max(y1, p.y + p.r + 16);
  }
  if (!points.length) { x0 = y0 = 0; x1 = y1 = 0; }
  let w = x1 - x0 + 2 * pad, hgt = y1 - y0 + 2 * pad;
  w = Math.max(w, minWidth, hgt * aspect);
  hgt = Math.max(hgt, w / aspect);
  const cx = (x0 + x1) / 2, cy = (y0 + y1) / 2;
  const r1 = (v) => Math.round(v * 10) / 10;
  return { x: r1(cx - w / 2), y: r1(cy - hgt / 2), width: r1(w), height: r1(hgt) };
}
