package item

import "testing"

func TestMentions(t *testing.T) {
	for text, want := range map[string]bool{
		"@bert please look":   true,
		"thanks @Bert.":       true,
		"@berta did it":       false,
		"mail bert@x.org":     false,
		"(@bert)":             true,
		"cc @bert-team":       false,
		"@bert, @berta":       true,
		"no mention of bert":  false,
		"@bert\nnext line":    true,
		"ends with @bert":     true,
		"twice @berta @bert!": true,
	} {
		if got := Mentions(text, "bert"); got != want {
			t.Errorf("Mentions(%q) = %v", text, got)
		}
	}
	if Mentions("@", "") {
		t.Error("an empty username is never mentioned")
	}
}

// TestCommentFeed: the overview holds comments on what one watches, one's
// own and those that mention one; only on items one may read; newest
// first, paged.
func TestCommentFeed(t *testing.T) {
	s := permFixture(t)
	anna, ben, carl := s.As("anna"), s.As("ben"), s.As("carl")
	runbook := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Runbook"}))
	faq := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "FAQ"}))
	_, err := anna.AddComment(runbook.ID, "First draft")
	noErr(t, err)
	_, err = anna.AddComment(faq.ID, "@ben can you check?")
	noErr(t, err)
	noErr(t, ben.Watch(runbook.ID, true))
	_, err = anna.AddComment(faq.ID, "unrelated")
	noErr(t, err)

	got, more := ben.CommentFeed("ben", "", 0, 10)
	if more || len(got) != 2 {
		t.Fatalf("ben's overview: %+v", got)
	}
	if got[0].Key != faq.Key || got[0].Why[0] != FeedMentions || got[1].Key != runbook.Key || got[1].Why[0] != FeedWatched {
		t.Fatalf("order and reasons: %+v", got)
	}
	if got, _ := ben.CommentFeed("ben", FeedMentions, 0, 10); len(got) != 1 {
		t.Fatalf("mentions only: %+v", got)
	}
	if got, _ := anna.CommentFeed("anna", FeedMine, 0, 10); len(got) != 3 {
		t.Fatalf("anna's own: %+v", got)
	}
	if got, more := anna.CommentFeed("anna", "", 1, 1); len(got) != 1 || !more {
		t.Fatalf("paged: %+v %v", got, more)
	}
	if got, _ := carl.CommentFeed("ben", "", 0, 10); len(got) != 0 {
		t.Fatalf("an outsider sees %+v", got)
	}
}
