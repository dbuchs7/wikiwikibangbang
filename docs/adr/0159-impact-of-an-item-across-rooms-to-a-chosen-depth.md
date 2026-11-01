# ADR-0159: Impact of an item across rooms, to a chosen depth

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-11-01
- **Deciders:** Project initiators (the maintainer asked to "build in the impact view with a selectable depth" and chose: a query the server answers, across rooms, not bounded by one room's map or its budget, with an MCP tool; both directions, switchable; after atlas's impact view and the traversal on the server of atlas ADR-0420 §5, 2026-11-01)

## Context and problem statement

The starmap (ADR-0157, ADR-0158) shows a room's items in their context and separates a node's neighbourhood at a depth of one to three, but only on the map already loaded: one room, its direct neighbours in other rooms, at most 600 nodes, and with every edge treated alike in both directions. Readers ask a different question before they change something: *what does this affect?* — the pages below it, the items that relate to it, mention or embed it, the copies of its pictures — and, the other way round, *what does it rely on?* That question has a direction, follows edges further than one hop, and does not stop at the room's edge.

atlas answers it with an impact view (`impactOf`/`walkImpact` in `api/web/panorama-mesh.js`, a breadth-first walk with a depth and a direction), and its ADR-0420 §5 moves that traversal to the server, so the answer is not bounded by what one page loaded.

What is a dependency between items, who walks it, how far, and how does the walk keep D10?

## Decision drivers

- **D10 is absolute:** no node, edge, count or title of the answer may reveal an item the reader may not read — and neither may *reachability*: an item reachable only through an unreadable one must not appear, or it would tell that something unreadable joins them.
- **Across rooms:** dependencies cross rooms; an answer cut at the room's edge, or at the room map's budget, says "nothing more" when there is more.
- **Bounded cost (ADR-0015):** one request must not make the server hold, or the browser draw, an unbounded answer.
- **D1, D4, D6:** nothing stored; the edges follow from the items as they are; no item type is special.
- **Determinism:** the same state gives the same answer, so a test can pin it and a reload does not look like a change.
- **The starmap's ways:** the answer is drawn as a map with the live physics of ADR-0158, kept in the address, reachable by keyboard, every string in the catalogue.

## Considered options

1. **A walk on the server, across rooms**, at `GET /api/items/{ref}/impact`, with a direction and a depth, offered as an MCP tool.
2. **A walk in the browser on the loaded room map**, as the separated neighbourhood of ADR-0158 is.
3. **Labels as impact edges:** items that share a label affect each other.
4. **Counting what cannot be shown:** say how many dependents the reader may not see, or draw them as anonymous nodes.

## Decision outcome

Chosen option: **1, "a walk on the server, across rooms"**, because only the server holds every room, can keep the walk from passing through what the reader may not read, and answers agents the same as the view.

- **Dependency edges.** An edge *X depends on Y* means a change to Y may affect X. They are the starmap's edges (ADR-0157), read with a direction: a child depends on its parent (containment, D5); a relation made by hand from From to To means From depends on To, with its type kept; a mention or an embed means the mentioning or embedding item depends on the one it names; a picture belongs to its item and depends on it as a child does, never the other way; a copy depends on its original picture (ADR-0055), and through it on the original's item. **Shared labels are no dependency:** a label classifies, it does not make one item rely on another, so labels are neither nodes nor edges of the answer.
- **Directions.** `affected` walks downstream, from the start to what depends on it (the edges backwards); `dependson` walks upstream, to what the start relies on. Both are offered and switched in the view. Walking upstream from an item never reaches a picture, since an item does not depend on its pictures.
- **The walk.** `Session.Impact(start, direction, depth, maxNodes)` in `internal/item/impact.go` walks breadth first. Each node carries its distance in hops and the edges along which it was reached from the nodes one hop nearer (`via`); the answer also holds every dependency edge between its nodes, `truncated` when the budget cut the walk, and `complete` when nothing further is reachable — the walk ended within its depth, or reached it with nothing beyond. Within a distance the nodes come in a fixed order — items before pictures, those of the start's room before others, items by room and as made, pictures newest first — so the answer is deterministic and the budget keeps the nearest. The node and edge construction is the starmap's, shared rather than copied (`itemNode`, `relationEdge`, `starPictures`, `copyOrigin`); the starmap's output is unchanged.
- **Depth.** One to five hops, or `all`, which walks as far as anything reaches, bounded by the budget.
- **The D10 rule.** The walk knows only live items the reader may read and their pictures; an edge exists only when both ends are such; so it never passes *through* an unreadable item, and what is reachable only through one is not in the answer. There are no placeholders and no counts of what was left out. An unknown, trashed or unreadable start answers *not found*, as every read of an item does. This deliberately differs from atlas ADR-0420 §8, which greys out hidden nodes and offers an opaque passage.
- **Across rooms.** The walk is not limited to a room; a node of another room than the start's is marked `outside`, drawn with the dashed outline the starmap uses for it, and its panel opens its item.
- **Budget.** `ImpactNodes` in `internal/budget` (default 1000, `OT_BUDGET_IMPACT_NODES`) bounds the nodes, the start included.
- **Route and tool.** `GET /api/items/{ref}/impact?direction=affected|dependson&depth=1..5|all`, next to the relations in the route table, authenticated, readable with a read token; `ref` is a key or an ID. A picture is a start through the parameter `picture`, an attachment ID of the item (the newest version of a stack, as the starmap draws it); a simpler route for pictures was not needed, since the view knows the item of every picture node. The MCP adapter offers the request as the read tool `get_impact` (ADR-0012).
- **The view.** A node's panel on the starmap (items and pictures) offers *Impact* (German *Auswirkungen*). The impact view is the same map, drawn from the server's answer with the live physics of ADR-0158: the start marked, a ring around each node graded by its distance (classes `dist-0` to `dist-5` and `dist-far`), the distance also in the node's accessible name and in its panel, and the edges the walk followed drawn bold. A direction toggle (*Affected* / *Betroffen*, *Depends on* / *Abhängig von*) and a depth select (1 to 5, *All* / *Alle*) ask again. Below the canvas a list holds what was reached, nearest first, with title, key, type, room and distance, each linked to its item; a header says "N items affected up to depth D" — a count of nodes that are all on the map, so it reveals nothing — with notes when the walk was cut or ended before its depth. The address keeps it: `#/starmap/<ROOM>/<stations…>/<start>?impact=affected&depth=3`, so a reload and the back button find it; leaving — the button *Back to the map*, Escape, or a station of the trail — returns to the map it was opened from. The pure parts (the answer as a graph, distance classes, the list's order, the address) are in `assets/starmap-graph.js`, tested in Node.

