package item

import (
	"errors"
	"slices"
	"testing"
)

func kinds(ns []Notification) []string {
	var out []string
	for _, n := range ns {
		k := string(n.Kind) + ":" + n.Title
		if n.Created {
			k += ":child"
		}
		out = append(out, k)
	}
	return out
}

func TestWatchersAreToldWhatOthersChange(t *testing.T) {
	s := permFixture(t)
	anna, ben, carl := s.As("anna"), s.As("ben"), s.As("carl")
	page := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Runbook"}))
	other := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Other"}))
	noErr(t, anna.SetTitle(page.ID, "Before the watch"))

	noErr(t, ben.Watch(page.ID, true))
	noErr(t, ben.Watch(page.ID, true)) // twice is once
	if !ben.Watching(page.ID) || len(ben.Watches()) != 1 {
		t.Fatalf("watches %v", ben.Watches())
	}
	if err := carl.Watch(page.ID, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an outsider watches: %v", err)
	}
	if err := carl.WatchSpace("PLAT", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an outsider watches a space: %v", err)
	}

	noErr(t, anna.SetTitle(page.ID, "Runbook v2"))
	noErr(t, anna.CommitBody(page.ID, Doc{Blocks: []Block{{Kind: Paragraph, Text: "steps"}}}))
	mustComment(t)(anna.AddComment(page.ID, "looks good"))
	mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Child", Parent: page.ID}))
	noErr(t, anna.SetTitle(other.ID, "Not watched"))
	noErr(t, s.GrantRole("root", "PLAT", "ben", Editor))
	noErr(t, ben.SetTitle(page.ID, "Runbook v3")) // one's own change is no news

	got, more := ben.Notifications(0, 10)
	want := []string{"ItemCreated:Child:child", "CommentAdded:Runbook v3", "BodyCommitted:Runbook v3", "TitleSet:Runbook v3"}
	if !slices.Equal(kinds(got), want) || more {
		t.Fatalf("notifications %v (more %v), want %v", kinds(got), more, want)
	}
	if got[0].Actor != "anna" || got[0].Key == "" {
		t.Errorf("notification %+v", got[0])
	}
	// Paging: newest first, then older ones before the last seen.
	first, more := ben.Notifications(0, 2)
	if len(first) != 2 || !more {
		t.Fatalf("first page %v, more %v", kinds(first), more)
	}
	rest, more := ben.Notifications(first[1].Seq, 10)
	if !slices.Equal(kinds(rest), want[2:]) || more {
		t.Fatalf("second page %v", kinds(rest))
	}
	if n := ben.Unread(first[1].Seq, 100); n != 1 {
		t.Errorf("unread after the second newest: %d", n)
	}
	if n := ben.Unread(0, 2); n != 2 {
		t.Errorf("unread is capped: %d", n)
	}

	// A space watch covers every item; a restriction hides an item's news.
	noErr(t, ben.Watch(page.ID, false))
	noErr(t, ben.WatchSpace("PLAT", true))
	noErr(t, anna.SetTitle(other.ID, "Now watched"))
	secret := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Secret"}))
	noErr(t, anna.Restrict(secret.ID, Restriction{Read: []string{"anna"}}))
	noErr(t, anna.SetTitle(secret.ID, "Still secret"))
	got, _ = ben.Notifications(0, 1)
	if len(got) != 1 || got[0].Title != "Now watched" {
		t.Fatalf("after a space watch %v", kinds(got))
	}
	// Watches are no part of an item's history, and survive a replay.
	for _, e := range s.History(page.ID) {
		if e.Kind == Watched || e.Kind == Unwatched {
			t.Fatalf("history holds %s", e.Kind)
		}
	}
	again, err := Replay(s.Log())
	noErr(t, err)
	if !again.As("ben").WatchingSpace("PLAT") || again.As("ben").Watching(page.ID) {
		t.Error("watches do not replay")
	}
	if len(carl.Watches()) != 0 {
		t.Error("an outsider has watches")
	}
}

