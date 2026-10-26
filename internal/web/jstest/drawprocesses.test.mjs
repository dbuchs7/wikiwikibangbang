// Unit tests of processes in drawings (drawprocesses.js, ADR-0139): what a
// drawing's elements say they show, where the live ones lie in the saved
// picture, and how the picture carries them. They run in Node.js without a
// browser and without dependencies:
//
//   node --test "internal/web/jstest/*.test.mjs"
//
// The drawing page and the page that shows a drawing are in e2e/smoke.cjs.
import { test } from "node:test";
import assert from "node:assert/strict";
import { crc32, PADDING, processOf, rootsOf, regionsOf, withRegions, regionsIn } from "../static/assets/drawprocesses.js";

const ascii = (s) => new TextEncoder().encode(s);

// chunk builds one PNG chunk with its length and checksum.
function chunk(type, data) {
  const out = new Uint8Array(12 + data.length);
  const view = new DataView(out.buffer);
  view.setUint32(0, data.length);
  out.set(ascii(type), 4);
  out.set(data, 8);
  view.setUint32(8 + data.length, crc32(out.subarray(4, 8 + data.length)));
  return out;
}

const concat = (...parts) => {
  const out = new Uint8Array(parts.reduce((n, p) => n + p.length, 0));
  let at = 0;
  for (const p of parts) {
    out.set(p, at);
    at += p.length;
  }
  return out;
};

const signature = new Uint8Array([137, 80, 78, 71, 13, 10, 26, 10]);
const ihdr = chunk("IHDR", new Uint8Array([0, 0, 0, 1, 0, 0, 0, 1, 8, 6, 0, 0, 0]));
const scene = chunk("tEXt", concat(ascii("application/vnd.excalidraw+json"), new Uint8Array([0]), ascii('{"scene":1}')));
const png = concat(signature, ihdr, chunk("IDAT", new Uint8Array([1, 2, 3])), scene, chunk("IEND", new Uint8Array()));

const live = (over = {}) => ({
  id: "a", type: "image", x: 110, y: 60, width: 200, height: 100, angle: 0, scale: [1, 1], isDeleted: false,
  customData: { wikiwikibangbang: { kind: "process", engine: "atlas", process: "order", name: "Bestellung prüfen", operations: true } },
  ...over,
});

test("crc32 is PNG's checksum", () => {
  assert.equal(crc32(ascii("IEND")), 0xae426082);
});

test("an element says which process it shows, and only a valid one counts", () => {
  assert.deepEqual(processOf(live()), { kind: "process", engine: "atlas", process: "order", name: "Bestellung prüfen", operations: true });
  assert.deepEqual(processOf({ type: "image", customData: { wikiwikibangbang: { kind: "bpmn", name: "Mine", xml: "<definitions/>" } } }),
    { kind: "bpmn", name: "Mine", xml: "<definitions/>" });
  for (const data of [
    { kind: "process", engine: "Atlas", process: "order" },
    { kind: "process", engine: "atlas", process: "1order" },
    { kind: "process", engine: "atlas", process: "a b" },
    { kind: "bpmn", xml: "" },
    { kind: "other" },
    "process",
  ]) {
    assert.equal(processOf({ type: "image", customData: { wikiwikibangbang: data } }), null, JSON.stringify(data));
  }
  assert.equal(processOf({ type: "rectangle", customData: live().customData }), null, "a shape that is no picture");
  assert.equal(processOf({ type: "image" }), null);
});

test("what lies in a frame is measured by its frame, as Excalidraw does", () => {
  const els = [{ id: "f", type: "frame" }, { id: "in", frameId: "f" }, { id: "out" }, { id: "lost", frameId: "gone" }];
  assert.deepEqual(rootsOf(els).map((e) => e.id), ["f", "out", "lost"]);
});

test("a live process lies where the picture shows it", () => {
  // The scene spans 100..500 × 50..250; the picture adds the padding on each side.
  const size = { width: 400 + 2 * PADDING, height: 200 + 2 * PADDING };
  const [r] = regionsOf([live()], [100, 50, 500, 250], size);
  // Shares are kept to five places: a hundred-thousandth of the picture.
  const five = (n) => Math.round(n * 1e5) / 1e5;
  assert.deepEqual(r, { x: five(20 / 420), y: five(20 / 220), w: five(200 / 420), h: five(100 / 220), engine: "atlas", process: "order", name: "Bestellung prüfen", operations: true });

  // A frame's name drawn above it makes the picture taller at the top only.
  const [named] = regionsOf([live()], [100, 50, 500, 250], { width: size.width, height: size.height + 30 });
  assert.equal(named.y, 50 / 250);

  // A model of one's own, a turned or mirrored picture, and a deleted one
  // are shown as drawn: no region.
  const own = { ...live(), customData: { wikiwikibangbang: { kind: "bpmn", name: "", xml: "<x/>" } } };
  assert.deepEqual(regionsOf([own, live({ angle: 0.3 }), live({ scale: [-1, 1] }), live({ isDeleted: true })], [100, 50, 500, 250], size), []);
});

test("the picture carries its regions after its header, and reads them back", () => {
  const regions = [{ x: 0.1, y: 0.2, w: 0.5, h: 0.25, engine: "atlas", process: "order", name: "Bestellung prüfen", operations: false }];
  const out = withRegions(png, regions);
  assert.deepEqual(regionsIn(out), regions);
  // Directly after IHDR, so the first bytes of a picture are enough.
  assert.equal(new TextDecoder().decode(out.subarray(37, 41)), "tEXt");
  assert.deepEqual(regionsIn(out.subarray(0, 300)), regions);
  // Excalidraw's own chunk stays untouched.
  assert.ok(new TextDecoder().decode(out).includes('{"scene":1}'));
  // Saving again replaces the regions, and none removes them.
  assert.deepEqual(regionsIn(withRegions(out, [{ ...regions[0], operations: true }])), [{ ...regions[0], operations: true }]);
  assert.deepEqual(withRegions(out, []), png);
  assert.deepEqual(regionsIn(png), []);
});

test("what a picture says is checked before it is believed", () => {
  const bad = (regions) => {
    const text = concat(ascii("wikiwikibangbang.processes"), new Uint8Array([0]), ascii(JSON.stringify({ v: 1, regions })));
    return concat(signature, ihdr, chunk("tEXt", text), chunk("IEND", new Uint8Array()));
  };
  const ok = { x: 0, y: 0, w: 1, h: 1, engine: "atlas", process: "order", name: "", operations: false };
  assert.deepEqual(regionsIn(bad([ok])), [ok]);
  for (const r of [{ ...ok, x: -0.1 }, { ...ok, w: 0 }, { ...ok, x: 0.5, w: 0.6 }, { ...ok, engine: "../x" }, { ...ok, process: "<a>" }, { ...ok, h: "1" }]) {
    assert.deepEqual(regionsIn(bad([r])), [], JSON.stringify(r));
  }
  // A damaged chunk is not read.
  const damaged = bad([ok]);
  damaged[45] ^= 1;
  assert.deepEqual(regionsIn(damaged), []);
  assert.deepEqual(regionsIn(ascii("not a picture")), []);
  assert.throws(() => withRegions(ascii("not a picture"), [ok]), /PNG/);
});
