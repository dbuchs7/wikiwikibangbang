package connector

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
	"github.com/dbuchs7/wikiwikibangbang/internal/gitrepo"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/storage"
)

// A Markdown file in a text (ADR-0136): an editor names a file of a
// repository; the server reads it and makes it a page below the edited
// item, which the text shows whole. A page that follows a branch or tag is
// read again every hour; one pinned to a commit is not.

// FileSystem names a page made from a file in a text in its origin. A
// connector's ID holds no colon, so no connector's pages share it.
const FileSystem = "git:file"

// FileActor is who the hourly update of such pages is recorded as.
const FileActor = "sync:git"

// FilesVar, set to "on", lets editors have the server read files of
// repositories; the operator decides, as for the link check (ADR-0039).
const FilesVar = "WWBB_GIT_FILES"

// filesEvery is how often pages that follow a branch or tag are read.
const filesEvery = time.Hour

// ErrFilesOff is a file asked for while reading files is off.
var ErrFilesOff = errors.New("connector: reading Markdown files of repositories is off; the operator sets WWBB_GIT_FILES=on")

// ErrNoMarkdownFile is an address that names no Markdown file of a
// repository.
var ErrNoMarkdownFile = errors.New("connector: not the address of a Markdown file in a repository of GitHub, GitLab, Bitbucket, Gitea or Forgejo")

// ErrNotMirrored is a page that was not made from a file in a text.
var ErrNotMirrored = errors.New("connector: the page is not made from a file of a repository")

// FilesOn reports whether editors may have the server read files.
func (r *Runner) FilesOn() bool { return r.getenv(FilesVar) == "on" }

// files reads without a credential: public repositories only (ADR-0136).
func (r *Runner) files() gitrepo.Client { return gitrepo.Client{HTTP: r.httpClient()} }

// Pinned reports whether a page made from a file shows it at a fixed
// commit, rather than following a branch or tag.
func Pinned(o item.Origin) bool {
	a, err := gitrepo.Parse(o.URL)
	return err == nil && gitrepo.Commit(a.Ref)
}

// filePath is the file's path in the repository, from its origin.
func filePath(o item.Origin) string {
	_, p, _ := strings.Cut(o.ExternalID, "/")
	return p
}

// MirrorFile reads the Markdown file at addr and makes it a page below
// host, as who, who must be allowed to add a page there. pin fixes the
// page at the commit read; otherwise it follows the address's branch or
// tag, or the default branch, unless the address names a commit. The
// same file in the same item is the page made before, now at this
// version.
func (r *Runner) MirrorFile(ctx context.Context, who string, host item.ID, addr string, pin bool) (item.Item, error) {
	if !r.FilesOn() {
		return item.Item{}, ErrFilesOff
	}
	a, err := gitrepo.Parse(addr)
	if err != nil {
		return item.Item{}, ErrNoMarkdownFile
	}
	client := r.files()
	a, commit, err := client.Locate(ctx, a)
	if err != nil {
		return item.Item{}, err
	}
	if a.Folder || !gitrepo.Markdown(a.Path) {
		return item.Item{}, ErrNoMarkdownFile
	}
	follow := a.Ref
	if gitrepo.Commit(follow) {
		follow = ""
	}
	o := item.Origin{System: FileSystem, Instance: a.RepoURL(), ExternalID: string(host) + "/" + a.Path, ExternalKey: follow, Version: commit}
	return r.writeFile(ctx, client, who, "", host, a, commit, o, pin || follow == "")
}

// writeFile reads the file at the commit and writes its page: as who,
// whose session finds the space, or as the update when who is empty, in
// the space and below the parent where the page is.
func (r *Runner) writeFile(ctx context.Context, client gitrepo.Client, who, space string, parent item.ID, a gitrepo.Address, commit string, o item.Origin, pinned bool) (item.Item, error) {
	t := gitTree{ctx: ctx, client: client, a: a, commit: commit, links: map[string]string{}}
	c, err := t.read(a.Path, a.Path[strings.LastIndex(a.Path, "/")+1:], "")
	if err != nil {
		return item.Item{}, err
	}
	// The page shows the file alone: where it comes from, the UI says in
	// each reader's language.
	c.Body = c.Body[1:]
	for i := range c.Pictures {
		c.Pictures[i].Block--
	}
	o.Raw, o.Version = c.Raw, commit
	o.URL = a.WebURL(o.ExternalKey, a.Path, false)
	if pinned {
		o.URL = a.WebURL(commit, a.Path, false)
	}
	u := item.Upsert{Origin: o, Space: space, Parent: parent, Type: PageType, Title: c.Title}
	w := pageWriter{who: who, actor: FileActor}
	var it item.Item
	err = r.db.Write(func(s *item.Store) error {
		if s.Type(PageType).Name == "" {
			return fmt.Errorf("connector: the type %s does not exist; an administrator defines it", PageType)
		}
		var err error
		it, err = w.upsert(s, u)
		return err
	})
	if err != nil {
		return it, err
	}
	return writeBody(r.db, w, it, item.Doc{Blocks: c.Body}, c.Pictures)
}

