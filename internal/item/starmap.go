package item

import (
	"cmp"
	"slices"
	"strings"
)

// ImageTypes are the image formats a browser shows without running
// anything: the pictures of the album (ADR-0054) and of the starmap
// (ADR-0157). SVG is not among them: it can hold scripts (ADR-0014).
var ImageTypes = []string{"image/png", "image/jpeg", "image/gif", "image/webp"}

// IsPicture reports whether a file is a picture: an image of one of the
// ImageTypes.
func IsPicture(a Attachment) bool { return slices.Contains(ImageTypes, a.Type) }

// The kinds of a starmap's nodes (ADR-0157).
const (
	StarItem    = "item"
	StarPicture = "picture"
	StarLabel   = "label"
)

// The kinds of a starmap's edges (ADR-0157). A hand-made relation is
// StarRelation with its type; the derived ones are RelMentions and
// RelEmbeds.
const (
	StarParent   = "parent"
	StarRelation = "relation"
	StarOwner    = "picture"
	StarCopy     = "copy"
	StarLabelled = "label"
)

// StarNode is one node of a starmap: an item, a picture of an item, or a
// label that several items of the map carry.
type StarNode struct {
	// ID is the item's ID, "picture:<item>/<attachment>" for a picture,
	// "label:<name>" for a label.
	ID   string
	Kind string
	// Key, Title, Type, Space and State describe an item; Space also the
	// item of a picture.
	Key, Title, Type, Space, State string
	// Outside marks a node of another space than the one shown, there
	// because it is connected to a node of that space.
	Outside bool
	// Labels are an item's labels.
	Labels []string
	// Item is the ID of a picture's item.
	Item ID
	// Name is a picture's file name or a label's name.
	Name string
}

// StarEdge is one edge of a starmap, between two of its nodes.
type StarEdge struct {
	From, To string
	Kind     string
	// Type is a hand-made relation's type.
	Type string
}

// Starmap is the graph of one space as its reader may see it (ADR-0157).
type Starmap struct {
	Nodes []StarNode
	Edges []StarEdge
	// Truncated says that nodes were left out to stay within the budget.
	Truncated bool
}

func pictureNodeID(item, attachment ID) string {
	return "picture:" + string(item) + "/" + string(attachment)
}