func TestCreatorsWatchWhatTheyCreate(t *testing.T) {
	s := permFixture(t)
	anna, ben := s.As("anna"), s.As("ben")
	noErr(t, s.GrantRole("root", "PLAT", "ben", Editor))
	page := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Runbook"}))
	if !anna.Watching(page.ID) || ben.Watching(page.ID) {
		t.Fatalf("the creator watches %v, another %v", anna.Watching(page.ID), ben.Watching(page.ID))
	}
	// The watch is no change of the page.
	if h, _ := anna.History(page.ID); len(h) != 1 || h[0].Kind != ItemCreated {
		t.Fatalf("history %v", h)
	}
	if got, _ := anna.Notifications(0, 10); len(got) != 0 {
		t.Fatalf("one's own creation is news: %v", kinds(got))
	}
	noErr(t, ben.SetTitle(page.ID, "Runbook v2"))
	if got, _ := anna.Notifications(0, 10); !slices.Equal(kinds(got), []string{"TitleSet:Runbook v2"}) {
		t.Fatalf("notifications %v", kinds(got))
	}

	inline := mustItem(t)(ben.CreateEmbedded(page.ID, NewItem{Type: "page", Title: "Inline"}))
	if !ben.Watching(inline.ID) || ben.Watching(page.ID) {
		t.Fatal("the creator of an inline item watches it, and only it")
	}

	// Stopping is for good; the store alone does not watch for anyone.
	noErr(t, anna.Watch(page.ID, false))
	if anna.Watching(page.ID) {
		t.Fatal("still watching")
	}
	plain := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "Plain"}))
	if anna.Watching(plain.ID) {
		t.Fatal("a create without Watch watches")
	}
}

// A comment that names someone by @username tells them, whether or not
// they watch the item; a deleted one no longer does (ADR-0084).
func TestAMentionInACommentIsANotification(t *testing.T) {
	s := permFixture(t)
	anna, ben, carl := s.As("anna"), s.As("ben").Named("ben"), s.As("carl").Named("carl")
	page := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Runbook"}))
	mustComment(t)(anna.AddComment(page.ID, "no one named"))
	c := mustComment(t)(anna.AddComment(page.ID, "@Ben, please look"))
	mustComment(t)(anna.AddComment(page.ID, "@benjamin is someone else"))
	mustComment(t)(anna.AddComment(page.ID, "@carl may not read this"))
	got, _ := ben.Notifications(0, 10)
	if len(got) != 1 || !got[0].Mentioned || got[0].Kind != CommentAdded || got[0].Title != "Runbook" || ben.Unread(0, 10) != 1 {
		t.Fatalf("ben is told %+v", got)
	}
	if got, _ := s.As("ben").Notifications(0, 10); len(got) != 0 {
		t.Errorf("without a name: %+v", got)
	}
	if got, _ := carl.Notifications(0, 10); len(got) != 0 {
		t.Errorf("one who may not read the item is told %+v", got)
	}
	noErr(t, s.GrantRole("root", "PLAT", "ben", Editor))
	mustComment(t)(ben.AddComment(page.ID, "@ben note to self"))
	if n := ben.Unread(0, 10); n != 1 {
		t.Errorf("one's own mention is news: %d", n)
	}
	noErr(t, anna.DeleteComment(page.ID, c.ID))
	if got, _ := ben.Notifications(0, 10); len(got) != 0 {
		t.Errorf("a deleted comment still tells %+v", got)
	}
}

// A name added to a text by someone else's edit is told once; a name in a
// protected passage is not (ADR-0096).
func TestAMentionInATextIsANotification(t *testing.T) {
	s := permFixture(t)
	anna, ben := s.As("anna"), s.As("ben").Named("ben")
	page := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Runbook"}))
	text := func(s string, marks ...Mark) Doc {
		return Doc{Blocks: []Block{{Kind: Paragraph, Text: s, Marks: marks}}}
	}
	noErr(t, anna.CommitBody(page.ID, text("Ask @Ben about it.")))
	got, _ := ben.Notifications(0, 10)
	if len(got) != 1 || !got[0].Mentioned || got[0].Kind != BodyCommitted {
		t.Fatalf("ben is told %+v", got)
	}
	noErr(t, anna.CommitBody(page.ID, text("Ask @Ben about it, soon.")))
	if n := ben.Unread(0, 10); n != 1 {
		t.Errorf("a name already there is told again: %d", n)
	}
	if got := bodyMentions(text("Secret: @ben", Mark{Type: MarkSecret, Start: 8, End: 12}), "ben"); got {
		t.Error("a name in a protected passage counts")
	}
	if !bodyMentions(text("Hi @ben"), "BEN") || bodyMentions(text("Hi @benjamin"), "ben") {
		t.Error("names are compared as in comments")
	}
}
