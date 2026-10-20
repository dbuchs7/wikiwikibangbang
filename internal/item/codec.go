package item

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"strconv"
	"time"
)

// CodecVersion is the version of the persisted event format (ADR-0025). A
// reader refuses events from a newer version rather than misreading them.
//
//   - 1: plaintext events.
//   - 2: content may be sealed with a key per item, and events may be marked
//     erased (ADR-0026). Version 1 events are still read.
//   - 3: body blocks may carry a heading level, marks, and the kinds bullet,
//     ordered, quote and code (ADR-0032). Versions 1 and 2 are still read.
//   - 4: comments: the kinds CommentAdded, CommentEdited and CommentDeleted
//     and the fields comment and text; text is sealed content (ADR-0035).
//     Versions 1 to 3 are still read.
//   - 5: ItemMoved may carry order, the new order of the siblings
//     (ADR-0041). Versions 1 to 4 are still read.
//   - 6: body blocks may be tasks, panels, tables, tables of contents and
//     lists of child pages, with the block fields Checked and Tone
//     (ADR-0042). Versions 1 to 5 are still read.
//   - 7: the kind LabelsSet and the field labels, sealed content
//     (ADR-0044). Versions 1 to 6 are still read.
//   - 8: the kinds AttachmentAdded and AttachmentDeleted with the field
//     attachment, sealed when it describes a file, and the block kind
//     image (ADR-0045). Versions 1 to 7 are still read.
//   - 9: the kinds Watched and Unwatched (ADR-0046). Versions 1 to 8 are
//     still read.
//   - 10: CommentAdded may carry anchor, sealed content, and reply; the
//     kinds CommentResolved and CommentReopened (ADR-0047). Versions 1 to
//     9 are still read.
//   - 11: the kinds RoleRevoked, GroupMemberRemoved and SystemRoleRevoked
//     (ADR-0048). Versions 1 to 10 are still read.
//   - 12: an attachment may carry Source, the file it was copied from or is
//     a new version of, sealed with the rest of it (ADR-0055). Versions 1
//     to 11 are still read.
//   - 13: an image block may carry Width, in percent of the text column
//     (ADR-0056). Versions 1 to 12 are still read.
//   - 14: the kinds HoldSet, with hold, and HoldReleased, with outcome and
//     text, both sealed content (ADR-0058). Versions 1 to 13 are still read.
const CodecVersion = 14

// Sealer encrypts the content of events with a key per item, so that
// destroying the key makes the content unrecoverable (crypto-shredding,
// ADR-0026). aad binds a ciphertext to its event.
type Sealer interface {
	Seal(item ID, plaintext, aad []byte) ([]byte, error)
	// Open returns ErrKeyShredded when the item's key was destroyed.
	Open(item ID, ciphertext, aad []byte) ([]byte, error)
}

// ErrKeyShredded is returned by a Sealer for content whose key was
// destroyed. The decoder turns such an event into an erased one.
var ErrKeyShredded = errors.New("item key was shredded")

// EncodeBatch encodes the events of one command for the journal, with
// content in plaintext.
func EncodeBatch(batch []Event) ([]byte, error) { return EncodeBatchSealed(batch, nil) }

// EncodeBatchSealed encodes a batch; with a sealer, the content of each
// event is encrypted with its item's key.
func EncodeBatchSealed(batch []Event, sl Sealer) ([]byte, error) {
	wire := make([]wireEvent, len(batch))
	for i, e := range batch {
		w, err := toWire(e)
		if err == nil && sl != nil && e.HasContent() && !e.Erased {
			err = seal(&w, sl)
		}
		if err != nil {
			return nil, fmt.Errorf("event %d: %w", e.Seq, err)
		}
		wire[i] = w
	}
	return json.Marshal(wire)
}

// DecodeBatch decodes a batch with plaintext content.
func DecodeBatch(data []byte) ([]Event, error) { return DecodeBatchSealed(data, nil) }

