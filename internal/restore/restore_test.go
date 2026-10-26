package restore

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

func noErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func must[T any](t *testing.T) func(T, error) T {
	return func(v T, err error) T {
		t.Helper()
		noErr(t, err)
		return v
	}
}

const pic = "0123456789abcdef0123456789abcdef"

// backup is a store as a backup holds it: space A with a page that
// mentions an item of space B, embeds its child, shows a picture and
// protects a name; comments, labels, a restriction and a relation; an
// item in the trash and one deleted for good.
type backup struct {
	s                                  *item.Store
	page, child, target, other, binned item.Item
	erased                             item.Item
	files                              map[item.ID][]byte
}

func newBackup(t *testing.T) backup {
	t.Helper()
	s := item.NewStore()
	noErr(t, s.CreateSpace("root", "A", "Alpha"))
	noErr(t, s.CreateSpace("root", "B", "Beta"))
	noErr(t, s.DefineType("root", item.TypeDef{Name: "page", Body: true}))
	noErr(t, s.GrantRole("root", "A", "anna", item.Editor))
	noErr(t, s.GrantRole("root", "A", "@team", item.Viewer))
	noErr(t, s.GrantRole("root", "A", "gone", item.Viewer))
	b := backup{s: s, files: map[item.ID][]byte{pic: []byte("\x89PNG\r\n\x1a\nfake")}}
	b.target = must[item.Item](t)(s.Create("anna", item.NewItem{Space: "B", Type: "page", Title: "Target"}))
	b.other = must[item.Item](t)(s.Create("anna", item.NewItem{Space: "B", Type: "page", Title: "Unreferenced"}))
	b.page = must[item.Item](t)(s.Create("anna", item.NewItem{Space: "A", Type: "page", Title: "Page"}))
	b.child = must[item.Item](t)(s.Create("anna", item.NewItem{Space: "A", Type: "page", Parent: b.page.ID, Title: "Child"}))
	sum := sha256.Sum256(b.files[pic])
	must[item.Attachment](t)(s.AddAttachment("anna", b.page.ID, item.Attachment{ID: pic, Name: "pic.png", Type: "image/png",
		Size: int64(len(b.files[pic])), SHA256: hex.EncodeToString(sum[:])}))
	noErr(t, s.CommitBody("anna", b.page.ID, item.Doc{Blocks: []item.Block{
		{Kind: item.Paragraph, Text: "See Target now", Mentions: []item.ID{b.target.ID}, Marks: []item.Mark{{Type: item.MarkMention, Start: 4, End: 10, Ref: b.target.ID}}},
		{Kind: item.Embed, Ref: b.child.ID},
		{Kind: item.Image, Ref: pic, Text: "a picture"},
		{Kind: item.Paragraph, Text: "Paul Muster is new."},
	}}))
	must[item.Secret](t)(s.As("anna").Protect(b.page.ID, 3, 0, 11, []string{"@hr"}))
	noErr(t, s.SetLabels("anna", b.page.ID, []string{"kept"}))
	noErr(t, s.Restrict("anna", b.child.ID, item.Restriction{Read: []string{"anna"}}))
	noErr(t, s.AddRelation("anna", "depends-on", b.child.ID, b.target.ID))
	q := must[item.Comment](t)(s.Comment("ben", b.page.ID, item.NewComment{Text: "Why?"}))
	must[item.Comment](t)(s.Comment("anna", b.page.ID, item.NewComment{Text: "Because.", Reply: q.ID}))
	noErr(t, s.ResolveComment("anna", b.page.ID, q.ID, true))
	gone := must[item.Comment](t)(s.Comment("ben", b.page.ID, item.NewComment{Text: "Oops"}))
	noErr(t, s.DeleteComment("ben", b.page.ID, gone.ID))
	b.binned = must[item.Item](t)(s.Create("anna", item.NewItem{Space: "A", Type: "page", Title: "Binned"}))
	noErr(t, s.Trash("anna", b.binned.ID))
	b.erased = must[item.Item](t)(s.Create("anna", item.NewItem{Space: "A", Type: "page", Title: "Erased secret"}))
	noErr(t, s.Trash("anna", b.erased.ID))
	noErr(t, s.Purge("root", b.erased.ID))
	return b
}

