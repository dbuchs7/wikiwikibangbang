// Lists the packages a bundle takes code from, one "name version license"
// per line, read from esbuild's account of the bundle's inputs (its
// metafile) and package-lock.json. With --texts it follows the list with
// each package's license text. build.sh writes the license files of the
// vendored bundles with it, so they name what is shipped, no more.
import { readFileSync, readdirSync } from "node:fs";

const [metafile, texts] = process.argv.slice(2);
const meta = JSON.parse(readFileSync(metafile, "utf8"));
const lock = JSON.parse(readFileSync("package-lock.json", "utf8"));
const names = new Set();
for (const input of Object.keys(meta.inputs)) {
  const m = input.match(/node_modules\/((?:@[^/]+\/)?[^/]+)\//g);
  if (m) names.add(m[m.length - 1].replace(/^node_modules\//, "").replace(/\/$/, ""));
}
const sorted = [...names].sort();
const license = (n) => {
  const v = lock.packages["node_modules/" + n] || {};
  const pkg = JSON.parse(readFileSync(`node_modules/${n}/package.json`, "utf8"));
  return v.license || pkg.license || (pkg.licenses || []).map((l) => l.type).join(" OR ");
};
const out = sorted.map((n) => `${n} ${(lock.packages["node_modules/" + n] || {}).version} ${license(n)}`);
if (texts === "--texts") {
  for (const n of sorted) {
    const dir = `node_modules/${n}`;
    const file = readdirSync(dir).find((f) => /^(licen[cs]e|copying)/i.test(f));
    out.push("", "== " + n, "");
    if (file) out.push(readFileSync(`${dir}/${file}`, "utf8").trim());
    // The npm package of Excalidraw carries no license file; this is the
    // text of the LICENSE in github.com/excalidraw/excalidraw.
    else if (n === "@excalidraw/excalidraw") out.push(readFileSync("node_modules/react/LICENSE", "utf8").trim()
      .replace(/^.*\n/, "MIT License\n").replace(/Copyright \(c\) [^\n]*/, "Copyright (c) 2020 Excalidraw"));
    else out.push(`(the package carries no license file; its package.json names the ${license(n)} license, whose text is the one of react in this file)`);
  }
}
console.log(out.join("\n"));