// Starmap returns the graph of a space (ADR-0157): its readable live
// items with their containment, their relations, their pictures and the
// copies' originals, and the labels that at least two of the map's items
// carry. Readable items of other spaces related to an item of the space
// are on the map too, marked Outside. Nothing on it names an item the
// principal may not read, not even as a count (D10). Beyond maxNodes the
// items of the space are kept first, in tree order, then the outside
// nodes, the pictures and the labels, and Truncated is set. The result is
// ordered, so the same state yields the same map.
func (x *Session) Starmap(space string, maxNodes int) (Starmap, error) {
	if !x.s.HasSpace(space) || x.Role(space) < Viewer {
		return Starmap{}, notFound("space %q", space)
	}
	readable := func(it *Item) bool { return it != nil && it.Presence == Live && x.s.canRead(x.who, it) }
	b := starBuilder{max: maxNodes, kept: map[string]bool{}}

	// 1. The items of the space, in tree order: depth first, siblings in
	// their order (ADR-0041). A readable item below an unreadable one is
	// listed too; access narrows on the item, not on its tree (D9).
	kids := map[ID][]Item{}
	for _, it := range x.s.items {
		if it.Presence == Live && it.Space == space {
			kids[it.Parent] = append(kids[it.Parent], it.summary())
		}
	}
	var inSpace []*Item
	var walk func(parent ID)
	walk = func(parent ID) {
		list := kids[parent]
		x.s.sortSiblings(space, parent, list)
		for _, c := range list {
			if it := x.s.items[c.ID]; readable(it) {
				inSpace = append(inSpace, it)
			}
			walk(c.ID)
		}
	}
	walk("")
	for _, it := range inSpace {
		b.add(itemNode(it, false))
	}
	for _, it := range inSpace {
		if it.Parent != "" {
			b.edge(StarEdge{From: string(it.Parent), To: string(it.ID), Kind: StarParent})
		}
	}

	// 2. Relations of the space's items, hand-made and derived; an end in
	// another space brings its item in as an outside node.
	rels := make([]*Relation, 0, len(x.s.rels))
	for _, r := range x.s.rels {
		rels = append(rels, r)
	}
	slices.SortFunc(rels, func(a, b *Relation) int {
		return cmp.Or(cmp.Compare(a.created, b.created), strings.Compare(string(a.ID), string(b.ID)))
	})
	// An outside item is on the map only while a node of the space that
	// brings it in is: its anchors.
	var outside []*Item
	anchors := map[ID][]string{}
	bringIn := func(it *Item, anchor string) {
		if it.Space == space {
			return
		}
		if _, ok := anchors[it.ID]; !ok {
			outside = append(outside, it)
		}
		anchors[it.ID] = append(anchors[it.ID], anchor)
	}
	var relEdges []StarEdge
	for _, r := range rels {
		from, to := x.s.items[r.From], x.s.items[r.To]
		if !readable(from) || !readable(to) || (from.Space != space && to.Space != space) {
			continue
		}
		bringIn(from, string(to.ID))
		bringIn(to, string(from.ID))
		e := StarEdge{From: string(r.From), To: string(r.To), Kind: StarRelation, Type: r.Type}
		if r.Derived {
			e = StarEdge{From: string(r.From), To: string(r.To), Kind: r.Type}
		}
		relEdges = append(relEdges, e)
	}

	// 3. Pictures: the newest version of each file of the space's items, as
	// the album shows it (ADR-0054, ADR-0103), and the original of a copy
	// (ADR-0055). An original in another space brings its item in.
	type stackKey struct{ item, first ID }
	seen := map[stackKey]bool{}
	var pictures []StarNode
	var picEdges []StarEdge
	type origin struct {
		owner  *Item
		top    *Attachment
		copies []string // the copies' picture nodes
	}
	var originals []*origin
	originAt := map[string]*origin{}
	for _, p := range x.Pictures(space, IsPicture) {
		k := stackKey{p.Item.ID, p.First}
		if seen[k] {
			continue // an older version of a stack shown already
		}
		seen[k] = true
		it := x.s.items[p.Item.ID]
		id := pictureNodeID(it.ID, p.Attachment.ID)
		pictures = append(pictures, StarNode{ID: id, Kind: StarPicture, Space: it.Space, Item: it.ID, Name: p.Attachment.Name})
		picEdges = append(picEdges, StarEdge{From: id, To: string(it.ID), Kind: StarOwner})
		first, _ := it.attachment(p.First)
		if first == nil || first.Source == nil || first.Source.Item == it.ID {
			continue // no copy
		}
		src := x.s.items[first.Source.Item]
		if !readable(src) {
			continue
		}
		srcFirst := first.Source.Attachment
		if a, ok := src.attachment(srcFirst); ok {
			srcFirst = firstVersion(src, *a)
		}
		top := latestVersion(src, srcFirst)
		if top == nil || !IsPicture(*top) {
			continue
		}
		to := pictureNodeID(src.ID, top.ID)
		picEdges = append(picEdges, StarEdge{From: id, To: to, Kind: StarCopy})
		if src.Space != space {
			o := originAt[to]
			if o == nil {
				o = &origin{owner: src, top: top}
				originAt[to] = o
				originals = append(originals, o)
			}
			o.copies = append(o.copies, id)
			bringIn(src, string(it.ID))
		}
	}

	// The outside items in a stable order: by space, then as made.
	slices.SortFunc(outside, func(a, b *Item) int {
		return cmp.Or(strings.Compare(a.Space, b.Space), cmp.Compare(a.created, b.created))
	})
	for _, it := range outside {
		if slices.ContainsFunc(anchors[it.ID], b.has) {
			b.add(itemNode(it, true))
		}
	}
	for _, p := range pictures {
		if b.has(string(p.Item)) {
			b.add(p)
		}
	}
	for _, o := range originals {
		id := pictureNodeID(o.owner.ID, o.top.ID)
		if !b.has(string(o.owner.ID)) || !slices.ContainsFunc(o.copies, b.has) {
			continue
		}
		b.add(StarNode{ID: id, Kind: StarPicture, Space: o.owner.Space, Item: o.owner.ID, Name: o.top.Name, Outside: true})
		picEdges = append(picEdges, StarEdge{From: id, To: string(o.owner.ID), Kind: StarOwner})
	}
	for _, e := range relEdges {
		b.edge(e)
	}
	for _, e := range picEdges {
		b.edge(e)
	}
	b.dropLoose()

	// 4. The labels that at least two items of the map carry, the most
	// carried first. A picture's labels are the album's (ADR-0090).
	carriers := map[string][]string{}
	for _, n := range b.nodes {
		if n.Kind == StarItem {
			for _, l := range n.Labels {
				carriers[l] = append(carriers[l], n.ID)
			}
		}
	}
	var labels []string
	for l, ids := range carriers {
		if len(ids) >= 2 {
			labels = append(labels, l)
		}
	}
	slices.SortFunc(labels, func(a, b string) int {
		return cmp.Or(cmp.Compare(len(carriers[b]), len(carriers[a])), strings.Compare(a, b))
	})
	for _, l := range labels {
		id := "label:" + l
		b.add(StarNode{ID: id, Kind: StarLabel, Name: l})
		for _, it := range carriers[l] {
			b.edge(StarEdge{From: id, To: it, Kind: StarLabelled})
		}
	}
	return Starmap{Nodes: b.nodes, Edges: b.edges, Truncated: b.truncated}, nil
}

