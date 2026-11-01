// The starmap's live physics (ADR-0158): a force simulation that runs a
// tick per animation frame and cools down to rest, in the manner of
// d3-force — charge repulsion between nodes, springs along edges, a pull
// to the middle and collision — written here so the UI stays buildless
// (ADR-0031). Above a few dozen nodes the repulsion is approximated with a
// Barnes–Hut quadtree, so a tick over the 600 nodes of the budget stays
// well inside a frame.
//
// Everything is plain arithmetic over typed arrays: additions,
// multiplications, divisions and square roots, which IEEE 754 makes exact
// on every engine. The same start and the same graph therefore come to
// rest in the same place everywhere; nothing here draws a random number.
// node --test runs it (internal/web/jstest/starmap.test.mjs); the view
// (starmap.js) only reads the positions and writes them into the SVG.

// The defaults, in world units (the units of the seeded layout).
export const DEFAULTS = {
  alpha: 1,
  alphaMin: 0.001,
  // As d3: from 1 to alphaMin in 300 ticks.
  alphaDecay: 1 - Math.pow(0.001, 1 / 300),
  velocityDecay: 0.4,
  charge: -300,
  // Repulsion beyond this distance is ignored; by default none is, as in
  // d3, since a cut-off spoils the quadtree's approximation.
  chargeMax: Infinity,
  theta: 0.9,
  // Up to this many nodes the repulsion is exact; above it, Barnes–Hut.
  exactBelow: 64,
  linkDistance: 70,
  gravity: 0.08,
  radius: 18,
};

// quadtree builds a Barnes–Hut tree over the points i of active (an array
// of indexes): every cell knows its weight (the number of points in it)
// and their centre. A leaf holds one point, or several on the same spot,
// chained through next.
export function quadtree(x, y, active) {
  let x0 = Infinity, y0 = Infinity, x1 = -Infinity, y1 = -Infinity;
  for (const i of active) {
    if (x[i] < x0) x0 = x[i];
    if (y[i] < y0) y0 = y[i];
    if (x[i] > x1) x1 = x[i];
    if (y[i] > y1) y1 = y[i];
  }
  if (!active.length) return null;
  const side = Math.max(x1 - x0, y1 - y0, 1);
  const root = cell(x0, y0, side);
  for (const i of active) insert(root, x, y, i, 0);
  sum(root, x, y);
  return root;
}

function cell(x0, y0, side) {
  return { x0, y0, side, kids: null, point: -1, next: null, weight: 0, cx: 0, cy: 0 };
}

function insert(node, x, y, i, depth) {
  for (;;) {
    if (node.kids) {
      const half = node.side / 2;
      const right = x[i] >= node.x0 + half ? 1 : 0, below = y[i] >= node.y0 + half ? 1 : 0;
      const q = right + 2 * below;
      if (!node.kids[q]) {
        node.kids[q] = cell(node.x0 + right * half, node.y0 + below * half, half);
        node.kids[q].point = i;
        return;
      }
      node = node.kids[q];
      depth++;
      continue;
    }
    if (node.point < 0) { node.point = i; return; }
    // Two points on one spot (or the tree too deep to tell them apart)
    // share the leaf.
    const j = node.point;
    if ((x[j] === x[i] && y[j] === y[i]) || depth > 40) {
      node.next = { point: i, next: node.next };
      return;
    }
    // Split: the old point goes one level down, then the new one follows.
    node.kids = [null, null, null, null];
    node.point = -1;
    let chain = { point: j, next: node.next };
    node.next = null;
    while (chain) {
      insert(node, x, y, chain.point, depth);
      chain = chain.next;
    }
  }
}

function sum(node, x, y) {
  if (node.kids) {
    let w = 0, cx = 0, cy = 0;
    for (const k of node.kids) {
      if (!k) continue;
      sum(k, x, y);
      w += k.weight; cx += k.cx * k.weight; cy += k.cy * k.weight;
    }
    node.weight = w; node.cx = cx / w; node.cy = cy / w;
    return;
  }
  let w = 1;
  for (let c = node.next; c; c = c.next) w++;
  node.weight = w; node.cx = x[node.point]; node.cy = y[node.point];
}

// jiggle is a tiny, deterministic push for two points on the same spot,
// so they can part without a random number.
const jiggle = (i, j) => ((i * 7 + j * 13) % 11 - 5) * 1e-3 || 1e-3;

// apart is the made-up direction from point i to point j when the two lie
// on one spot: the same pair always parts the same way, from either end.
function apart(i, j) {
  const lo = Math.min(i, j), hi = Math.max(i, j), sign = i < j ? 1 : -1;
  return [jiggle(lo, hi) * sign, jiggle(hi, lo) * sign];
}

