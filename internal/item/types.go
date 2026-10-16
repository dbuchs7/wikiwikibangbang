package item

import (
	"fmt"
	"strings"
	"time"
)

// FieldKind is the value kind of a field.
type FieldKind string

// The supported field kinds.
const (
	KindText   FieldKind = "text"
	KindNumber FieldKind = "number"
	KindBool   FieldKind = "bool"
	KindDate   FieldKind = "date"
	KindUser   FieldKind = "user"
	KindEnum   FieldKind = "enum"
)

// FieldDef defines one property of a type.
type FieldDef struct {
	Name     string
	Kind     FieldKind
	Required bool
	// Values lists the allowed values of an enum field.
	Values []string
}

// Transition is a named move between workflow states. Requires names the
// fields that must be set for the transition to be allowed.
type Transition struct {
	Name     string
	From     []string
	To       string
	Requires []string
}

// Workflow is the lifecycle of a type. State changes only through its
// transitions (D3).
type Workflow struct {
	Initial     string
	States      []string
	Transitions []Transition
}

// TypeDef defines an item type. Types are data, not code (ADR-0001): pages,
// tasks and decisions are all TypeDefs.
type TypeDef struct {
	Name string
	// Version is assigned by the store; redefining a type increments it.
	Version  int
	Fields   []FieldDef
	Workflow *Workflow
	// Body says whether items of this type carry a document body.
	Body bool
}

// reservedFields are intrinsic to every item and cannot be declared as
// properties. "state" in particular is reserved so that it can only change
// through transitions (D3).
var reservedFields = map[string]bool{
	"id": true, "key": true, "type": true, "space": true, "parent": true,
	"title": true, "state": true, "body": true,
}

var validKinds = map[FieldKind]bool{
	KindText: true, KindNumber: true, KindBool: true, KindDate: true, KindUser: true, KindEnum: true,
}

func (t TypeDef) field(name string) (FieldDef, bool) {
	for _, f := range t.Fields {
		if f.Name == name {
			return f, true
		}
	}
	return FieldDef{}, false
}

func (w *Workflow) transition(name string) (Transition, bool) {
	for _, tr := range w.Transitions {
		if tr.Name == name {
			return tr, true
		}
	}
	return Transition{}, false
}

func (t TypeDef) validate() error {
	if strings.TrimSpace(t.Name) == "" {
		return invalid("type name is empty")
	}
	seen := map[string]bool{}
	for _, f := range t.Fields {
		switch {
		case reservedFields[f.Name]:
			return invalid("field %q is reserved", f.Name)
		case seen[f.Name]:
			return invalid("field %q declared twice", f.Name)
		case !validKinds[f.Kind]:
			return invalid("field %q has unknown kind %q", f.Name, f.Kind)
		case f.Kind == KindEnum && len(f.Values) == 0:
			return invalid("enum field %q has no values", f.Name)
		}
		seen[f.Name] = true
	}
	if t.Workflow == nil {
		return nil
	}
	w := t.Workflow
	states := map[string]bool{}
	for _, s := range w.States {
		states[s] = true
	}
	if !states[w.Initial] {
		return invalid("initial state %q is not a state", w.Initial)
	}
	names := map[string]bool{}
	for _, tr := range w.Transitions {
		if names[tr.Name] {
			return invalid("transition %q declared twice", tr.Name)
		}
		names[tr.Name] = true
		if !states[tr.To] {
			return invalid("transition %q goes to unknown state %q", tr.Name, tr.To)
		}
		for _, from := range tr.From {
			if !states[from] {
				return invalid("transition %q starts from unknown state %q", tr.Name, from)
			}
		}
		for _, req := range tr.Requires {
			if !seen[req] {
				return invalid("transition %q requires unknown field %q", tr.Name, req)
			}
		}
	}
	return nil
}

// checkValue validates one property value against its field. A nil value
// means "unset" and is only refused for required fields.
func checkValue(f FieldDef, v any) error {
	if v == nil {
		if f.Required {
			return invalid("field %q is required", f.Name)
		}
		return nil
	}
	ok := false
	switch f.Kind {
	case KindText:
		_, ok = v.(string)
	case KindNumber:
		switch v.(type) {
		case float64, int:
			ok = true
		}
	case KindBool:
		_, ok = v.(bool)
	case KindDate:
		_, ok = v.(time.Time)
	case KindUser:
		s, isString := v.(string)
		ok = isString && strings.TrimSpace(s) != ""
	case KindEnum:
		s, isString := v.(string)
		for _, allowed := range f.Values {
			if isString && s == allowed {
				ok = true
			}
		}
	}
	if !ok {
		return invalid("field %q: value %#v is not a valid %s", f.Name, v, f.Kind)
	}
	return nil
}

// checkProps validates a set of property values against a type, rejecting
// unknown fields.
func checkProps(t TypeDef, props Props) error {
	for name, v := range props {
		f, ok := t.field(name)
		if !ok {
			return invalid("type %q has no field %q", t.Name, name)
		}
		if err := checkValue(f, v); err != nil {
			return err
		}
	}
	return nil
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrValidation, fmt.Sprintf(format, args...))
}

func notFound(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrNotFound, fmt.Sprintf(format, args...))
}