func itemNode(it *Item, outside bool) StarNode {
	return StarNode{ID: string(it.ID), Kind: StarItem, Key: it.Key, Title: it.Title, Type: it.Type, Space: it.Space,
		State: it.State, Outside: outside, Labels: slices.Clone(it.Labels)}
}

// starBuilder collects a starmap's nodes in the order of their priority,
// up to the budget, and the edges whose ends are both on it, each once.
type starBuilder struct {
	max       int
	nodes     []StarNode
	kept      map[string]bool
	edges     []StarEdge
	edgeSeen  map[StarEdge]bool
	truncated bool
}

func (b *starBuilder) add(n StarNode) {
	if b.kept[n.ID] {
		return
	}
	if len(b.nodes) >= b.max {
		b.truncated = true
		return
	}
	b.kept[n.ID] = true
	b.nodes = append(b.nodes, n)
}

func (b *starBuilder) has(id string) bool { return b.kept[id] }

func (b *starBuilder) edge(e StarEdge) {
	if !b.kept[e.From] || !b.kept[e.To] || e.From == e.To {
		return
	}
	if b.edgeSeen == nil {
		b.edgeSeen = map[StarEdge]bool{}
	}
	if !b.edgeSeen[e] {
		b.edgeSeen[e] = true
		b.edges = append(b.edges, e)
	}
}

// dropLoose removes the outside nodes left without an edge: the item of
// a copy's original whose copy the budget left out is on the map only by
// it. Their edges are gone already, so no other node loses one.
func (b *starBuilder) dropLoose() {
	degree := map[string]int{}
	for _, e := range b.edges {
		degree[e.From]++
		degree[e.To]++
	}
	b.nodes = slices.DeleteFunc(b.nodes, func(n StarNode) bool {
		if n.Outside && degree[n.ID] == 0 {
			delete(b.kept, n.ID)
			return true
		}
		return false
	})
}
