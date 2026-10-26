package item

import (
	"bytes"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/query"
)

// QueryEnv is what a query of the query language reads besides the items
// (ADR-0136).
type QueryEnv struct {
	// Now is the time now(), the other time functions and offsets such as
	// -7d count from.
	Now time.Time
	// Users returns the user IDs a name stands for in a condition on a
	// user field or the creator, e.g. the account with that username. A
	// name also stands for the user ID it is. An unknown name stands for
	// nobody, without an error, so a query does not tell which accounts
	// exist.
	Users func(name string) []string
}

// Select returns the readable items that base and the query text select,
// in the query's order; items it orders alike keep the order they were made
// in (ADR-0136). It reads the items as the principal does: protected
// passages veiled, and the key of an item the principal may not read
// names nothing (D10).
func (x *Session) Select(base Query, text string, env QueryEnv) ([]Item, error) {
	matches, err := x.selected(base, text, env)
	if err != nil {
		return nil, err
	}
	out := make([]Item, len(matches))
	for i, it := range matches {
		out[i] = x.s.veiled(x.who, it).clone()
	}
	return out, nil
}

// SelectPage is Select, paged like FindPage.
func (x *Session) SelectPage(base Query, text string, env QueryEnv, after ID, limit int) (page []Item, total int, next ID, err error) {
	matches, err := x.selected(base, text, env)
	if err != nil {
		return nil, 0, "", err
	}
	return x.page(matches, after, limit)
}

func (x *Session) selected(base Query, text string, env QueryEnv) ([]*Item, error) {
	q, err := query.Parse(text)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrValidation, err)
	}
	c := &compiler{x: x, env: env, fields: x.s.fieldDefs()}
	match, err := c.expr(q.Where)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrValidation, err)
	}
	keys, err := c.order(q.Order)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrValidation, err)
	}
	var out []*Item
	for _, it := range x.s.matching(base, func(it *Item) bool { return x.s.canRead(x.who, it) }) {
		// Passages the principal may not read are not searched (ADR-0088).
		if match == nil || match(x.s.veiled(x.who, it)) {
			out = append(out, it)
		}
	}
	if len(keys) == 0 {
		return out, nil
	}
	type row struct {
		it   *Item
		keys []sortKey
	}
	rows := make([]row, len(out))
	for i, it := range out {
		rows[i] = row{it, make([]sortKey, len(keys))}
		for k, key := range keys {
			rows[i].keys[k] = key.of(it)
		}
	}
	slices.SortStableFunc(rows, func(a, b row) int {
		for k, key := range keys {
			if c := compareKeys(a.keys[k], b.keys[k], key.desc); c != 0 {
				return c
			}
		}
		return 0
	})
	for i, r := range rows {
		out[i] = r.it
	}
	return out, nil
}

// fieldDefs returns every definition of every property, by name, from
// every version of every type: an item keeps the version it was made with.
func (s *Store) fieldDefs() map[string][]FieldDef {
	out := map[string][]FieldDef{}
	for _, versions := range s.types {
		for _, td := range versions {
			for _, f := range td.Fields {
				out[f.Name] = append(out[f.Name], f)
			}
		}
	}
	return out
}

// fieldOf returns the definition of a property in the item's type version.
func (s *Store) fieldOf(it *Item, name string) (FieldDef, bool) {
	v := s.types[it.Type]
	if it.TypeVersion < 1 || it.TypeVersion > len(v) {
		return FieldDef{}, false
	}
	return v[it.TypeVersion-1].field(name)
}

// workflowOf returns the workflow of the item's type version, or nil.
func (s *Store) workflowOf(it *Item) *Workflow {
	v := s.types[it.Type]
	if it.TypeVersion < 1 || it.TypeVersion > len(v) {
		return nil
	}
	return v[it.TypeVersion-1].Workflow
}

// changesItem are the events that change an item itself, for the field
// updated: its content, state, place, files and comments. Relations,
// access, holds and watches are not changes of the item.
var changesItem = map[EventKind]bool{
	ItemCreated: true, ItemImported: true, TitleSet: true, PropertySet: true, Transitioned: true, BodyCommitted: true,
	LabelsSet: true, ItemMoved: true, CommentAdded: true, CommentEdited: true, AttachmentAdded: true, AttachmentDeleted: true,
}

