// Unit tests of what a selection may protect (protect.js, ADR-0088,
// ADR-0160): the words of one block, the blocks a selection touches, and
// the section a heading opens. They run in Node.js without a browser and
// without dependencies:
//
//   node --test "internal/web/jstest/*.test.mjs"
//
// The page itself is in e2e/smoke.cjs.
import { test } from "node:test";
import assert from "node:assert/strict";
import { sectionEnd, protectable, protectChoices, protectedBlocks } from "../static/assets/protect.js";

const body = [
  { kind: "heading", level: 1, text: "Budget" },     // 0
  { kind: "paragraph", text: "Salaries" },           // 1
  { kind: "heading", level: 2, text: "Plan" },       // 2
  { kind: "image", ref: "f1", text: "The plan" },    // 3
  { kind: "heading", level: 1, text: "Office" },     // 4
  { kind: "paragraph", text: "Open", marks: [{ type: "secret", start: 0, end: 4, ref: "s1" }] }, // 5
  { kind: "paragraph", text: "Done", secret: "s2" }, // 6
];

test("a section runs to the next heading of the same or a higher level", () => {
  assert.equal(sectionEnd(body, 0), 3);
  assert.equal(sectionEnd(body, 2), 3);
  assert.equal(sectionEnd(body, 4), 6);
  assert.equal(sectionEnd(body, 1), 1, "no heading: the block itself");
  assert.equal(sectionEnd([{ kind: "heading" }], 0), 0, "a heading alone");
});

test("blocks protected already or holding a passage are not protected whole", () => {
  assert.ok(protectable(body, 0, 3));
  assert.ok(!protectable(body, 4, 5), "a passage inside");
  assert.ok(!protectable(body, 6, 6), "protected already");
  assert.ok(!protectable(body, 3, 2), "an empty range");
  assert.ok(!protectable(body, 0, 99), "out of the body");
});

test("the choices of a cursor in a heading: the block and its section", () => {
  const got = protectChoices(body, null, { from: 0, to: 0 });
  assert.deepEqual(got.map((c) => [c.kind, c.request]), [
    ["blocks", { fromBlock: 0, toBlock: 0 }],
    ["section", { fromBlock: 0, toBlock: 3 }],
  ]);
});

test("selected words come first; a selection across blocks protects them", () => {
  const words = protectChoices(body, { block: 1, start: 0, end: 4 }, { from: 1, to: 1 });
  assert.deepEqual(words.map((c) => c.kind), ["passage", "blocks"]);
  assert.deepEqual(words[0].request, { block: 1, start: 0, end: 4 });
  const across = protectChoices(body, null, { from: 1, to: 3 });
  assert.deepEqual(across.map((c) => [c.kind, c.from, c.to]), [["blocks", 1, 3]]);
  assert.deepEqual(protectChoices(body, null, { from: 4, to: 4 }).map((c) => c.kind), ["blocks"], "a section holding a passage is no choice");
  assert.deepEqual(protectChoices(body, { block: 6, start: 0, end: 2 }, { from: 6, to: 6 }), [], "inside a protected block nothing");
  assert.deepEqual(protectChoices(body, { block: 1, start: 2, end: 2 }, null), [], "an empty selection");
});

test("the blocks of a protection", () => {
  assert.deepEqual(protectedBlocks(body, "s2"), [6]);
  assert.deepEqual(protectedBlocks(body, "none"), []);
});
