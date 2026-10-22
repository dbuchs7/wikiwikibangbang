package item

import (
	"slices"
	"strings"
	"unicode/utf8"
)

// Protected passages (ADR-0088): words or whole paragraphs of a body that
// only some of those who read the item read in clear. A passage is a
// MarkSecret over its text; the mark's Ref names a Secret of the item,
// which says who protected it and who reads it. Everyone else reads a
// placeholder of fixed length, so not even the length shows, and may ask
// to read it; the one who protected it and the space's administrators
// decide. Protection only narrows who reads the item (D9): a passage's
// readers who may not read the item still read nothing of it.

// Secret is the protection of a passage.
type Secret struct {
	ID ID `json:"id"`
	// By protected the passage and reads it always.
	By string `json:"by"`
	// Readers read it besides By: user IDs and @groups.
	Readers []string `json:"readers,omitempty"`
	// Requests are those who asked to read it and wait for an answer, in
	// the order they asked. They follow from the events, so they are not
	// part of one.
	Requests []string `json:"-"`
}

func (sec Secret) clone() Secret {
	sec.Readers = slices.Clone(sec.Readers)
	sec.Requests = slices.Clone(sec.Requests)
	return sec
}

// SecretKey names the key a passage's text is sealed with in the log, its
// own beside the item's (ADR-0088): destroying it erases the passage from
// every version, and the item's purge destroys it with the item's.
func SecretKey(item, secret ID) ID { return item + "~" + secret }

// SecretKeyItem returns the item a passage's key belongs to, and whether
// the ID names a passage's key at all.
func SecretKeyItem(key ID) (ID, bool) {
	item, _, ok := strings.Cut(string(key), "~")
	return ID(item), ok
}

// SecretPlaceholder is what one who may not read a passage reads instead:
// always the same, whatever the passage's length.
const SecretPlaceholder = "▒▒▒▒▒▒▒▒"

// maxSecretReaders bounds who a passage names as its readers.
const maxSecretReaders = 50

func (it *Item) secret(id ID) (int, bool) {
	i := slices.IndexFunc(it.Secrets, func(sec Secret) bool { return sec.ID == id })
	return i, i >= 0
}

// readsSecret reports whether the principal reads a passage in clear.
func (s *Store) readsSecret(principal string, sec Secret) bool {
	return principal == sec.By || slices.ContainsFunc(sec.Readers, func(r string) bool { return s.matches(principal, r) })
}

// decidesSecret reports whether the principal decides who reads a passage:
// the one who protected it and the space's administrators.
func (s *Store) decidesSecret(principal string, it *Item, sec Secret) bool {
	return principal == sec.By || s.spaceRole(principal, it.Space) >= SpaceAdmin
}

// readsAllSecrets reports whether the principal reads every passage of an
// item; only then may they change its body, so nothing they cannot read
// is lost or overwritten.
func (s *Store) readsAllSecrets(principal string, it *Item) bool {
	for _, sec := range it.Secrets {
		if !s.readsSecret(principal, sec) {
			return false
		}
	}
	return true
}

// hidden returns the passages of an item the principal may not read.
func (s *Store) hidden(principal string, it *Item) map[ID]bool {
	var out map[ID]bool
	for _, sec := range it.Secrets {
		if !s.readsSecret(principal, sec) {
			if out == nil {
				out = map[ID]bool{}
			}
			out[sec.ID] = true
		}
	}
	return out
}

// veiled returns the item as the principal reads it: every passage they
// may not read replaced by the placeholder. Without such passages it is the
// item itself.
func (s *Store) veiled(principal string, it *Item) *Item {
	hide := s.hidden(principal, it)
	if hide == nil {
		return it
	}
	c := it.clone()
	c.Body = veil(c.Body, hide)
	return &c
}