// DecodeBatchSealed decodes a batch written by EncodeBatchSealed. Content
// whose key was shredded decodes as an erased event.
func DecodeBatchSealed(data []byte, sl Sealer) ([]Event, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var wire []wireEvent
	if err := dec.Decode(&wire); err != nil {
		return nil, err
	}
	if len(wire) == 0 {
		return nil, fmt.Errorf("empty batch")
	}
	batch := make([]Event, len(wire))
	for i, w := range wire {
		shredded := false
		if w.Sealed != nil {
			if sl == nil {
				return nil, fmt.Errorf("event %d: content is sealed and no keys are available", w.Seq)
			}
			var err error
			if shredded, err = unseal(&w, sl); err != nil {
				return nil, fmt.Errorf("event %d: %w", w.Seq, err)
			}
		}
		e, err := fromWire(w)
		if shredded {
			e.erase()
		}
		if err != nil {
			return nil, fmt.Errorf("event %d: %w", w.Seq, err)
		}
		batch[i] = e
	}
	return batch, nil
}

// wireEvent is the persisted form of an Event. Fields are named explicitly
// so that renaming a Go field cannot silently change the format; nested
// structs (TypeDef, Doc, Relation, Restriction, Origin) are encoded with
// their Go field names, which the golden test in testdata pins.
type wireEvent struct {
	V           int                   `json:"v"`
	Seq         int                   `json:"seq"`
	At          time.Time             `json:"at"`
	Actor       string                `json:"actor,omitempty"`
	Kind        EventKind             `json:"kind"`
	Item        ID                    `json:"item,omitempty"`
	Batch       int                   `json:"batch,omitempty"`
	TypeDef     *TypeDef              `json:"typeDef,omitempty"`
	Space       string                `json:"space,omitempty"`
	SpaceName   string                `json:"spaceName,omitempty"`
	TypeName    string                `json:"typeName,omitempty"`
	TypeVersion int                   `json:"typeVersion,omitempty"`
	Parent      ID                    `json:"parent,omitempty"`
	Order       []ID                  `json:"order,omitempty"`
	Title       string                `json:"title,omitempty"`
	Props       map[string]*wireValue `json:"props,omitempty"`
	Field       string                `json:"field,omitempty"`
	Value       *wireValue            `json:"value,omitempty"`
	Transition  string                `json:"transition,omitempty"`
	From        string                `json:"from,omitempty"`
	To          string                `json:"to,omitempty"`
	Body        *Doc                  `json:"body,omitempty"`
	Relation    *Relation             `json:"relation,omitempty"`
	Members     []ID                  `json:"members,omitempty"`
	Group       string                `json:"group,omitempty"`
	Principal   string                `json:"principal,omitempty"`
	Role        string                `json:"role,omitempty"`
	SystemRole  string                `json:"systemRole,omitempty"`
	Restriction *Restriction          `json:"restriction,omitempty"`
	Reason      string                `json:"reason,omitempty"`
	Origin      *Origin               `json:"origin,omitempty"`
	Key         string                `json:"key,omitempty"`
	Managed     string                `json:"managed,omitempty"`
	Comment     ID                    `json:"comment,omitempty"`
	Text        string                `json:"text,omitempty"`
	Labels      []string              `json:"labels,omitempty"`
	Attachment  *Attachment           `json:"attachment,omitempty"`
	Anchor      *Anchor               `json:"anchor,omitempty"`
	Reply       ID                    `json:"reply,omitempty"`
	Hold        *Hold                 `json:"hold,omitempty"`
	Outcome     string                `json:"outcome,omitempty"`
	Erased      bool                  `json:"erased,omitempty"`
	// Sealed holds the encrypted content (wireContent) when a Sealer was
	// used; the plaintext content fields are then empty.
	Sealed []byte `json:"sealed,omitempty"`
}

// wireContent is the part of an event that is sealed: everything
// Event.erase removes.
type wireContent struct {
	Title      string                `json:"title,omitempty"`
	Props      map[string]*wireValue `json:"props,omitempty"`
	Value      *wireValue            `json:"value,omitempty"`
	Body       *Doc                  `json:"body,omitempty"`
	Raw        string                `json:"raw,omitempty"`
	Text       string                `json:"text,omitempty"`
	Labels     []string              `json:"labels,omitempty"`
	Attachment *Attachment           `json:"attachment,omitempty"`
	Anchor     *Anchor               `json:"anchor,omitempty"`
	Hold       *Hold                 `json:"hold,omitempty"`
}

