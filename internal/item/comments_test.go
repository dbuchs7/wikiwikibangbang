package item

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// mustComment wraps a call returning (Comment, error) and fails the test on error.
func mustComment(t *testing.T) func(Comment, error) Comment {
	return func(c Comment, err error) Comment {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
}

func TestCommentsAreAddedEditedAndDeleted(t *testing.T) {
	s := setup(t)
	page := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "Runbook"}))

	first := mustComment(t)(s.AddComment("anna", page.ID, "Looks good"))
	second := mustComment(t)(s.AddComment("bert", page.ID, "Step 3 is <b>wrong</b>"))
	if first.Author != "anna" || first.Text != "Looks good" || first.Created.IsZero() || !first.Edited.IsZero() {
		t.Fatalf("first comment %+v", first)
	}
	noErr(t, s.EditComment("bert", page.ID, second.ID, "Step 3 is fine"))
	noErr(t, s.DeleteComment("anna", page.ID, first.ID))

	got, _ := s.Get(page.ID)
	if len(got.Comments) != 2 {
		t.Fatalf("%d comments, want 2 (a deleted one keeps its place)", len(got.Comments))
	}
	if c := got.Comments[0]; !c.Deleted || c.Text != "" {
		t.Fatalf("deleted comment %+v still shows text", c)
	}
	if c := got.Comments[1]; c.Text != "Step 3 is fine" || c.Edited.IsZero() || c.Author != "bert" {
		t.Fatalf("edited comment %+v", c)
	}
	// A comment is not a change of the item itself (ADR-0035).
	if got.Version != page.Version {
		t.Fatalf("version %d, want %d", got.Version, page.Version)
	}
	// The history names the comment events; they are the item's.
	var kinds []EventKind
	for _, e := range s.History(page.ID) {
		kinds = append(kinds, e.Kind)
	}
	for _, k := range []EventKind{CommentAdded, CommentEdited, CommentDeleted} {
		if !slices.Contains(kinds, k) {
			t.Errorf("history has no %s: %v", k, kinds)
		}
	}
}

func TestCommentsAreValidated(t *testing.T) {
	s := setup(t)
	page := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "Runbook"}))
	c := mustComment(t)(s.AddComment("anna", page.ID, "ok"))

	if _, err := s.AddComment("anna", page.ID, "  \n "); !errors.Is(err, ErrValidation) {
		t.Errorf("empty comment: %v", err)
	}
	if err := s.EditComment("anna", page.ID, c.ID, ""); !errors.Is(err, ErrValidation) {
		t.Errorf("empty edit: %v", err)
	}
	if err := s.EditComment("anna", page.ID, "nope", "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown comment: %v", err)
	}
	if _, err := s.AddComment("anna", "nope", "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown item: %v", err)
	}
	noErr(t, s.DeleteComment("anna", page.ID, c.ID))
	if err := s.EditComment("anna", page.ID, c.ID, "x"); !errors.Is(err, ErrValidation) {
		t.Errorf("edit after delete: %v", err)
	}
	if err := s.DeleteComment("anna", page.ID, c.ID); !errors.Is(err, ErrValidation) {
		t.Errorf("second delete: %v", err)
	}
	noErr(t, s.Trash("anna", page.ID))
	if _, err := s.AddComment("anna", page.ID, "x"); !errors.Is(err, ErrValidation) {
		t.Errorf("comment on a trashed item: %v", err)
	}
}

func TestCommentRights(t *testing.T) {
	s := permFixture(t)
	anna, ben, carl, pat := s.As("anna"), s.As("ben"), s.As("carl"), s.As("pat")
	page := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Runbook"}))

	c := mustComment(t)(anna.AddComment(page.ID, "mine"))
	// A viewer reads comments but does not write them (ADR-0035).
	if got, err := ben.Comments(page.ID); err != nil || len(got) != 1 || got[0].Text != "mine" {
		t.Fatalf("viewer reads %v, %v", got, err)
	}
	if _, err := ben.AddComment(page.ID, "x"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("viewer comments: %v", err)
	}
	// An outsider learns nothing, not even that the item exists (D10).
	if _, err := carl.Comments(page.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("outsider reads comments: %v", err)
	}
	if _, err := carl.AddComment(page.ID, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("outsider comments: %v", err)
	}
	// Only the author edits; the author or a space admin deletes.
	if err := pat.EditComment(page.ID, c.ID, "x"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("someone else edits: %v", err)
	}
	noErr(t, anna.EditComment(page.ID, c.ID, "mine, edited"))
	other := mustComment(t)(pat.AddComment(page.ID, "from the admin"))
	if err := anna.DeleteComment(page.ID, other.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("editor deletes someone else's comment: %v", err)
	}
	noErr(t, pat.DeleteComment(page.ID, c.ID))

	// A restriction that takes away the edit right takes the comment right.
	noErr(t, pat.Restrict(page.ID, Restriction{Edit: []string{"pat"}}))
	if _, err := anna.AddComment(page.ID, "x"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("comment despite the edit restriction: %v", err)
	}
	// A read restriction hides the comments.
	noErr(t, pat.Restrict(page.ID, Restriction{Read: []string{"pat"}}))
	if _, err := ben.Comments(page.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("restricted comments read: %v", err)
	}
}