func (b backup) source() Source {
	return Source{Store: b.s, Instance: "bk1", Open: func(_, a item.ID) (io.ReadCloser, error) {
		data, ok := b.files[a]
		if !ok {
			return nil, errors.New("no such file")
		}
		return io.NopCloser(bytes.NewReader(data)), nil
	}}
}

func get(t *testing.T, s *item.Store, id item.ID) item.Item {
	t.Helper()
	return must[item.Item](t)(s.Get(id))
}

func outcome(list []Outcome, id item.ID) (Outcome, bool) {
	i := slices.IndexFunc(list, func(o Outcome) bool { return o.ID == id })
	if i < 0 {
		return Outcome{}, false
	}
	return list[i], true
}

// TestASpaceComesBackWhole covers ADR-0124 into an installation that does
// not know the space: every item of it comes back with its identity and
// content, and the item it mentions in another space comes back too.
func TestASpaceComesBackWhole(t *testing.T) {
	b := newBackup(t)
	target := item.NewStore()
	rep, err := Run(dry{target}, b.source(), Options{Spaces: []string{"A"}, Actor: "restore",
		Users: map[string]bool{"anna": true}, Grants: []item.Grant{{Principal: "admin", Role: item.SpaceAdmin}}})
	noErr(t, err)

	if len(rep.Restored) != 3 || !slices.Equal(rep.Spaces, []string{"A", "B"}) || rep.Granted != 3 {
		t.Fatalf("restored %+v, spaces %v, granted %d", rep.Restored, rep.Spaces, rep.Granted)
	}
	if o, ok := outcome(rep.Restored, b.target.ID); !ok || o.Reason != "referenced by "+b.page.Key {
		t.Fatalf("the mentioned item: %+v", o)
	}
	if _, ok := outcome(rep.Restored, b.other.ID); ok {
		t.Fatal("an item nothing refers to came back from another space")
	}
	if o, _ := outcome(rep.Skipped, b.binned.ID); o.Reason != TrashedInBackup {
		t.Fatalf("an item in the backup's trash: %+v", o)
	}
	if o, _ := outcome(rep.Skipped, b.erased.ID); o.Reason != ErasedInBackup || o.Title != "" || len(rep.Erased()) != 1 {
		t.Fatalf("an item deleted for good in the backup: %+v", o)
	}
	for _, want := range []string{"group @team had the role viewer", "user gone of the backup has no account here"} {
		if !slices.ContainsFunc(rep.Notes, func(n string) bool { return strings.Contains(n, want) }) {
			t.Errorf("no note %q in %v", want, rep.Notes)
		}
	}
	if target.GrantedRole("A", "anna") != item.Editor || target.GrantedRole("A", "@team") != 0 || target.GrantedRole("B", "admin") != item.SpaceAdmin {
		t.Fatal("the roles of a created space")
	}

	page := get(t, target, b.page.ID)
	if page.Key != b.page.Key || page.Title != "Page" || !slices.Equal(page.Labels, []string{"kept"}) || page.Origin == nil ||
		page.Origin.System != System || page.Origin.Instance != "bk1" || page.Origin.ExternalID != string(b.page.ID) {
		t.Fatalf("the page: %+v", page)
	}
	if len(page.Attachments) != 1 || page.Attachments[0].ID == pic || page.Attachments[0].Name != "pic.png" {
		t.Fatalf("the page's files: %+v", page.Attachments)
	}
	blocks := page.Body.Blocks
	if len(blocks) != 4 || blocks[0].Mentions[0] != b.target.ID || blocks[1].Ref != b.child.ID || blocks[2].Ref != page.Attachments[0].ID {
		t.Fatalf("the page's text: %+v", blocks)
	}
	if len(page.Secrets) != 1 || !slices.Equal(page.Secrets[0].Readers, []string{"@hr"}) || page.Secrets[0].By != "anna" ||
		!slices.ContainsFunc(blocks[3].Marks, func(m item.Mark) bool { return m.Type == item.MarkSecret && m.Ref == page.Secrets[0].ID }) {
		t.Fatalf("the protected passage: %+v %+v", page.Secrets, blocks[3])
	}
	var comments []string
	for _, c := range page.Comments {
		comments = append(comments, c.Author+":"+c.Text)
	}
	if !slices.Equal(comments, []string{"ben:Why?", "anna:Because."}) || !page.Comments[0].Resolved || page.Comments[1].Reply != page.Comments[0].ID {
		t.Fatalf("the comments: %+v", page.Comments)
	}
	if len(page.Restriction.Read)+len(page.Restriction.Edit) > 0 {
		t.Fatalf("the page is restricted: %+v", page.Restriction)
	}
	child := get(t, target, b.child.ID)
	if child.Parent != b.page.ID || !slices.Equal(child.Restriction.Read, []string{"anna"}) {
		t.Fatalf("the child: %+v", child)
	}
	if rels := target.Relations(b.child.ID, item.Outgoing); !slices.ContainsFunc(rels, func(r item.Relation) bool { return r.Type == "depends-on" && r.To == b.target.ID }) {
		t.Fatalf("the relation: %+v", rels)
	}

	// Again: everything is there now, and nothing changes.
	seq := target.Seq()
	again, err := Run(dry{target}, b.source(), Options{Spaces: []string{"A"}, Actor: "restore"})
	noErr(t, err)
	if len(again.Restored) != 0 || target.Seq() != seq {
		t.Fatalf("a second run restored %+v", again.Restored)
	}
	if o, _ := outcome(again.Skipped, b.page.ID); o.Reason != Exists {
		t.Fatalf("a second run: %+v", o)
	}
}