// pageWriter writes a page as a person, through their session (D10), or,
// when who is empty, as actor, a synchronisation.
type pageWriter struct{ who, actor string }

func (w pageWriter) upsert(s *item.Store, u item.Upsert) (item.Item, error) {
	if w.who == "" {
		it, _, err := s.Upsert(w.actor, u)
		return it, err
	}
	return s.As(w.who).Mirror(u)
}

func (w pageWriter) attach(s *item.Store, id item.ID, a item.Attachment) error {
	var err error
	if w.who == "" {
		_, err = s.AddAttachment(w.actor, id, a)
	} else {
		_, err = s.As(w.who).AddAttachment(id, a)
	}
	return err
}

func (w pageWriter) commit(s *item.Store, id item.ID, d item.Doc) error {
	if w.who == "" {
		return s.CommitBody(w.actor, id, d)
	}
	return s.As(w.who).CommitBody(id, d)
}

// get reads the page as written: as the person reads it, protected
// passages veiled (ADR-0088), or whole for a synchronisation.
func (w pageWriter) get(s *item.Store, id item.ID) (item.Item, error) {
	if w.who == "" {
		return s.Get(id)
	}
	return s.As(w.who).Get(id)
}

// maxPictures bounds how many pictures one page keeps (ADR-0136).
const maxPictures = 50

// pictureTypes are the kinds of picture a page keeps: the ones a page
// shows inline (ADR-0045). An SVG can carry script and stays a link.
var pictureTypes = []string{"image/png", "image/jpeg", "image/gif", "image/webp"}

// writeBody attaches the pictures a page shows and commits its body, if
// it changed. A picture already attached with the same content is not
// stored again; one of another kind, too large or beyond the first
// maxPictures stays the link it is.
func writeBody(db *storage.DB, w pageWriter, it item.Item, body item.Doc, pictures []Picture) (item.Item, error) {
	body.Blocks = slices.Clone(body.Blocks)
	var added []item.Attachment
	for n, p := range pictures {
		if n == maxPictures || !slices.Contains(pictureTypes, http.DetectContentType(p.Data)) || int64(len(p.Data)) > budget.Get().AttachmentBytes {
			continue
		}
		sum := sha256.Sum256(p.Data)
		var ref item.ID
		for _, a := range it.Attachments {
			if !a.Deleted && a.SHA256 == hex.EncodeToString(sum[:]) {
				ref = a.ID
			}
		}
		if ref == "" {
			a, err := db.StoreAttachment(it.ID, bytes.NewReader(p.Data), budget.Get().AttachmentBytes)
			if err != nil {
				return it, err
			}
			a.Name = p.Name
			added = append(added, a)
			ref = a.ID
		}
		body.Blocks[p.Block] = item.Block{Kind: item.Image, Ref: ref, Text: p.Caption}
	}
	recorded := 0
	err := db.Write(func(s *item.Store) error {
		for _, a := range added {
			if err := w.attach(s, it.ID, a); err != nil {
				return err
			}
			recorded++
		}
		if !sameBody(it.Body, body) {
			if err := w.commit(s, it.ID, body); err != nil {
				return err
			}
		}
		var err error
		it, err = w.get(s, it.ID)
		return err
	})
	// A stored file that was never recorded goes at once; the next start
	// would remove it too.
	for _, a := range added[recorded:] {
		db.DiscardAttachment(it.ID, a.ID)
	}
	return it, err
}

// mirrored is a page made from a file, as who may see it, with the
// address of its file.
func (r *Runner) mirrored(who string, id item.ID) (item.Item, gitrepo.Address, error) {
	var it item.Item
	var err error
	r.db.Read(func(s *item.Store) {
		if who == "" {
			it, err = s.Get(id)
		} else {
			it, err = s.As(who).Get(id)
		}
	})
	if err != nil {
		return it, gitrepo.Address{}, err
	}
	if it.Origin == nil || it.Origin.System != FileSystem {
		return it, gitrepo.Address{}, ErrNotMirrored
	}
	a, err := gitrepo.Parse(it.Origin.URL)
	if err != nil {
		return it, a, ErrNotMirrored
	}
	return it, a.At(it.Origin.ExternalKey, filePath(*it.Origin)), nil
}