// veil replaces the text of the passages in hide by the placeholder. Other
// marks keep what lies outside them; mentions and links never lie inside
// one (checkSecrets).
func veil(d Doc, hide map[ID]bool) Doc {
	for i, b := range d.Blocks {
		var cut []Mark
		for _, m := range b.Marks {
			if m.Type == MarkSecret && hide[m.Ref] {
				cut = append(cut, m)
			}
		}
		if len(cut) == 0 {
			continue
		}
		slices.SortFunc(cut, func(a, b Mark) int { return a.Start - b.Start })
		runes := []rune(b.Text)
		ph := utf8.RuneCountInString(SecretPlaceholder)
		// at maps an offset of the old text to the new one.
		at := func(p int) int {
			shift := 0
			for _, m := range cut {
				switch {
				case p >= m.End:
					shift += ph - (m.End - m.Start)
				case p > m.Start:
					return m.Start + shift + ph
				}
			}
			return p + shift
		}
		var text strings.Builder
		last := 0
		for _, m := range cut {
			text.WriteString(string(runes[last:m.Start]))
			text.WriteString(SecretPlaceholder)
			last = m.End
		}
		text.WriteString(string(runes[last:]))
		var marks []Mark
		for _, m := range b.Marks {
			if m.Type == MarkSecret && hide[m.Ref] {
				m.Start = at(m.Start)
				m.End = m.Start + ph
				marks = append(marks, m)
				continue
			}
			start, end := at(m.Start), at(m.End)
			// A mark that lay wholly inside a hidden passage goes.
			if end > start && !inside(cut, m) {
				m.Start, m.End = start, end
				marks = append(marks, m)
			}
		}
		b.Text = text.String()
		b.Marks = marks
		d.Blocks[i] = b
	}
	return d
}

func inside(cut []Mark, m Mark) bool {
	return slices.ContainsFunc(cut, func(c Mark) bool { return m.Start >= c.Start && m.End <= c.End })
}

// secretTexts returns the clear text of each passage of a body, the parts
// of one passage joined in the order they appear.
func secretTexts(d Doc) map[ID]string {
	out := map[ID]string{}
	for _, b := range d.Blocks {
		runes := []rune(b.Text)
		for _, m := range b.Marks {
			if m.Type == MarkSecret && m.End <= len(runes) {
				out[m.Ref] += string(runes[m.Start:m.End])
			}
		}
	}
	return out
}

// checkSecrets checks the passages of a body against the item: each names
// a passage the item has, passages do not overlap, and no mention or link
// lies in one, as it would show where the passage points.
func checkSecrets(it *Item, d Doc) error {
	for i, b := range d.Blocks {
		var secrets, others []Mark
		for _, m := range b.Marks {
			switch m.Type {
			case MarkSecret:
				if _, ok := it.secret(m.Ref); !ok {
					return wrapf(invalid("the protected passage %s is not one of this item's", m.Ref), "block %d", i+1)
				}
				secrets = append(secrets, m)
			case MarkLink, MarkMention:
				others = append(others, m)
			}
		}
		for j, a := range secrets {
			for _, o := range append(secrets[j+1:], others...) {
				if a.Start < o.End && o.Start < a.End {
					return wrapf(invalid("a protected passage holds no link, no mention and no other protected passage"), "block %d", i+1)
				}
			}
		}
	}
	return nil
}

