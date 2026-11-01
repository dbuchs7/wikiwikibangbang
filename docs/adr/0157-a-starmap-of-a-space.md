# ADR-0157: A starmap of a space

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-11-01
- **Deciders:** Project initiators (the maintainer asked for a starmap like atlas's panorama and chose: one space at a time, switchable; edges for the tree, relations and mentions, pictures and their origin, and shared labels; filters by element, 2026-11-01)

## Context and problem statement

A space holds pages, tasks and pictures that refer to each other in several ways: the page tree (containment, D5), typed relations made by hand, the mentions and embeds a body implies (D4), the pictures of an item and the copies that came from another item's picture (ADR-0054, ADR-0055), and labels (ADR-0044). Each of these is visible one item at a time — on the item's page, in the tree, in the album — but nowhere together. Readers ask what hangs together in a space, what a page is connected to, and which pictures came from where.

atlas has such a picture, the panorama (atlas ADR-0211): a graph the server derives from what it holds, laid out in the browser by a deterministic force simulation and drawn as SVG without a library. The maintainer wants the same for Orbithek: a *starmap* that shows a space's items and pictures in their context and can be filtered by element.

How is the map made, what does it show, and how does it keep the permission model's promise that an unreadable item does not exist (D10)?

## Decision drivers

- **D10 is absolute:** nothing a caller receives may reveal an item the caller may not read — not as a node, not as an edge's end, not as a count, not as a member of a label.
- **D1, D4:** no stored graph object beside the items; what the map shows follows from the items as they are.
- **Buildless and self-contained (ADR-0009, ADR-0031):** no bundler, no CDN, no new vendored library without need; the content-security policy `default-src 'self'` holds (no inline styles or scripts).
- **Bounded cost (ADR-0015):** one request must not make the server build, or the browser draw, a graph of unbounded size.
- **Stable picture:** the same space looks the same on every load, so a reload never looks like a change.
- **D6:** item types are the users' own; the map treats every type alike and names none.
- **Accessibility:** kinds are told apart by shape and outline, not colour alone; nodes are reachable by keyboard; the legend is text.

## Considered options

1. **A view derived per request** from the live state through the reader's session, laid out in the browser.
2. **Vendoring a graph library** (cytoscape.js or d3-force) to lay out and draw the graph.
3. **A map of the whole instance** rather than of one space.
4. **atlas-style restricted placeholders:** draw an unreadable neighbour as an anonymous "restricted" node, so the reader sees that something is there.

## Decision outcome

Chosen option: **1, "a view derived per request"**, because it stores nothing (D1), shows exactly what the reader may read at that moment, and needs no dependency.

- **Route and method:** `GET /api/spaces/{space}/starmap`, in the route table next to the page tree, authenticated, readable with a read token like every GET; it answers `{nodes, edges, truncated}`. `Session.Starmap(space, maxNodes)` in `internal/item/starmap.go` builds it under the store's read lock, through the session's read checks. An unknown space, a space in the trash and a space in which the reader holds no role all answer *not found*, as other reads of a space do. The MCP adapter offers the same request as the read tool `get_starmap` (ADR-0012).
- **Nodes:** the readable live items of the space (`kind: item`, with key, title, type, space, state and labels); the pictures of those items — image files of the formats the album lists (PNG, JPEG, GIF, WebP), only the newest version of each stack (ADR-0103), the same files `Session.Pictures` lists (`kind: picture`, id `picture:<item>/<attachment>`); and the labels that **at least two item nodes of the map** carry (`kind: label`, id `label:<name>`). A picture's own labels stay in the album (ADR-0090) and make no label node.
- **Edges, in four groups:** the tree — `parent`, from a parent to its child; relations — `relation` with its type for one made by hand, and the derived `mentions` and `embeds`; pictures — `picture`, from a picture to its item, and `copy`, from a copy to the newest version of its original in the other item (ADR-0055); labels — `label`, from a label node to every item node that carries it.
- **Boundary nodes:** a readable live item of *another* space that a relation joins to an item of the space, or whose picture an item of the space copied, is on the map, marked `outside`, with that edge; the original picture of such a copy is an outside picture node with its item. Nothing beyond that one step is followed, and an outside item is kept only while the node of the space that brings it in is on the map.
- **The D10 rule:** a node is on the map only when the reader may read it; an edge only when both its ends are on the map; a label node only when at least two *readable* items of the map carry it, so a label shared with an unreadable item is not a hub. There are no restricted placeholders and no counts of what was left out. This deliberately differs from atlas, whose panorama draws restricted placeholders.
- **Budget:** `StarmapNodes` in `internal/budget` (default 600, `OT_BUDGET_STARMAP_NODES`) bounds the nodes. Beyond it the map keeps, in this order, the items of the space in tree order (depth first, siblings in their order, ADR-0041), then the outside items (by space, then as made), then the pictures (newest first, the space's before the outside originals), then the labels (most carried first, then by name); edges to nodes left out are dropped, and an outside node left without an edge goes too. `truncated` is set, and the view says the map is truncated, without a count.
- **Determinism:** the server sorts everything it emits, so the same state gives the same answer. The browser lays the graph out with a fixed-seed, fixed-iteration force simulation (repulsion between nodes, springs along edges, a weak pull to the middle, cooling), as atlas's panorama does, computed once for the whole map (`assets/starmap-graph.js`, tested in Node). Filters only hide; what stays keeps its place.
- **The view:** the module *Starmap* behind the nine dots, at `#/starmap/<SPACE>`, with a room select (favourites first, ADR-0145). Filters: a search over title, key, labels and name that keeps the matches and one hop of context and marks the matches; a switch per item type present on the map, for pictures, for labels and for outside nodes; a switch per edge group. Choosing a node (click, Enter or Space) dims everything outside its neighbourhood and opens a panel with its title, key, type, state, space and a link to open it (the item, the item of a picture, or the label's page); Escape ends it. Drag pans, the wheel and buttons zoom. Items are circles, pictures squares, labels diamonds, outside nodes have a dashed outline; edge kinds have their own line styles; the legend says all of it in words. Every colour is a token of the look, so light, dark and cockpit (ADR-0156) all work. Nodes are focusable buttons with a name; the canvas is a named group.

### Consequences

- **Positive:** a space's structure becomes visible at a glance with no stored object and no dependency; what one sees is exactly what one may read; the map is cheap to make (one pass over the state) and bounded.
- **Negative / trade-offs accepted:** two readers of one space may see differently shaped maps, and a reader cannot tell that something was left out for want of access — that is D10, not a defect. Truncation keeps items before pictures and labels, so a very large space may show few pictures. The layout is O(n²) per step in the browser; at the default budget it takes a fraction of a second. The layout's last digits may differ between JavaScript engines, since trigonometric functions are not exact; within one browser it is stable.
- **Follow-ups / risks to watch:** a map across several spaces, saved views, or pinning nodes would each be a decision of their own. If spaces routinely exceed the budget, a server-side filter (by type or label) may be needed so the budget is spent on what the reader asked for.

## Pros and cons of the options

### Option 1: a view derived per request (chosen)

Strongest form: the graph is a pure function of the state and the reader, like the board (ADR-0036) and the album; nothing to keep in step, nothing to migrate, and the permission check is the one every read uses.

- Good: D1 and D10 hold by construction; no dependency; deterministic.
- Bad: rebuilt on every request; the layout is ours to maintain.

### Option 2: vendoring a graph library (cytoscape.js, d3-force)

Strongest form: mature layouts (cose, fcose, force with Barnes–Hut), gestures and rendering for thousands of nodes, maintained by others; d3-force alone is small.

- Good: better layouts at large sizes; less code of our own.
- Bad: cytoscape is several hundred kilobytes to embed in every binary and review on every update; libraries of this kind commonly set inline styles, which the policy `default-src 'self'` refuses; ADR-0031 keeps the UI buildless and vendors only what cannot reasonably be written (the editor, the drawing editor, BPMN). A fixed-iteration simulation over at most 600 nodes is a page of code.

### Option 3: a map of the whole instance

Strongest form: the connections *between* spaces are often the interesting ones, and one map answers "what is connected to what" everywhere.

- Good: no switching; cross-space structure in one picture.
- Bad: the size is unbounded — an instance with a catalog space of ten thousand entries (ADR-0081) would always be truncated and illegible; the budget would be spent on whatever comes first. One space with its direct neighbours in other spaces keeps the cross-space edges that matter at a size one can read.

### Option 4: atlas-style restricted placeholders

Strongest form: a reader learns that a page has connections they cannot see, and can ask for access, instead of drawing wrong conclusions from a map that looks complete.

- Good: honest about incompleteness.
- Bad: it reveals that an unreadable item exists and is connected to a readable one — exactly what D10 forbids (ADR-0022); a count or an anonymous node is still a disclosure. Embeds already show restricted targets, but only where the reader's own item names them; the map would reveal links the reader has no way to know of.

## Links

- amended by [ADR-0158](0158-the-starmap-moves-live-physics-and-separating-a-neighbourhood.md) (live physics and separating a neighbourhood)
- amended by [ADR-0159](0159-impact-of-an-item-across-rooms-to-a-chosen-depth.md) (impact of an item across rooms, to a chosen depth)
- relates to ADR-0001 (D1, D4, D5, D6, D10), ADR-0015 (budgets), ADR-0022 (permission model), ADR-0031 (buildless UI), ADR-0036 (a view, not a stored object), ADR-0041 (sibling order), ADR-0044 (labels), ADR-0054, ADR-0055, ADR-0090, ADR-0103 (pictures, copies, picture labels, versions), ADR-0145 (favourites first), ADR-0156 (looks)
- after atlas ADR-0211 (panorama: a derived landscape mesh), `api/web/panorama-mesh.js` in pblumer/atlas