// lastChange is the newest event that changed the item itself.
func (s *Store) lastChange(it *Item) Event {
	l := s.touching[it.ID]
	for i := len(l) - 1; i >= 0; i-- {
		if e := s.log[l[i]]; e.Item == it.ID && changesItem[e.Kind] {
			return e
		}
	}
	return s.log[it.created-1]
}

// --- conditions ----------------------------------------------------------------

type pred func(*Item) bool

// builtins are the fields every item has, by their name in lower case. A
// bare name names one of them; a quoted name names a property, except for
// the reserved names, which no property can have.
var builtins = map[string]string{
	"id": "id", "key": "key", "title": "title", "type": "type", "space": "space", "state": "state",
	"parent": "parent", "ancestor": "ancestor", "label": "label", "labels": "label",
	"text": "text", "body": "body", "comment": "comment",
	"created": "created", "updated": "updated", "creator": "creator", "finished": "finished",
}

// negated are the negative comparisons and what they negate: a != v holds
// wherever a = v does not, for an item without a value of a too.
var negated = map[query.Op]query.Op{query.Ne: query.Eq, query.NotIn: query.In, query.HasNot: query.Has, query.NotEmpty: query.Empty}

type compiler struct {
	x      *Session
	env    QueryEnv
	fields map[string][]FieldDef
	// buf holds the folded text of the item being matched, reused.
	buf []byte
}

func (c *compiler) expr(e query.Expr) (pred, error) {
	switch e := e.(type) {
	case *query.And:
		l, r, err := c.both(e.Left, e.Right)
		if err != nil {
			return nil, err
		}
		return func(it *Item) bool { return l(it) && r(it) }, nil
	case *query.Or:
		l, r, err := c.both(e.Left, e.Right)
		if err != nil {
			return nil, err
		}
		return func(it *Item) bool { return l(it) || r(it) }, nil
	case *query.Not:
		p, err := c.expr(e.X)
		if err != nil {
			return nil, err
		}
		return func(it *Item) bool { return !p(it) }, nil
	case *query.Clause:
		op, neg := negated[e.Op]
		if !neg {
			op = e.Op
		}
		p, err := c.clause(e, op)
		if err != nil || !neg {
			return p, err
		}
		return func(it *Item) bool { return !p(it) }, nil
	}
	return nil, nil
}

func (c *compiler) both(a, b query.Expr) (pred, pred, error) {
	l, err := c.expr(a)
	if err != nil {
		return nil, nil, err
	}
	r, err := c.expr(b)
	return l, r, err
}

// builtin returns the built-in field a name names, if it names one.
func builtin(n query.Name) (string, bool) {
	lower := strings.ToLower(n.Text)
	f, ok := builtins[lower]
	if !ok || (n.Quoted && !reservedFields[lower]) {
		return "", false
	}
	return f, true
}

// takes refuses a comparison a field does not take.
func takes(cl *query.Clause, op query.Op, ops ...query.Op) error {
	if !slices.Contains(ops, op) {
		return query.Errorf(cl.OpPos, "%s does not take %s", cl.Field.Text, cl.Op)
	}
	return nil
}

// notAValue refuses a value the field cannot hold.
func notAValue(cl *query.Clause, v query.Value) error {
	return query.Errorf(v.Pos, "%q is not a value of %s", shown(v), cl.Field.Text)
}

func shown(v query.Value) string {
	if v.Func {
		return v.Text + "()"
	}
	return v.Text
}

var (
	equality = []query.Op{query.Eq, query.In}
	ordering = []query.Op{query.Eq, query.In, query.Lt, query.Le, query.Gt, query.Ge}
)