This amends ADR-0157: the starmap gains an impact view whose graph is not one room's map but a walk across rooms.

### Consequences

- **Positive:** before a change, a reader and an agent see what it may affect, or what an item relies on, across rooms and further than one hop, with distances; nothing is stored; D10 holds by construction.
- **Negative / trade-offs accepted:** the server builds the reader's dependency graph of the whole instance on each request — one pass over the state, as the starmap's, but over every room; two readers may get different answers, and an answer can understate the real impact where an unreadable item lies between — that is D10, not a defect. A label shared by two items does not show as impact, even where readers use labels to group related work.
- **Follow-ups / risks to watch:** if instances grow far beyond the scale measured in ADR-0081, the graph may need an index kept beside the state rather than a pass per request. Weighting edges by kind (a mention matters less than an embed), several starts at once (atlas's window), and saving an impact view are decisions of their own.

## Pros and cons of the options

### Option 1: a walk on the server, across rooms (chosen)

Strongest form: the server holds every room and the permission check; one walk answers the view and agents alike, beyond any one map's budget, and its D10 checks are the ones every read uses.

- Good: complete across rooms; deterministic; one answer for the UI and MCP; no reachability through unreadable items.
- Bad: a second request with its own budget and tests; each change of direction or depth waits for the network.

### Option 2: a walk in the browser on the loaded room map

Strongest form: the map is already loaded and the separated neighbourhood of ADR-0158 is already a walk over it; adding a direction and a depth is a few lines, answers at once, and asks nothing new of the server or its D10 checks.

- Good: instant; no new route.
- Bad: bounded by the room and by the 600 nodes of `StarmapNodes`: the map holds the room's neighbours in other rooms only one step out, so a dependency chain that leaves the room and comes back, or runs on in another room, is cut short, and a truncated map gives a wrong answer silently. atlas ADR-0420 §5 moved its traversal to the server for the same reason.

### Option 3: labels as impact edges

Strongest form: teams label the items of one piece of work alike; when one of them changes, the others with the label are the first to look at, and the starmap already draws labels as hubs.

- Good: finds related items that nobody linked.
- Bad: a label classifies, it does not depend: every item labelled `runbook` would affect every other, and a common label would make the whole instance one hop wide, drowning the real dependencies. The label's page (ADR-0044) already lists what carries it.

### Option 4: counting what cannot be shown

Strongest form: an impact answer that silently omits hidden dependents reads as "nothing else", which is the dangerous answer; atlas states every point at which its walk stopped, and an operator would rather know that more exists.

- Good: honest about incompleteness.
- Bad: a count or an anonymous node tells that an unreadable item exists and depends on a readable one — exactly what D10 forbids (ADR-0022), and ADR-0157 rejected the same placeholders for the map. The view instead says only what is true of what the reader may see: that the walk was cut by the budget, or ended.

## Links

- amends [ADR-0157](0157-a-starmap-of-a-space.md) (a starmap of a space)
- relates to ADR-0158 (live physics, separating a neighbourhood), ADR-0001 (D1, D4, D5, D6, D10), ADR-0012 (MCP adapter), ADR-0015 (budgets), ADR-0017 (the catalogue), ADR-0022 (permission model), ADR-0044 (labels), ADR-0054, ADR-0055, ADR-0103 (pictures, copies, versions), ADR-0133 (UI modules tested in Node)
- after atlas's impact view, `impactOf` and `walkImpact` in `api/web/panorama-mesh.js` in pblumer/atlas, and atlas ADR-0420 §5 (traversal moves to the server)