// checkReaders checks who a passage names as its readers.
func checkReaders(readers []string) ([]string, error) {
	var out []string
	for _, r := range readers {
		r = strings.TrimSpace(r)
		if r == "" || r == "@" || strings.ContainsAny(r, " \t\n") {
			return nil, invalid("%q is not a user or @group", r)
		}
		if !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	if len(out) > maxSecretReaders {
		return nil, invalid("a protected passage names at most %d readers", maxSecretReaders)
	}
	return out, nil
}

// applySecret keeps an item's passages; it runs live and on replay.
func (s *Store) applySecret(e Event) {
	it := s.items[e.Item]
	if it == nil || e.Secret == nil {
		return
	}
	i, ok := it.secret(e.Secret.ID)
	switch e.Kind {
	case SecretSet:
		sec := e.Secret.clone()
		if ok {
			// The answer to a request is in who reads it now.
			sec.Requests = slices.DeleteFunc(slices.Clone(it.Secrets[i].Requests), func(p string) bool { return s.readsSecret(p, sec) })
			it.Secrets[i] = sec
		} else {
			sec.Requests = nil
			it.Secrets = append(it.Secrets, sec)
		}
	case SecretRemoved:
		if ok {
			it.Secrets = slices.Delete(slices.Clone(it.Secrets), i, i+1)
		}
	case SecretRequested:
		if ok && !slices.Contains(it.Secrets[i].Requests, e.Principal) {
			it.Secrets[i].Requests = append(slices.Clone(it.Secrets[i].Requests), e.Principal)
		}
	case SecretGranted:
		if ok {
			sec := it.Secrets[i].clone()
			if !slices.Contains(sec.Readers, e.Principal) {
				sec.Readers = append(sec.Readers, e.Principal)
			}
			sec.Requests = slices.DeleteFunc(sec.Requests, func(p string) bool { return p == e.Principal })
			it.Secrets[i] = sec
		}
	case SecretDeclined:
		if ok {
			sec := it.Secrets[i].clone()
			sec.Requests = slices.DeleteFunc(sec.Requests, func(p string) bool { return p == e.Principal })
			it.Secrets[i] = sec
		}
	}
}

// --- sessions ---------------------------------------------------------------

// SecretView is a passage as one reader sees it.
type SecretView struct {
	ID ID
	By string
	// Readable: the reader reads it in clear. Decides: the reader decides
	// who reads it. Requested: the reader asked to and waits.
	Readable, Decides, Requested bool
	// Readers and Requests are told to those who read or decide it.
	Readers  []string
	Requests []string
}

// Secrets lists the passages of a readable item as the principal sees
// them.
func (x *Session) Secrets(id ID) ([]SecretView, error) {
	it, err := x.readable(id)
	if err != nil {
		return nil, err
	}
	out := []SecretView{}
	for _, sec := range it.Secrets {
		v := SecretView{ID: sec.ID, By: sec.By, Readable: x.s.readsSecret(x.who, sec), Decides: x.s.decidesSecret(x.who, it, sec),
			Requested: slices.Contains(sec.Requests, x.who)}
		if v.Readable || v.Decides {
			v.Readers = slices.Clone(sec.Readers)
		}
		if v.Decides {
			v.Requests = slices.Clone(sec.Requests)
		}
		out = append(out, v)
	}
	return out, nil
}

// ReadsAllSecrets reports whether the principal reads every passage of a
// readable item, and so may change its body.
func (x *Session) ReadsAllSecrets(id ID) bool {
	it, err := x.readable(id)
	return err == nil && x.s.readsAllSecrets(x.who, it)
}

// Protect protects a passage of a block of an item's body: start and end
// count characters of the block's text; readers read it besides the one
// who protects it. Who may edit the item and reads all of its passages
// may.
func (x *Session) Protect(id ID, block, start, end int, readers []string) (Secret, error) {
	it, err := x.editableBody(id)
	if err != nil {
		return Secret{}, err
	}
	if block < 0 || block >= len(it.Body.Blocks) {
		return Secret{}, invalid("the body has no block %d", block+1)
	}
	readers, err = checkReaders(readers)
	if err != nil {
		return Secret{}, err
	}
	sec := Secret{ID: x.s.newID(), By: x.who, Readers: readers}
	body := it.Body.clone()
	b := body.Blocks[block]
	b.Marks = append(b.Marks, Mark{Type: MarkSecret, Start: start, End: end, Ref: sec.ID})
	body.Blocks[block] = b
	if err := checkBlock(b, id); err != nil {
		return Secret{}, wrapf(err, "block %d", block+1)
	}
	trial := it.clone()
	trial.Secrets = append(trial.Secrets, sec)
	if err := checkSecrets(&trial, body); err != nil {
		return Secret{}, err
	}
	return sec, x.s.recordBatch(x.who, Event{Kind: SecretSet, Item: id, Secret: &sec}, Event{Kind: BodyCommitted, Item: id, Body: &body})
}

// editableBody returns an item whose body the principal may change: they
// may edit it and read all of its passages.
func (x *Session) editableBody(id ID) (*Item, error) {
	it, err := x.editable(id)
	if err != nil {
		return nil, err
	}
	if !x.s.readsAllSecrets(x.who, it) {
		return nil, forbidden("%s holds protected passages that %s may not read; its text is changed by those who read them all", it.Key, x.who)
	}
	return it, nil
}

// decidable returns an item and one of its passages that the principal
// decides about.
func (x *Session) decidable(id, secret ID) (*Item, Secret, error) {
	it, err := x.readable(id)
	if err != nil {
		return nil, Secret{}, err
	}
	i, ok := it.secret(secret)
	if !ok {
		return nil, Secret{}, notFound("protected passage %s of %s", secret, it.Key)
	}
	sec := it.Secrets[i]
	if !x.s.decidesSecret(x.who, it, sec) {
		return nil, Secret{}, forbidden("the one who protected the passage and the space's administrators decide who reads it")
	}
	return it, sec, nil
}

// CopySecrets gives a copy the protected passages of its source, with the
// same readers, so the copied body keeps them (ADR-0067, ADR-0088). Who
// copies must read them all: a copy never holds a placeholder for text.
func (x *Session) CopySecrets(from, to ID) error {
	src, err := x.readable(from)
	if err != nil {
		return err
	}
	if !x.s.readsAllSecrets(x.who, src) {
		return forbidden("%s holds protected passages that %s may not read; it is copied by those who read them all", src.Key, x.who)
	}
	if _, err := x.editable(to); err != nil {
		return err
	}
	var events []Event
	for _, sec := range src.Secrets {
		c := Secret{ID: sec.ID, By: sec.By, Readers: slices.Clone(sec.Readers)}
		events = append(events, Event{Kind: SecretSet, Item: to, Secret: &c})
	}
	if len(events) == 0 {
		return nil
	}
	return x.s.recordBatch(x.who, events...)
}

// SetSecretReaders changes who reads a passage besides the one who
// protected it.
func (x *Session) SetSecretReaders(id, secret ID, readers []string) error {
	_, sec, err := x.decidable(id, secret)
	if err != nil {
		return err
	}
	readers, err = checkReaders(readers)
	if err != nil {
		return err
	}
	if slices.Equal(readers, sec.Readers) {
		return nil
	}
	set := Secret{ID: sec.ID, By: sec.By, Readers: readers}
	return x.s.record(x.who, Event{Kind: SecretSet, Item: id, Secret: &set})
}

// Unprotect takes a passage's protection off: its text reads as any other.
func (x *Session) Unprotect(id, secret ID) error {
	it, sec, err := x.decidable(id, secret)
	if err != nil {
		return err
	}
	if it.Presence != Live {
		return invalid("%s is %s", it.Key, it.Presence)
	}
	body := it.Body.clone()
	for i, b := range body.Blocks {
		b.Marks = slices.DeleteFunc(b.Marks, func(m Mark) bool { return m.Type == MarkSecret && m.Ref == sec.ID })
		if len(b.Marks) == 0 {
			b.Marks = nil // as a decoded body has it
		}
		body.Blocks[i] = b
	}
	return x.s.recordBatch(x.who, Event{Kind: SecretRemoved, Item: id, Secret: &Secret{ID: sec.ID}}, Event{Kind: BodyCommitted, Item: id, Body: &body})
}

// RequestSecret asks to read a passage one may not read yet.
func (x *Session) RequestSecret(id, secret ID) error {
	it, err := x.readable(id)
	if err != nil {
		return err
	}
	i, ok := it.secret(secret)
	if !ok {
		return notFound("protected passage %s of %s", secret, it.Key)
	}
	sec := it.Secrets[i]
	switch {
	case x.s.readsSecret(x.who, sec):
		return invalid("you read this passage already")
	case slices.Contains(sec.Requests, x.who):
		return nil
	}
	return x.s.record(x.who, Event{Kind: SecretRequested, Item: id, Secret: &Secret{ID: sec.ID}, Principal: x.who})
}

// DecideSecret answers a request to read a passage: grant lets the one who
// asked read it, otherwise the request is declined.
func (x *Session) DecideSecret(id, secret ID, principal string, grant bool) error {
	_, sec, err := x.decidable(id, secret)
	if err != nil {
		return err
	}
	if !slices.Contains(sec.Requests, principal) {
		return notFound("a request of %s to read the passage", principal)
	}
	kind := SecretDeclined
	if grant {
		kind = SecretGranted
	}
	return x.s.record(x.who, Event{Kind: kind, Item: id, Secret: &Secret{ID: sec.ID}, Principal: principal})
}

// VeilAnchor hides what an inline comment quotes of a passage the
// principal may not read (ADR-0047, ADR-0088): the quote, and the text
// before and after it, become the placeholder where they share a part of
// such a passage.
func (x *Session) veilAnchor(it *Item, a *Anchor) *Anchor {
	if a == nil {
		return nil
	}
	hide := x.s.hidden(x.who, it)
	if hide == nil {
		return a
	}
	texts := secretTexts(it.Body)
	out := *a
	for id := range hide {
		for _, f := range []*string{&out.Quote, &out.Prefix, &out.Suffix} {
			if shares(*f, texts[id]) {
				*f = SecretPlaceholder
			}
		}
	}
	return &out
}

// shares reports whether two texts share a part: one holds the other, or
// they have four characters in a row in common. It errs towards sharing:
// veiling a quote too many is safe, one too few is not.
func shares(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if strings.Contains(a, b) || strings.Contains(b, a) {
		return true
	}
	const k = 4
	ra := []rune(a)
	for i := 0; i+k <= len(ra); i++ {
		if strings.Contains(b, string(ra[i:i+k])) {
			return true
		}
	}
	return false
}
