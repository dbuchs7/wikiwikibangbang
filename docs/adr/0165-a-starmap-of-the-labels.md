# ADR-0165: A starmap of the labels

- **Status:** Proposed
- **Implementation:** Partial
- **Date:** 2026-11-03
- **Deciders:** Project initiators (the maintainer's request of 2026-11-03 for a view of the labels and the number of items labelled with each as a starmap, sized by the number of labels, and filtered by labels)
- **Open question:** whether the map should move under the live simulation of ADR-0158 like the starmap of a room, and whether choosing several labels should narrow the map to the items that carry all of them (an intersection) rather than show them with their connected labels
- **Question checked:** 2026-11

## Context and problem statement

Labels group items across types and rooms (ADR-0044). The labels page lists them with counts as chips, a label's page lists its items, and the starmap of a room (ADR-0157) draws a label as a node only when two items of that one room carry it, beside the items themselves. Nowhere does one see the labels themselves at a glance: which are used most, which are used together, which are rare. Whoever tidies labels or looks for the themes of a wiki has to read counts one by one.

How are the labels and the number of items each labels shown as a starmap, in every room or one, filtered by labels, with a canvas that is small for a few labels and big for many, without a stored object and without revealing anything the reader may not read?

## Decision drivers

- D1, D4: the map is not content of its own; it follows from the items' labels as they are.
- D10: counts include only items the reader may read; a shared link shows another reader only their own counts.
- Bounded cost (ADR-0015): the request must not make the server hold an unbounded number of pairs.
- Buildless UI (ADR-0031): no graph library, no inline styles, the look's tokens.
- The size of the drawing follows what it shows: a handful of labels should not sit lost on a huge canvas, hundreds should not be squeezed.

## Considered options

1. **Counts and pairs from the label listing, a view in the browser.** `GET /api/labels` gains `pairs`, which adds how many readable items carry two of the listed labels at once; the view lays out a star per label and a line per pair.
2. **Stars only, no lines.** The existing listing as it is, drawn as sized discs.
3. **Labels in the starmap of a room.** A filter of ADR-0157's map that keeps only its label nodes.
4. **Pairs computed in the browser** from the item listing, as the roadmap reads items (ADR-0164).

## Decision outcome

Chosen option: **1**, because the lines are what makes the picture a map — which labels belong together — and the server already counts labels over readable items in one pass; counting pairs in the same pass adds no new permission path.

- **API:** `GET /api/labels?pairs=on` answers, besides `labels` and `total`, `pairs` — `{labels: [a, b], count}` with `a` before `b` in the order of the alphabet, most shared first — and `pairsTotal`. Only pairs of the labels on the page are counted (the most used labels, as many as `PageItems` allows, after `q`), and at most `PageItems` pairs are answered, so what the server holds is bounded by the existing budget. `space` and `tasks` narrow pairs as they narrow counts. `Session.LabelPairs` in `internal/item` counts them through the same read check as `Session.Labels`; nothing is stored, no event or codec changes.
- **Route:** `#/labelmap`, with the parameters `space` (empty for every room), `labels` (chosen labels, comma-separated), `near` (`off` hides connected labels), `min` (the least count shown) and `q` (a word labels must hold). Defaults are left out of the address.
- **What is shown:** without a choice and a search, every listed label carried at least `min` times. Chosen labels and the labels a search finds are the focus: shown whatever their count, marked, and — unless `near=off` — with the labels connected to them by a pair, if carried at least `min` times.
- **Stars:** a star's area grows with the number of items that carry its label, relative to the most carried label shown (radius from 6 to 30 units, by the square root); its caption names the label and the count. A line's width grows the same way with the number of items carrying both labels.
- **Layout:** deterministic in the browser (`assets/labelmap-graph.js`): the stars, most carried first, start on a golden-angle spiral; lines pull connected labels together, discs with room for their captions never overlap, a weak pull holds everything near the middle. The same labels give the same picture.
- **Size:** the canvas's height grows with the square root of the number of stars shown — so its area grows with their number — from 240 to 960 pixels; the view box frames the stars in the canvas's proportions and is never smaller than a few stars need, so one label is not blown up. Beyond the ceiling, the wheel and buttons zoom and dragging pans. Above sixty stars only the biggest sixty, the chosen and the lit ones are captioned.
- **Interaction:** pointing at a star or focusing it lights its lines and connected labels; a click or Enter opens a panel with the count, links to the label's items, board and roadmap, a button that adds it to or removes it from the filter, and its connected labels with the items shared, each leading to its star. A table under the map lists the shown labels with counts for those who do not read the picture.
- **Entry points:** tabs *Room* and *Labels* on the starmap (the module keeps its name), and *On the starmap* on a label's page.
- **Code:** `assets/labelmap-graph.js` (tested by `internal/web/jstest/labelmap.test.mjs`), `assets/labelmap.js`, a step in `e2e/smoke.cjs`; `Session.LabelPairs` with its tests in `internal/item` and `internal/server`.

### Consequences

- **Positive:** the labels of a wiki or a room are seen at once, with their weight and their neighbourhoods; a label is one click from its map; a link shares a filtered view.
- **Negative / trade-offs accepted:** beyond `PageItems` labels only the most used are drawn and the view says so; beyond `PageItems` pairs only the strongest lines are drawn. The layout is O(n²) per step; at 500 labels it takes a fraction of a second. Two readers may see differently sized stars — that is D10, not a defect.
- **Follow-ups / risks to watch:** an intersection filter (items carrying all chosen labels) would need the listing to count labels of a selection, a parameter of its own; renaming or merging labels from the map would be a write path with its own decision. The MCP adapter does not offer the label listing; offering it with pairs would be a decision of ADR-0012.

## Pros and cons of the options

### Option 2: stars only

Strongest form: no API change at all; the listing already gives everything a sized-disc picture needs, and a bubble chart of counts is easy to read.

- Good: nothing new on the server.
- Bad: without lines it is a chart of counts, not a map: it cannot say which labels belong together, and choosing a label could not bring its neighbourhood along.

### Option 3: labels in the starmap of a room

Strongest form: one view instead of two; ADR-0157 already draws label nodes and their edges, with the live simulation of ADR-0158.

- Good: no new view, the physics for free.
- Bad: that map is of one room, counts a label only within its node budget and only when two items there carry it, and spends its budget on items first; a label used once or across rooms would be missing, and its size would follow the drawn items, not the count.

### Option 4: pairs in the browser

Strongest form: no server change; the view would read the items with their labels, as the roadmap does, and count pairs itself.

- Good: no API change.
- Bad: a map of every room would read every labelled item, page by page, beyond the boards' limit of 2 000 — far more than the counts it needs; the server counts the same in one pass over its state.

## Links

- relates to ADR-0044 (labels), ADR-0087 (a label is a board), ADR-0157 and ADR-0158 (the starmap of a room), ADR-0164 (a label is a roadmap), ADR-0015 (budgets), ADR-0022 (D10)
