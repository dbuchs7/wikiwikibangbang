package connector

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

// filesSetup is a runner that reads files, a forge, and a host page that
// edda may edit and anna only read.
func filesSetup(t *testing.T) (*fixture, *forge, string, item.Item) {
	t.Helper()
	f, src, base := gitSetup(t, "docs")
	f.r.getenv = allowPrivate(func(k string) string {
		if k == FilesVar {
			return "on"
		}
		return ""
	})
	var host item.Item
	if err := f.db.Write(func(s *item.Store) error {
		if err := s.DefineType("admin", PageTypeDef()); err != nil {
			return err
		}
		if err := s.GrantRole("admin", "API", "edda", item.Editor); err != nil {
			return err
		}
		var err error
		host, err = s.As("edda").Create(item.NewItem{Space: "API", Type: PageType, Title: "Host"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return f, src, base, host
}

func (f *fixture) item(t *testing.T, id item.ID) item.Item {
	t.Helper()
	var it item.Item
	var err error
	f.db.Read(func(s *item.Store) { it, err = s.Get(id) })
	if err != nil {
		t.Fatal(err)
	}
	return it
}

// A file in a text becomes a page below the item, following its branch:
// the hourly update writes it when the file changed (ADR-0131).
func TestAFileInATextFollowsItsBranch(t *testing.T) {
	f, src, base, host := filesSetup(t)
	ctx := context.Background()
	addr := base + "/o/r/src/branch/main/docs/a.md"
	page, err := f.r.MirrorFile(ctx, "edda", host.ID, addr, false)
	if err != nil {
		t.Fatal(err)
	}
	o := page.Origin
	if page.Title != "Page A" || page.Parent != host.ID || page.Space != "API" || o.System != FileSystem || o.Instance != base+"/o/r" ||
		o.ExternalID != string(host.ID)+"/docs/a.md" || o.ExternalKey != "main" || o.URL != addr || o.Version != src.commit || Pinned(*o) {
		t.Fatalf("page %+v origin %+v", page, o)
	}
	if b := page.Body.Blocks; b[0].Kind != item.Paragraph || marks(b[0])["B"].Href != base+"/o/r/src/branch/main/docs/guide/b.md" || b[1].Kind != item.Image || len(page.Attachments) != 1 {
		t.Fatalf("the page's text %+v", b)
	}
	// The same file in the same item is the same page.
	again, err := f.r.MirrorFile(ctx, "edda", host.ID, addr, false)
	if err != nil || again.ID != page.ID {
		t.Fatalf("again %+v %v", again, err)
	}
	// Nothing changed: the update reads the file at its branch, asks for
	// no commit and writes nothing.
	src.counts()
	if _, changed, err := f.r.UpdateFile(ctx, "edda", page.ID); err != nil || changed {
		t.Fatalf("an unchanged file: %v %v", changed, err)
	}
	if _, commits, reads := src.counts(); commits != 0 || len(reads) != 1 {
		t.Errorf("%d commits asked, %v read", commits, reads)
	}
	// The hourly update writes a changed file, as the update.
	src.set("docs/a.md", "# Page A\n\nChanged.")
	f.r.TickFiles(ctx)
	got := f.item(t, page.ID)
	if got.Body.Blocks[0].Text != "Changed." || got.Origin.Version != src.commit || got.Origin.ExternalKey != "main" {
		t.Fatalf("after the update %+v %+v", got.Body.Blocks, got.Origin)
	}
	var who string
	f.db.Read(func(s *item.Store) {
		h := s.History(page.ID)
		who = h[len(h)-1].Actor
	})
	if who != FileActor {
		t.Errorf("the update is recorded as %q", who)
	}
	// Not again within the hour.
	src.set("docs/a.md", "# Page A\n\nLater.")
	f.r.TickFiles(ctx)
	if f.item(t, page.ID).Body.Blocks[0].Text != "Changed." {
		t.Error("read again within the hour")
	}
	f.clock = f.clock.Add(time.Hour)
	f.r.TickFiles(ctx)
	if f.item(t, page.ID).Body.Blocks[0].Text != "Later." {
		t.Error("not read after an hour")
	}
}

// "This version" pins a page at the commit read; it is not updated until
// it follows its branch again.
func TestAPinnedFileStays(t *testing.T) {
	f, src, base, host := filesSetup(t)
	ctx := context.Background()
	first := src.commit
	page, err := f.r.MirrorFile(ctx, "edda", host.ID, base+"/o/r/src/branch/main/docs/guide/b.md", true)
	if err != nil {
		t.Fatal(err)
	}
	if !Pinned(*page.Origin) || page.Origin.URL != base+"/o/r/src/commit/"+first+"/docs/guide/b.md" || page.Origin.ExternalKey != "main" {
		t.Fatalf("pinned %+v", page.Origin)
	}
	src.set("docs/guide/b.md", "# Page B\n\nNewer.")
	f.r.TickFiles(ctx)
	if _, changed, err := f.r.UpdateFile(ctx, "edda", page.ID); err != nil || changed {
		t.Fatalf("a pinned page is read at its commit: %v %v", changed, err)
	}
	if got := f.item(t, page.ID); got.Body.Blocks[0].Text == "Newer." || got.Origin.Version != first {
		t.Fatalf("a pinned page changed: %+v", got.Body.Blocks)
	}
	// Following again shows what the branch holds now; pinning again
	// fixes that commit.
	followed, err := f.r.PinFile(ctx, "edda", page.ID, false)
	if err != nil || Pinned(*followed.Origin) || followed.Body.Blocks[0].Text != "Newer." || followed.Origin.Version != src.commit {
		t.Fatalf("followed %+v %v", followed, err)
	}
	if same, err := f.r.PinFile(ctx, "edda", page.ID, false); err != nil || same.Origin.URL != followed.Origin.URL {
		t.Fatalf("following twice %v", err)
	}
	pinned, err := f.r.PinFile(ctx, "edda", page.ID, true)
	if err != nil || !Pinned(*pinned.Origin) || !strings.Contains(pinned.Origin.URL, src.commit) {
		t.Fatalf("pinned again %+v %v", pinned.Origin, err)
	}
	// An address that names a commit has no branch to follow.
	fixed, err := f.r.MirrorFile(ctx, "edda", host.ID, base+"/o/r/src/commit/"+first+"/docs/a.md", false)
	if err != nil || !Pinned(*fixed.Origin) || fixed.Origin.ExternalKey != "" {
		t.Fatalf("a commit's address %+v %v", fixed.Origin, err)
	}
	if _, err := f.r.PinFile(ctx, "edda", fixed.ID, false); !errors.Is(err, item.ErrValidation) {
		t.Errorf("following without a branch: %v", err)
	}
}

func TestFilesInATextAreGuarded(t *testing.T) {
	f, _, base, host := filesSetup(t)
	ctx := context.Background()
	addr := base + "/o/r/src/branch/main/docs/a.md"
	for name, c := range map[string]struct {
		who, addr string
		want      error
	}{
		"a viewer":           {"anna", addr, item.ErrForbidden},
		"a folder":           {"edda", base + "/o/r/src/branch/main/docs", ErrNoMarkdownFile},
		"another file":       {"edda", base + "/o/r/src/branch/main/docs/notes.txt", ErrNoMarkdownFile},
		"no repository":      {"edda", "https://example.org/a.md", ErrNoMarkdownFile},
		"a file that is not": {"edda", base + "/o/r/src/branch/main/docs/nope.md", nil},
	} {
		_, err := f.r.MirrorFile(ctx, c.who, host.ID, c.addr, false)
		if err == nil || (c.want != nil && !errors.Is(err, c.want)) {
			t.Errorf("%s: %v", name, err)
		}
	}
	page, err := f.r.MirrorFile(ctx, "edda", host.ID, addr, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.r.UpdateFile(ctx, "anna", page.ID); !errors.Is(err, item.ErrForbidden) {
		t.Errorf("a viewer updates: %v", err)
	}
	if _, err := f.r.PinFile(ctx, "anna", page.ID, true); !errors.Is(err, item.ErrForbidden) {
		t.Errorf("a viewer pins: %v", err)
	}
	if _, _, err := f.r.UpdateFile(ctx, "edda", host.ID); !errors.Is(err, ErrNotMirrored) {
		t.Errorf("a page not made from a file: %v", err)
	}
	if _, err := f.r.PinFile(ctx, "edda", host.ID, true); !errors.Is(err, ErrNotMirrored) {
		t.Errorf("pinning a page not made from a file: %v", err)
	}
	// Off unless the operator switches it on.
	f.r.getenv = allowPrivate(func(string) string { return "" })
	if _, err := f.r.MirrorFile(ctx, "edda", host.ID, addr, false); !errors.Is(err, ErrFilesOff) {
		t.Errorf("off: %v", err)
	}
	if _, _, err := f.r.UpdateFile(ctx, "edda", page.ID); !errors.Is(err, ErrFilesOff) {
		t.Errorf("off: %v", err)
	}
	if _, err := f.r.PinFile(ctx, "edda", page.ID, true); !errors.Is(err, ErrFilesOff) {
		t.Errorf("off: %v", err)
	}
	f.r.TickFiles(ctx) // does nothing
}