func (c *compiler) clause(cl *query.Clause, op query.Op) (pred, error) {
	f, ok := builtin(cl.Field)
	if !ok {
		return c.property(cl, op)
	}
	s := c.x.s
	switch f {
	case "id":
		return c.set(cl, op, func(it *Item) []string { return []string{string(it.ID)} }, func(v query.Value) (string, bool) { return v.Text, !v.Func })
	case "key":
		if err := takes(cl, op, equality...); err != nil {
			return nil, err
		}
		ids := map[ID]bool{}
		for _, v := range cl.Values {
			if v.Func {
				return nil, notAValue(cl, v)
			}
			if id, ok := c.keyed(v.Text); ok {
				ids[id] = true
			}
		}
		return func(it *Item) bool { return ids[it.ID] }, nil
	case "title":
		return c.text(cl, op, func(it *Item) (string, bool) { return it.Title, it.Title != "" })
	case "type":
		names := map[string]bool{}
		for name := range s.types {
			names[fold(name)] = true
		}
		return c.set(cl, op, func(it *Item) []string { return []string{it.Type} }, func(v query.Value) (string, bool) { return v.Text, !v.Func },
			func(v query.Value) error {
				if !names[fold(v.Text)] {
					return query.Errorf(v.Pos, "no type is called %s", v.Text)
				}
				return nil
			})
	case "space":
		return c.set(cl, op, func(it *Item) []string { return []string{it.Space} }, func(v query.Value) (string, bool) { return v.Text, !v.Func })
	case "state":
		states := map[string]bool{}
		for _, versions := range s.types {
			for _, td := range versions {
				if td.Workflow != nil {
					for _, st := range td.Workflow.States {
						states[fold(st)] = true
					}
				}
			}
		}
		return c.set(cl, op, func(it *Item) []string { return nonEmpty(it.State) }, func(v query.Value) (string, bool) { return v.Text, !v.Func },
			func(v query.Value) error {
				if !states[fold(v.Text)] {
					return query.Errorf(v.Pos, "no workflow has a state %s", v.Text)
				}
				return nil
			}, query.Empty)
	case "label":
		return c.set(cl, op, func(it *Item) []string { return it.Labels }, func(v query.Value) (string, bool) { return v.Text, !v.Func }, query.Empty)
	case "creator":
		return c.users(cl, op, func(it *Item) string { return s.log[it.created-1].Actor })
	case "finished":
		if err := takes(cl, op, query.Eq, query.In, query.Empty); err != nil {
			return nil, err
		}
		want := map[bool]bool{}
		for _, v := range cl.Values {
			lower := strings.ToLower(v.Text)
			if v.Func || (lower != "true" && lower != "false") {
				return nil, notAValue(cl, v)
			}
			want[lower == "true"] = true
		}
		return func(it *Item) bool {
			w := s.workflowOf(it)
			if op == query.Empty {
				return w == nil
			}
			return w != nil && want[w.Finished(it.State)]
		}, nil
	case "created", "updated":
		at := func(it *Item) time.Time { return s.log[it.created-1].At }
		if f == "updated" {
			at = func(it *Item) time.Time { return s.lastChange(it).At }
		}
		return c.times(cl, op, at)
	case "parent", "ancestor":
		ops := slices.Clone(equality)
		if f == "parent" {
			ops = append(ops, query.Empty)
		}
		if err := takes(cl, op, ops...); err != nil {
			return nil, err
		}
		ids := map[ID]bool{}
		for _, v := range cl.Values {
			if v.Func {
				return nil, notAValue(cl, v)
			}
			if id, ok := c.keyed(v.Text); ok {
				// The key of an item the principal may not read names
				// nothing, as if there were none (D10).
				if _, err := c.x.readable(id); err == nil {
					ids[id] = true
				}
			} else {
				ids[ID(v.Text)] = true
			}
		}
		if op == query.Empty {
			return func(it *Item) bool { return it.Parent == "" }, nil
		}
		if f == "parent" {
			return func(it *Item) bool { return ids[it.Parent] }, nil
		}
		return func(it *Item) bool {
			// The containment tree has no cycles (D5); the bound only
			// guards against a store that broke it.
			for p, n := it.Parent, 0; p != "" && n <= len(s.items); n++ {
				if ids[p] {
					return true
				}
				up, ok := s.items[p]
				if !ok {
					return false
				}
				p = up.Parent
			}
			return false
		}, nil
	case "text":
		return c.words(cl, op, false, func(buf []byte, it *Item) []byte {
			buf = appendPart(buf, it.Title)
			buf = appendPart(buf, it.Key)
			for _, b := range it.Body.Blocks {
				buf = appendPart(buf, b.Text)
			}
			for _, v := range it.Properties {
				if s, ok := v.(string); ok {
					buf = appendPart(buf, s)
				}
			}
			for _, l := range it.Labels {
				buf = appendPart(buf, l)
			}
			for _, cm := range it.Comments {
				buf = appendPart(buf, cm.Text)
			}
			return buf
		})
	case "body":
		return c.words(cl, op, true, func(buf []byte, it *Item) []byte {
			for _, b := range it.Body.Blocks {
				buf = appendPart(buf, b.Text)
			}
			return buf
		})
	}
	// comment: a deleted comment's text is already empty.
	return c.words(cl, op, true, func(buf []byte, it *Item) []byte {
		for _, cm := range it.Comments {
			buf = appendPart(buf, cm.Text)
		}
		return buf
	})
}