// UpdateFile reads a page's file again, as who, who must be allowed to
// edit the page: at the followed branch or tag, or at its commit when it
// is pinned. It reports whether the file changed.
func (r *Runner) UpdateFile(ctx context.Context, who string, id item.ID) (item.Item, bool, error) {
	if !r.FilesOn() {
		return item.Item{}, false, ErrFilesOff
	}
	it, a, err := r.mirrored(who, id)
	if err != nil {
		return it, false, err
	}
	var editable bool
	r.db.Read(func(s *item.Store) { editable = who == "" || s.As(who).MayEdit(id) })
	if !editable {
		return it, false, fmt.Errorf("%w: %s may not edit %s", item.ErrForbidden, who, it.Key)
	}
	return r.update(ctx, who, it, a)
}

// update reads a page's file at the ref it follows and writes the page
// when the Markdown changed. A pinned page is read at its commit, which
// changes nothing but pictures that could not be read before.
func (r *Runner) update(ctx context.Context, who string, it item.Item, a gitrepo.Address) (item.Item, bool, error) {
	client := r.files()
	o := *it.Origin
	pinned := Pinned(o) || o.ExternalKey == ""
	ref := o.ExternalKey
	if pinned {
		ref = o.Version
	}
	// The file is read at the ref, which needs no request to the host's
	// API; the commit is asked for only when the file changed.
	data, err := client.Read(ctx, a, ref, a.Path, maxMarkdown)
	if errors.Is(err, gitrepo.ErrTooLarge) {
		return it, false, fmt.Errorf("%s is larger than %d MB", a.Path, maxMarkdown>>20)
	}
	if err != nil {
		return it, false, err
	}
	if strings.ToValidUTF8(string(data), "�") == o.Raw {
		return it, false, nil
	}
	commit := o.Version
	if !pinned {
		var located gitrepo.Address
		if located, commit, err = client.Locate(ctx, a.At(ref, a.Path)); err != nil {
			return it, false, err
		}
		a = located
	}
	it, err = r.writeFile(ctx, client, who, it.Space, it.Parent, a, commit, o, pinned)
	return it, err == nil, err
}

// PinFile fixes a page at the commit it shows, or with pin false lets it
// follow the branch or tag it was made from again, as who, who must be
// allowed to edit it.
func (r *Runner) PinFile(ctx context.Context, who string, id item.ID, pin bool) (item.Item, error) {
	if !r.FilesOn() {
		return item.Item{}, ErrFilesOff
	}
	it, a, err := r.mirrored(who, id)
	if err != nil {
		return it, err
	}
	o := *it.Origin
	if !pin && o.ExternalKey == "" {
		return it, fmt.Errorf("%w: the page was made from a commit and follows no branch or tag", item.ErrValidation)
	}
	if pin == Pinned(o) {
		return it, nil
	}
	err = r.db.Write(func(s *item.Store) error {
		o.URL = a.WebURL(o.ExternalKey, a.Path, false)
		if pin {
			o.URL = a.WebURL(o.Version, a.Path, false)
		}
		var err error
		it, err = pageWriter{who: who, actor: FileActor}.upsert(s, item.Upsert{Origin: o, Space: it.Space, Parent: it.Parent, Type: it.Type, Title: it.Title})
		return err
	})
	if err != nil || pin {
		return it, err
	}
	// Following again shows what the branch or tag holds now.
	it, _, err = r.UpdateFile(ctx, who, id)
	return it, err
}

// TickFiles reads, once an hour, every page that follows a branch or tag
// and writes those whose file changed, as the update.
func (r *Runner) TickFiles(ctx context.Context) {
	r.mu.Lock()
	due := r.FilesOn() && r.now().Sub(r.filesRun) >= filesEvery-checkEvery
	if due {
		r.filesRun = r.now()
	}
	r.mu.Unlock()
	if !due {
		return
	}
	var pages []item.Item
	r.db.Read(func(s *item.Store) { pages = s.FindBySystem(FileSystem) })
	for _, it := range pages {
		if Pinned(*it.Origin) || it.Origin.ExternalKey == "" {
			continue
		}
		_, a, err := r.mirrored("", it.ID)
		if err == nil {
			_, _, err = r.update(ctx, "", it, a)
		}
		if err != nil {
			r.log.Warn("a page made from a file was not updated", "item", it.Key, "err", err)
		}
	}
}
