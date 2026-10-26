// Lists the packages a bundle takes code from, one "name version license"
// per line, read from esbuild's account of the bundle's inputs (its
// metafile) and package-lock.json. With --texts it follows the list with
// each package's license text. build.sh writes the license files of the
// vendored bundles with it, so they name what is shipped, no more.
import { readFileSync, readdirSync } from "node:fs";

const [metafile, texts] = process.argv.slice(2);
const meta = JSON.parse(readFileSync(metafile, "utf8"));
const lock = JSON.parse(readFileSync("package-lock.json", "utf8"));
// A package nested below another (node_modules/a/node_modules/b) may be
// another version than the one at the top: it is looked up by its whole
// path, and each version of a package is named once.
const at = new Map();
for (const input of Object.keys(meta.inputs)) {
  const m = input.match(/^(?:.*\/)?node_modules\/(?:@[^/]+\/)?[^/]+/);
  if (!m) continue;
  const path = m[0].slice(m[0].indexOf("node_modules/"));
  const key = path.replace(/^.*node_modules\//, "") + " " + (lock.packages[path] || {}).version;
  if (!at.has(key)) at.set(key, path);
}
const sorted = [...at.keys()].sort();
const nameOf = (k) => k.split(" ")[0];
const license = (k) => {
  const v = lock.packages[at.get(k)] || {};
  const pkg = JSON.parse(readFileSync(`${at.get(k)}/package.json`, "utf8"));
  return v.license || pkg.license || (pkg.licenses || []).map((l) => l.type).join(" OR ");
};
const out = sorted.map((k) => `${nameOf(k)} ${(lock.packages[at.get(k)] || {}).version} ${license(k)}`);
if (texts === "--texts") {
  for (const k of sorted) {
    const n = nameOf(k);
    const dir = at.get(k);
    const file = readdirSync(dir).find((f) => /^(licen[cs]e|copying)/i.test(f));
    out.push("", "== " + n, "");
    if (file) out.push(readFileSync(`${dir}/${file}`, "utf8").trim());
    // The npm package of Excalidraw carries no license file; this is the
    // text of the LICENSE in github.com/excalidraw/excalidraw.
    else if (n === "@excalidraw/excalidraw") out.push(readFileSync("node_modules/react/LICENSE", "utf8").trim()
      .replace(/^.*\n/, "MIT License\n").replace(/Copyright \(c\) [^\n]*/, "Copyright (c) 2020 Excalidraw"));
    else out.push(`(the package carries no license file; its package.json names the ${license(k)} license, whose text is the one of react in this file)`);
  }
}
console.log(out.join("\n"));
