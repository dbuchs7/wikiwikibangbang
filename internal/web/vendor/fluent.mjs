// Turns the Fluent UI System Icons (github.com/microsoft/fluentui-system-icons,
// MIT) into Excalidraw shapes for the drawing editor's palette (ADR-0125).
// Excalidraw (0.17 and 0.18) previews library items without their files, so an icon
// cannot be an embedded picture: each outline of the SVG path becomes a
// filled polygon instead. Excalidraw fills a polygon without holes, so an
// outline inside another is filled with white, one inside that again with
// the ink, and so on; the dark scheme inverts both, as it does every shape.

// tokens splits path data into commands and numbers.
function tokens(d) {
  return d.match(/[MmLlHhVvCcSsQqTtAaZz]|-?(?:\d+\.?\d*|\.\d+)(?:[eE][-+]?\d+)?/g) || [];
}

// arcPoints approximates an SVG arc from (x1, y1) to (x2, y2) (SVG 1.1,
// appendix F.6) with points, the start left out.
function arcPoints(x1, y1, rx, ry, phi, large, sweep, x2, y2) {
  if (rx === 0 || ry === 0) return [[x2, y2]];
  rx = Math.abs(rx);
  ry = Math.abs(ry);
  const cos = Math.cos((phi * Math.PI) / 180), sin = Math.sin((phi * Math.PI) / 180);
  const dx = (x1 - x2) / 2, dy = (y1 - y2) / 2;
  const xp = cos * dx + sin * dy, yp = -sin * dx + cos * dy;
  const lambda = (xp * xp) / (rx * rx) + (yp * yp) / (ry * ry);
  if (lambda > 1) {
    rx *= Math.sqrt(lambda);
    ry *= Math.sqrt(lambda);
  }
  const num = rx * rx * ry * ry - rx * rx * yp * yp - ry * ry * xp * xp;
  const den = rx * rx * yp * yp + ry * ry * xp * xp;
  const k = (large === sweep ? -1 : 1) * Math.sqrt(Math.max(0, num / den));
  const cxp = (k * rx * yp) / ry, cyp = (-k * ry * xp) / rx;
  const cx = cos * cxp - sin * cyp + (x1 + x2) / 2, cy = sin * cxp + cos * cyp + (y1 + y2) / 2;
  const angle = (ux, uy, vx, vy) => Math.atan2(ux * vy - uy * vx, ux * vx + uy * vy);
  const t1 = angle(1, 0, (xp - cxp) / rx, (yp - cyp) / ry);
  let dt = angle((xp - cxp) / rx, (yp - cyp) / ry, (-xp - cxp) / rx, (-yp - cyp) / ry);
  if (!sweep && dt > 0) dt -= 2 * Math.PI;
  if (sweep && dt < 0) dt += 2 * Math.PI;
  const n = Math.max(2, Math.ceil(Math.abs(dt) / (Math.PI / 12)));
  const out = [];
  for (let i = 1; i <= n; i++) {
    const t = t1 + (dt * i) / n;
    const ex = rx * Math.cos(t), ey = ry * Math.sin(t);
    out.push([cos * ex - sin * ey + cx, sin * ex + cos * ey + cy]);
  }
  return out;
}

