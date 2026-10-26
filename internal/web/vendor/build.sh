#!/bin/sh
# Rebuilds the vendored bundles in ../static/vendor from the pinned packages:
# prosemirror.js, the editor (ADR-0032), excalidraw.js with the files it
# loads from excalidraw-assets/, the drawing editor (ADR-0053), and
# bpmn-viewer.js and bpmn-modeler.js with their stylesheets and icon font in
# bpmn/, the BPMN viewer and modeler (ADR-0126). Run after changing a
# version in package.json:
#
#   cd internal/web/vendor && npm ci && sh build.sh
#
# Commit the bundles, their license files and package-lock.json together,
# and update THIRD_PARTY_NOTICES.md; internal/web tests check both.
set -eu
out=../static/vendor
# drawing lists the packages of the drawing bundle, diagram those of the BPMN
# bundles; the rest are the editor's.
drawing='@excalidraw/excalidraw react react-dom scheduler loose-envify js-tokens'
diagram='bpmn-js bpmn-moddle diagram-js diagram-js-direct-editing @bpmn-io/diagram-js-ui didi domify htm ids inherits-browser min-dash min-dom moddle moddle-xml object-refs path-intersection preact saxen tiny-svg clsx'
packages() {
  DRAWING="$drawing" DIAGRAM="$diagram" WANT="$1" node -e '
    const lock = require("./package-lock.json");
    const group = (name) => process.env.DRAWING.split(" ").includes(name) ? "drawing"
      : process.env.DIAGRAM.split(" ").includes(name) ? "diagram" : "editor";
    for (const [p, v] of Object.entries(lock.packages)) {
      if (p === "" || v.dev) continue;
      const name = p.replace(/^node_modules\//, "");
      if (group(name) === process.env.WANT) console.log(`${name} ${v.version} ${v.license}`);
    }'
}

npx esbuild entry.js --bundle --format=esm --target=es2020 --minify \
  --legal-comments=none --outfile="$out/prosemirror.js"
{
  echo "The bundle prosemirror.js contains these packages, each under the MIT license below."
  echo
  packages editor
  echo
  cat node_modules/prosemirror-model/LICENSE
} > "$out/prosemirror.LICENSE.txt"

npx esbuild draw-entry.js --bundle --format=esm --target=es2020 --minify \
  --legal-comments=none --define:process.env.NODE_ENV='"production"' \
  --define:process.env.IS_PREACT='"false"' --outfile="$out/excalidraw.js"
# Excalidraw loads its fonts, a chunk of libraries and its translations at
# run time; only the languages of the UI are kept (English is built in).
assets=node_modules/@excalidraw/excalidraw/dist/excalidraw-assets
rm -rf "$out/excalidraw-assets"
mkdir -p "$out/excalidraw-assets/locales"
cp "$assets"/*.woff2 "$assets"/vendor-*.js "$out/excalidraw-assets/"
cp "$assets"/locales/de-DE-json-*.js "$out/excalidraw-assets/locales/"
{
  echo "The bundle excalidraw.js and the files in excalidraw-assets/ contain these"
  echo "packages, each under the MIT license, followed by the license texts and the"
  echo "notices of the libraries Excalidraw itself bundles. The fonts Virgil,"
  echo "Cascadia and Assistant are under the SIL Open Font License 1.1."
  echo
  packages drawing
  # The npm package of Excalidraw carries no license file; this is the text
  # of the LICENSE in github.com/excalidraw/excalidraw.
  echo; echo "== @excalidraw/excalidraw"; echo
  sed -e '1s/.*/MIT License/' -e '3s/.*/Copyright (c) 2020 Excalidraw/' node_modules/react/LICENSE
  for p in react react-dom; do
    echo; echo "== $p"; echo
    cat "node_modules/$p/LICENSE"
  done
  echo; echo "== Libraries bundled by Excalidraw"; echo
  cat node_modules/@excalidraw/excalidraw/dist/excalidraw.production.min.js.LICENSE.txt
  cat "$assets"/vendor-*.js.LICENSE.txt
} > "$out/excalidraw.LICENSE.txt"

# The BPMN viewer and modeler (ADR-0126): two bundles, so a page that shows a
# diagram does not load the modeler. The stylesheets and the icon font are
# copied as published; the watermark code is part of the bundles, unchanged,
# as the bpmn.io license requires.
npx esbuild bpmn-entry.js --bundle --format=esm --target=es2020 --minify \
  --legal-comments=none --outfile="$out/bpmn-viewer.js"
npx esbuild bpmn-modeler-entry.js --bundle --format=esm --target=es2020 --minify \
  --legal-comments=none --outfile="$out/bpmn-modeler.js"
bpmn=node_modules/bpmn-js/dist/assets
rm -rf "$out/bpmn"
mkdir -p "$out/bpmn/bpmn-font/css" "$out/bpmn/bpmn-font/font"
cp "$bpmn/bpmn-js.css" "$bpmn/diagram-js.css" "$out/bpmn/"
cp "$bpmn/bpmn-font/css/bpmn.css" "$out/bpmn/bpmn-font/css/"
cp "$bpmn/bpmn-font/font/bpmn.woff2" "$bpmn/bpmn-font/font/bpmn.woff" "$out/bpmn/bpmn-font/font/"
{
  echo "The bundles bpmn-viewer.js and bpmn-modeler.js and the files in bpmn/ contain"
  echo "these packages. bpmn-js is under the bpmn.io license, the MIT terms with the"
  echo "condition that the bpmn.io watermark in rendered diagrams is neither removed"
  echo "nor changed and stays fully visible; its text comes first. htm is under the"
  echo "Apache License 2.0; every other package is under the MIT or ISC license. The"
  echo "license texts and notices of all of them follow."
  echo
  packages diagram
  for p in $diagram; do
    for f in node_modules/$p/LICENSE node_modules/$p/license node_modules/$p/NOTICE; do
      [ -f "$f" ] || continue
      echo; echo "== $p ($(basename "$f"))"; echo
      cat "$f"
    done
  done
} > "$out/bpmn.LICENSE.txt"
