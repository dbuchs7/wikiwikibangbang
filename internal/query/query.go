// Package query reads the query language of ADR-0136: conditions on the
// fields of items, joined with AND, OR and NOT, and an order, e.g.
//
//	type = task AND assignee = currentUser() AND state != done ORDER BY due
//
// It knows the language, not the items: it parses a query into a tree and
// reads values as points in time. Which fields exist and what a condition
// means for an item is the item store's part (internal/item), which
// evaluates a query through the caller's session.
package query

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
)

// Op is the comparison of a condition.
type Op string

// The comparisons. Has asks for every word of the value to occur in the
// field's text; In and NotIn take a list; Empty and NotEmpty take none.
const (
	Eq       Op = "="
	Ne       Op = "!="
	Lt       Op = "<"
	Le       Op = "<="
	Gt       Op = ">"
	Ge       Op = ">="
	Has      Op = "~"
	HasNot   Op = "!~"
	In       Op = "in"
	NotIn    Op = "not in"
	Empty    Op = "is empty"
	NotEmpty Op = "is not empty"
)

// Name names a field. A quoted name names a property even where a
// built-in field has the same name, and may hold any character.
type Name struct {
	Text   string
	Quoted bool
	// Pos is where the name starts, in characters from 0.
	Pos int
}

// Value is a literal, or a call of one of the functions (Func set, Text
// its name as the language spells it, Args its arguments).
type Value struct {
	Text   string
	Quoted bool
	Func   bool
	Args   []Value
	Pos    int
}

// Expr is a condition: an *And, *Or, *Not or *Clause.
type Expr interface{ expr() }

// And holds when both sides hold, Or when either does, Not when X does not.
type (
	And struct{ Left, Right Expr }
	Or  struct{ Left, Right Expr }
	Not struct{ X Expr }
)

// Clause compares a field with values: one, a list for In and NotIn, none
// for Empty and NotEmpty.
type Clause struct {
	Field  Name
	Op     Op
	Values []Value
	// OpPos is where the comparison starts.
	OpPos int
}

func (*And) expr()    {}
func (*Or) expr()     {}
func (*Not) expr()    {}
func (*Clause) expr() {}

// Sort is one key of the order, ascending unless Desc.
type Sort struct {
	Field Name
	Desc  bool
}

// Query is a parsed query. A nil Where selects every item.
type Query struct {
	Where Expr
	Order []Sort
}

// Error is a mistake in a query, at a position in characters from 0.
type Error struct {
	Pos int
	Msg string
}

func (e *Error) Error() string { return fmt.Sprintf("at character %d: %s", e.Pos+1, e.Msg) }

// Errorf returns an *Error at pos.
func Errorf(pos int, format string, args ...any) error {
	return &Error{Pos: pos, Msg: fmt.Sprintf(format, args...)}
}

// functions are the functions a value may call, by their name in lower
// case: the spelling the language uses, and the arguments they take at
// most. The time functions take an offset: a whole number in their own
// unit (startOfMonth(-1) is the start of last month) or a number with a
// unit, added to the start or end (startOfDay(+8h)).
var functions = map[string]struct {
	name string
	args int
}{
	"currentuser":  {"currentUser", 0},
	"now":          {"now", 0},
	"startofday":   {"startOfDay", 1},
	"endofday":     {"endOfDay", 1},
	"startofweek":  {"startOfWeek", 1},
	"endofweek":    {"endOfWeek", 1},
	"startofmonth": {"startOfMonth", 1},
	"endofmonth":   {"endOfMonth", 1},
	"startofyear":  {"startOfYear", 1},
	"endofyear":    {"endOfYear", 1},
}

// functionNames lists the functions for an error message, in the order
// people look for them.
var functionNames = []string{"currentUser", "now", "startOfDay", "endOfDay", "startOfWeek", "endOfWeek",
	"startOfMonth", "endOfMonth", "startOfYear", "endOfYear"}

// keywords are the words of the language, in lower case. Bare, they are
// never a field or a value; quoted, they are.
var keywords = map[string]bool{
	"and": true, "or": true, "not": true, "in": true, "is": true, "empty": true, "null": true,
	"order": true, "by": true, "asc": true, "desc": true,
}

// --- lexer -------------------------------------------------------------------

type tokenKind int

