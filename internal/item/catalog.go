package item

import "time"

// duplicate refuses a value of a unique field that another live item of
// the type in the space already holds (ADR-0074). self is the item being
// changed, or empty for a new one.
func (s *Store) duplicate(space string, td TypeDef, self ID, props Props) error {
	for _, f := range td.Fields {
		v := props[f.Name]
		if !f.Unique || v == nil {
			continue
		}
		want := FormatValue(v)
		for _, it := range s.items {
			if it.ID == self || it.Presence != Live || it.Space != space || it.Type != td.Name {
				continue
			}
			if other := it.Properties[f.Name]; other != nil && FormatValue(other) == want {
				return invalid("field %q: %s is already used by %s", f.Name, want, it.Key)
			}
		}
	}
	return nil
}

// duplicatesAmong refuses unique fields of td that live items of the
// type already hold twice in one space, before the type is redefined so.
func (s *Store) duplicatesAmong(td TypeDef) error {
	for _, f := range td.Fields {
		if !f.Unique {
			continue
		}
		seen := map[string]string{}
		for _, it := range s.items {
			v := it.Properties[f.Name]
			if it.Presence != Live || it.Type != td.Name || v == nil {
				continue
			}
			k := it.Space + "\x00" + FormatValue(v)
			if key, ok := seen[k]; ok {
				return invalid("field %q cannot be unique: %s and %s both hold %s", f.Name, key, it.Key, FormatValue(v))
			}
			seen[k] = it.Key
		}
	}
	return nil
}

// duplicatesMoving refuses moving an item with its subtree to another
// space where items hold the values of its unique fields.
func (s *Store) duplicatesMoving(id ID, toSpace string) error {
	for _, m := range s.subtree(id) {
		it := s.items[m]
		if it.Presence != Live || it.Space == toSpace {
			continue
		}
		td, _ := s.latest(it.Type)
		if err := s.duplicate(toSpace, td, m, it.Properties); err != nil {
			return err
		}
	}
	return nil
}

// ReviewDue is an item of a type with a review rule that is due to be
// checked again (ADR-0074).
type ReviewDue struct {
	Item  ID
	Key   string
	Title string
	// LastReview is the date in the review field, zero when it was never
	// reviewed; Due is when the review fell due.
	LastReview  time.Time
	Due         time.Time
	Responsible string
}

// reviewDue reports whether an item of a type with a review rule is due.
func reviewDue(td TypeDef, it *Item, now time.Time) (ReviewDue, bool) {
	r := td.Review
	if r == nil {
		return ReviewDue{}, false
	}
	rd := ReviewDue{Item: it.ID, Key: it.Key, Title: it.Title}
	last, ok := it.Properties[r.Field].(time.Time)
	if !ok {
		return rd, true
	}
	rd.LastReview, rd.Due = last, last.AddDate(0, 0, r.EveryDays)
	return rd, !rd.Due.After(now)
}

// owner is who the type's review rule makes responsible for an item, or
// empty.
func owner(td TypeDef, it *Item) string {
	if td.Review == nil || td.Review.Owner == "" {
		return ""
	}
	who, _ := it.Properties[td.Review.Owner].(string)
	return who
}

// MarkReviewed records that the item was checked today: its type's
// review field is set to the date of now (ADR-0074).
func (x *Session) MarkReviewed(id ID, now time.Time) error {
	it, err := x.s.live(id)
	if err != nil {
		return err
	}
	td, _ := x.s.latest(it.Type)
	if td.Review == nil {
		return invalid("type %q has no review rule", td.Name)
	}
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	return x.SetProperty(id, td.Review.Field, day)
}

// whereMatches reports whether an item's properties match every
// condition, each compared as text (FormatValue).
func whereMatches(it *Item, where map[string]string) bool {
	for k, v := range where {
		if got := it.Properties[k]; got == nil || FormatValue(got) != v {
			return false
		}
	}
	return true
}