// chargeExact adds to fx, fy the repulsion every active point feels from
// every other, as d3's many-body force does: strength / distance in the
// direction away from the other point (in the units of a velocity per
// unit of alpha).
export function chargeExact(x, y, active, fx, fy, { charge = DEFAULTS.charge, chargeMax = DEFAULTS.chargeMax } = {}) {
  const max2 = chargeMax * chargeMax;
  for (let a = 0; a < active.length; a++) {
    const i = active[a];
    for (let b = a + 1; b < active.length; b++) {
      const j = active[b];
      let dx = x[j] - x[i], dy = y[j] - y[i];
      let l2 = dx * dx + dy * dy;
      if (l2 >= max2) continue;
      if (l2 === 0) { [dx, dy] = apart(i, j); l2 = dx * dx + dy * dy; }
      if (l2 < 1) l2 = Math.sqrt(l2); // as d3: tame the force at very short range
      const k = charge / l2;
      fx[i] += dx * k; fy[i] += dy * k;
      fx[j] -= dx * k; fy[j] -= dy * k;
    }
  }
}

// chargeTree is chargeExact approximated through the quadtree: a cell far
// enough away (its side over the distance below theta) acts as one point
// of its weight at its centre.
export function chargeTree(x, y, active, fx, fy, { charge = DEFAULTS.charge, chargeMax = DEFAULTS.chargeMax, theta = DEFAULTS.theta } = {}) {
  const root = quadtree(x, y, active);
  if (!root) return;
  const theta2 = theta * theta, max2 = chargeMax * chargeMax;
  const stack = [];
  for (const i of active) {
    const xi = x[i], yi = y[i];
    let sx = 0, sy = 0;
    stack.length = 0;
    stack.push(root);
    while (stack.length) {
      const node = stack.pop();
      let dx = node.cx - xi, dy = node.cy - yi;
      let l2 = dx * dx + dy * dy;
      const w = node.side;
      if (node.kids && w * w / theta2 < l2) {
        if (l2 < max2) sx += dx * charge * node.weight / l2, sy += dy * charge * node.weight / l2;
        continue;
      }
      if (node.kids) {
        for (const k of node.kids) if (k) stack.push(k);
        continue;
      }
      // A leaf: each of its points but i itself.
      for (let c = { point: node.point, next: node.next }; c; c = c.next) {
        const j = c.point;
        if (j === i) continue;
        dx = x[j] - xi; dy = y[j] - yi;
        l2 = dx * dx + dy * dy;
        if (l2 >= max2) continue;
        if (l2 === 0) { [dx, dy] = apart(i, j); l2 = dx * dx + dy * dy; }
        if (l2 < 1) l2 = Math.sqrt(l2);
        sx += dx * charge / l2; sy += dy * charge / l2;
      }
    }
    fx[i] += sx; fy[i] += sy;
  }
}

