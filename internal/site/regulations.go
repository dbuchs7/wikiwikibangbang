package site

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// Regulations name categories of personal data in need of special
// protection and the indicators that point to them in content (ADR-0058).
// The law itself is linked or attached for the people who review; the
// software only looks for the indicators, as a hint, never as a judgement.

// Regulation is a law or rule set, such as the Swiss Federal Act on Data
// Protection.
type Regulation struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	URL    string `json:"url"`
	Active bool   `json:"active"`
	// Categories are its categories of data in need of special protection.
	Categories []Category `json:"categories"`
}

// Category is one category of a regulation with its indicators: words
// matched at the start of a word, case-insensitive, or, after "re:", a
// regular expression.
type Category struct {
	Name       string   `json:"name"`
	Indicators []string `json:"indicators"`
}

// Indication is an indicator of a regulation found in content.
type Indication struct {
	Regulation string
	Category   string
	Indicator  string
}

var regulationID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

// Validate checks a regulation.
func (r Regulation) Validate() error {
	if !regulationID.MatchString(r.ID) {
		return fmt.Errorf("%w: an ID has 1 to 40 lower-case letters, digits and dashes", ErrInvalid)
	}
	if strings.TrimSpace(r.Name) == "" || utf8.RuneCountInString(r.Name) > 120 || strings.ContainsAny(r.Name, "\n<>") {
		return fmt.Errorf("%w: the name is a single line of 1 to 120 characters", ErrInvalid)
	}
	if r.URL != "" {
		u, err := url.Parse(r.URL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return fmt.Errorf("%w: the link must be an http or https address", ErrInvalid)
		}
	}
	if len(r.Categories) == 0 || len(r.Categories) > 30 {
		return fmt.Errorf("%w: a regulation has 1 to 30 categories", ErrInvalid)
	}
	for _, c := range r.Categories {
		if strings.TrimSpace(c.Name) == "" || utf8.RuneCountInString(c.Name) > 160 {
			return fmt.Errorf("%w: a category has a name of 1 to 160 characters", ErrInvalid)
		}
		if len(c.Indicators) > 200 {
			return fmt.Errorf("%w: category %q has more than 200 indicators", ErrInvalid, c.Name)
		}
		for _, ind := range c.Indicators {
			if _, err := compileIndicator(ind); err != nil {
				return fmt.Errorf("%w: category %q, indicator %q: %v", ErrInvalid, c.Name, ind, err)
			}
		}
	}
	return nil
}

// compileIndicator turns an indicator into a pattern.
func compileIndicator(ind string) (*regexp.Regexp, error) {
	ind = strings.TrimSpace(ind)
	if ind == "" || utf8.RuneCountInString(ind) > 200 {
		return nil, fmt.Errorf("1 to 200 characters")
	}
	if expr, ok := strings.CutPrefix(ind, "re:"); ok {
		return regexp.Compile(expr)
	}
	return regexp.Compile(`(?i)(?:^|[^\p{L}\p{N}])` + regexp.QuoteMeta(ind))
}

type matcher struct {
	finding Indication
	re      *regexp.Regexp
}

// compileMatchers builds the patterns of the active regulations.
func compileMatchers(regs map[string]Regulation) []matcher {
	var out []matcher
	for _, id := range slices.Sorted(mapsKeys(regs)) {
		r := regs[id]
		if !r.Active {
			continue
		}
		for _, c := range r.Categories {
			for _, ind := range c.Indicators {
				if re, err := compileIndicator(ind); err == nil {
					out = append(out, matcher{Indication{Regulation: r.ID, Category: c.Name, Indicator: strings.TrimSpace(ind)}, re})
				}
			}
		}
	}
	return out
}

func mapsKeys(m map[string]Regulation) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

// Regulations lists the regulations, by ID.
func (s *Settings) Regulations() []Regulation {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Regulation
	for _, id := range slices.Sorted(mapsKeys(s.regulations)) {
		out = append(out, s.regulations[id])
	}
	return out
}

// Regulation returns one regulation.
func (s *Settings) Regulation(id string) (Regulation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.regulations[id]
	return r, ok
}

// SetRegulation adds or replaces a regulation.
func (s *Settings) SetRegulation(actor string, r Regulation) error {
	if err := r.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.record(event{Kind: regulationSet, Actor: actor, Regulation: &r})
}

// RemoveRegulation removes a regulation and its document.
func (s *Settings) RemoveRegulation(actor, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.regulations[id]; !ok {
		return fmt.Errorf("%w: no regulation %q", ErrInvalid, id)
	}
	return s.record(event{Kind: regulationRemoved, Actor: actor, Asset: id})
}

// DocumentAsset is the asset name of a regulation's attached document.
func DocumentAsset(id string) string { return "regulation-" + id }

// Assess finds the indicators of the active regulations in texts, each
// finding once, in the order of the regulations and their categories.
func (s *Settings) Assess(texts []string) []Indication {
	s.mu.Lock()
	ms := s.matchers
	s.mu.Unlock()
	var out []Indication
	for _, m := range ms {
		for _, t := range texts {
			if t != "" && m.re.MatchString(t) {
				out = append(out, m.finding)
				break
			}
		}
	}
	return out
}
