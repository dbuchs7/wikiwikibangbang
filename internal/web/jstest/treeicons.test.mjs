// Unit tests of the page tree's symbols (treeicons.js): which symbol an
// item shows, and that a stack draws its top symbol whole with two
// outlines behind it, inside the symbol's box. They run in Node.js without
// a browser and without dependencies:
//
//   node --test "internal/web/jstest/*.test.mjs"
//
// The tree itself is in e2e/smoke.cjs.
import { test } from "node:test";
import assert from "node:assert/strict";
import { treeIconKind, treeIconParts } from "../static/assets/treeicons.js";

// points returns the end points a path's commands name, in absolute
// coordinates; the paths use only M, H, V, A and relative h and l.
function points(d) {
  const out = [];
  let x = 0;
  let y = 0;
  for (const [, cmd, args] of d.matchAll(/([MHVAZhl])([^MHVAZhl]*)/g)) {
    const n = (args.match(/-?(?:\d+\.?\d*|\.\d+)/g) || []).map(Number);
    if (cmd === "M") [x, y] = n;
    else if (cmd === "H") x = n[0];
    else if (cmd === "V") y = n[0];
    else if (cmd === "A") [x, y] = n.slice(5);
    else if (cmd === "h") x += n[0];
    else if (cmd === "l") for (let i = 0; i < n.length; i += 2) [x, y] = [x + n[i], y + n[i + 1]];
    else continue;
    assert.ok(Number.isFinite(x) && Number.isFinite(y), `${cmd}${args} is not read`);
    out.push([x, y]);
  }
  return out;
}

// placed maps a point through a part's transform: translate, then scale.
function placed(part, [x, y]) {
  const m = part.transform?.match(/^translate\(([-\d.]+) ([-\d.]+)\) scale\(([\d.]+)\)$/);
  if (!m) return [x, y];
  const [tx, ty, s] = m.slice(1).map(Number);
  return [x * s + tx, y * s + ty];
}

test("a task type shows the task's symbol, every other type the page's", () => {
  const tasks = new Set(["task", "bug"]);
  assert.equal(treeIconKind("task", tasks), "task");
  assert.equal(treeIconKind("bug", tasks), "task");
  assert.equal(treeIconKind("page", tasks), "page");
  assert.equal(treeIconKind("catalog-entry", tasks), "page");
  assert.equal(treeIconKind("task", undefined), "page");
});

test("an item without items below it shows one symbol, untransformed", () => {
  for (const kind of ["page", "task"]) {
    const parts = treeIconParts(kind, false);
    assert.equal(parts.length, 1);
    assert.equal(parts[0].transform, undefined);
    assert.match(parts[0].d, /Z/, "the outline is closed");
  }
});

test("a stack draws two outlines behind the symbol, the symbol last", () => {
  for (const kind of ["page", "task"]) {
    const parts = treeIconParts(kind, true);
    assert.equal(parts.length, 3);
    assert.equal(parts[2].d, treeIconParts(kind, false)[0].d, "the top of the stack is the symbol itself");
    assert.ok(parts[2].transform, "the symbol is scaled onto the stack");
    for (const back of parts.slice(0, 2)) {
      assert.equal(back.transform, undefined);
      assert.doesNotMatch(back.d, /Z/, "an outline behind is open where the one in front covers it");
    }
  }
});

test("every stack fits the symbol's box", () => {
  for (const kind of ["page", "task"]) {
    for (const part of treeIconParts(kind, true)) {
      for (const p of points(part.d)) {
        const [x, y] = placed(part, p);
        assert.ok(x >= 0 && x <= 24 && y >= 0 && y <= 24, `${kind}: ${x},${y} lies outside the box`);
      }
    }
  }
});

test("no outline behind is drawn where the symbol in front covers it", () => {
  for (const kind of ["page", "task"]) {
    const [far, near, top] = treeIconParts(kind, true);
    const front = points(top.d).map((p) => placed(top, p));
    const [minX, maxX] = [Math.min(...front.map((p) => p[0])), Math.max(...front.map((p) => p[0]))];
    const [minY, maxY] = [Math.min(...front.map((p) => p[1])), Math.max(...front.map((p) => p[1]))];
    for (const part of [far, near]) {
      for (const [x, y] of points(part.d)) {
        const inside = x > minX + 1e-6 && x < maxX - 1e-6 && y > minY + 1e-6 && y < maxY - 1e-6;
        assert.ok(!inside, `${kind}: ${x},${y} lies under the symbol in front`);
      }
    }
  }
});
