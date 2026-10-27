// Unit tests of architecture sketches in a text (archparts.js, ADR-0145):
// the layers a block names and the picker checks, the address of a sketch,
// and which release a block shows. They run in Node.js without a browser
// and without dependencies:
//
//   node --test "internal/web/jstest/*.test.mjs"
//
// The block itself is in e2e/smoke.cjs.
import { test } from "node:test";
import assert from "node:assert/strict";
import { LAYERS, DEFAULT_LAYERS, canonical, checkedLayers, sketchPath, resolve } from "../static/assets/archparts.js";

test("layers are named each once, in the engine's order", () => {
  assert.deepEqual(LAYERS, ["strategy", "business", "application", "data", "technology"]);
  assert.deepEqual(DEFAULT_LAYERS, ["application", "technology"]);
  assert.deepEqual(canonical(["technology", "data", "technology", "physics", "Data"]), ["data", "technology"]);
  assert.deepEqual(canonical([]), []);
  assert.deepEqual(canonical(undefined), []);
});

test("the picker checks the block's layers, else the engine's default, among those the release records", () => {
  const all = ["strategy", "business", "application", "data", "technology"];
  assert.deepEqual(checkedLayers([], all), ["application", "technology"], "none named is the engine's default");
  assert.deepEqual(checkedLayers(undefined, ["application", "technology"]), ["application", "technology"]);
  assert.deepEqual(checkedLayers(["business", "data"], all), ["business", "data"]);
  assert.deepEqual(checkedLayers(["business", "data"], ["application", "technology"]), [], "a layer the release does not record is not checked");
  assert.deepEqual(checkedLayers([], ["business", "strategy"]), ["strategy", "business"], "a release without the default layers shows what it records");
  assert.deepEqual(checkedLayers([], ["technology", "application", "data"]), ["application", "technology"]);
});

test("a sketch's address names the release, or newest, and the layers in order", () => {
  assert.equal(sketchPath("atlas", "c46e", 0, []), "/api/engines/atlas/architectures/c46e/newest");
  assert.equal(sketchPath("atlas", "c46e", 3, ["technology", "business"]), "/api/engines/atlas/architectures/c46e/3?layers=business,technology");
  assert.equal(sketchPath("atlas.prod", "a/b c", 0, ["data", "evil&x=1"]), "/api/engines/atlas.prod/architectures/a%2Fb%20c/newest?layers=data");
  assert.equal(sketchPath("atlas", "c46e", -2, undefined), "/api/engines/atlas/architectures/c46e/newest");
});

test("a block shows the newest release, or its own, as the list offers it", () => {
  const list = [
    { application: "a1", name: "Abrechnung", releases: [{ version: 1 }] },
    { application: "c46e", name: "Bestellwesen", releases: [{ version: 3, recorded: ["application"] }, { version: 2 }] },
  ];
  assert.deepEqual(resolve(list, "c46e", 0), { app: list[1], rel: list[1].releases[0] });
  assert.deepEqual(resolve(list, "c46e", 2), { app: list[1], rel: list[1].releases[1] });
  assert.deepEqual(resolve(list, "c46e", 9), { app: list[1], rel: null }, "a release the list does not offer");
  assert.deepEqual(resolve(list, "gone", 0), { app: null, rel: null });
  assert.deepEqual(resolve([], "c46e", 0), { app: null, rel: null }, "a token that reaches no application lists none");
  assert.deepEqual(resolve(undefined, "c46e", 0), { app: null, rel: null });
  assert.deepEqual(resolve([{ application: "x", name: "Leer", releases: [] }], "x", 0), { app: { application: "x", name: "Leer", releases: [] }, rel: null });
});
