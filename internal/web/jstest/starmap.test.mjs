// Unit tests of the starmap's graph (starmap-graph.js, ADR-0157): the
// node kinds and edge groups, what the filters keep with one hop of
// context, a node's neighbourhood, and a layout that is the same for the
// same map. They run in Node.js without a browser and without
// dependencies:
//
//   node --test "internal/web/jstest/*.test.mjs"
//
// The view itself is in e2e/smoke.cjs.
import { test } from "node:test";
import assert from "node:assert/strict";
import { GROUPS, filter, groupOf, kinds, kindOf, layout, matches, neighbourhood, size, toneOf } from "../static/assets/starmap-graph.js";

const graph = {
  nodes: [
    { id: "a", kind: "item", key: "PLAT-1", title: "Home", type: "page", space: "PLAT", labels: ["runbook"] },
    { id: "b", kind: "item", key: "PLAT-2", title: "Patch the kernel", type: "task", space: "PLAT", labels: ["runbook"] },
    { id: "c", kind: "item", key: "PLAT-3", title: "Größe", type: "page", space: "PLAT" },
    { id: "x", kind: "item", key: "OPS-1", title: "Ops guide", type: "page", space: "OPS", outside: true },
    { id: "picture:a/1", kind: "picture", item: "a", name: "logo.png", space: "PLAT" },
    { id: "label:runbook", kind: "label", name: "runbook" },
  ],
  edges: [
    { from: "a", to: "b", kind: "parent" },
    { from: "b", to: "c", kind: "mentions" },
    { from: "c", to: "x", kind: "relation", type: "implements" },
    { from: "picture:a/1", to: "a", kind: "picture" },
    { from: "label:runbook", to: "a", kind: "label" },
    { from: "label:runbook", to: "b", kind: "label" },
  ],
};

test("edges fall into four groups, nodes into their type, pictures and labels", () => {
  assert.deepEqual(Object.keys(GROUPS), ["tree", "relations", "pictures", "labels"]);
  assert.equal(groupOf("parent"), "tree");
  assert.equal(groupOf("embeds"), "relations");
  assert.equal(groupOf("copy"), "pictures");
  assert.equal(groupOf("label"), "labels");
  assert.equal(kindOf(graph.nodes[1]), "type:task");
  assert.equal(kindOf(graph.nodes[4]), "picture");
  assert.deepEqual(kinds(graph.nodes), [{ key: "type:page", type: "page" }, { key: "type:task", type: "task" }, { key: "picture" }, { key: "label" }, { key: "outside" }]);
  assert.deepEqual(kinds(graph.nodes.slice(0, 2)).map((k) => k.key), ["type:page", "type:task"], "only the kinds the map has");
  const tone = toneOf(graph.nodes);
  assert.equal(tone("page"), 0);
  assert.equal(tone("task"), 1);
});

test("a query matches title, key, label and name, every word, without regard to case and accents", () => {
  assert.ok(matches(graph.nodes[1], "kernel patch"));
  assert.ok(matches(graph.nodes[1], "plat-2"));
  assert.ok(matches(graph.nodes[0], "RUNBOOK"));
  assert.ok(matches(graph.nodes[2], "grosse") === false, "ß is not ss");
  assert.ok(matches(graph.nodes[2], "GRÖ"));
  assert.ok(matches({ ...graph.nodes[2], title: "Café" }, "cafe"));
  assert.ok(matches(graph.nodes[4], "logo"));
  assert.ok(!matches(graph.nodes[1], "kernel home"));
  assert.ok(!matches(graph.nodes[1], "  "));
});

test("without filters everything shows", () => {
  const f = filter(graph);
  assert.equal(f.nodes.size, graph.nodes.length);
  assert.deepEqual(f.edges, [0, 1, 2, 3, 4, 5]);
  assert.equal(f.matches.size, 0);
});

test("a query keeps the matches and one hop of context", () => {
  const f = filter(graph, { query: "kernel" });
  assert.deepEqual([...f.matches], ["b"]);
  assert.deepEqual([...f.nodes].sort(), ["a", "b", "c", "label:runbook"], "b's neighbours, not theirs");
  assert.deepEqual(f.edges, [0, 1, 4, 5], "the edges whose ends both show");
  const none = filter(graph, { query: "nothing like it" });
  assert.equal(none.nodes.size, 0);
  assert.deepEqual(none.edges, []);
});

test("a kind switched off hides its nodes and their edges; an edge group switched off hides its edges and the context they gave", () => {
  const pages = filter(graph, { hidden: new Set(["type:page"]) });
  assert.deepEqual([...pages.nodes].sort(), ["b", "label:runbook", "picture:a/1"]);
  assert.deepEqual(pages.edges, [5]);
  const outside = filter(graph, { hidden: new Set(["outside"]) });
  assert.ok(!outside.nodes.has("x"));
  assert.ok(outside.nodes.has("c"));
  const tree = filter(graph, { hidden: new Set(["tree", "labels"]) });
  assert.deepEqual(tree.edges, [1, 2, 3]);
  const context = filter(graph, { query: "kernel", hidden: new Set(["labels", "tree"]) });
  assert.deepEqual([...context.nodes].sort(), ["b", "c"], "the context comes through edges shown only");
});

test("a node's neighbourhood is it and what a shown edge joins to it", () => {
  assert.deepEqual([...neighbourhood(graph, "b")].sort(), ["a", "b", "c", "label:runbook"]);
  assert.deepEqual([...neighbourhood(graph, "b", [1])].sort(), ["b", "c"]);
  assert.deepEqual([...neighbourhood(graph, "x")].sort(), ["c", "x"]);
});

test("the layout is the same for the same map, inside the canvas, and moves no node onto another", () => {
  const one = layout(graph), two = layout(structuredClone(graph));
  assert.deepEqual([...one], [...two]);
  const side = size(graph.nodes.length);
  assert.equal(side, 800);
  for (const { x, y } of one.values()) assert.ok(x >= 20 && x <= side - 20 && y >= 20 && y <= side - 20, `${x},${y}`);
  const points = [...one.values()];
  for (let i = 0; i < points.length; i++) {
    for (let j = i + 1; j < points.length; j++) {
      assert.ok(Math.hypot(points[i].x - points[j].x, points[i].y - points[j].y) > 20, "nodes apart");
    }
  }
  // Pinned for the fixed input: a change of the simulation shows here.
  assert.deepEqual(one.get("a"), layout(graph).get("a"));
  const pinned = Object.fromEntries([...one].map(([id, p]) => [id, [p.x, p.y]]));
  assert.deepEqual(pinned, PINNED);
  // A joined pair sits closer than the farthest pair.
  const d = (p, q) => Math.hypot(one.get(p).x - one.get(q).x, one.get(p).y - one.get(q).y);
  assert.ok(d("a", "b") < d("picture:a/1", "x"));
  assert.ok(size(10000) > 800);
  assert.equal(layout({ nodes: [], edges: [] }).size, 0);
});

const PINNED = { "a": [448.2, 414.4], "b": [358.2, 435.1], "c": [401, 372.5], "x": [422.5, 480.9], "picture:a/1": [334.5, 367.4], "label:runbook": [436.2, 329.2] };
