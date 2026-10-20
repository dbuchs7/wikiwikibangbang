// Package site keeps the settings of the whole installation that are no
// item, account or audit entry: its corporate design (ADR-0052), the logo,
// colours, background, fonts and an own stylesheet that make the web UI
// look like the organisation that runs it.
//
// Like the accounts (ADR-0027), the settings are events in a journal of
// their own, applied in order; the latest setting of each kind wins. They
// change rarely, so the journal stays small.
package site

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// Journal makes one record durable before it is applied.
type Journal func(record []byte) error

// ErrInvalid marks a setting that is refused.
var ErrInvalid = errors.New("invalid")

// Assets are the files the design may carry, by name.
const (
	AssetLogo       = "logo"
	AssetBackground = "background"
	AssetFont       = "font"
)

// Assets lists the asset names.
var Assets = []string{AssetLogo, AssetBackground, AssetFont}

// Fonts are the font choices besides an uploaded font: stacks every
// system has, so nothing is fetched from elsewhere.
var Fonts = map[string]string{
	"system":   `system-ui, -apple-system, "Segoe UI", Roboto, sans-serif`,
	"humanist": `"Segoe UI", "Helvetica Neue", Helvetica, Arial, sans-serif`,
	"serif":    `Georgia, Cambria, "Times New Roman", Times, serif`,
	"rounded":  `ui-rounded, "SF Pro Rounded", "Nunito", system-ui, sans-serif`,
	"mono":     `ui-monospace, SFMono-Regular, Menlo, Consolas, monospace`,
}

// Design is the corporate design. Empty fields keep the product's own
// look.
type Design struct {
	// Name is the organisation's or the installation's name, shown with
	// the logo and in the browser's title.
	Name string `json:"name"`
	// Colours, each #rrggbb or empty.
	Accent           string `json:"accent"`
	Background       string `json:"background"`
	HeaderBackground string `json:"headerBackground"`
	HeaderText       string `json:"headerText"`
	// Font is a key of Fonts, "upload" for the uploaded font, or empty.
	Font string `json:"font"`
	// FontFamily names a font installed on the readers' computers, tried
	// before Font, e.g. "Frutiger".
	FontFamily string `json:"fontFamily"`
	// CSS is an own stylesheet, applied after everything else.
	CSS string `json:"css"`
}

// Asset is one file of the design.
type Asset struct {
	Type string
	Data []byte
	// Tag names the content, for caching.
	Tag string
}

var (
	colour = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	family = regexp.MustCompile(`^[\p{L}\p{N} ._-]{1,60}$`)
)

// Validate checks a design.
func (d Design) Validate() error {
	for name, c := range map[string]string{"accent": d.Accent, "background": d.Background, "headerBackground": d.HeaderBackground, "headerText": d.HeaderText} {
		if c != "" && !colour.MatchString(c) {
			return fmt.Errorf("%w: %s must be a colour like #1a2b3c", ErrInvalid, name)
		}
	}
	if _, ok := Fonts[d.Font]; d.Font != "" && d.Font != "upload" && !ok {
		return fmt.Errorf("%w: font %q", ErrInvalid, d.Font)
	}
	if d.FontFamily != "" && !family.MatchString(d.FontFamily) {
		return fmt.Errorf("%w: a font name has letters, digits, spaces, dots and dashes only", ErrInvalid)
	}
	if strings.ContainsAny(d.Name, "<>\n") || len([]rune(d.Name)) > 80 {
		return fmt.Errorf("%w: the name is a single line of at most 80 characters", ErrInvalid)
	}
	return nil
}

type event struct {
	V      int       `json:"v"`
	Kind   string    `json:"kind"`
	At     time.Time `json:"at"`
	Actor  string    `json:"actor,omitempty"`
	Design *Design   `json:"design,omitempty"`
	Asset  string    `json:"asset,omitempty"`
	Type   string    `json:"type,omitempty"`
	Data   []byte    `json:"data,omitempty"`
}

const (
	designSet   = "DesignSet"
	assetSet    = "AssetSet"
	assetRemove = "AssetRemoved"
)

// Settings is safe for concurrent use.
type Settings struct {
	mu      sync.Mutex
	journal Journal
	now     func() time.Time
	design  Design
	assets  map[string]Asset
}

// New rebuilds the settings from records and journals later changes.
func New(records [][]byte, j Journal) (*Settings, error) {
	s := &Settings{journal: j, now: time.Now, assets: map[string]Asset{}}
	for i, r := range records {
		var e event
		if err := json.Unmarshal(r, &e); err != nil {
			return nil, fmt.Errorf("site: record %d: %w", i+1, err)
		}
		if err := s.apply(e); err != nil {
			return nil, fmt.Errorf("site: record %d: %w", i+1, err)
		}
	}
	return s, nil
}

func (s *Settings) apply(e event) error {
	if e.V != 1 {
		return fmt.Errorf("format version %d is not supported", e.V)
	}
	switch e.Kind {
	case designSet:
		if e.Design == nil {
			return errors.New("a design event without a design")
		}
		s.design = *e.Design
	case assetSet:
		s.assets[e.Asset] = Asset{Type: e.Type, Data: e.Data, Tag: tag(e.Data)}
	case assetRemove:
		delete(s.assets, e.Asset)
	default:
		return fmt.Errorf("unknown event %q", e.Kind)
	}
	return nil
}

