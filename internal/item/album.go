package item

import (
	"cmp"
	"slices"
	"strings"
)

// The album's folders and the labels of its pictures (ADR-0090). A folder
// is an item of the type folder, without a body: its pictures are its
// files, kept, encrypted and erased as every item's (ADR-0045), and read
// by whoever may read the folder. Folders nest by the parent, as pages
// do.

// FolderType names the type of the album's folders.
const FolderType = "folder"

// maxPictureLabels bounds the labels of one picture.
const maxPictureLabels = 20

// folderType is the type the album defines when its first folder is made.
func folderType() TypeDef { return TypeDef{Name: FolderType} }

// isFolderType reports whether a type is the album's folder: no body, no
// workflow and no field one must fill.
func isFolderType(td TypeDef) bool {
	return !td.Body && td.Workflow == nil && !slices.ContainsFunc(td.Fields, func(f FieldDef) bool { return f.Required })
}

// CreateFolder makes a folder of the album in a space, or in a folder of
// it. Who may create there may.
func (x *Session) CreateFolder(space string, parent ID, name string) (Item, error) {
	if parent != "" {
		p, err := x.readable(parent)
		if err != nil {
			return Item{}, err
		}
		if p.Type != FolderType {
			return Item{}, invalid("%s is no folder of the album", p.Key)
		}
		space = p.Space
	}
	if err := x.canCreateIn(space, parent); err != nil {
		return Item{}, err
	}
	switch td, ok := x.s.latest(FolderType); {
	case !ok:
		if err := x.s.DefineType(x.who, folderType()); err != nil {
			return Item{}, err
		}
	case !isFolderType(td):
		return Item{}, invalid("the type %s is in use for something else than the album's folders", FolderType)
	}
	return x.Create(NewItem{Space: space, Type: FolderType, Title: name, Parent: parent})
}

// Folders lists the album's folders the principal may read in a space
// (every space when empty) right below parent (the top when empty), by
// name.
func (x *Session) Folders(space string, parent ID) []Item {
	var out []Item
	for _, it := range x.s.items {
		if it.Presence != Live || it.Type != FolderType || it.Parent != parent || (space != "" && it.Space != space) || !x.s.canRead(x.who, it) {
			continue
		}
		out = append(out, Item{ID: it.ID, Key: it.Key, Title: it.Title, Space: it.Space, Type: it.Type, Parent: it.Parent})
	}
	slices.SortFunc(out, func(a, b Item) int {
		if c := cmp.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title)); c != 0 {
			return c
		}
		return cmp.Compare(a.Key, b.Key)
	})
	return out
}

// SetPictureLabels replaces the labels of a file of an editable item;
// they are normalised, sorted and made unique, as an item's (ADR-0044).
func (x *Session) SetPictureLabels(id, attachment ID, labels []string) error {
	it, err := x.editable(id)
	if err != nil {
		return err
	}
	if it.Presence != Live {
		return invalid("%s is %s", it.Key, it.Presence)
	}
	a, ok := it.attachment(attachment)
	if !ok || a.Deleted {
		return notFound("attachment %s of item %s", attachment, it.Key)
	}
	var out []string
	for _, l := range labels {
		n, ok := NormalLabel(l)
		if !ok {
			return invalid("label %q: use letters and digits, joined by -, _ or ., up to 64 characters", l)
		}
		out = append(out, n)
	}
	slices.Sort(out)
	out = slices.Compact(out)
	if len(out) > maxPictureLabels {
		return invalid("a picture has at most %d labels", maxPictureLabels)
	}
	if slices.Equal(out, a.Labels) {
		return nil
	}
	return x.s.record(x.who, Event{Kind: AttachmentLabelled, Item: id, Attachment: &Attachment{ID: attachment, Labels: out}})
}

// inBody reports whether a body shows a file of its item.
func inBody(d Doc, attachment ID) bool {
	return slices.ContainsFunc(d.Blocks, func(b Block) bool { return b.Kind == Image && b.Ref == attachment })
}