// simulation makes a live simulation over n nodes at the given start
// positions (x0, y0), with links as pairs of node indexes; center is the
// point the nodes are pulled to. Every node and link takes part until
// setActive says otherwise. Pinning a node (pin) holds it where it is put
// while the others move; unpin lets it go.
export function simulation({ x0, y0, links = [], center = { x: 0, y: 0 }, ...options }) {
  const o = { ...DEFAULTS, ...options };
  const n = x0.length;
  const x = Float64Array.from(x0), y = Float64Array.from(y0);
  const vx = new Float64Array(n), vy = new Float64Array(n);
  const px = new Float64Array(n).fill(NaN), py = new Float64Array(n).fill(NaN);
  const fx = new Float64Array(n), fy = new Float64Array(n);
  let active = Array.from({ length: n }, (_, i) => i);
  let on = new Uint8Array(n).fill(1);
  let springs = [];
  let alpha = o.alpha, alphaTarget = 0;

  // springs: the active links, each with d3's default strength (one over
  // the smaller degree of its ends, so hubs do not pull everything in) and
  // bias (how much of the correction each end takes).
  const wire = (linkOn) => {
    const degree = new Float64Array(n);
    const live = [];
    links.forEach(([a, b], k) => {
      if (a === b || !on[a] || !on[b] || (linkOn && !linkOn(k))) return;
      live.push([a, b]);
      degree[a]++; degree[b]++;
    });
    springs = live.map(([a, b]) => ({ a, b, strength: 1 / Math.min(degree[a], degree[b]), bias: degree[a] / (degree[a] + degree[b]) }));
  };
  wire();

  const sim = {
    x, y, vx, vy, n,
    get alpha() { return alpha; },
    set alpha(a) { alpha = a; },
    get alphaTarget() { return alphaTarget; },
    set alphaTarget(a) { alphaTarget = a; },
    get settled() { return alpha < o.alphaMin && alphaTarget === 0; },
    get active() { return active; },
    isActive: (i) => on[i] === 1,
    pinned: (i) => !Number.isNaN(px[i]),

    // setActive keeps only the nodes for which nodeOn holds, and the links
    // between them for which linkOn (given the link's index) holds; the
    // rest is frozen where it is and exerts no force.
    setActive(nodeOn, linkOn) {
      on = new Uint8Array(n);
      active = [];
      for (let i = 0; i < n; i++) if (nodeOn(i)) { on[i] = 1; active.push(i); }
      for (let i = 0; i < n; i++) if (!on[i]) { vx[i] = 0; vy[i] = 0; }
      wire(linkOn);
      return sim;
    },

    // reheat raises the temperature, never lowers it.
    reheat(a = 0.3) { alpha = Math.max(alpha, a); return sim; },

    pin(i, at = { x: x[i], y: y[i] }) { px[i] = at.x; py[i] = at.y; x[i] = at.x; y[i] = at.y; vx[i] = 0; vy[i] = 0; return sim; },
    unpin(i) { px[i] = NaN; py[i] = NaN; return sim; },

    // tick advances one step and reports whether the simulation still
    // moves.
    tick() {
      alpha += (alphaTarget - alpha) * o.alphaDecay;
      fx.fill(0); fy.fill(0);
      // Springs, as d3's link force: from the positions the velocities
      // will give, so a chain does not overshoot.
      for (const s of springs) {
        let dx = x[s.b] + vx[s.b] - x[s.a] - vx[s.a];
        let dy = y[s.b] + vy[s.b] - y[s.a] - vy[s.a];
        let l = Math.sqrt(dx * dx + dy * dy);
        if (l === 0) { [dx, dy] = apart(s.a, s.b); l = Math.sqrt(dx * dx + dy * dy); }
        const k = ((l - o.linkDistance) / l) * alpha * s.strength;
        dx *= k; dy *= k;
        vx[s.b] -= dx * s.bias; vy[s.b] -= dy * s.bias;
        vx[s.a] += dx * (1 - s.bias); vy[s.a] += dy * (1 - s.bias);
      }
      if (active.length < o.exactBelow) chargeExact(x, y, active, fx, fy, o);
      else chargeTree(x, y, active, fx, fy, o);
      for (const i of active) {
        vx[i] += fx[i] * alpha + (center.x - x[i]) * o.gravity * alpha;
        vy[i] += fy[i] * alpha + (center.y - y[i]) * o.gravity * alpha;
      }
      collide(sim, o.radius);
      for (const i of active) {
        if (!Number.isNaN(px[i])) { x[i] = px[i]; y[i] = py[i]; vx[i] = 0; vy[i] = 0; continue; }
        vx[i] *= 1 - o.velocityDecay; vy[i] *= 1 - o.velocityDecay;
        x[i] += vx[i]; y[i] += vy[i];
      }
      return !sim.settled;
    },

    // run ticks until the simulation rests (or max ticks): the settled
    // layout at once, for a reader who asked for reduced motion.
    run(max = 1000) {
      let ticks = 0;
      while (!sim.settled && ticks < max) { sim.tick(); ticks++; }
      return ticks;
    },

    // positions are the active nodes' places, rounded to tenths.
    positions() {
      return active.map((i) => [i, Math.round(x[i] * 10) / 10, Math.round(y[i] * 10) / 10]);
    },
  };
  return sim;
}

// collide keeps active nodes at least two radii apart: overlapping pairs,
// found through a grid of cells one diameter wide, are pushed apart along
// the line between them by half the overlap each.
function collide(sim, r) {
  const { x, y, vx, vy, active } = sim;
  const d = 2 * r, d2 = d * d;
  const grid = new Map();
  const keyOf = (cx, cy) => cx * 65536 + cy;
  for (const i of active) {
    const cx = Math.floor((x[i] + vx[i]) / d), cy = Math.floor((y[i] + vy[i]) / d);
    const k = keyOf(cx, cy);
    const list = grid.get(k);
    if (list) list.push(i); else grid.set(k, [i]);
  }
  for (const i of active) {
    const xi = x[i] + vx[i], yi = y[i] + vy[i];
    const cx = Math.floor(xi / d), cy = Math.floor(yi / d);
    for (let gx = cx - 1; gx <= cx + 1; gx++) {
      for (let gy = cy - 1; gy <= cy + 1; gy++) {
        const list = grid.get(keyOf(gx, gy));
        if (!list) continue;
        for (const j of list) {
          if (j <= i) continue;
          let dx = xi - x[j] - vx[j], dy = yi - y[j] - vy[j];
          let l2 = dx * dx + dy * dy;
          if (l2 >= d2) continue;
          if (l2 === 0) { [dx, dy] = apart(j, i); l2 = dx * dx + dy * dy; }
          const l = Math.sqrt(l2);
          const k = ((d - l) / l) * 0.35;
          dx *= k; dy *= k;
          const pi = sim.pinned(i), pj = sim.pinned(j);
          const si = pi ? 0 : pj ? 1 : 0.5, sj = pj ? 0 : pi ? 1 : 0.5;
          vx[i] += dx * si; vy[i] += dy * si;
          vx[j] -= dx * sj; vy[j] -= dy * sj;
        }
      }
    }
  }
}
