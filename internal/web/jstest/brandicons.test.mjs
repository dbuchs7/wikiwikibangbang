// The UI's symbols as files in docs/brand/icons are copies of what app.js
// and style.css draw. This test fails when the code changes a symbol and
// the files are not written anew with docs/brand/source/icons.mjs.
import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { render, sources } from "../../../docs/brand/source/icons.mjs";

const dir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../../docs/brand/icons");
const files = (sub) => fs.readdirSync(path.join(dir, sub)).filter((f) => f.endsWith(".svg")).map((f) => `${sub}/${f}`);

test("the symbol files are the symbols the UI draws", () => {
  const want = render(...sources());
  const have = [...files("navigation"), ...files("module-symbols")].sort();
  assert.deepEqual(have, Object.keys(want).sort(), "write them anew: node docs/brand/source/icons.mjs docs/brand/icons");
  for (const name of have) assert.equal(fs.readFileSync(path.join(dir, name), "utf8"), want[name], `${name} differs from the code`);
});

test("every module of the switcher and every start page symbol has a file", () => {
  const want = render(...sources());
  assert.ok(Object.keys(want).some((n) => n === "navigation/nav-starmap.svg"));
  assert.ok(Object.keys(want).some((n) => n === "module-symbols/module-starmap.svg"));
});
