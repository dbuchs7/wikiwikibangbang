package item

import (
	"net/url"
	"slices"
	"strings"
)

// WebLink is a link to a web address in an item's body (ADR-0039).
type WebLink struct {
	Item  ID
	Key   string
	Title string
	// Block is the index of the body block that holds the link; for a
	// link in a url field, Field names the field and Block is -1, and for
	// the item's place in its system of record Field is "origin"
	// (ADR-0074).
	Block int
	Field string
	Href  string
	// Responsible is who committed the body last: the person to fix it.
	Responsible string
}

// WebLinks returns the http and https links in the bodies, the url fields
// and the places in a system of record of the live items of a space that the principal may read, in creation and block
// order. A space in which the principal holds no role is not found.
func (x *Session) WebLinks(space string) ([]WebLink, error) {
	if x.s.spaceRole(x.who, space) < Viewer {
		return nil, notFound("space %q", space)
	}
	var items []*Item
	for _, it := range x.s.items {
		if it.Space == space && it.Presence == Live && x.s.canRead(x.who, it) {
			items = append(items, it)
		}
	}
	slices.SortFunc(items, func(a, b *Item) int { return a.created - b.created })
	var out []WebLink
	for _, it := range items {
		responsible := x.s.lastBodyAuthor(it.ID)
		td, _ := x.s.latest(it.Type)
		fieldOwner := owner(td, it)
		if fieldOwner == "" {
			fieldOwner, _ = x.s.lastEditor(it.ID)
		}
		for _, f := range td.Fields {
			if href, ok := it.Properties[f.Name].(string); ok && f.Kind == KindURL && isWebLink(href) {
				out = append(out, WebLink{Item: it.ID, Key: it.Key, Title: it.Title, Block: -1, Field: f.Name, Href: href, Responsible: fieldOwner})
			}
		}
		if it.Origin != nil && isWebLink(it.Origin.URL) {
			out = append(out, WebLink{Item: it.ID, Key: it.Key, Title: it.Title, Block: -1, Field: "origin", Href: it.Origin.URL, Responsible: fieldOwner})
		}
		for i, b := range it.Body.Blocks {
			seen := map[string]bool{}
			for _, m := range b.Marks {
				if m.Type != MarkLink || seen[m.Href] || !isWebLink(m.Href) {
					continue
				}
				seen[m.Href] = true
				out = append(out, WebLink{Item: it.ID, Key: it.Key, Title: it.Title, Block: i, Href: m.Href, Responsible: responsible})
			}
		}
	}
	return out, nil
}

// MayAdminister reports whether the principal is an administrator of a
// space, directly, through a group or as a system administrator.
func (x *Session) MayAdminister(space string) bool { return x.s.spaceRole(x.who, space) >= SpaceAdmin }

// lastBodyAuthor is the actor of the latest body commit of an item.
func (s *Store) lastBodyAuthor(id ID) string {
	t := s.touching[id]
	for i := len(t) - 1; i >= 0; i-- {
		if e := s.log[t[i]]; e.Kind == BodyCommitted && e.Item == id {
			return e.Actor
		}
	}
	return ""
}

func isWebLink(href string) bool {
	u, err := url.Parse(href)
	return err == nil && (strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https")) && u.Host != ""
}
