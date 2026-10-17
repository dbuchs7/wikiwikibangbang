package item

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"strconv"
	"time"
)

// CodecVersion is the version of the persisted event format (ADR-0025). A
// reader refuses events from a newer version rather than misreading them.
const CodecVersion = 1

// EncodeBatch encodes the events of one command for the journal.
func EncodeBatch(batch []Event) ([]byte, error) {
	wire := make([]wireEvent, len(batch))
	for i, e := range batch {
		w, err := toWire(e)
		if err != nil {
			return nil, fmt.Errorf("event %d: %w", e.Seq, err)
		}
		wire[i] = w
	}
	return json.Marshal(wire)
}

// DecodeBatch decodes a batch written by EncodeBatch.
func DecodeBatch(data []byte) ([]Event, error) {
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
		e, err := fromWire(w)
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
		TypeVersion: e.TypeVersion, Parent: e.Parent, Title: e.Title, Field: e.Field,
		Transition: e.Transition, From: e.From, To: e.To, Body: e.Body, Relation: e.Relation,
		Members: e.Members, Group: e.Group, Principal: e.Principal, SystemRole: e.SystemRole,
		Restriction: e.Restriction, Reason: e.Reason, Origin: e.Origin, Key: e.Key, Managed: e.Managed,
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
		TypeVersion: w.TypeVersion, Parent: w.Parent, Title: w.Title, Field: w.Field,
		Transition: w.Transition, From: w.From, To: w.To, Body: w.Body, Relation: w.Relation,
		Members: w.Members, Group: w.Group, Principal: w.Principal, SystemRole: w.SystemRole,
		Restriction: w.Restriction, Reason: w.Reason, Origin: w.Origin, Key: w.Key, Managed: w.Managed,
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
