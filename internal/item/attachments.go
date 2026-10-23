package item

import (
	"cmp"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Attachment is a file attached to an item (ADR-0045). Its content lives
// outside the event log, encrypted with the item's key; the log holds what
// describes it. Name, Type and SHA256 are content, sealed like a title.
type Attachment struct {
	ID     ID
	Name   string
	Type   string // the media type detected from the content
	Size   int64
	SHA256 string
	// Source names the file this one was made from (ADR-0055): a copy of
	// another item's file, or a new version of a file of the same item. It
	// is content, sealed with the rest.
	Source *Source `json:",omitempty"`
	// Labels are the picture's own labels (ADR-0090): they narrow the
	// album and are no labels of the item, so no kanban or label board
	// shows them. They are content, sealed with the rest.
	Labels []string `json:",omitempty"`
	// Retention is the rule of the file's versions (ADR-0104), kept on its
	// first version; nil follows the album's default. It is content,
	// sealed with the rest.
	Retention *Retention `json:",omitempty"`
	// Author and Created come from the event that added it.
	Author  string `json:"-"`
	Created int64  `json:"-"` // Unix seconds
	Deleted bool   `json:"-"`
}

// Source is where a file came from: a file of the same item for a new
// version (a drawing edited again), of another item for a copy.
type Source struct {
	Item       ID
	Attachment ID
}

var (
	itemIDRE       = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	attachmentIDRE = regexp.MustCompile(`^[0-9a-f]{32}$`)
	sha256RE       = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// ValidItemID reports whether s has the form of an item ID, so storage
// can use it as a directory name.
func ValidItemID(s string) bool { return itemIDRE.MatchString(s) }

// ValidAttachmentID reports whether s can name an attachment, so storage
// can use it as a file name.
func ValidAttachmentID(s string) bool { return attachmentIDRE.MatchString(s) }

// checkAttachmentName refuses names that are empty, too long, or hold
// control characters or path separators.
func checkAttachmentName(name string) error {
	if strings.TrimSpace(name) == "" || utf8.RuneCountInString(name) > 255 ||
		strings.ContainsAny(name, `/\`) || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return invalid("file name %q: 1 to 255 characters, no slashes or control characters", name)
	}
	return nil
}

func (it *Item) attachment(id ID) (*Attachment, bool) {
	for i := range it.Attachments {
		if it.Attachments[i].ID == id {
			return &it.Attachments[i], true
		}
	}
	return nil, false
}

// AddAttachment records a file stored for a live item. The caller has
// stored its content under a.ID.
func (s *Store) AddAttachment(actor string, id ID, a Attachment) (Attachment, error) {
	it, err := s.live(id)
	if err != nil {
		return Attachment{}, err
	}
	switch {
	case !ValidAttachmentID(string(a.ID)):
		return Attachment{}, invalid("attachment ID %q", a.ID)
	case a.Size < 0 || !sha256RE.MatchString(a.SHA256) || strings.TrimSpace(a.Type) == "":
		return Attachment{}, invalid("attachment %s: size, type and SHA-256 are required", a.ID)
	}
	if err := checkAttachmentName(a.Name); err != nil {
		return Attachment{}, err
	}
	if _, dup := it.attachment(a.ID); dup {
		return Attachment{}, invalid("attachment %s exists", a.ID)
	}
	if a.Source != nil && (!ValidItemID(string(a.Source.Item)) || !ValidAttachmentID(string(a.Source.Attachment))) {
		return Attachment{}, invalid("attachment %s: source %v", a.ID, *a.Source)
	}
	rec := Attachment{ID: a.ID, Name: a.Name, Type: a.Type, Size: a.Size, SHA256: a.SHA256, Source: a.Source}
	if err := s.record(actor, Event{Kind: AttachmentAdded, Item: id, Attachment: &rec}); err != nil {
		return Attachment{}, err
	}
	got, _ := s.items[id].attachment(a.ID)
	return *got, nil
}

// DeleteAttachment removes a file from a live item. Its content is
// destroyed once the deletion is recorded (ADR-0045).
func (s *Store) DeleteAttachment(actor string, id, attachment ID) error {
	it, err := s.live(id)
	if err != nil {
		return err
	}
	if a, ok := it.attachment(attachment); !ok || a.Deleted {
		return notFound("attachment %s of item %s", attachment, id)
	}
	return s.record(actor, Event{Kind: AttachmentDeleted, Item: id, Attachment: &Attachment{ID: attachment}})
}

func (s *Store) applyAttachment(e Event, touch func(...ID)) {
	it := s.items[e.Item]
	touch(it.ID)
	if e.Attachment == nil { // erased with its item (ADR-0026)
		return
	}
	switch e.Kind {
	case AttachmentAdded:
		a := *e.Attachment
		a.Author, a.Created = e.Actor, e.At.Unix()
		it.Attachments = append(it.Attachments, a)
	case AttachmentDeleted:
		if a, ok := it.attachment(e.Attachment.ID); ok {
			// The source stays, so later versions still trace back through
			// a deleted one (ADR-0055); it names files, not their content.
			// So does the rule of the versions, which the first one holds
			// (ADR-0104).
			*a = Attachment{ID: a.ID, Source: a.Source, Retention: a.Retention, Author: a.Author, Created: a.Created, Deleted: true}
		}
	case AttachmentLabelled:
		if a, ok := it.attachment(e.Attachment.ID); ok {
			a.Labels = slices.Clone(e.Attachment.Labels)
		}
	case AttachmentRetentionSet:
		if a, ok := it.attachment(e.Attachment.ID); ok {
			if r := e.Attachment.Retention; r != nil {
				c := *r
				a.Retention = &c
			} else {
				a.Retention = nil
			}
		}
	}
}

// LiveAttachments returns the attachments of every item that are not
// deleted, by item, so storage can tell which stored files are still
// needed.
func (s *Store) LiveAttachments() map[ID][]ID {
	out := map[ID][]ID{}
	for _, it := range s.items {
		for _, a := range it.Attachments {
			if !a.Deleted {
				out[it.ID] = append(out[it.ID], a.ID)
			}
		}
	}
	return out
}

// --- sessions ---------------------------------------------------------------

// Attachments returns the attachments of a readable item that are not
// deleted, oldest first.
func (x *Session) Attachments(id ID) ([]Attachment, error) {
	it, err := x.readable(id)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(slices.Clone(it.Attachments), func(a Attachment) bool { return a.Deleted }), nil
}

// Attachment returns one attachment of a readable item.
func (x *Session) Attachment(id, attachment ID) (Attachment, error) {
	it, err := x.readable(id)
	if err != nil {
		return Attachment{}, err
	}
	a, ok := it.attachment(attachment)
	if !ok || a.Deleted {
		return Attachment{}, notFound("attachment %s of item %s", attachment, id)
	}
	return *a, nil
}

// CanAttach reports whether the principal may add files to an item, so a
// caller can refuse before storing a file nobody may record.
func (x *Session) CanAttach(id ID) error {
	it, err := x.editable(id)
	if err != nil {
		return err
	}
	if it.Presence != Live {
		return invalid("item %s is %s", it.Key, it.Presence)
	}
	return nil
}

// AddAttachment records a stored file on an item the principal may edit.
// A source must be a file the principal may read; a copy from another item
// must have its content, so a mark of origin cannot be made up (ADR-0055).
func (x *Session) AddAttachment(id ID, a Attachment) (Attachment, error) {
	if err := x.CanAttach(id); err != nil {
		return Attachment{}, err
	}
	if a.Source != nil {
		src, err := x.readable(a.Source.Item)
		if err != nil {
			return Attachment{}, err
		}
		from, ok := src.attachment(a.Source.Attachment)
		if !ok || (a.Source.Item != id && from.Deleted) {
			return Attachment{}, notFound("attachment %s of item %s", a.Source.Attachment, a.Source.Item)
		}
		if a.Source.Item != id && from.SHA256 != a.SHA256 {
			return Attachment{}, invalid("a copy of %s must have its content", a.Source.Attachment)
		}
	}
	return x.s.AddAttachment(x.who, id, a)
}

// FileOrigin is where a copied file came from, as its reader may know it.
type FileOrigin struct {
	// Readable is false when the source item is gone or not readable by
	// the principal; nothing else is told then.
	Readable bool
	Item     Item
	// Latest is the newest version of the source file in its item, when it
	// is newer than the one copied (a drawing edited since).
	Latest *Attachment
}

// FileOrigin returns where a copied file of a readable item came from, and
// whether its source has a newer version. ok is false for a file that is
// no copy.
func (x *Session) FileOrigin(id, attachment ID) (o FileOrigin, ok bool, err error) {
	it, err := x.readable(id)
	if err != nil {
		return FileOrigin{}, false, err
	}
	a, found := it.attachment(attachment)
	if !found || a.Deleted {
		return FileOrigin{}, false, notFound("attachment %s of item %s", attachment, id)
	}
	// A version made in the same item leads back to the copy it began as.
	for seen := 0; a.Source != nil && a.Source.Item == id && seen < len(it.Attachments); seen++ {
		prev, found := it.attachment(a.Source.Attachment)
		if !found {
			break
		}
		a = prev
	}
	if a.Source == nil || a.Source.Item == id {
		return FileOrigin{}, false, nil
	}
	src, err := x.readable(a.Source.Item)
	if err != nil || src.Presence != Live {
		return FileOrigin{}, true, nil
	}
	o = FileOrigin{Readable: true, Item: Item{ID: src.ID, Key: src.Key, Title: src.Title, Space: src.Space, Type: src.Type}}
	if latest := latestVersion(src, a.Source.Attachment); latest != nil && latest.ID != a.Source.Attachment {
		l := *latest
		o.Latest = &l
	}
	return o, true, nil
}

// latestVersion follows the versions of a file within its item and returns
// the newest that is not deleted, or nil.
func latestVersion(it *Item, from ID) *Attachment {
	family := map[ID]bool{from: true}
	for grew := true; grew; {
		grew = false
		for _, a := range it.Attachments {
			if !family[a.ID] && a.Source != nil && a.Source.Item == it.ID && family[a.Source.Attachment] {
				family[a.ID], grew = true, true
			}
		}
	}
	var latest *Attachment
	for i := range it.Attachments {
		a := &it.Attachments[i]
		if family[a.ID] && !a.Deleted && (latest == nil || a.Created >= latest.Created) {
			latest = a
		}
	}
	return latest
}

// DeleteAttachment removes a file from an item the principal may edit.
func (x *Session) DeleteAttachment(id, attachment ID) error {
	if _, err := x.editable(id); err != nil {
		return err
	}
	return x.s.DeleteAttachment(x.who, id, attachment)
}

// Picture is a file of a readable item, as the album lists it (ADR-0054).
type Picture struct {
	Item       Item
	Attachment Attachment
	// Used: the item's body shows it. Editable: the principal may edit the
	// item, so delete the picture or label it (ADR-0090).
	Used, Editable bool
	// First is the first version of the file in its item (ADR-0103): a
	// file edited again, e.g. a drawing, names the one it was made from,
	// and all versions of one file share First. A file that is no new
	// version is its own First. Deleted versions still link the chain.
	First ID
	// Retention is the rule of the file's versions, set on its first
	// version (ADR-0104); nil follows the album's default.
	Retention *Retention
	// order is the file's place among its item's files: of two added in
	// the same second, the later is the newer.
	order int
}

// firstVersion follows a file's versions within its item back to the
// first. A copy from another item begins a chain of its own.
func firstVersion(it *Item, a Attachment) ID {
	for seen := 0; a.Source != nil && a.Source.Item == it.ID && seen < len(it.Attachments); seen++ {
		prev, ok := it.attachment(a.Source.Attachment)
		if !ok {
			break
		}
		a = *prev
	}
	return a.ID
}

// Pictures returns the files of the live items the principal may read,
// for which keep returns true, in space (every space when empty), newest
// first. Reading an item is the right to see its files; the album shows
// nothing one could not open item by item.
func (x *Session) Pictures(space string, keep func(Attachment) bool) []Picture {
	var out []Picture
	for _, it := range x.s.items {
		if it.Presence != Live || (space != "" && it.Space != space) || len(it.Attachments) == 0 || !x.s.canRead(x.who, it) {
			continue
		}
		editable := x.s.canEdit(x.who, it) && it.Managed == ""
		for i, a := range it.Attachments {
			if !a.Deleted && keep(a) {
				first := firstVersion(it, a)
				out = append(out, Picture{Item: Item{ID: it.ID, Key: it.Key, Title: it.Title, Space: it.Space, Type: it.Type, Parent: it.Parent}, Attachment: a,
					Used: inBody(it.Body, a.ID), Editable: editable, First: first, Retention: ruleOf(it, first), order: i})
			}
		}
	}
	slices.SortFunc(out, func(a, b Picture) int {
		if c := cmp.Compare(b.Attachment.Created, a.Attachment.Created); c != 0 {
			return c
		}
		if c := strings.Compare(string(a.Item.ID), string(b.Item.ID)); c != 0 {
			return c
		}
		return cmp.Compare(b.order, a.order)
	})
	return out
}
