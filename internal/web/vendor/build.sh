#!/bin/sh
# Rebuilds the vendored bundles in ../static/vendor from the pinned packages:
# prosemirror.js, the editor (ADR-0032), excalidraw.js with its stylesheet,
# chunks and fonts, the drawing editor (ADR-0053, ADR-0126), and
# bpmn-viewer.js and bpmn-modeler.js with their stylesheets and icon font in
# bpmn/, the BPMN viewer and modeler (ADR-0129).
# Their license files name the packages each bundle takes code from
# (bundled.mjs). Run after changing a version in package.json:
#
#   cd internal/web/vendor && npm ci && sh build.sh
#
# Commit the bundles, their license files and package-lock.json together,
# and update THIRD_PARTY_NOTICES.md; internal/web tests check both.
set -eu
out=../static/vendor
npx esbuild entry.js --bundle --format=esm --target=es2020 --minify \
  --legal-comments=none --metafile=editor-meta.json --outfile="$out/prosemirror.js"
{
  echo "The bundle prosemirror.js contains these packages, each under the MIT license below."
  echo
  node bundled.mjs editor-meta.json
  echo
  cat node_modules/prosemirror-model/LICENSE
} > "$out/prosemirror.LICENSE.txt"
rm editor-meta.json

# The drawing bundle (ADR-0053, ADR-0126): excalidraw.js and excalidraw.css,
# and the chunks they load when needed in excalidraw-assets/. Excalidraw's
# Mermaid import is replaced (no-mermaid.js), and of its translations only
# the languages of the UI are kept (English is built in).
rm -rf "$out/excalidraw-assets" "$out/fonts" "$out"/excalidraw.*
npx esbuild draw-entry.js --bundle --splitting --format=esm --target=es2020 --minify \
  --legal-comments=none --define:process.env.NODE_ENV='"production"' \
  --define:process.env.IS_PREACT='"false"' --alias:@excalidraw/mermaid-to-excalidraw=./no-mermaid.js --conditions=production \
  --external:'./fonts/*' --metafile=draw-meta.json --entry-names=excalidraw --chunk-names='excalidraw-assets/[name]-[hash]' --outdir="$out"
for f in node_modules/@excalidraw/excalidraw/dist/prod/locales/*.js; do
  name=$(basename "$f" .js)
  case "$name" in de-DE-*|en-*|percentages-*) ;; *) rm "$out/excalidraw-assets/$name"-*.js ;; esac
done
# Excalidraw lists a CDN after the path the page sets for its fonts. The
# content security policy blocks it, but browsers still try it; the bundle
# names the installation's own path only. The build fails if the code it
# replaces is not found exactly once, as after an update that changed it.
node -e '
  const fs = require("fs");
  const dir = process.argv[1];
  const re = /return ([A-Za-z_$][\w$]*)\.push\(new URL\([A-Za-z_$][\w$]*,[A-Za-z_$][\w$]*\.ASSETS_FALLBACK_URL\)\),\1\}/g;
  let n = 0;
  for (const f of fs.readdirSync(dir)) {
    const p = dir + "/" + f;
    const src = fs.readFileSync(p, "utf8");
    const out = src.replace(re, (_, list) => { n++; return `return ${list}}`; });
    if (out !== src) fs.writeFileSync(p, out);
  }
  if (n !== 1) { console.error(`the CDN fallback for fonts was found ${n} times, not once`); process.exit(1); }
' "$out/excalidraw-assets"
# Its fonts come from this server, not from a CDN: the page sets
# EXCALIDRAW_ASSET_PATH, and the stylesheet names them relative to itself.
# Xiaolai, the fallback for Chinese, Japanese and Korean (13 MB), is left out.
dist=node_modules/@excalidraw/excalidraw/dist/prod
mkdir -p "$out/fonts"
for d in "$dist"/fonts/*; do
  [ "$(basename "$d")" = Xiaolai ] || cp -r "$d" "$out/fonts/"
done
# Every package the bundle took code from, with its version, license and
# license text.
{
  echo "The bundle excalidraw.js, excalidraw.css and the files in excalidraw-assets/"
  echo "contain code of these packages, each under the license named; their license"
  echo "texts follow. The fonts in fonts/ (Excalifont, Virgil, Cascadia, Assistant,"
  echo "Liberation Sans, Lilita One and Nunito) are under the SIL Open Font License"
  echo "1.1, Comic Shanns under the MIT license."
  echo
  node bundled.mjs draw-meta.json --texts
} > "$out/excalidraw.LICENSE.txt"
rm draw-meta.json

# The BPMN viewer and modeler (ADR-0129): two bundles, so a page that shows a
# diagram does not load the modeler, with bpmn-js's stylesheets and icon font
# in bpmn/, copied as published. The watermark code is part of the bundles,
# unchanged, as the bpmn.io license requires. The modeler holds all the
# viewer does, so its account names the packages of both.
npx esbuild bpmn-entry.js --bundle --format=esm --target=es2020 --minify \
  --legal-comments=none --outfile="$out/bpmn-viewer.js"
npx esbuild bpmn-modeler-entry.js --bundle --format=esm --target=es2020 --minify \
  --legal-comments=none --metafile=bpmn-meta.json --outfile="$out/bpmn-modeler.js"
bpmn=node_modules/bpmn-js/dist/assets
rm -rf "$out/bpmn"
mkdir -p "$out/bpmn/bpmn-font/css" "$out/bpmn/bpmn-font/font"
cp "$bpmn/bpmn-js.css" "$bpmn/diagram-js.css" "$out/bpmn/"
cp "$bpmn/bpmn-font/css/bpmn.css" "$out/bpmn/bpmn-font/css/"
cp "$bpmn/bpmn-font/font/bpmn.woff2" "$bpmn/bpmn-font/font/bpmn.woff" "$out/bpmn/bpmn-font/font/"
{
  echo "The bundles bpmn-viewer.js and bpmn-modeler.js and the files in bpmn/ contain"
  echo "code of these packages, each under the license named; their license texts"
  echo "follow. bpmn-js is under the bpmn.io license: the MIT terms with the condition"
  echo "that the code displaying the bpmn.io watermark in rendered diagrams is neither"
  echo "removed nor changed, and that the watermark stays fully visible. path-"
  echo "intersection carries a notice for the code it derives from Snap.svg, at the end."
  echo
  node bundled.mjs bpmn-meta.json --texts
  echo; echo "== path-intersection (NOTICE)"; echo
  cat node_modules/path-intersection/NOTICE
} > "$out/bpmn.LICENSE.txt"
rm bpmn-meta.json
