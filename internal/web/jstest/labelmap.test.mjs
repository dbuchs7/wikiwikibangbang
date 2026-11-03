// Unit tests of the labels' starmap (labelmap-graph.js, ADR-0165): the
// address, which labels the filters leave, how big a star, a line and the
// canvas are, and where the stars stand. They run in Node.js without a
// browser and without dependencies:
//
//   node --test "internal/web/jstest/*.test.mjs"
//
// The view itself is in e2e/smoke.cjs.
import { test } from "node:test";
import assert from "node:assert/strict";
import { HEIGHT, RADIUS, address, canvasHeight, frame, layout, lineWidth, neighbours, readState, shown, starRadius } from "../static/assets/labelmap-graph.js";

const labels = [
  { label: "ops", count: 9 }, { label: "security", count: 5 }, { label: "keys", count: 2 },
  { label: "meeting", count: 1 }, { label: "release", count: 4 },
];
const pairs = [
  { labels: ["ops", "security"], count: 4 }, { labels: ["keys", "security"], count: 2 },
  { labels: ["ops", "release"], count: 1 }, { labels: ["keys", "ops"], count: 1 },
];

test("the address holds the state and leaves defaults out", () => {
  assert.equal(address(readState(new URLSearchParams(""))), "#/labelmap");
  const state = readState(new URLSearchParams("space=PLAT&labels=ops,%20keys,ops,&near=off&min=3&q=sec"));
  assert.deepEqual(state, { space: "PLAT", labels: ["ops", "keys"], near: false, min: 3, q: "sec" });
  assert.equal(address(state), "#/labelmap?space=PLAT&labels=ops%2Ckeys&near=off&min=3&q=sec");
  assert.deepEqual(readState(new URLSearchParams(address(state).split("?")[1])), state);
  for (const bad of ["0", "-2", "x", "1"]) assert.equal(readState(new URLSearchParams("min=" + bad)).min, 1, bad);
});

test("without a filter every label is shown, or those carried often enough", () => {
  assert.deepEqual([...shown(labels, pairs).labels], ["ops", "security", "keys", "meeting", "release"]);
  assert.deepEqual([...shown(labels, pairs, { min: 4 }).labels], ["ops", "security", "release"]);
  assert.equal(shown(labels, pairs).focus.size, 0);
});

test("chosen labels are shown with their connected labels, or alone", () => {
  const near = shown(labels, pairs, { labels: ["keys"] });
  assert.deepEqual([...near.labels].sort(), ["keys", "ops", "security"]);
  assert.deepEqual([...near.focus], ["keys"]);
  assert.deepEqual([...shown(labels, pairs, { labels: ["keys"], near: false }).labels], ["keys"]);
  // A chosen label is shown whatever its count; its neighbours only if
  // carried often enough.
  assert.deepEqual([...shown(labels, pairs, { labels: ["keys"], min: 6 }).labels].sort(), ["keys", "ops"]);
  // A label no longer known is no choice.
  assert.deepEqual([...shown(labels, pairs, { labels: ["gone"], near: false }).labels], []);
});

test("a search finds labels by a word and joins the choice", () => {
  const found = shown(labels, pairs, { q: " SEC ", near: false });
  assert.deepEqual([...found.labels], ["security"]);
  const both = shown(labels, pairs, { q: "meet", labels: ["release"], near: false });
  assert.deepEqual([...both.labels].sort(), ["meeting", "release"]);
  assert.deepEqual([...shown(labels, pairs, { q: "sec" }).labels].sort(), ["keys", "ops", "security"]);
});

test("neighbours are the labels carried together, most shared first", () => {
  assert.deepEqual(neighbours(pairs, "ops"), [{ label: "security", count: 4 }, { label: "keys", count: 1 }, { label: "release", count: 1 }]);
  assert.deepEqual(neighbours(pairs, "meeting"), []);
});

test("a star's area grows with its count, a line's width with the items shared", () => {
  assert.equal(starRadius(9, 9), RADIUS.max);
  assert.equal(starRadius(0, 9), RADIUS.min);
  assert.equal(starRadius(1, 0), RADIUS.min);
  const quarter = starRadius(9 / 4, 9);
  assert.equal(quarter, RADIUS.min + (RADIUS.max - RADIUS.min) / 2);
  assert.ok(starRadius(2, 9) < starRadius(5, 9));
  assert.equal(lineWidth(4, 4), 5);
  assert.equal(lineWidth(1, 0), 1);
  assert.ok(lineWidth(1, 4) < lineWidth(2, 4));
});

test("the canvas is small for a few labels and grows with many, within bounds", () => {
  assert.equal(canvasHeight(0), HEIGHT.min);
  assert.equal(canvasHeight(1), HEIGHT.min);
  assert.ok(canvasHeight(10) < canvasHeight(40));
  assert.ok(canvasHeight(40) < canvasHeight(120));
  assert.equal(canvasHeight(5000), HEIGHT.max);
});

test("stars do not overlap, the biggest stands near the middle, and the layout is stable", () => {
  const stars = labels.map((l) => ({ label: l.label, r: starRadius(l.count, 9) }));
  const links = pairs.map((p) => ({ a: p.labels[0], b: p.labels[1], count: p.count }));
  const at = layout(stars, links);
  assert.equal(at.size, stars.length);
  for (let i = 0; i < stars.length; i++) {
    for (let j = i + 1; j < stars.length; j++) {
      const a = at.get(stars[i].label), b = at.get(stars[j].label);
      const d = Math.hypot(a.x - b.x, a.y - b.y);
      assert.ok(d >= stars[i].r + stars[j].r, `${stars[i].label} and ${stars[j].label} overlap`);
    }
  }
  const dist = (l) => Math.hypot(at.get(l).x, at.get(l).y);
  assert.ok(dist("ops") < dist("meeting"), "the most carried label stands nearer the middle than a lone one");
  assert.deepEqual([...layout(stars, links)], [...at]);
  assert.equal(layout([]).size, 0);
});

test("many labels are laid out without overlap too", () => {
  const stars = Array.from({ length: 150 }, (_, i) => ({ label: "l" + i, r: starRadius(150 - i, 150) }));
  const links = stars.slice(1).map((s, i) => ({ a: stars[i % 7].label, b: s.label, count: 1 + (i % 3) }));
  const at = layout(stars, links);
  let overlaps = 0;
  for (let i = 0; i < stars.length; i++) {
    for (let j = i + 1; j < stars.length; j++) {
      const a = at.get(stars[i].label), b = at.get(stars[j].label);
      if (Math.hypot(a.x - b.x, a.y - b.y) < stars[i].r + stars[j].r - 0.5) overlaps++;
    }
  }
  assert.equal(overlaps, 0);
});

test("the frame holds every star with its caption, keeps the canvas's proportions and is never tiny", () => {
  const one = frame([{ x: 0, y: 0, r: 30 }], { aspect: 2 });
  assert.ok(one.width >= 360);
  assert.equal(Math.round(one.width / one.height * 10) / 10, 2);
  assert.ok(one.x < -30 && one.x + one.width > 30 && one.y < -30 && one.y + one.height > 46);
  const wide = frame([{ x: -500, y: 0, r: 10 }, { x: 500, y: 0, r: 10 }], { aspect: 1 });
  assert.ok(wide.width >= 1020 && wide.height === wide.width);
  assert.deepEqual(frame([], { aspect: 1.5 }).width, 360);
});