// TestNothingThereIsOverwrittenAndNothingErasedComesBack covers the same
// installation: what it has is skipped, what it deleted for good is
// reported by key alone and never comes back, and nothing is written.
func TestNothingThereIsOverwrittenAndNothingErasedComesBack(t *testing.T) {
	b := newBackup(t)
	target := must[*item.Store](t)(item.Replay(b.s.Log()))
	noErr(t, target.SetTitle("anna", b.page.ID, "Changed since"))
	noErr(t, target.Redact("root", b.child.ID, "erasure request 2026-17"))
	noErr(t, target.Redact("root", b.target.ID, "erasure request 2026-18"))
	seq := target.Seq()
	rep, err := Run(dry{target}, b.source(), Options{Spaces: []string{"A", "B"}, Actor: "restore"})
	noErr(t, err)
	if len(rep.Restored) != 0 || target.Seq() != seq {
		t.Fatalf("restored %+v", rep.Restored)
	}
	want := map[item.ID]string{b.page.ID: Exists, b.child.ID: Erased, b.target.ID: Erased, b.other.ID: Exists}
	for id, reason := range want {
		o, _ := outcome(rep.Skipped, id)
		if o.Reason != reason || (reason == Erased) != (o.Title == "") {
			t.Errorf("%s: %+v, want %s", id, o, reason)
		}
	}
	if len(rep.Erased()) != 3 {
		t.Fatalf("erased %+v", rep.Erased())
	}

	// In the trash, it is reported, not touched.
	target = must[*item.Store](t)(item.Replay(b.s.Log()))
	noErr(t, target.Trash("anna", b.other.ID))
	rep, err = Run(dry{target}, b.source(), Options{Spaces: []string{"B"}, Actor: "restore"})
	noErr(t, err)
	if o, _ := outcome(rep.Skipped, b.other.ID); o.Reason != InTrash || get(t, target, b.other.ID).Presence != item.Trashed {
		t.Fatalf("an item in the trash: %+v", o)
	}
}

