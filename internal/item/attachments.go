package item

import (
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
	// Author and Created come from the event that added it.
	Author  string `json:"-"`
	Created int64  `json:"-"` // Unix seconds
	Deleted bool   `json:"-"`
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
	rec := Attachment{ID: a.ID, Name: a.Name, Type: a.Type, Size: a.Size, SHA256: a.SHA256}
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
			*a = Attachment{ID: a.ID, Author: a.Author, Created: a.Created, Deleted: true}
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
func (x *Session) AddAttachment(id ID, a Attachment) (Attachment, error) {
	if err := x.CanAttach(id); err != nil {
		return Attachment{}, err
	}
	return x.s.AddAttachment(x.who, id, a)
}

// DeleteAttachment removes a file from an item the principal may edit.
func (x *Session) DeleteAttachment(id, attachment ID) error {
	if _, err := x.editable(id); err != nil {
		return err
	}
	return x.s.DeleteAttachment(x.who, id, attachment)
}
