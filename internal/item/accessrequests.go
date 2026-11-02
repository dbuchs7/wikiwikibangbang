package item

import (
	"cmp"
	"slices"
	"time"
)

// Access requests answered from the task list (ADR-0161). A request to
// read a protection (ADR-0088, ADR-0160) is a SecretRequested event on its
// item; whoever decides about the protection finds it among their open
// work. Nothing is stored for it: the list is a view of the requests that
// wait, read from the items, as a board is a view (ADR-0036), and a
// request leaves it when it is granted or declined.

// AccessRequest is one waiting request to read a protection, as one who
// decides about it sees it.
type AccessRequest struct {
	Item  ID
	Key   string
	Title string
	Space string
	// Secret is the protection asked for; Number counts it among the
	// protections of its item from 1, as the item's card does, and Blocks
	// says it protects whole blocks rather than a passage.
	Secret ID
	Number int
	Blocks bool
	// Principal asked, at At; Seq is the event that asked, which orders
	// the requests and continues a page.
	Principal string
	At        time.Time
	Seq       int
}

// AccessRequests lists the requests to read a protection that the
// principal may decide (they protected it, or administer its space), on
// live items they read, oldest first: at most limit of those asked after
// the event after. total counts every such request; next is the cursor
// to continue from, 0 when none follow. Items the principal may not read
// are not looked at (D10).
func (x *Session) AccessRequests(after, limit int) (out []AccessRequest, total, next int) {
	var all []AccessRequest
	for _, it := range x.s.items {
		if it.Presence != Live || !slices.ContainsFunc(it.Secrets, func(sec Secret) bool { return len(sec.Requests) > 0 }) {
			continue
		}
		if _, err := x.readable(it.ID); err != nil {
			continue
		}
		for n, sec := range it.Secrets {
			if len(sec.Requests) == 0 || !x.s.decidesSecret(x.who, it, sec) {
				continue
			}
			for _, p := range sec.Requests {
				a := sec.asked[p]
				all = append(all, AccessRequest{Item: it.ID, Key: it.Key, Title: it.Title, Space: it.Space, Secret: sec.ID, Number: n + 1,
					Blocks: it.protectsBlocks(sec.ID), Principal: p, At: a.at, Seq: a.seq})
			}
		}
	}
	// Each request is an event of its own, so its sequence number orders
	// it alone.
	slices.SortFunc(all, func(a, b AccessRequest) int { return cmp.Compare(a.Seq, b.Seq) })
	total = len(all)
	for i, r := range all {
		if r.Seq <= after {
			continue
		}
		if len(out) == limit {
			next = all[i-1].Seq
			break
		}
		out = append(out, r)
	}
	return out, total, next
}
