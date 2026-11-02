package item

import (
	"cmp"
	"slices"
	"strings"
)

// The directions of an impact walk (ADR-0159).
const (
	// ImpactAffected walks downstream: what depends on the start, so that
	// a change to it may affect them.
	ImpactAffected = "affected"
	// ImpactDependsOn walks upstream: what the start relies on.
	ImpactDependsOn = "dependson"
)

// ImpactNode is one node of an impact walk: a starmap node with its
// distance from the start, in hops, and the edges along which the walk
// reached it from the nodes one hop nearer.
type ImpactNode struct {
	StarNode
	Distance int
	Via      []StarEdge
}

// Impact is what depends on an item or picture, or what it depends on,
// to a depth, as its reader may see it (ADR-0159).
type Impact struct {
	// Start is the node the walk began at; Space its space.
	Start, Space string
	Direction    string
	// Depth is the most hops walked; 0 walks as far as anything reaches.
	Depth int
	// Nodes are the start, then what was reached, nearest first.
	Nodes []ImpactNode
	// Edges are the dependency edges between the nodes.
	Edges []StarEdge
	// Truncated says that the budget cut the walk short.
	Truncated bool
	// Complete says that nothing further is reachable: the walk ended
	// within its depth, or reached its depth with nothing beyond.
	Complete bool
}

// dependency names the ends of a dependency edge: the dependent depends
// on the dependency, so a change to the dependency may affect it. A child
// depends on its parent, a relation's From on its To (mentions and embeds
// alike), a picture on its item, a copy on its original.
func dependency(e StarEdge) (dependent, on string) {
	if e.Kind == StarParent {
		return e.To, e.From
	}
	return e.From, e.To
}

// Impact walks the dependency edges from start, an item's ID or a
// picture's node ID ("picture:<item>/<attachment>"), breadth first: in
// the direction ImpactAffected to what depends on it, in ImpactDependsOn
// to what it depends on, at most depth hops (0 for no limit) and at most
// maxNodes nodes, the start included. The edges are the starmap's
// (ADR-0157) without labels: a label classifies, it is no dependency. The
// walk spans spaces; a node of another space than the start's is marked
// Outside. It passes only live items the principal may read and their
// pictures, so nothing it returns names an unreadable item, nor anything
// reachable only through one (D10). An unknown or unreadable start is not
// found. Nodes of one distance come in a stable order — items before
// pictures, those of the start's space before the others, items by space
// and as made, pictures newest first — so the same state gives the same
// answer, and the budget keeps the nearest.
func (x *Session) Impact(start, direction string, depth, maxNodes int) (Impact, error) {
	if direction != ImpactAffected && direction != ImpactDependsOn {
		return Impact{}, wrapf(ErrValidation, "direction must be %s or %s", ImpactAffected, ImpactDependsOn)
	}
	depth = max(depth, 0)
	maxNodes = max(maxNodes, 1)

	// The graph: every readable live item, its pictures, and the edges
	// between them. rank orders nodes within a distance.
	nodes := map[string]StarNode{}
	rank := map[string]int{}
	var items []*Item
	for _, it := range x.s.items {
		if x.liveReadable(it) {
			items = append(items, it)
		}
	}
	slices.SortFunc(items, func(a, b *Item) int {
		return cmp.Or(strings.Compare(a.Space, b.Space), cmp.Compare(a.created, b.created))
	})
	add := func(n StarNode) {
		rank[n.ID] = len(rank)
		nodes[n.ID] = n
	}
	for _, it := range items {
		add(itemNode(it, false))
	}
	var edges []StarEdge
	for _, it := range items {
		if it.Parent != "" && x.liveReadable(x.s.items[it.Parent]) {
			edges = append(edges, StarEdge{From: string(it.Parent), To: string(it.ID), Kind: StarParent})
		}
	}
	for _, r := range x.visibleRelations() {
		if x.liveReadable(x.s.items[r.From]) && x.liveReadable(x.s.items[r.To]) {
			edges = append(edges, relationEdge(r))
		}
	}
	pictures := x.starPictures("")
	for _, p := range pictures {
		add(p.node)
	}
	for _, p := range pictures {
		edges = append(edges, p.ownerEdge())
		if e, ok := p.copyEdge(); ok && nodes[e.To].ID != "" {
			edges = append(edges, e)
		}
	}

	from, ok := nodes[start]
	if !ok {
		return Impact{}, notFound("item %s", start)
	}
	space := from.Space

	// The walk's steps: from a node to the nodes one edge away in the
	// walk's direction, each once per edge.
	next := map[string][]StarEdge{}
	seen := map[StarEdge]bool{}
	for _, e := range edges {
		if e.From == e.To || seen[e] {
			continue
		}
		seen[e] = true
		dependent, on := dependency(e)
		if direction == ImpactAffected {
			next[on] = append(next[on], e)
		} else {
			next[dependent] = append(next[dependent], e)
		}
	}
	other := func(e StarEdge, at string) string {
		dependent, on := dependency(e)
		if at == on {
			return dependent
		}
		return on
	}

	out := Impact{Start: start, Space: space, Direction: direction, Depth: depth}
	dist := map[string]int{start: 0}
	via := map[string][]StarEdge{}
	order := []string{start}
	frontier := []string{start}
	for hop := 1; ; hop++ {
		var found []string
		for _, id := range frontier {
			for _, e := range next[id] {
				to := other(e, id)
				if d, ok := dist[to]; ok && d < hop {
					continue
				}
				if _, ok := dist[to]; !ok {
					dist[to] = hop
					found = append(found, to)
				}
				via[to] = append(via[to], e)
			}
		}
		if len(found) == 0 {
			out.Complete = true
			break
		}
		if depth > 0 && hop > depth {
			break // more is reachable beyond the depth
		}
		slices.SortFunc(found, func(a, b string) int {
			na, nb := nodes[a], nodes[b]
			return cmp.Or(cmp.Compare(na.Kind, nb.Kind), // "item" before "picture"
				compareBool(na.Space != space, nb.Space != space), cmp.Compare(rank[a], rank[b]))
		})
		if room := maxNodes - len(order); len(found) > room {
			found, out.Truncated = found[:room], true
		}
		order = append(order, found...)
		if out.Truncated {
			break
		}
		frontier = found
	}

	at := map[string]int{}
	for i, id := range order {
		at[id] = i
		n := nodes[id]
		n.Outside = n.Space != space
		in := slices.Clone(via[id])
		slices.SortFunc(in, func(a, b StarEdge) int { return compareEdges(at, a, b) })
		out.Nodes = append(out.Nodes, ImpactNode{StarNode: n, Distance: dist[id], Via: in})
	}
	for e := range seen {
		if _, ok := at[e.From]; ok {
			if _, ok := at[e.To]; ok {
				out.Edges = append(out.Edges, e)
			}
		}
	}
	slices.SortFunc(out.Edges, func(a, b StarEdge) int { return compareEdges(at, a, b) })
	return out, nil
}

func compareBool(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return 1
	}
	return -1
}

// compareEdges orders edges by the places of their ends, then by kind
// and type.
func compareEdges(at map[string]int, a, b StarEdge) int {
	return cmp.Or(cmp.Compare(at[a.From], at[b.From]), cmp.Compare(at[a.To], at[b.To]),
		strings.Compare(a.Kind, b.Kind), strings.Compare(a.Type, b.Type))
}
