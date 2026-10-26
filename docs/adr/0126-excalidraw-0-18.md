# ADR-0126: The drawing editor runs on Excalidraw 0.18

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-26
- **Deciders:** Project initiators (the maintainer asked to update the drawing editor to Excalidraw 0.18, 2026-10-26)

## Context and problem statement

The drawing editor (ADR-0053) shipped Excalidraw 0.17.6 of April 2024. Since 0.18, Excalidraw draws diamonds, hollow triangles and circles as arrowheads, which ArchiMate's relationships need (ADR-0125). Arrows bind to shapes more reliably and can be elbowed. Its default font is Excalifont, and the library tells published items from one's own.

0.18 also changed how it is delivered:
- it is published as modules with chunks and a separate stylesheet;
- it loads its fonts at run time from the path the page sets, then from a CDN (esm.sh);
- it imports a Mermaid-to-drawing converter, which brings Mermaid, KaTeX and lodash-es, with open security advisories.

## Decision drivers

- Nothing is fetched from outside the installation (ADR-0009: one binary; the content security policy of ADR-0053).
- Drawings made with 0.17 open, are edited and saved again as before.
- Only what the editor uses is shipped; no code with known advisories that the editor does not need.
- Every package in the bundle is credited with its license.

## Considered options

1. Update to 0.18.1 and bundle it with esbuild into a module with chunks, serving its fonts from the installation and leaving out the Mermaid converter.
2. Stay on 0.17.6.
3. Update to 0.18.1 and ship its files as published, with the Mermaid converter.

## Decision outcome

Chosen option: **1**, Excalidraw 0.18.1 (April 2026) with React 18.3.1.

### The bundle

`internal/web/vendor/build.sh` writes the following into `static/vendor/`:
- `excalidraw.js`, the module the drawing page imports;
- `excalidraw.css`, Excalidraw's stylesheet, linked by `draw.html`;
- `excalidraw-assets/`, the chunks loaded when needed. Of the translations only English and German are kept.
- `fonts/`, its fonts, without Xiaolai: the 13 MB fallback for Chinese, Japanese and Korean. Text in those scripts shows in a system font.

It also handles two parts of Excalidraw differently:
- **Mermaid:** the converter is replaced by `no-mermaid.js`, which refuses with a message. Excalidraw's "Mermaid to Excalidraw" dialog then reports that Mermaid is not part of the installation.
- **Licenses:** `excalidraw.LICENSE.txt` names every package the bundle takes code from, read from esbuild's account of its inputs, with its license text.

### Fonts from the installation only

- `draw-boot.js` sets the path of the fonts, `/vendor/`. It is a classic script that `draw.html` loads before any module.
- Excalidraw lists esm.sh after that path. The policy blocks it, but browsers still try it and log the refusal. The build therefore removes the CDN from the bundle. The build fails if the code it changes is not found exactly once, as after an update that changed it.
- The sketch theme (ADR-0085) takes Virgil from `vendor/fonts/`.

### ArchiMate relationships

ADR-0125's relationships are single arrows with Excalidraw's heads now:
- composition and aggregation: a filled and a hollow diamond;
- assignment: a circle and a triangle;
- realization and specialization: a hollow triangle.

They bind to the elements they connect and keep their heads when stretched.

### Consequences

- **Positive:**
  - Arrowheads ArchiMate needs, better binding, elbow arrows and the current editor.
  - No requests outside the installation.
  - The JavaScript loaded at first is half of 0.17's: 716 KB against 1.4 MB.
- **Negative / trade-offs accepted:**
  - New text is written in Excalifont, older drawings keep Virgil. Both ship.
  - The library lists the palettes under "Excalidraw library" and one's own shapes under "Personal library". Those names are Excalidraw's.
  - Mermaid diagrams cannot be turned into drawings.
  - Chinese, Japanese and Korean text has no hand-drawn font.
- **Follow-ups / risks to watch:**
  - An update of Excalidraw re-runs `build.sh`. A changed font fallback stops it, and the e2e opens, edits and saves a drawing.
  - Should the Mermaid converter be wanted, it can come back once its dependencies carry no advisories.

## Pros and cons of the options

### Option 2: stay on 0.17.6

Strongest form: it works, and an update risks drawings people rely on.

- Bad: no ArchiMate arrowheads; an editor two years old that gets no fixes.

### Option 3: ship 0.18.1 as published

Strongest form: the files as their authors tested them, nothing changed.

- Bad: fonts from a CDN unless configured; the Mermaid converter with its advisories and size; all 50 translations.

## Links

- amends [ADR-0053](0053-drawings-with-excalidraw-on-a-page-of-their-own.md) (the version and the bundle) and [ADR-0125](0125-archimate-relationships-and-a-microsoft-palette.md) (the relationships are single arrows)
