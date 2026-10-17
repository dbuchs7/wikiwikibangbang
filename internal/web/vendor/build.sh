#!/bin/sh
# Rebuilds ../static/vendor/prosemirror.js from the pinned packages
# (ADR-0032). Run after changing a version in package.json:
#
#   cd internal/web/vendor && npm ci && sh build.sh
#
# Commit the bundle, its license file and package-lock.json together, and
# update THIRD_PARTY_NOTICES.md; internal/web tests check both.
set -eu
out=../static/vendor
npx esbuild entry.js --bundle --format=esm --target=es2020 --minify \
  --legal-comments=none --outfile="$out/prosemirror.js"
{
  echo "The bundle prosemirror.js contains these packages, each under the MIT license below."
  echo
  node -e '
    const lock = require("./package-lock.json");
    for (const [p, v] of Object.entries(lock.packages)) {
      if (p === "" || v.dev) continue;
      console.log(`${p.replace(/^node_modules\//, "")} ${v.version} ${v.license}`);
    }'
  echo
  cat node_modules/prosemirror-model/LICENSE
} > "$out/prosemirror.LICENSE.txt"