// keyed resolves a current or former key, as typed or in upper case.
func (c *compiler) keyed(key string) (ID, bool) {
	if id, ok := c.x.s.keys[key]; ok {
		return id, true
	}
	id, ok := c.x.s.keys[strings.ToUpper(key)]
	return id, ok
}

func nonEmpty(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}

func appendPart(buf []byte, text string) []byte { return append(appendFold(buf, text), '\n') }

// set compiles a condition on a field whose values are names, compared
// without regard to case: = and IN, and the extra comparisons given. check
// refuses a value no item can have.
func (c *compiler) set(cl *query.Clause, op query.Op, get func(*Item) []string, value func(query.Value) (string, bool), more ...any) (pred, error) {
	ops := slices.Clone(equality)
	var check func(query.Value) error
	for _, m := range more {
		switch m := m.(type) {
		case query.Op:
			ops = append(ops, m)
		case func(query.Value) error:
			check = m
		}
	}
	if err := takes(cl, op, ops...); err != nil {
		return nil, err
	}
	if op == query.Empty {
		return func(it *Item) bool { return len(get(it)) == 0 }, nil
	}
	want := map[string]bool{}
	for _, v := range cl.Values {
		text, ok := value(v)
		if !ok {
			return nil, notAValue(cl, v)
		}
		if check != nil {
			if err := check(v); err != nil {
				return nil, err
			}
		}
		want[fold(text)] = true
	}
	return func(it *Item) bool {
		for _, v := range get(it) {
			if want[fold(v)] {
				return true
			}
		}
		return false
	}, nil
}

// text compiles a condition on a text field of every item.
func (c *compiler) text(cl *query.Clause, op query.Op, get func(*Item) (string, bool)) (pred, error) {
	if op == query.Has {
		return c.words(cl, op, true, func(buf []byte, it *Item) []byte {
			v, _ := get(it)
			return appendFold(buf, v)
		})
	}
	if err := takes(cl, op, append(slices.Clone(ordering), query.Empty)...); err != nil {
		return nil, err
	}
	if op == query.Empty {
		return func(it *Item) bool { _, set := get(it); return !set }, nil
	}
	var want []string
	for _, v := range cl.Values {
		if v.Func {
			return nil, notAValue(cl, v)
		}
		want = append(want, fold(v.Text))
	}
	return func(it *Item) bool {
		v, set := get(it)
		if !set {
			return false
		}
		got := fold(v)
		for _, w := range want {
			if holds(op, strings.Compare(got, w)) {
				return true
			}
		}
		return false
	}, nil
}

// holds reports whether a comparison holds for the result of comparing
// the item's value with the query's.
func holds(op query.Op, c int) bool {
	switch op {
	case query.Lt:
		return c < 0
	case query.Le:
		return c <= 0
	case query.Gt:
		return c > 0
	case query.Ge:
		return c >= 0
	}
	return c == 0
}