// TestALaterBackupFillsAnOlderInstallation covers an installation restored
// from an older backup: what came later comes back, below a parent it
// still has or at the top when the parent was deleted for good, and a
// reference to what it deleted for good is left out.
func TestALaterBackupFillsAnOlderInstallation(t *testing.T) {
	s := item.NewStore()
	noErr(t, s.CreateSpace("root", "A", "Alpha"))
	noErr(t, s.DefineType("root", item.TypeDef{Name: "page", Body: true}))
	keep := must[item.Item](t)(s.Create("anna", item.NewItem{Space: "A", Type: "page", Title: "Kept parent"}))
	lost := must[item.Item](t)(s.Create("anna", item.NewItem{Space: "A", Type: "page", Title: "Lost parent"}))
	older := s.Log()
	under := must[item.Item](t)(s.Create("anna", item.NewItem{Space: "A", Type: "page", Parent: keep.ID, Title: "Under kept"}))
	orphan := must[item.Item](t)(s.Create("anna", item.NewItem{Space: "A", Type: "page", Parent: lost.ID, Title: "Under lost",
		Body: &item.Doc{Blocks: []item.Block{{Kind: item.Paragraph, Text: "See Lost", Mentions: []item.ID{lost.ID},
			Marks: []item.Mark{{Type: item.MarkMention, Start: 4, End: 8, Ref: lost.ID}}}, {Kind: item.Embed, Ref: lost.ID}}}}))

	target := must[*item.Store](t)(item.Replay(older))
	noErr(t, target.Trash("anna", lost.ID))
	noErr(t, target.Purge("root", lost.ID))
	other := must[item.Item](t)(target.Create("anna", item.NewItem{Space: "A", Type: "page", Title: "Here only"}))
	rep, err := Run(dry{target}, Source{Store: s, Instance: "bk2"}, Options{Spaces: []string{"A"}, Actor: "restore"})
	noErr(t, err)
	if len(rep.Restored) != 2 {
		t.Fatalf("restored %+v, skipped %+v", rep.Restored, rep.Skipped)
	}
	if got := get(t, target, under.ID); got.Parent != keep.ID {
		t.Fatalf("below the kept parent: %+v", got)
	}
	got := get(t, target, orphan.ID)
	if got.Parent != "" || len(got.Body.Blocks) != 1 || len(got.Body.Blocks[0].Mentions) != 0 || len(got.Body.Blocks[0].Marks) != 0 || got.Body.Blocks[0].Text != "See Lost" {
		t.Fatalf("below the lost parent: %+v", got)
	}
	// "Here only" took the key the backup gave "Under kept".
	if other.Key != under.Key || get(t, target, under.ID).Key == under.Key || got.Key != orphan.Key {
		t.Fatalf("keys: here %s, under kept %s, under lost %s", other.Key, get(t, target, under.ID).Key, got.Key)
	}
	for _, want := range []string{"placed at the top", "plain text now", "embedded " + lost.Key + " is left out", "comes back as"} {
		if !slices.ContainsFunc(rep.Notes, func(n string) bool { return strings.Contains(n, want) }) {
			t.Errorf("no note %q in %v", want, rep.Notes)
		}
	}
}

func TestWhatCannotComeBack(t *testing.T) {
	b := newBackup(t)
	if _, err := Run(dry{item.NewStore()}, b.source(), Options{Spaces: []string{"Z"}, Actor: "restore"}); err == nil {
		t.Fatal("a space the backup does not have")
	}
	// A space in the trash here, and one deleted for good.
	target := item.NewStore()
	noErr(t, target.CreateSpace("root", "A", "Alpha"))
	noErr(t, target.CreateSpace("root", "B", "Beta"))
	noErr(t, target.TrashSpace("root", "A"))
	noErr(t, target.TrashSpace("root", "B"))
	noErr(t, target.PurgeSpace("root", "B"))
	rep, err := Run(dry{target}, b.source(), Options{Spaces: []string{"A", "B"}, Actor: "restore"})
	noErr(t, err)
	if o, _ := outcome(rep.Skipped, b.page.ID); o.Reason != SpaceTrashed {
		t.Fatalf("a space in the trash: %+v", o)
	}
	if o, _ := outcome(rep.Skipped, b.other.ID); o.Reason != SpacePurged {
		t.Fatalf("a space deleted for good: %+v", o)
	}
	// A type here that refuses the item, and a file that cannot be read.
	target = item.NewStore()
	noErr(t, target.DefineType("root", item.TypeDef{Name: "page", Body: true, Fields: []item.FieldDef{{Name: "owner", Kind: item.KindText, Required: true}}}))
	src := b.source()
	src.Open = func(item.ID, item.ID) (io.ReadCloser, error) { return nil, errors.New("disk gone") }
	rep, err = Run(dry{target}, src, Options{Spaces: []string{"A"}, Actor: "restore"})
	noErr(t, err)
	if o, _ := outcome(rep.Skipped, b.page.ID); !strings.HasPrefix(o.Reason, "refused: ") {
		t.Fatalf("a type that refuses it: %+v", o)
	}
	target = item.NewStore()
	rep, err = Run(dry{target}, src, Options{Spaces: []string{"A"}, Actor: "restore"})
	noErr(t, err)
	if page := get(t, target, b.page.ID); len(page.Attachments) != 0 || slices.ContainsFunc(page.Body.Blocks, func(bl item.Block) bool { return bl.Kind == item.Image }) {
		t.Fatalf("a file that cannot be read: %+v", page)
	}
	if !slices.ContainsFunc(rep.Notes, func(n string) bool { return strings.Contains(n, "disk gone") }) {
		t.Fatalf("notes %v", rep.Notes)
	}
}