func aad(w *wireEvent) []byte {
	return []byte(fmt.Sprintf("wikiwikibangbang event %d %s %s", w.Seq, w.Kind, w.Item))
}

func seal(w *wireEvent, sl Sealer) error {
	c := wireContent{Title: w.Title, Props: w.Props, Value: w.Value, Body: w.Body, Text: w.Text, Labels: w.Labels, Attachment: w.Attachment, Anchor: w.Anchor, Hold: w.Hold}
	if w.Origin != nil {
		c.Raw = w.Origin.Raw
		o := *w.Origin
		o.Raw = ""
		w.Origin = &o
	}
	pt, err := json.Marshal(c)
	if err != nil {
		return err
	}
	ct, err := sl.Seal(w.Item, pt, aad(w))
	if err != nil {
		return err
	}
	w.Title, w.Props, w.Value, w.Body, w.Text, w.Labels, w.Attachment, w.Anchor, w.Hold, w.Sealed = "", nil, nil, nil, "", nil, nil, nil, nil, ct
	return nil
}

// unseal restores the content fields; it reports true when the key was
// shredded and the event must be erased.
func unseal(w *wireEvent, sl Sealer) (bool, error) {
	pt, err := sl.Open(w.Item, w.Sealed, aad(w))
	if errors.Is(err, ErrKeyShredded) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	dec := json.NewDecoder(bytes.NewReader(pt))
	dec.DisallowUnknownFields()
	var c wireContent
	if err := dec.Decode(&c); err != nil {
		return false, fmt.Errorf("sealed content: %w", err)
	}
	w.Title, w.Props, w.Value, w.Body, w.Text, w.Labels, w.Attachment, w.Anchor, w.Hold = c.Title, c.Props, c.Value, c.Body, c.Text, c.Labels, c.Attachment, c.Anchor, c.Hold
	if w.Origin != nil {
		o := *w.Origin
		o.Raw = c.Raw
		w.Origin = &o
	}
	return false, nil
}

// wireValue is a property value with its Go type, so that a replayed value
// is identical to the live one: an int stays an int, a date stays a time.
type wireValue struct {
	T string `json:"t"` // s(tring), i(nt), f(loat64), b(ool), t(ime)
	V string `json:"v"`
}

// Role names in the persisted format; the numeric values are not persisted,
// so the constants may be reordered.
var roleNames = map[Role]string{Viewer: "viewer", Editor: "editor", SpaceAdmin: "space-admin"}

func toWire(e Event) (wireEvent, error) {
	w := wireEvent{
		V: CodecVersion, Seq: e.Seq, At: e.At, Actor: e.Actor, Kind: e.Kind, Item: e.Item, Batch: e.Batch,
		TypeDef: e.TypeDef, Space: e.Space, SpaceName: e.SpaceName, TypeName: e.TypeName,
		TypeVersion: e.TypeVersion, Parent: e.Parent, Order: e.Order, Title: e.Title, Field: e.Field,
		Transition: e.Transition, From: e.From, To: e.To, Body: e.Body, Relation: e.Relation,
		Members: e.Members, Group: e.Group, Principal: e.Principal, SystemRole: e.SystemRole,
		Restriction: e.Restriction, Reason: e.Reason, Origin: e.Origin, Key: e.Key, Managed: e.Managed,
		Comment: e.Comment, Text: e.Text, Labels: e.Labels, Attachment: e.Attachment, Erased: e.Erased,
		Anchor: e.Anchor, Reply: e.Reply, Hold: e.Hold, Outcome: e.Outcome,
	}
	if e.Role != 0 {
		name, ok := roleNames[e.Role]
		if !ok {
			return w, fmt.Errorf("unknown role %d", e.Role)
		}
		w.Role = name
	}
	if e.Props != nil {
		w.Props = make(map[string]*wireValue, len(e.Props))
		for k, v := range e.Props {
			wv, err := encodeValue(v)
			if err != nil {
				return w, fmt.Errorf("property %q: %w", k, err)
			}
			w.Props[k] = wv
		}
	}
	wv, err := encodeValue(e.Value)
	if err != nil {
		return w, fmt.Errorf("value: %w", err)
	}
	w.Value = wv
	return w, nil
}

