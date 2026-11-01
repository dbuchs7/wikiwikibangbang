# ADR-0158: The starmap moves: live physics and separating a neighbourhood

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-11-01
- **Deciders:** Project initiators (the maintainer asked for the starmap to be as dynamic as atlas's panorama, after the graph view of a well-known notes application, and for a double-click on an element to separate it and zoom in, 2026-11-01)

## Context and problem statement

The starmap of a space (ADR-0157) lays its graph out once, with a fixed-seed, fixed-iteration simulation, and then stands still: one can pan, zoom and choose a node, but the picture does not answer to the hand. With fifty nodes that is readable; with a few hundred, a reader wants to pull a cluster apart to see what hangs on what, to see a node's neighbours light up as the pointer passes, and to step into one part of the map and out again.

atlas's panorama (`api/web/panorama-mesh.js` in pblumer/atlas) does this: dragging moves a node's neighbourhood, a double-click drills into a node with a breadcrumb trail, Escape steps back, and the view frames what is shown. Graph views of notes applications add live physics — the layout keeps moving while it cools, a dragged node pulls its neighbours along, hovering lights a neighbourhood and dims the rest, and captions fade in as one zooms in. The maintainer asked for the starmap to behave like that: "as dynamic as in atlas, following the graph view; a double-click on an element separates it — zooms in."

How does the map move, within the budget of 600 nodes, without a dependency, under the content-security policy, deterministically enough to test — and how is a separated neighbourhood kept, left and addressed?

## Decision drivers

- **Buildless and self-contained (ADR-0009, ADR-0031):** no bundler, no CDN, no new vendored library without need; `default-src 'self'` holds, so no inline style attributes — classes and SVG presentation attributes only.
- **Bounded cost (ADR-0015):** the budget `StarmapNodes` (600) is unchanged, and a frame at that size must stay well inside 16 ms.
- **D10 is untouched:** the map still shows only what the server sent for the reader; nothing new is asked of the server.
- **Testable without a browser:** the physics, the drill path, the neighbourhood at a depth and the captions a zoom shows are plain functions that `node --test` runs (ADR-0133).
- **Stable picture:** the same space starts in the same place and comes to the same rest on every load.
- **Accessibility:** what the pointer does has a keyboard equivalent; motion is not forced on a reader who asked the system for less of it.
- **One route per view:** a separated neighbourhood survives a reload and the back button without a second module.

## Considered options

1. **A live simulation in the browser**, written for the starmap, with a separated neighbourhood as a drill path over the graph already loaded.
2. **A static layout with animated transitions only:** keep the one-shot layout and tween between layouts on filter and drill.
3. **Vendoring d3-force** (with d3-quadtree) for the physics.
4. **Drilling by a server request with a depth:** ask the server for a node's neighbourhood at a depth.

## Decision outcome

Chosen option: **1, "a live simulation in the browser"**, because it gives the behaviour asked for with a page of plain arithmetic, needs nothing from the server, and keeps everything the reader sees a function of what was loaded.

- **The simulation** (`assets/starmap-sim.js`) follows d3-force's model: an alpha that cools from its start to rest in 300 ticks (`alphaDecay = 1 − 0.001^(1/300)`), velocities that decay by 0.4 per tick, many-body repulsion (strength −300), springs along the edges (length 70, strength one over the smaller degree of the two ends, as d3), a pull to the middle proportional to the distance, and collision that keeps nodes two radii apart (a grid of cells one diameter wide). Above 64 active nodes the repulsion is approximated with a **Barnes–Hut quadtree** (θ = 0.9); below, it is exact. The view runs two ticks per animation frame and writes the positions into the existing SVG elements' attributes — `transform` of a node, the ends of an edge — never rebuilding the DOM. Measured in Node 22 on the development container: 600 nodes and 899 edges at about 700 ticks per second with the quadtree (1.4 ms a tick; 570 per second exact), 300 ticks to rest in 0.4 s; 200 nodes at about 3 000 ticks per second.
- **Determinism:** the simulation starts from the seeded layout of ADR-0157, unchanged, and uses only additions, multiplications, divisions and square roots, which IEEE 754 makes exact on every engine; two points on one spot part in a direction made from their indexes, not from a random number. The same start and graph therefore come to the same rest; a unit test pins a resting state.
- **Rest and leaving:** the loop runs only while the simulation is warm or the view eases; at rest it stops and the canvas carries `data-settled="true"` (which the smoke test waits for instead of sleeping). It is reheated by a drag, by a change of what is shown (filters, search, drill, depth). Leaving the view — a route change, or the canvas no longer on the page — cancels the frame and removes the listeners it set on the document and the window. With `prefers-reduced-motion: reduce` nothing animates: every change runs the simulation to rest at once and frames the result, as the map behaved before.
- **Dragging a node** (pointer events: mouse, pen and touch alike) pins it under the pointer, holds the simulation warm (alpha target 0.3) so its neighbours follow through the springs, and unpins it on release, so it settles with the rest. Dragging the empty canvas pans; the wheel zooms around the pointer; the zoom buttons stay, and the third frames what is shown.
- **Lighting a neighbourhood:** pointing at a node, or focusing it by keyboard, marks it, its neighbours and their edges (`lit`) and dims the rest, without opening the panel. A click still chooses the node and opens its panel (ADR-0157).
- **Captions fade with the zoom**, by class: the canvas is `zoom-far` below 0.6, `zoom-mid` below 1.2 and `zoom-near` above. Far, only the captions of lit, chosen, matching and separated nodes show; mid, items' captions join them; near, all, relation names included. The style sheet holds the rule; `captionShown` states it for the tests.
- **Separating a neighbourhood:** a double-click on a node, the panel's button *Separate* (German *Vertiefen*), or Shift+Enter on a focused node narrows the map to that node and everything within a depth of it — 1 by default, 2 or 3 by a control in the toolbar — through the edges the switches leave on. The simulation takes only those nodes and edges, the rest is frozen where it was (so stepping back finds it there), and the view eases to frame them. Separating inside a separated view goes one station further; a node already on the path cuts the path back to it, as atlas does. A trail above the canvas (*Whole room › PLAT-2 › PLAT-5*) shows the path; a station of it goes back there; Escape ends a chosen node first and then steps back one station. The search and the switches still apply inside a separated neighbourhood, and the search narrows it rather than reaching out of it.
- **The address keeps the path:** `#/starmap/<SPACE>/<station>/<station>…`, a station being an item's key or another node's id, with `?depth=N` when the depth is not 1. Each step is a new history entry, so the back button returns to the previous station (the view is rebuilt from the address, as every view is); a station the map no longer holds is dropped.
- **Looks:** every new mark uses the look's tokens, so light, dark and cockpit (ADR-0156) all work.

This amends ADR-0157: its layout is now the start of a live simulation rather than the final picture, and filters move what stays rather than leaving it in place.

### Consequences

- **Positive:** the map answers to the hand and reads at a glance: a cluster can be pulled apart, a neighbourhood lit or separated, and a reader can walk into a part of the space and back. Nothing new is asked of the server; D10 and the budget are unchanged; no dependency was added.
- **Negative / trade-offs accepted:** the map moves on every load and after every filter, which a reader may find restless (reduced motion turns it off). The resting state differs from the seeded layout, so filters no longer leave every node where it was; a node's place is stable between loads, not between filter states. The seeded layout of ADR-0157 still costs about 0.3 s at 600 nodes before the first frame. The simulation is ours to maintain.
- **Follow-ups / risks to watch:** pinning a node for good, saving a view, and a map across several spaces remain decisions of their own (ADR-0157). If the starmap budget is raised far beyond 600, the seeded layout should go through the quadtree too.

## Pros and cons of the options

### Option 1: a live simulation in the browser (chosen)

Strongest form: the force model of d3-force is small and well understood; written for the map, it fits the CSP, needs no build, is exact enough to pin in a unit test, and at 600 nodes with a quadtree costs a tenth of a frame.

- Good: the behaviour asked for; no dependency; deterministic; works on the data already loaded.
- Bad: a few hundred lines of physics to keep; a moving picture costs a reader's attention, which reduced motion has to switch off.

### Option 2: a static layout with animated transitions only

Strongest form: the picture never moves on its own, so a reader's mental map is never disturbed; a filter or a drill computes the new layout at once and tweens every node from its old to its new place, which is cheap, calm and testable.

- Good: calm; one layout per state, so every state is reproducible; less code at run time.
- Bad: it is not what was asked — dragging a node cannot pull its neighbours along without a simulation that runs while the hand moves, and the tween between two independent layouts makes nodes cross the whole map. The live simulation with reduced motion already gives the calm variant to those who want it.

### Option 3: vendoring d3-force

Strongest form: d3-force and d3-quadtree are small (a few kilobytes each), mature, BSD-licensed and exactly this model; vendoring them as a pinned file, as the editor and the BPMN viewer are (ADR-0032, ADR-0129), costs one build script and saves maintaining our own physics.

- Good: proven code; less of our own.
- Bad: ADR-0031 vendors only what cannot reasonably be written, and d3-force's model is a page; a vendored module needs a build step, a pinned npm tree and a review on each update; d3's random jiggle (seeded or not) and its module structure would have to be wrapped to give the deterministic, testable rest this map needs. The quadtree is the only part of any size, and it is a hundred lines.

### Option 4: drilling by a server request with a depth

Strongest form: the server could answer a node's neighbourhood at a depth across the whole space, beyond the 600 nodes of the budget, so a separated neighbourhood in a truncated map is complete.

- Good: complete neighbourhoods in very large spaces.
- Bad: not needed — the map is already loaded, and within the budget the neighbourhood is a walk over it; a second route needs its own D10 checks and budget, and every step of a drill would wait for the network. A truncated map is the case ADR-0157 already names as a follow-up (a server-side filter).

## Links

- amends ADR-0157 (a starmap of a space)
- relates to ADR-0009, ADR-0031 (buildless UI and its content-security policy), ADR-0015 (budgets), ADR-0017 (the catalogue), ADR-0133 (UI modules tested in Node), ADR-0156 (looks)
- after atlas's panorama, `api/web/panorama-mesh.js` in pblumer/atlas (dragging a neighbourhood, `drillInto` with a breadcrumb trail, Escape steps back, `fitView`), and the d3-force model (alpha cooling, many-body with Barnes–Hut, link springs, collision)