const (
	tEOF tokenKind = iota
	tWord
	tString
	tOp
	tOpen
	tClose
	tComma
)

type token struct {
	kind tokenKind
	text string
	pos  int
}

// special are the characters that end a bare word.
const special = `()=!<>~,"'`

func lex(text string) ([]token, error) {
	rs := []rune(text)
	var out []token
	for i := 0; i < len(rs); {
		r := rs[i]
		switch {
		case unicode.IsSpace(r):
			i++
		case r == '(':
			out = append(out, token{tOpen, "(", i})
			i++
		case r == ')':
			out = append(out, token{tClose, ")", i})
			i++
		case r == ',':
			out = append(out, token{tComma, ",", i})
			i++
		case r == '"' || r == '\'':
			var b strings.Builder
			j := i + 1
			for ; j < len(rs) && rs[j] != r; j++ {
				if rs[j] == '\\' && j+1 < len(rs) {
					j++
				}
				b.WriteRune(rs[j])
			}
			if j >= len(rs) {
				return nil, Errorf(i, "the text is not closed with %c", r)
			}
			out = append(out, token{tString, b.String(), i})
			i = j + 1
		case strings.ContainsRune("=<>~!", r):
			op := string(r)
			if i+1 < len(rs) && rs[i+1] == '=' && r != '=' && r != '~' {
				op += "="
			} else if r == '!' && i+1 < len(rs) && rs[i+1] == '~' {
				op = "!~"
			}
			if op == "!" {
				return nil, Errorf(i, "unexpected !")
			}
			out = append(out, token{tOp, op, i})
			i += len([]rune(op))
		default:
			j := i
			for j < len(rs) && !unicode.IsSpace(rs[j]) && !strings.ContainsRune(special, rs[j]) {
				j++
			}
			out = append(out, token{tWord, string(rs[i:j]), i})
			i = j
		}
	}
	return append(out, token{tEOF, "", len(rs)}), nil
}

// --- parser ------------------------------------------------------------------

type parser struct {
	toks  []token
	at    int
	terms int
}

// Parse reads a query. A query is at most QueryBytes long and has at most
// QueryTerms conditions (ADR-0015).
//
//	query  = [ or ] [ ORDER BY sort { "," sort } ]
//	or     = and { OR and }
//	and    = not { AND not }
//	not    = NOT not | "(" or ")" | clause
//	clause = field ( op value | [ NOT ] IN "(" value { "," value } ")" | IS [ NOT ] ( EMPTY | NULL ) )
//	op     = "=" | "!=" | "<" | "<=" | ">" | ">=" | "~" | "!~"
//	value  = word | text | function "(" [ value ] ")"
//	sort   = field [ ASC | DESC ]
func Parse(text string) (*Query, error) {
	limits := budget.Get()
	if len(text) > limits.QueryBytes {
		return nil, Errorf(0, "a query is at most %d bytes", limits.QueryBytes)
	}
	toks, err := lex(text)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	q := &Query{}
	if !p.isKeyword("order") && p.peek().kind != tEOF {
		if q.Where, err = p.or(); err != nil {
			return nil, err
		}
	}
	if p.isKeyword("order") {
		p.next()
		if !p.isKeyword("by") {
			return nil, Errorf(p.peek().pos, "expected BY after ORDER")
		}
		p.next()
		for {
			f, err := p.field()
			if err != nil {
				return nil, err
			}
			s := Sort{Field: f}
			if p.isKeyword("asc") {
				p.next()
			} else if p.isKeyword("desc") {
				p.next()
				s.Desc = true
			}
			q.Order = append(q.Order, s)
			if p.peek().kind != tComma {
				if p.peek().kind != tEOF {
					return nil, Errorf(p.peek().pos, "expected ASC, DESC or , after %s", f.Text)
				}
				break
			}
			p.next()
		}
	}
	if t := p.peek(); t.kind != tEOF {
		return nil, Errorf(t.pos, "expected AND, OR or ORDER BY")
	}
	if p.terms > limits.QueryTerms {
		return nil, Errorf(0, "a query has at most %d conditions, each value of a list counted as one", limits.QueryTerms)
	}
	return q, nil
}

func (p *parser) peek() token { return p.toks[p.at] }