func fromWire(w wireEvent) (Event, error) {
	if w.V < 1 || w.V > CodecVersion {
		return Event{}, fmt.Errorf("format version %d is not supported (this build reads up to %d)", w.V, CodecVersion)
	}
	e := Event{
		Seq: w.Seq, At: w.At, Actor: w.Actor, Kind: w.Kind, Item: w.Item, Batch: w.Batch,
		TypeDef: w.TypeDef, Space: w.Space, SpaceName: w.SpaceName, TypeName: w.TypeName,
		TypeVersion: w.TypeVersion, Parent: w.Parent, Order: w.Order, Title: w.Title, Field: w.Field,
		Transition: w.Transition, From: w.From, To: w.To, Body: w.Body, Relation: w.Relation,
		Members: w.Members, Group: w.Group, Principal: w.Principal, SystemRole: w.SystemRole,
		Restriction: w.Restriction, Reason: w.Reason, Origin: w.Origin, Key: w.Key, Managed: w.Managed,
		Comment: w.Comment, Text: w.Text, Labels: w.Labels, Attachment: w.Attachment, Erased: w.Erased,
		Anchor: w.Anchor, Reply: w.Reply, Hold: w.Hold, Outcome: w.Outcome,
	}
	if w.Role != "" {
		for r, name := range roleNames {
			if name == w.Role {
				e.Role = r
			}
		}
		if e.Role == 0 {
			return e, fmt.Errorf("unknown role %q", w.Role)
		}
	}
	if w.Props != nil {
		e.Props = make(Props, len(w.Props))
		for k, wv := range w.Props {
			v, err := decodeValue(wv)
			if err != nil {
				return e, fmt.Errorf("property %q: %w", k, err)
			}
			e.Props[k] = v
		}
	}
	v, err := decodeValue(w.Value)
	if err != nil {
		return e, fmt.Errorf("value: %w", err)
	}
	e.Value = v
	return e, nil
}

func encodeValue(v any) (*wireValue, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case string:
		return &wireValue{"s", x}, nil
	case int:
		return &wireValue{"i", strconv.Itoa(x)}, nil
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return nil, fmt.Errorf("number %v cannot be stored", x)
		}
		return &wireValue{"f", strconv.FormatFloat(x, 'g', -1, 64)}, nil
	case bool:
		return &wireValue{"b", strconv.FormatBool(x)}, nil
	case time.Time:
		return &wireValue{"t", x.UTC().Format(time.RFC3339Nano)}, nil
	default:
		return nil, fmt.Errorf("value of type %T cannot be stored", v)
	}
}

func decodeValue(wv *wireValue) (any, error) {
	if wv == nil {
		return nil, nil
	}
	switch wv.T {
	case "s":
		return wv.V, nil
	case "i":
		return strconv.Atoi(wv.V)
	case "f":
		return strconv.ParseFloat(wv.V, 64)
	case "b":
		return strconv.ParseBool(wv.V)
	case "t":
		return time.Parse(time.RFC3339Nano, wv.V)
	default:
		return nil, fmt.Errorf("unknown value type %q", wv.T)
	}
}

// normalize brings an event into the form it has after a round trip through
// the journal, so that live state and replayed state are identical: dates
// are UTC without a monotonic clock reading.
func (e *Event) normalize() {
	for k, v := range e.Props {
		if t, ok := v.(time.Time); ok {
			if t != t.UTC() || t.Location() != time.UTC {
				e.Props = maps.Clone(e.Props) // never change the caller's map
			}
			e.Props[k] = t.UTC()
		}
	}
	if t, ok := e.Value.(time.Time); ok {
		e.Value = t.UTC()
	}
}