func TestADryRunChangesNothing(t *testing.T) {
	b := newBackup(t)
	target := item.NewStore()
	d, err := Dry(target)
	noErr(t, err)
	rep, err := Run(d, b.source(), Options{Spaces: []string{"A"}, Actor: "restore"})
	noErr(t, err)
	if len(rep.Restored) != 3 || target.Seq() != 0 {
		t.Fatalf("restored %d, store at %d", len(rep.Restored), target.Seq())
	}
}

// TestReferencesToWhatIsSetAsideAreLeftOut covers references to an item
// the installation has in its trash: they are left out and noted, and a
// file made from another keeps that link.
func TestReferencesToWhatIsSetAsideAreLeftOut(t *testing.T) {
	b := newBackup(t)
	v2 := item.ID("fedcba9876543210fedcba9876543210")
	b.files[v2] = []byte("second version")
	sum := sha256.Sum256(b.files[v2])
	must[item.Attachment](t)(b.s.AddAttachment("anna", b.page.ID, item.Attachment{ID: v2, Name: "pic.png", Type: "text/plain",
		Size: int64(len(b.files[v2])), SHA256: hex.EncodeToString(sum[:]), Source: &item.Source{Item: b.page.ID, Attachment: pic}}))
	root := must[item.Comment](t)(b.s.Comment("ben", b.child.ID, item.NewComment{Text: "Thread"}))
	must[item.Comment](t)(b.s.Comment("anna", b.child.ID, item.NewComment{Text: "Reply", Reply: root.ID}))
	noErr(t, b.s.DeleteComment("ben", b.child.ID, root.ID))

	target := item.NewStore()
	noErr(t, target.CreateSpace("root", "B", "Beta"))
	noErr(t, target.DefineType("root", item.TypeDef{Name: "page", Body: true}))
	upsert := func(it item.Item) {
		_, _, err := target.Upsert("root", item.Upsert{Origin: item.Origin{System: "x", Instance: "y", ExternalID: string(it.ID)},
			ID: it.ID, Space: "B", Type: "page", Title: it.Title})
		noErr(t, err)
	}
	upsert(b.target)
	noErr(t, target.Trash("root", b.target.ID))
	rep, err := Run(dry{target}, b.source(), Options{Spaces: []string{"A"}, Actor: "restore"})
	noErr(t, err)
	if len(rep.Restored) != 2 {
		t.Fatalf("restored %+v", rep.Restored)
	}
	for _, want := range []string{"relation depends-on to " + b.target.Key + " left out", "mention of " + b.target.Key + " is plain text now", "a reply is left out"} {
		if !slices.ContainsFunc(rep.Notes, func(n string) bool { return strings.Contains(n, want) }) {
			t.Errorf("no note %q in %v", want, rep.Notes)
		}
	}
	page := get(t, target, b.page.ID)
	if len(page.Attachments) != 2 || page.Attachments[1].Source == nil || page.Attachments[1].Source.Attachment != page.Attachments[0].ID {
		t.Fatalf("the versions of a file: %+v", page.Attachments)
	}
	if roleName(item.Viewer) != "viewer" || roleName(item.Editor) != "editor" || roleName(item.SpaceAdmin) != "space-admin" || roleName(item.Role(9)) != "9" {
		t.Fatal("role names")
	}
	dry{}.Discard("", "")
}