// words compiles ~, every word of the value in the item's text as add
// gathers it; with empty, also IS EMPTY, no text at all.
func (c *compiler) words(cl *query.Clause, op query.Op, empty bool, add func([]byte, *Item) []byte) (pred, error) {
	ops := []query.Op{query.Has}
	if empty {
		ops = append(ops, query.Empty)
	}
	if err := takes(cl, op, ops...); err != nil {
		return nil, err
	}
	if op == query.Empty {
		return func(it *Item) bool {
			c.buf = add(c.buf[:0], it)
			return len(bytes.TrimSpace(c.buf)) == 0
		}, nil
	}
	v := cl.Values[0]
	if v.Func {
		return nil, notAValue(cl, v)
	}
	var words [][]byte
	for _, w := range wordsOf(v.Text) {
		words = append(words, []byte(w))
	}
	if len(words) == 0 {
		return nil, query.Errorf(v.Pos, "~ needs a letter or a digit")
	}
	return func(it *Item) bool {
		c.buf = add(c.buf[:0], it)
		for _, w := range words {
			if !bytes.Contains(c.buf, w) {
				return false
			}
		}
		return true
	}, nil
}

// users compiles a condition on a user: currentUser() is the principal,
// a name the users it stands for.
func (c *compiler) users(cl *query.Clause, op query.Op, get func(*Item) string) (pred, error) {
	if err := takes(cl, op, equality...); err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, v := range cl.Values {
		ids, ok := c.literal(v).users, !v.Func || v.IsCurrentUser()
		if !ok {
			return nil, notAValue(cl, v)
		}
		for _, id := range ids {
			want[id] = true
		}
	}
	return func(it *Item) bool { return want[get(it)] }, nil
}

// times compiles a condition on a point in time of every item.
func (c *compiler) times(cl *query.Clause, op query.Op, get func(*Item) time.Time) (pred, error) {
	if err := takes(cl, op, ordering...); err != nil {
		return nil, err
	}
	var want []literal
	for _, v := range cl.Values {
		l := c.literal(v)
		if !l.isTime {
			return nil, notAValue(cl, v)
		}
		want = append(want, l)
	}
	return func(it *Item) bool {
		t := get(it)
		for _, l := range want {
			if holds(op, compareTimes(t, false, l)) {
				return true
			}
		}
		return false
	}, nil
}

// compareTimes compares t with a value of the query. When either names a
// whole day, both count by their day.
func compareTimes(t time.Time, day bool, l literal) int {
	if day || l.day {
		return strings.Compare(t.Format(time.DateOnly), l.when.Format(time.DateOnly))
	}
	return t.Compare(l.when)
}

// --- properties ------------------------------------------------------------------

// literal is a value of the query read in every way a field may need it.
type literal struct {
	fn     bool
	text   string // folded
	num    float64
	isNum  bool
	when   time.Time
	day    bool
	isTime bool
	truth  bool
	isBool bool
	users  []string
}

func (c *compiler) literal(v query.Value) literal {
	l := literal{fn: v.Func}
	l.when, l.day, l.isTime = v.Time(c.env.Now)
	if v.IsCurrentUser() {
		l.users = []string{c.x.who}
	}
	if v.Func {
		return l
	}
	l.text = fold(v.Text)
	if n, err := strconv.ParseFloat(v.Text, 64); err == nil && !math.IsNaN(n) && !math.IsInf(n, 0) {
		l.num, l.isNum = n, true
	}
	if l.text == "true" || l.text == "false" {
		l.truth, l.isBool = l.text == "true", true
	}
	l.users = []string{v.Text}
	if c.env.Users != nil {
		l.users = append(l.users, c.env.Users(v.Text)...)
	}
	return l
}

// kindTakes says which comparisons a kind of field takes; every kind
// takes =, IN and IS EMPTY.
func kindTakes(k FieldKind, op query.Op) bool {
	switch op {
	case query.Eq, query.In, query.Empty:
		return true
	case query.Has:
		return k == KindText || k == KindURL
	}
	return k == KindText || k == KindURL || k == KindNumber || k == KindDate || k == KindEnum
}