// outlines returns the closed outlines of path data as lists of points.
export function outlines(d) {
  const tk = tokens(d);
  const out = [];
  let last = "";
  let i = 0, cmd = "", x = 0, y = 0, sx = 0, sy = 0, cx = 0, cy = 0, qx = 0, qy = 0, cur = [];
  const num = () => parseFloat(tk[i++]);
  const close = () => {
    if (cur.length > 2) out.push(cur);
    cur = [];
  };
  const cubic = (x1, y1, x2, y2, ex, ey) => {
    for (let s = 1; s <= 8; s++) {
      const t = s / 8, u = 1 - t;
      cur.push([u * u * u * x + 3 * u * u * t * x1 + 3 * u * t * t * x2 + t * t * t * ex,
        u * u * u * y + 3 * u * u * t * y1 + 3 * u * t * t * y2 + t * t * t * ey]);
    }
    cx = x2; cy = y2; x = ex; y = ey;
  };
  const quad = (x1, y1, ex, ey) => {
    for (let s = 1; s <= 6; s++) {
      const t = s / 6, u = 1 - t;
      cur.push([u * u * x + 2 * u * t * x1 + t * t * ex, u * u * y + 2 * u * t * y1 + t * t * ey]);
    }
    qx = x1; qy = y1; x = ex; y = ey;
  };
  while (i < tk.length) {
    if (/[A-Za-z]/.test(tk[i])) cmd = tk[i++];
    const rel = cmd === cmd.toLowerCase(), ox = rel ? x : 0, oy = rel ? y : 0;
    const prev = cmd;
    switch (cmd.toUpperCase()) {
      case "M":
        close();
        x = ox + num(); y = oy + num(); sx = x; sy = y;
        cur.push([x, y]);
        cmd = rel ? "l" : "L"; // further pairs are lines
        break;
      case "L": x = ox + num(); y = oy + num(); cur.push([x, y]); break;
      case "H": x = ox + num(); cur.push([x, y]); break;
      case "V": y = oy + num(); cur.push([x, y]); break;
      case "C": { const a = [ox + num(), oy + num(), ox + num(), oy + num(), ox + num(), oy + num()]; cubic(...a); break; }
      case "S": {
        const smooth = /[CcSs]/.test(last) ? [2 * x - cx, 2 * y - cy] : [x, y];
        const a = [ox + num(), oy + num(), ox + num(), oy + num()];
        cubic(smooth[0], smooth[1], ...a);
        break;
      }
      case "Q": { const a = [ox + num(), oy + num(), ox + num(), oy + num()]; quad(...a); break; }
      case "T": {
        const smooth = /[QqTt]/.test(last) ? [2 * x - qx, 2 * y - qy] : [x, y];
        quad(smooth[0], smooth[1], ox + num(), oy + num());
        break;
      }
      case "A": {
        const rx = num(), ry = num(), phi = num(), large = num(), sweep = num(), ex = ox + num(), ey = oy + num();
        cur.push(...arcPoints(x, y, rx, ry, phi, large, sweep, ex, ey));
        x = ex; y = ey;
        break;
      }
      case "Z":
        x = sx; y = sy;
        close();
        break;
      default:
        throw new Error(`path command ${cmd} not understood`);
    }
    last = prev;
  }
  close();
  return out;
}

function inside([px, py], poly) {
  let hit = false;
  for (let i = 0, j = poly.length - 1; i < poly.length; j = i++) {
    const [xi, yi] = poly[i], [xj, yj] = poly[j];
    if ((yi > py) !== (yj > py) && px < ((xj - xi) * (py - yi)) / (yj - yi) + xi) hit = !hit;
  }
  return hit;
}

// icon returns the Excalidraw elements of an SVG icon, scaled by scale,
// grouped as group: outlines outside any other first, filled with ink.
export function icon(svg, group, { scale = 3, ink = "#1e1e1e", seed = 1 } = {}) {
  const paths = [...svg.matchAll(/<path[^>]*\sd="([^"]+)"/g)].map((m) => m[1]);
  const polys = paths.flatMap(outlines);
  const depth = polys.map((p, i) => polys.filter((q, j) => j !== i && inside(p[0], q)).length);
  const order = polys.map((_, i) => i).sort((a, b) => depth[a] - depth[b] || a - b);
  return order.map((i, n) => {
    const pts = polys[i].map(([px, py]) => [px * scale, py * scale]);
    const x0 = Math.min(...pts.map((p) => p[0])), y0 = Math.min(...pts.map((p) => p[1]));
    const fill = depth[i] % 2 === 0 ? ink : "#ffffff";
    const first = pts[0];
    const points = [...pts, first].map(([px, py]) => [+(px - first[0]).toFixed(2), +(py - first[1]).toFixed(2)]);
    return {
      id: `${group}-${n + 1}`, type: "line", x: +first[0].toFixed(2), y: +first[1].toFixed(2),
      width: +(Math.max(...pts.map((p) => p[0])) - x0).toFixed(2), height: +(Math.max(...pts.map((p) => p[1])) - y0).toFixed(2),
      angle: 0, strokeColor: fill, backgroundColor: fill, fillStyle: "solid", strokeWidth: 0.5, strokeStyle: "solid",
      roughness: 0, opacity: 100, groupIds: [group], frameId: null, roundness: null, seed: seed + n, version: 1,
      versionNonce: 0, isDeleted: false, boundElements: null, updated: 0, link: null, locked: false,
      points, lastCommittedPoint: null, startBinding: null, endBinding: null, startArrowhead: null, endArrowhead: null,
    };
  });
}