func (p *parser) next() token {
	t := p.toks[p.at]
	if t.kind != tEOF {
		p.at++
	}
	return t
}

func (p *parser) isKeyword(word string) bool {
	t := p.peek()
	return t.kind == tWord && strings.EqualFold(t.text, word)
}

func (p *parser) or() (Expr, error) {
	left, err := p.and()
	for err == nil && p.isKeyword("or") {
		p.next()
		var right Expr
		if right, err = p.and(); err == nil {
			left = &Or{left, right}
		}
	}
	return left, err
}

func (p *parser) and() (Expr, error) {
	left, err := p.not()
	for err == nil && p.isKeyword("and") {
		p.next()
		var right Expr
		if right, err = p.not(); err == nil {
			left = &And{left, right}
		}
	}
	return left, err
}

func (p *parser) not() (Expr, error) {
	// A run of NOT is read in a loop, so a long one cannot nest deeply.
	n := 0
	for p.isKeyword("not") {
		p.next()
		n++
	}
	var e Expr
	var err error
	if open := p.peek(); open.kind == tOpen {
		p.next()
		if e, err = p.or(); err != nil {
			return nil, err
		}
		if p.peek().kind != tClose {
			return nil, Errorf(p.peek().pos, "expected ) to close the ( at character %d", open.pos+1)
		}
		p.next()
	} else if e, err = p.clause(); err != nil {
		return nil, err
	}
	for range n {
		e = &Not{e}
	}
	return e, nil
}

func (p *parser) field() (Name, error) {
	t := p.peek()
	switch {
	case t.kind == tString:
		p.next()
		return Name{Text: t.text, Quoted: true, Pos: t.pos}, nil
	case t.kind == tWord && !keywords[strings.ToLower(t.text)]:
		p.next()
		return Name{Text: t.text, Pos: t.pos}, nil
	}
	return Name{}, Errorf(t.pos, "expected a field name")
}

func (p *parser) clause() (Expr, error) {
	f, err := p.field()
	if err != nil {
		return nil, err
	}
	t := p.peek()
	c := &Clause{Field: f, OpPos: t.pos}
	switch {
	case t.kind == tOp:
		p.next()
		c.Op = Op(t.text)
		v, err := p.value("expected a value after " + t.text)
		if err != nil {
			return nil, err
		}
		c.Values = []Value{v}
		p.terms++
	case p.isKeyword("not") || p.isKeyword("in"):
		c.Op = In
		if p.isKeyword("not") {
			p.next()
			if !p.isKeyword("in") {
				return nil, Errorf(p.peek().pos, "expected IN after NOT")
			}
			c.Op = NotIn
		}
		p.next()
		if p.peek().kind != tOpen {
			return nil, Errorf(p.peek().pos, "expected ( after IN")
		}
		p.next()
		for {
			v, err := p.value("expected a value")
			if err != nil {
				return nil, err
			}
			c.Values = append(c.Values, v)
			p.terms++
			if t := p.next(); t.kind == tClose {
				break
			} else if t.kind != tComma {
				return nil, Errorf(t.pos, "expected , or ) in the list")
			}
		}
	case p.isKeyword("is"):
		p.next()
		c.Op = Empty
		if p.isKeyword("not") {
			p.next()
			c.Op = NotEmpty
		}
		if !p.isKeyword("empty") && !p.isKeyword("null") {
			return nil, Errorf(p.peek().pos, "expected EMPTY after IS")
		}
		p.next()
		p.terms++
	default:
		return nil, Errorf(t.pos, "expected an operator after %s", f.Text)
	}
	return c, nil
}

