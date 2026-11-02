// icons.mjs writes the web UI's symbols as SVG files: the module switcher's
// line icons (`modules` in app.js) and the start page's coloured module
// symbols (`moduleGlyphs` in app.js, their colours in style.css). The UI
// draws them inline; the files are copies for documents and slides, and
// internal/web/jstest/brandicons.test.mjs keeps them equal to the code.
//
//   node docs/brand/source/icons.mjs docs/brand/icons
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

// literal returns the object or array literal that follows start in src.
function literal(src, start) {
  const at = src.indexOf(start);
  if (at < 0) throw new Error(`${start} not found in app.js`);
  const open = at + start.length - 1;
  let depth = 0;
  for (let i = open; i < src.length; i++) {
    const c = src[i];
    if (c === "[" || c === "{") depth++;
    else if (c === "]" || c === "}") {
      depth--;
      if (depth === 0) return vm.runInNewContext("(" + src.slice(open, i + 1) + ")");
    }
  }
  throw new Error(`${start} is not closed in app.js`);
}

const escape = (v) => String(v).replace(/&/g, "&amp;").replace(/"/g, "&quot;").replace(/</g, "&lt;");

// render returns the files, by relative path, that the app's and the
// stylesheet's source text describe.
export function render(appSource, cssSource) {
  const modules = literal(appSource, "const modules = [");
  const glyphs = literal(appSource, "const moduleGlyphs = {");
  const prop = (kind, name) => cssSource.match(new RegExp(`\\.module-${kind} \\{[^}]*--${name}: ([^;]+);`))?.[1].trim();
  const files = {};
  for (const m of modules) {
    files[`navigation/nav-${m.id}.svg`] = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" width="24" height="24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="${escape(m.icon)}"/></svg>\n`;
  }
  for (const [kind, parts] of Object.entries(glyphs)) {
    const ground = prop(kind, "module");
    if (!ground) throw new Error(`no colour for .module-${kind} in style.css`);
    // A gradient ground draws its ink in the colour named for it.
    const stops = ground.startsWith("linear-gradient") ? ground.match(/#[0-9a-fA-F]{3,8}/g) : null;
    const ink = prop(kind, "module-ink") || ground;
    const defs = stops ? `<defs><linearGradient id="ground" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="${stops[0]}"/><stop offset="1" stop-color="${stops[1]}"/></linearGradient></defs>` : "";
    const marks = parts.map(([tag, attrs]) => {
      const a = { ...attrs };
      if (a.class === "module-ink") a.fill = ink;
      if (a.class === "module-stroke") a.stroke = ink;
      delete a.class;
      return `<${tag} ${Object.entries(a).map(([k, v]) => `${k}="${escape(v)}"`).join(" ")}/>`;
    }).join("");
    files[`module-symbols/module-${kind}.svg`] = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 36 36" width="72" height="72">${defs}<rect width="36" height="36" rx="10" fill="${stops ? "url(#ground)" : ground}"/><g transform="translate(6 6)">${marks}</g></svg>\n`;
  }
  return files;
}

// sources reads app.js and style.css from the repository.
export function sources() {
  const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");
  const assets = path.join(root, "internal/web/static/assets");
  return [fs.readFileSync(path.join(assets, "app.js"), "utf8"), fs.readFileSync(path.join(assets, "style.css"), "utf8")];
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const out = process.argv[2];
  if (!out) throw new Error("usage: node docs/brand/source/icons.mjs <directory>");
  for (const [name, svg] of Object.entries(render(...sources()))) {
    fs.mkdirSync(path.dirname(path.join(out, name)), { recursive: true });
    fs.writeFileSync(path.join(out, name), svg);
  }
}
