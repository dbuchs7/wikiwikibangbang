# ADR-0125: ArchiMate relationships and a Microsoft palette for the drawing editor

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-26
- **Deciders:** Project initiators (the maintainer asked for the ArchiMate relationship types, such as composition and aggregation, in the ArchiMate palette, and for a basic Microsoft palette, open source and without fees, 2026-10-26)

## Context and problem statement

The ArchiMate palette (ADR-0117) holds the elements of the business, application and technology layers, but none of the relationships that connect them. The Excalidraw library collection has no library with them.

Excalidraw 0.17, the version the editor ships, draws four kinds of arrowheads: an open arrow, a bar, a dot and a filled triangle. ArchiMate also needs a filled and a hollow diamond and a hollow triangle.

Microsoft-like symbols were asked for as well. Microsoft's product and architecture icons (Azure, Entra, Microsoft 365) may be used only in diagrams and documentation; they are not under an open license and are trademarks. Microsoft publishes the Fluent UI System Icons, about 2000 icons in its own style, under the MIT license; they hold no product logos. They are SVG paths: filled outlines, with holes.

Excalidraw 0.17 draws the previews of a library without its files (`exportToSvg` with `files: null`). An icon kept as a picture would show as an empty box in the library.

## Decision drivers

- ArchiMate 3.1 notation, recognisable at a glance.
- Only material under an open license without fees; no product logos.
- The shapes can be moved, recoloured and resized like any other.
- The library shows what each item is.

## Considered options

1. Draw the relationships from Excalidraw's shapes; turn the Fluent icons into filled polygons.
2. Upgrade Excalidraw to 0.18, which has diamond and hollow triangle arrowheads; embed the Fluent icons as pictures.
3. Take the UML library of the collection for the relationships, and the Azure libraries of the collection for the Microsoft symbols.

## Decision outcome

Chosen option: **1**.

### The relationships

At the end of the ArchiMate palette, in the specification's order: composition, aggregation, assignment, realization, serving, access, influence, association, directed association, triggering, flow, specialization, and the and- and or-junction.

- **The line:** each is an Excalidraw arrow 160 pixels long, crisp (no sloppiness), in the text colour. Its line is solid, dashed (influence, flow) or dotted (realization, access).
- **The ends:** Excalidraw's own heads where they match ArchiMate's: a filled triangle (triggering, flow), an open arrow (serving, access, influence, directed association), a dot with a filled triangle (assignment).
- **What Excalidraw lacks:** a small diamond (composition filled, aggregation hollow) or hollow triangle (realization, specialization), grouped with the line.
- **The junctions:** circles, filled or hollow.

### The Microsoft palette

- **Content:** 67 Fluent UI System Icons in the regular style, chosen for architecture and IT sketches. Among them are people, buildings, devices, servers, database, storage, cloud, globe, WLAN, router, security, documents, communication, apps, code, charts and status.
- **Source:** `internal/web/vendor/fluent.mjs` reads them from a pinned commit of `microsoft/fluentui-system-icons`.
- **Conversion:** each outline of an icon's path becomes a filled polygon, curves and arcs as short segments. Excalidraw fills a polygon without holes, so an outline inside another is filled white, one inside that again with the ink. The dark scheme inverts both, as it does every shape.
- **Size:** icons are 72 pixels, three times their design size.
- **Left out:** the certificate icon, whose outlines this way of filling does not draw correctly.

### Files and credit

- `palettes.mjs` builds both, next to the palettes of ADR-0117.
- `palettes.LICENSE.txt` names the relationships as this project's own and gives the Fluent icons' source, commit and MIT license text. `THIRD_PARTY_NOTICES.md` names them as well.
- The chooser offers the palette as "Microsoft (Fluent icons)".

### Consequences

- **Positive:**
  - Every ArchiMate relationship is one click away.
  - Microsoft-style symbols are available without license questions.
  - Both are ordinary shapes: they recolour, scale and show in the library.
- **Negative / trade-offs accepted:**
  - A relationship with a diamond or a hollow triangle is a group. Dragging its line's end moves the line, not the diamond or triangle, which stays at the other end or must be moved with it. Its line is not bound to the elements it connects.
  - An icon is several polygons. A white hole shows on a coloured background; the icon is best placed on white.
  - Influence carries no "+" or "−"; it is written beside it where needed.
  - The Microsoft palette is about 330 KB, fetched only when chosen.
- **Follow-ups / risks to watch:**
  - Excalidraw 0.18 draws diamonds and hollow triangles as arrowheads. With it, the relationships could be single arrows that bind to elements.
  - A path whose holes depend on the nonzero rule may be filled wrongly; each new icon is looked at before it is added.

## Pros and cons of the options

### Option 2: Excalidraw 0.18 and pictures

Strongest form: native arrowheads that follow the line and bind to elements, and the icons exactly as designed.

- Good: the best relationships.
- Bad: a major upgrade of the editor for this alone. Pictures show empty in the library and cannot be recoloured.

### Option 3: libraries of the collection

Strongest form: nothing to draw or convert.

- Bad: the UML library's arrows are not ArchiMate's. The Azure libraries hold Microsoft's product icons, whose terms the collection's MIT label cannot replace.

## Links

- amends [ADR-0117](0117-palettes-for-the-drawing-editor.md) (two more sets of shapes)
- relates to [ADR-0053](0053-drawings-with-excalidraw-on-a-page-of-their-own.md) (the drawing editor)