// fits reports whether a value of the query can be a value of the field.
func fits(f FieldDef, l literal) bool {
	switch f.Kind {
	case KindNumber:
		return l.isNum
	case KindBool:
		return l.isBool
	case KindDate:
		return l.isTime
	case KindUser:
		return len(l.users) > 0
	case KindEnum:
		return !l.fn && enumIndex(f, l.text) >= 0
	}
	return !l.fn
}

func enumIndex(f FieldDef, folded string) int {
	return slices.IndexFunc(f.Values, func(v string) bool { return fold(v) == folded })
}

func (c *compiler) unknownField(n query.Name) error {
	for _, name := range slices.Sorted(maps.Keys(c.fields)) {
		if strings.EqualFold(name, n.Text) {
			return query.Errorf(n.Pos, "no type has a field %s; the field is called %s", n.Text, name)
		}
	}
	return query.Errorf(n.Pos, "no type has a field %s", n.Text)
}

// property compiles a condition on a property. A property may have
// different kinds in different types; each item is compared by the kind
// its own type gives the field, and a value that does not fit that kind
// does not match. The comparison and every value must fit at least one of
// the field's definitions.
func (c *compiler) property(cl *query.Clause, op query.Op) (pred, error) {
	name := cl.Field.Text
	defs := c.fields[name]
	if len(defs) == 0 {
		return nil, c.unknownField(cl.Field)
	}
	if !slices.ContainsFunc(defs, func(f FieldDef) bool { return kindTakes(f.Kind, op) }) {
		return nil, query.Errorf(cl.OpPos, "%s does not take %s", name, cl.Op)
	}
	s := c.x.s
	if op == query.Empty {
		return func(it *Item) bool { return it.Properties[name] == nil }, nil
	}
	var want []literal
	var words [][]byte
	for _, v := range cl.Values {
		l := c.literal(v)
		if !slices.ContainsFunc(defs, func(f FieldDef) bool { return kindTakes(f.Kind, op) && fits(f, l) }) {
			return nil, notAValue(cl, v)
		}
		want = append(want, l)
		if op == query.Has {
			for _, w := range wordsOf(v.Text) {
				words = append(words, []byte(w))
			}
			if len(words) == 0 {
				return nil, query.Errorf(v.Pos, "~ needs a letter or a digit")
			}
		}
	}
	return func(it *Item) bool {
		f, ok := s.fieldOf(it, name)
		got := it.Properties[name]
		if !ok || got == nil || !kindTakes(f.Kind, op) {
			return false
		}
		if op == query.Has {
			c.buf = appendFold(c.buf[:0], FormatValue(got))
			for _, w := range words {
				if !bytes.Contains(c.buf, w) {
					return false
				}
			}
			return true
		}
		for _, l := range want {
			if cmp, ok := compareValue(f, got, l); ok && holds(op, cmp) {
				return true
			}
		}
		return false
	}, nil
}

// compareValue compares a property's value with a value of the query, by
// the field's kind; ok is false where the query's value does not fit it.
func compareValue(f FieldDef, v any, l literal) (int, bool) {
	switch f.Kind {
	case KindNumber:
		n, isNum := number(v)
		if !l.isNum || !isNum {
			return 0, false
		}
		return cmpFloat(n, l.num), true
	case KindBool:
		b, isBool := v.(bool)
		if !l.isBool || !isBool || b != l.truth {
			return 1, l.isBool && isBool
		}
		return 0, true
	case KindDate:
		t, isTime := v.(time.Time)
		if !l.isTime || !isTime {
			return 0, false
		}
		return compareTimes(t, true, l), true
	case KindUser:
		s, _ := v.(string)
		if slices.Contains(l.users, s) {
			return 0, true
		}
		return 1, false
	case KindEnum:
		at, want := enumIndex(f, fold(FormatValue(v))), enumIndex(f, l.text)
		if l.fn || at < 0 || want < 0 {
			return 0, false
		}
		return at - want, true
	}
	if l.fn {
		return 0, false
	}
	return strings.Compare(fold(FormatValue(v)), l.text), true
}