func (s *Settings) record(e event) error {
	e.V, e.At = 1, s.now().UTC()
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if err := s.journal(data); err != nil {
		return err
	}
	return s.apply(e)
}

// Design returns the corporate design.
func (s *Settings) Design() Design {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.design
}

// SetDesign replaces the corporate design.
func (s *Settings) SetDesign(actor string, d Design) error {
	if err := d.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.record(event{Kind: designSet, Actor: actor, Design: &d})
}

// Asset returns an asset of the design, if it is set.
func (s *Settings) Asset(name string) (Asset, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.assets[name]
	return a, ok
}

// SetAsset stores an asset of the design; checking its content is the
// caller's concern.
func (s *Settings) SetAsset(actor, name, mediaType string, data []byte) error {
	if !slices.Contains(Assets, name) {
		return fmt.Errorf("%w: asset %q", ErrInvalid, name)
	}
	if len(data) == 0 {
		return fmt.Errorf("%w: the file is empty", ErrInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.record(event{Kind: assetSet, Actor: actor, Asset: name, Type: mediaType, Data: slices.Clone(data)})
}

// RemoveAsset removes an asset of the design.
func (s *Settings) RemoveAsset(actor, name string) error {
	if !slices.Contains(Assets, name) {
		return fmt.Errorf("%w: asset %q", ErrInvalid, name)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.assets[name]; !ok {
		return nil
	}
	return s.record(event{Kind: assetRemove, Actor: actor, Asset: name})
}

func tag(data []byte) string {
	// FNV-1a: a change marker for caches, not a security measure.
	h := uint64(14695981039346656037)
	for _, b := range data {
		h ^= uint64(b)
		h *= 1099511628211
	}
	return fmt.Sprintf("%016x", h)
}

// Stylesheet is the CSS that applies the design over the product's own
// stylesheet: custom properties the UI is built on, the header, the font,
// the background, then the own stylesheet. Colours that suit a light page
// apply when the reader's system is light; the accent applies always.
func (s *Settings) Stylesheet() string {
	s.mu.Lock()
	d := s.design
	_, hasBackground := s.assets[AssetBackground]
	font, hasFont := s.assets[AssetFont]
	s.mu.Unlock()
	var b strings.Builder
	b.WriteString("/* The corporate design of this installation (ADR-0052). */\n")
	if hasFont {
		fmt.Fprintf(&b, "@font-face { font-family: \"Corporate\"; src: url(\"/branding/font?v=%s\"); font-display: swap; }\n", font.Tag)
	}
	var stack []string
	if d.FontFamily != "" {
		stack = append(stack, fmt.Sprintf("%q", d.FontFamily))
	}
	if d.Font == "upload" && hasFont {
		stack = append(stack, `"Corporate"`)
	}
	if f, ok := Fonts[d.Font]; ok {
		stack = append(stack, f)
	} else if len(stack) > 0 {
		stack = append(stack, Fonts["system"])
	}
	if d.Accent != "" || len(stack) > 0 {
		b.WriteString(":root {\n")
		if d.Accent != "" {
			fmt.Fprintf(&b, "  --accent: %s;\n  --accent-hover: color-mix(in srgb, %s 82%%, black);\n", d.Accent, d.Accent)
		}
		if len(stack) > 0 {
			fmt.Fprintf(&b, "  --font-sans: %s;\n", strings.Join(stack, ", "))
		}
		b.WriteString("}\n")
		if len(stack) > 0 {
			b.WriteString("body { font-family: var(--font-sans); }\n")
		}
	}
	if d.Accent != "" {
		fmt.Fprintf(&b, "@media (prefers-color-scheme: light) { :root { --accent-soft: color-mix(in srgb, %s 12%%, white); } }\n", d.Accent)
	}
	var light strings.Builder
	if d.Background != "" {
		fmt.Fprintf(&light, "  :root { --bg: %s; }\n  body { background-color: %s; }\n", d.Background, d.Background)
	}
	if d.HeaderBackground != "" {
		fmt.Fprintf(&light, "  .top { background: %s; }\n", d.HeaderBackground)
	}
	if d.HeaderText != "" {
		fmt.Fprintf(&light, "  .top, .top .brand, #nav > a, #nav > button.link, #app-switcher, #nav-notifications { color: %s; }\n", d.HeaderText)
	}
	if light.Len() > 0 {
		b.WriteString("@media (prefers-color-scheme: light) {\n" + light.String() + "}\n")
	}
	if hasBackground {
		bg, _ := s.Asset(AssetBackground)
		fmt.Fprintf(&b, "body { background-image: url(\"/branding/background?v=%s\"); background-size: cover; background-attachment: fixed; background-position: center; }\n", bg.Tag)
	}
	if strings.TrimSpace(d.CSS) != "" {
		b.WriteString("/* The installation's own stylesheet. */\n")
		b.WriteString(d.CSS)
		b.WriteString("\n")
	}
	return b.String()
}