func (p *parser) value(missing string) (Value, error) {
	t := p.peek()
	switch {
	case t.kind == tString:
		p.next()
		return Value{Text: t.text, Quoted: true, Pos: t.pos}, nil
	case t.kind != tWord || keywords[strings.ToLower(t.text)]:
		return Value{}, Errorf(t.pos, "%s", missing)
	}
	p.next()
	if p.peek().kind != tOpen {
		return Value{Text: t.text, Pos: t.pos}, nil
	}
	fn, ok := functions[strings.ToLower(t.text)]
	if !ok {
		return Value{}, Errorf(t.pos, "unknown function %s(); the functions are %s", t.text, strings.Join(functionNames, ", "))
	}
	p.next()
	v := Value{Text: fn.name, Func: true, Pos: t.pos}
	for p.peek().kind != tClose {
		arg, err := p.value("expected an argument or )")
		if err != nil {
			return Value{}, err
		}
		if fn.args == 0 {
			return Value{}, Errorf(t.pos, "%s() takes no argument", fn.name)
		}
		if len(v.Args) == fn.args {
			return Value{}, Errorf(t.pos, "%s() takes at most one argument", fn.name)
		}
		if arg.Func || (!wholeNumber.MatchString(arg.Text) && !offset.MatchString(arg.Text)) {
			return Value{}, Errorf(arg.Pos, "the argument of %s() is a whole number or an offset such as -1d", fn.name)
		}
		v.Args = append(v.Args, arg)
		if p.peek().kind == tComma {
			p.next()
		} else if p.peek().kind != tClose {
			return Value{}, Errorf(p.peek().pos, "expected , or ) after the argument")
		}
	}
	p.next()
	return v, nil
}

// --- values ------------------------------------------------------------------

var (
	wholeNumber = regexp.MustCompile(`^[+-]?[0-9]{1,6}$`)
	// offset is a span from now: hours, days, weeks or years.
	offset = regexp.MustCompile(`^[+-]?[0-9]{1,6}[hdwy]$`)
)

// IsCurrentUser reports whether the value is currentUser(), the caller.
func (v Value) IsCurrentUser() bool { return v.Func && v.Text == "currentUser" }

// Time reads the value as a point in time at now: a date (2026-10-01), a
// date and time (RFC 3339), an offset from now (-7d, 2w, +12h, -1y), or a
// time function. day reports a date, which names a whole day: compared
// with it, a time counts by its day. Days and weeks are those of UTC, and a
// week starts on Monday.
func (v Value) Time(now time.Time) (t time.Time, day bool, ok bool) {
	now = now.UTC()
	if !v.Func {
		if d, err := time.Parse(time.DateOnly, v.Text); err == nil {
			return d, true, true
		}
		if d, err := time.Parse(time.RFC3339Nano, v.Text); err == nil {
			return d.UTC(), false, true
		}
		if offset.MatchString(v.Text) {
			return shift(now, v.Text), false, true
		}
		return time.Time{}, false, false
	}
	if v.Text == "now" {
		return now, false, true
	}
	n, add := 0, ""
	if len(v.Args) == 1 {
		if wholeNumber.MatchString(v.Args[0].Text) {
			n, _ = strconv.Atoi(v.Args[0].Text)
		} else {
			add = v.Args[0].Text
		}
	}
	y, m, d := now.Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	var start time.Time
	var span func(time.Time) time.Time
	switch strings.TrimPrefix(strings.TrimPrefix(v.Text, "start"), "end") {
	case "OfDay":
		start, span = today.AddDate(0, 0, n), func(t time.Time) time.Time { return t.AddDate(0, 0, 1) }
	case "OfWeek":
		monday := today.AddDate(0, 0, -(int(today.Weekday())+6)%7)
		start, span = monday.AddDate(0, 0, 7*n), func(t time.Time) time.Time { return t.AddDate(0, 0, 7) }
	case "OfMonth":
		start, span = time.Date(y, m+time.Month(n), 1, 0, 0, 0, 0, time.UTC), func(t time.Time) time.Time { return t.AddDate(0, 1, 0) }
	case "OfYear":
		start, span = time.Date(y+n, 1, 1, 0, 0, 0, 0, time.UTC), func(t time.Time) time.Time { return t.AddDate(1, 0, 0) }
	default:
		return time.Time{}, false, false
	}
	if strings.HasPrefix(v.Text, "end") {
		start = span(start).Add(-time.Nanosecond)
	}
	if add != "" {
		start = shift(start, add)
	}
	return start, false, true
}

// shift adds an offset such as -7d to t.
func shift(t time.Time, off string) time.Time {
	n, _ := strconv.Atoi(strings.TrimPrefix(off[:len(off)-1], "+"))
	switch off[len(off)-1] {
	case 'h':
		return t.Add(time.Duration(n) * time.Hour)
	case 'd':
		return t.AddDate(0, 0, n)
	case 'w':
		return t.AddDate(0, 0, 7*n)
	}
	return t.AddDate(n, 0, 0)
}