func number(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}

func cmpFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// --- order ------------------------------------------------------------------------

// sortKey is one item's value for one key of the order. Values of
// different kinds sort by their kind, so they do not mix.
type sortKey struct {
	empty bool
	kind  string
	num   float64
	text  string
}

type sortBy struct {
	of   func(*Item) sortKey
	desc bool
}

// compareKeys orders two values; items without a value come last in
// either direction.
func compareKeys(a, b sortKey, desc bool) int {
	if a.empty || b.empty {
		switch {
		case a.empty == b.empty:
			return 0
		case a.empty:
			return 1
		}
		return -1
	}
	c := strings.Compare(a.kind, b.kind)
	if c == 0 {
		c = cmpFloat(a.num, b.num)
	}
	if c == 0 {
		c = strings.Compare(a.text, b.text)
	}
	if desc {
		return -c
	}
	return c
}

func (c *compiler) order(sorts []query.Sort) ([]sortBy, error) {
	s := c.x.s
	var out []sortBy
	for _, srt := range sorts {
		var of func(*Item) sortKey
		f, ok := builtin(srt.Field)
		switch {
		case !ok:
			name := srt.Field.Text
			if len(c.fields[name]) == 0 {
				return nil, c.unknownField(srt.Field)
			}
			of = func(it *Item) sortKey {
				f, ok := s.fieldOf(it, name)
				v := it.Properties[name]
				if !ok || v == nil {
					return sortKey{empty: true}
				}
				k := sortKey{kind: string(f.Kind)}
				switch f.Kind {
				case KindNumber:
					k.num, _ = number(v)
				case KindBool:
					if b, _ := v.(bool); b {
						k.num = 1
					}
				case KindEnum:
					k.num = float64(enumIndex(f, fold(FormatValue(v))))
				case KindDate:
					k.text = FormatValue(v)
				default:
					k.text = fold(FormatValue(v))
				}
				return k
			}
		case f == "id":
			of = func(it *Item) sortKey { return sortKey{text: string(it.ID)} }
		case f == "key":
			of = func(it *Item) sortKey {
				at := strings.LastIndexByte(it.Key, '-')
				n, _ := strconv.Atoi(it.Key[at+1:])
				return sortKey{text: it.Key[:max(at, 0)], num: float64(n)}
			}
		case f == "title":
			of = func(it *Item) sortKey { return sortKey{text: fold(it.Title)} }
		case f == "type":
			of = func(it *Item) sortKey { return sortKey{text: fold(it.Type)} }
		case f == "space":
			of = func(it *Item) sortKey { return sortKey{text: fold(it.Space)} }
		case f == "state":
			// By the state's place in the item's workflow: open before done.
			of = func(it *Item) sortKey {
				w := s.workflowOf(it)
				if w == nil || it.State == "" {
					return sortKey{empty: true}
				}
				return sortKey{num: float64(slices.Index(w.States, it.State)), text: fold(it.State)}
			}
		case f == "label":
			of = func(it *Item) sortKey {
				if len(it.Labels) == 0 {
					return sortKey{empty: true}
				}
				return sortKey{text: fold(it.Labels[0])}
			}
		case f == "created":
			of = func(it *Item) sortKey { return sortKey{num: float64(it.created)} }
		case f == "updated":
			of = func(it *Item) sortKey { return sortKey{num: float64(s.lastChange(it).Seq)} }
		case f == "creator":
			of = func(it *Item) sortKey { return sortKey{text: s.log[it.created-1].Actor} }
		case f == "finished":
			of = func(it *Item) sortKey {
				w := s.workflowOf(it)
				if w == nil {
					return sortKey{empty: true}
				}
				if w.Finished(it.State) {
					return sortKey{num: 1}
				}
				return sortKey{}
			}
		default:
			return nil, query.Errorf(srt.Field.Pos, "the items cannot be ordered by %s", srt.Field.Text)
		}
		out = append(out, sortBy{of: of, desc: srt.Desc})
	}
	return out, nil
}