func TestErasureRemovesComments(t *testing.T) {
	for _, how := range []string{"purge", "redact"} {
		t.Run(how, func(t *testing.T) {
			s := setup(t)
			noErr(t, s.GrantSystemRole("admin", "carla", RoleErasure))
			page := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "Runbook"}))
			c := mustComment(t)(s.AddComment("anna", page.ID, "secret one"))
			noErr(t, s.EditComment("anna", page.ID, c.ID, "secret two"))
			switch how {
			case "purge":
				noErr(t, s.Trash("anna", page.ID))
				noErr(t, s.Purge("anna", page.ID))
			case "redact":
				noErr(t, s.Redact("carla", page.ID, "personal data"))
			}
			got, _ := s.Get(page.ID)
			if len(got.Comments) != 0 {
				t.Fatalf("a %sd item keeps %d comments", how, len(got.Comments))
			}
			for _, e := range s.Log() {
				if strings.Contains(e.Text, "secret") {
					t.Fatalf("event %d still holds the comment text", e.Seq)
				}
			}
		})
	}
}

func TestInlineCommentsRepliesAndResolving(t *testing.T) {
	s := permFixture(t)
	anna, ben, pat := s.As("anna"), s.As("ben"), s.As("pat")
	page := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Runbook"}))
	noErr(t, anna.CommitBody(page.ID, Doc{Blocks: []Block{{Kind: Paragraph, Text: "Stop the writers first."}}}))
	at := &Anchor{Quote: "writers", Prefix: "Stop the ", Suffix: " first."}

	root := mustComment(t)(anna.Comment(page.ID, NewComment{Text: "Which ones?", Anchor: at}))
	at.Quote = "changed after the call"
	if root.Anchor == nil || root.Anchor.Quote != "writers" || root.Reply != "" {
		t.Fatalf("inline comment %+v", root)
	}
	if _, err := ben.Comment(page.ID, NewComment{Text: "me too", Reply: root.ID}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("a viewer replies: %v", err)
	}
	reply := mustComment(t)(pat.Comment(page.ID, NewComment{Text: "All of them.", Reply: root.ID}))
	// Answering a reply answers the thread.
	again := mustComment(t)(anna.Comment(page.ID, NewComment{Text: "Thanks.", Reply: reply.ID}))
	if reply.Reply != root.ID || again.Reply != root.ID {
		t.Fatalf("replies answer %s and %s, want %s", reply.Reply, again.Reply, root.ID)
	}

	if err := anna.ResolveComment(page.ID, reply.ID, true); !errors.Is(err, ErrValidation) {
		t.Fatalf("a reply is resolved: %v", err)
	}
	if err := ben.ResolveComment(page.ID, root.ID, true); !errors.Is(err, ErrForbidden) {
		t.Fatalf("a viewer resolves: %v", err)
	}
	noErr(t, pat.ResolveComment(page.ID, root.ID, true))
	noErr(t, pat.ResolveComment(page.ID, root.ID, true)) // twice is once
	got, _ := anna.Comments(page.ID)
	if c := got[0]; !c.Resolved || c.ResolvedBy != "pat" || c.ResolvedAt.IsZero() {
		t.Fatalf("resolved thread %+v", c)
	}
	noErr(t, anna.ResolveComment(page.ID, root.ID, false))
	got, _ = anna.Comments(page.ID)
	if c := got[0]; c.Resolved || c.ResolvedBy != "" {
		t.Fatalf("reopened thread %+v", c)
	}

	// Deleting the first comment hides its passage; the thread takes no
	// more answers.
	noErr(t, anna.DeleteComment(page.ID, root.ID))
	got, _ = anna.Comments(page.ID)
	if got[0].Anchor != nil {
		t.Fatalf("a deleted comment keeps its passage: %+v", got[0])
	}
	if _, err := pat.Comment(page.ID, NewComment{Text: "late", Reply: reply.ID}); !errors.Is(err, ErrValidation) {
		t.Fatalf("an answer to a deleted thread: %v", err)
	}
	if h := s.History(page.ID); !slices.ContainsFunc(h, func(e Event) bool { return e.Kind == CommentResolved }) ||
		!slices.ContainsFunc(h, func(e Event) bool { return e.Kind == CommentReopened }) {
		t.Fatal("the history does not show resolving and reopening")
	}
}

func TestInlineCommentsAreValidated(t *testing.T) {
	s := setup(t)
	page := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "Runbook"}))
	root := mustComment(t)(s.AddComment("anna", page.ID, "general"))
	cases := map[string]NewComment{
		"empty quote":         {Text: "x", Anchor: &Anchor{Quote: "  "}},
		"reply with a quote":  {Text: "x", Reply: root.ID, Anchor: &Anchor{Quote: "a"}},
		"reply to no comment": {Text: "x", Reply: "nope"},
	}
	for name, n := range cases {
		if _, err := s.Comment("anna", page.ID, n); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestInlineCommentTextIsGoverned(t *testing.T) {
	s := setup(t)
	noErr(t, s.GrantRole("root", "PLAT", "anna", Editor))
	page := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "Runbook"}))
	mustComment(t)(s.Comment("anna", page.ID, NewComment{Text: "see this", Anchor: &Anchor{Quote: "card 4111 1111 1111 1111"}}))
	got, _ := s.Get(page.ID)
	if !strings.Contains(strings.Join(got.Restriction.Read, ","), "anna") {
		t.Fatalf("sensitive data in a quote does not restrict the item: %+v", got.Restriction)
	}
}
