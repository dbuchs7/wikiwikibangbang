package item

import (
	"testing"
)

// requestsOf lists the access requests a principal is to decide, as
// "key#number principal".
func requestsOf(t *testing.T, s *Store, who string) []string {
	t.Helper()
	list, total, _ := s.As(who).AccessRequests(0, 100)
	if total != len(list) {
		t.Fatalf("%s: %d requests listed of %d", who, len(list), total)
	}
	var out []string
	for _, r := range list {
		out = append(out, r.Key+"#"+string(rune('0'+r.Number))+" "+r.Principal)
	}
	return out
}

func sameList(t *testing.T, who string, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s decides %v, want %v", who, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s decides %v, want %v", who, got, want)
		}
	}
}

// Those who decide about a protection — the one who protected it and the
// space's administrators — find its waiting requests in their list; no
// one else does, and an answer takes the request off (ADR-0161).
func TestAccessRequestsAreListedToThoseWhoDecide(t *testing.T) {
	s, page, sec := secretFixture(t)
	noErr(t, s.As("ben").RequestSecret(page.ID, sec.ID))
	noErr(t, s.As("vic").RequestSecret(page.ID, sec.ID))
	for _, who := range []string{"anna", "pat"} {
		sameList(t, who, requestsOf(t, s, who), page.Key+"#1 ben", page.Key+"#1 vic")
	}
	// A reader of the passage, a viewer, an editor who asked: none decides.
	for _, who := range []string{"hanna", "ben", "vic", "carl"} {
		sameList(t, who, requestsOf(t, s, who))
	}
	list, _, _ := s.As("anna").AccessRequests(0, 100)
	if r := list[0]; r.Item != page.ID || r.Title != "Review" || r.Space != "PLAT" || r.Secret != sec.ID || r.Blocks || r.At.IsZero() || r.Seq == 0 {
		t.Fatalf("request %+v", r)
	}
	// The one who asked sees when they asked.
	views, err := s.As("ben").Secrets(page.ID)
	noErr(t, err)
	if !views[0].Requested || !views[0].RequestedAt.Equal(list[0].At) {
		t.Fatalf("ben sees %+v", views[0])
	}

	// Granting and declining take the requests off the list.
	noErr(t, s.As("pat").DecideSecret(page.ID, sec.ID, "ben", true))
	sameList(t, "anna", requestsOf(t, s, "anna"), page.Key+"#1 vic")
	noErr(t, s.As("anna").DecideSecret(page.ID, sec.ID, "vic", false))
	sameList(t, "anna", requestsOf(t, s, "anna"))
	sameList(t, "pat", requestsOf(t, s, "pat"))

	// Asking again after a decline is a new request, with its own time.
	noErr(t, s.As("vic").RequestSecret(page.ID, sec.ID))
	again, _, _ := s.As("pat").AccessRequests(0, 100)
	if len(again) != 1 || !again[0].At.After(list[1].At) {
		t.Fatalf("asked again: %+v", again)
	}
}

// The list spans spaces, numbers each protection as its item's card does,
// tells protected blocks from passages, and pages oldest first.
func TestAccessRequestsSpanSpacesAndPage(t *testing.T) {
	s, page, sec := secretFixture(t)
	noErr(t, s.GrantRole("root", "OPS", "anna", Editor))
	noErr(t, s.GrantRole("root", "OPS", "ben", Viewer))
	anna := s.As("anna")
	ops := mustItem(t)(anna.Create(NewItem{Space: "OPS", Type: "page", Title: "Keys", Body: &Doc{Blocks: []Block{
		{Kind: Paragraph, Text: "Open."}, {Kind: Heading, Level: 1, Text: "Vault"}, {Kind: Paragraph, Text: "The code is 1234."},
	}}}))
	_, err := anna.Protect(ops.ID, 0, 0, 4, nil)
	noErr(t, err)
	blocks, err := anna.ProtectBlocks(ops.ID, 1, 2, nil)
	noErr(t, err)
	noErr(t, s.As("ben").RequestSecret(ops.ID, blocks.ID))
	noErr(t, s.As("ben").RequestSecret(page.ID, sec.ID))
	sameList(t, "anna", requestsOf(t, s, "anna"), ops.Key+"#2 ben", page.Key+"#1 ben")
	// Pat administers PLAT only.
	sameList(t, "pat", requestsOf(t, s, "pat"), page.Key+"#1 ben")
	first, total, next := anna.AccessRequests(0, 1)
	if len(first) != 1 || total != 2 || next == 0 || !first[0].Blocks || first[0].Space != "OPS" {
		t.Fatalf("first page %+v, total %d, next %d", first, total, next)
	}
	second, total, next := anna.AccessRequests(next, 1)
	if len(second) != 1 || total != 2 || next != 0 || second[0].Item != page.ID || second[0].Blocks {
		t.Fatalf("second page %+v, total %d, next %d", second, total, next)
	}
}

// A request on an item the decider may no longer read is not listed to
// them (D10), nor is one on an item in the trash.
func TestAccessRequestsNeverNameAnUnreadableItem(t *testing.T) {
	s, page, sec := secretFixture(t)
	noErr(t, s.As("ben").RequestSecret(page.ID, sec.ID))
	sameList(t, "anna", requestsOf(t, s, "anna"), page.Key+"#1 ben")
	// The administrator closes the item to everyone but themself: anna,
	// who protected the passage, reads the item no more.
	noErr(t, s.As("pat").Restrict(page.ID, Restriction{Read: []string{"pat"}}))
	sameList(t, "anna", requestsOf(t, s, "anna"))
	sameList(t, "pat", requestsOf(t, s, "pat"), page.Key+"#1 ben")
	noErr(t, s.As("pat").Restrict(page.ID, Restriction{}))
	noErr(t, s.As("pat").Trash(page.ID))
	sameList(t, "pat", requestsOf(t, s, "pat"))
	sameList(t, "anna", requestsOf(t, s, "anna"))
}
