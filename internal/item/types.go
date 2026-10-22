package item

import (
	"fmt"
	"math"
	"net/url"
	"slices"
	"strconv"
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
	// KindURL is a web address, http or https (ADR-0074).
	KindURL FieldKind = "url"
)

// maxURL is the longest web address a url field holds.
const maxURL = 2048

// FieldDef defines one property of a type.
type FieldDef struct {
	Name     string
	Kind     FieldKind
	Required bool
	// Values lists the allowed values of an enum field.
	Values []string
	// Unique says that no two live items of the type in one space hold the
	// same value, e.g. an identifier in another system (ADR-0074).
	Unique bool
	// Recommended says that the field should be set; the governance report
	// lists items where it is empty (ADR-0074).
	Recommended bool
}

// Review is a type's rule for checking its items again (ADR-0074): an
// item is due when the date field Field is empty or older than EveryDays.
// Owner names a user field; who it holds is responsible for the item's
// governance findings instead of whoever changed it last.
type Review struct {
	EveryDays int
	Field     string
	Owner     string
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
	// Review, if set, has the governance report ask for the items to be
	// checked again from time to time (ADR-0074).
	Review *Review
}

// reservedFields are intrinsic to every item and cannot be declared as
// properties. "state" in particular is reserved so that it can only change
// through transitions (D3).
var reservedFields = map[string]bool{
	"id": true, "key": true, "type": true, "space": true, "parent": true,
	"title": true, "state": true, "body": true,
}

// ReservedField reports whether a name is intrinsic to every item and so
// cannot be used for a field. Importers use it to rename source fields.
func ReservedField(name string) bool { return reservedFields[name] }

var validKinds = map[FieldKind]bool{
	KindText: true, KindNumber: true, KindBool: true, KindDate: true, KindUser: true, KindEnum: true, KindURL: true,
}

func (t TypeDef) field(name string) (FieldDef, bool) {
	for _, f := range t.Fields {
		if f.Name == name {
			return f, true
		}
	}
	return FieldDef{}, false
}

// Finished reports whether an item in state is done with (ADR-0082): the
// workflow's last state, or a state no transition leaves, such as the
// accepted of a decision whose last state is rejected.
func (w *Workflow) Finished(state string) bool {
	if w == nil || len(w.States) == 0 {
		return false
	}
	if state == w.States[len(w.States)-1] {
		return true
	}
	return slices.Contains(w.States, state) && !slices.ContainsFunc(w.Transitions, func(tr Transition) bool { return slices.Contains(tr.From, state) })
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
		case f.Unique && f.Kind != KindText && f.Kind != KindURL && f.Kind != KindNumber:
			return invalid("field %q: only text, url and number fields can be unique", f.Name)
		}
		seen[f.Name] = true
	}
	if r := t.Review; r != nil {
		if r.EveryDays < 1 || r.EveryDays > 3650 {
			return invalid("a review is due every 1 to 3650 days")
		}
		if f, ok := t.field(r.Field); !ok || f.Kind != KindDate {
			return invalid("the review's field %q is not a date field of the type", r.Field)
		}
		if f, ok := t.field(r.Owner); r.Owner != "" && (!ok || f.Kind != KindUser) {
			return invalid("the review's owner %q is not a user field of the type", r.Owner)
		}
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
		switch x := v.(type) {
		case int:
			ok = true
		case float64:
			ok = !math.IsNaN(x) && !math.IsInf(x, 0) // must survive the journal
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
	case KindURL:
		s, isString := v.(string)
		ok = isString && validURL(s)
	}
	if !ok {
		return invalid("field %q: value %#v is not a valid %s", f.Name, v, f.Kind)
	}
	return nil
}

// validURL accepts an absolute http or https address with a host.
func validURL(s string) bool {
	if len(s) > maxURL || strings.ContainsAny(s, " \t\r\n") {
		return false
	}
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// FormatValue is a property value as text: as typed for text, url, enum
// and user fields, a date as YYYY-MM-DD, a number in its shortest form.
// The API's where filter compares with it (ADR-0074).
func FormatValue(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case time.Time:
		return x.Format(time.DateOnly)
	case int:
		return strconv.Itoa(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	}
	return fmt.Sprint(v)
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

func invalid(format string, args ...any) error { return wrapf(ErrValidation, format, args...) }

func notFound(format string, args ...any) error { return wrapf(ErrNotFound, format, args...) }

// referenced refuses a removal while something still uses what is removed.
func referenced(format string, args ...any) error {
	return fmt.Errorf("%w: %w: %s", ErrValidation, ErrReferenced, fmt.Sprintf(format, args...))
}

func wrapf(sentinel error, format string, args ...any) error {
	return fmt.Errorf("%w: %s", sentinel, fmt.Sprintf(format, args...))
}
