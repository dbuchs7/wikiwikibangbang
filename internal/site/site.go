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
	"math"
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
	// AssetWelcome is the picture of the start page, in place of the
	// product's (ADR-0095).
	AssetWelcome = "welcome"
)

// Assets lists the asset names.
var Assets = []string{AssetLogo, AssetBackground, AssetFont, AssetWelcome}

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
	// Theme is a ready-made theme (ADR-0085), one of Themes, or empty
	// for the product's own look. The fields above apply over it.
	Theme string `json:"theme,omitempty"`
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
	if d.Theme != "" && !slices.Contains(Themes, d.Theme) {
		return fmt.Errorf("%w: %q is not a theme (%s)", ErrInvalid, d.Theme, strings.Join(Themes, ", "))
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
	// Regulation is set by RegulationSet; RegulationRemoved names it in
	// Asset (ADR-0058).
	Regulation *Regulation `json:"regulation,omitempty"`
	// Access is set by AccessSet (ADR-0061).
	Access *Access `json:"access,omitempty"`
	// Backup is set by BackupSet (ADR-0073).
	Backup *Backup `json:"backup,omitempty"`
	// OpenData is set by OpenDataSet (ADR-0075), which a connector
	// replaced (ADR-0077).
	OpenData *openData `json:"openData,omitempty"`
	// Connector is set by ConnectorSet; ConnectorRemoved names it in
	// Asset (ADR-0077).
	Connector *Connector `json:"connector,omitempty"`
	Type      string     `json:"type,omitempty"`
	Data      []byte     `json:"data,omitempty"`
}

const (
	designSet   = "DesignSet"
	assetSet    = "AssetSet"
	assetRemove = "AssetRemoved"

	regulationSet     = "RegulationSet"
	regulationRemoved = "RegulationRemoved"

	accessSet = "AccessSet"

	backupSet = "BackupSet"

	openDataSet = "OpenDataSet"

	connectorSet     = "ConnectorSet"
	connectorRemoved = "ConnectorRemoved"
)

// Settings is safe for concurrent use.
type Settings struct {
	mu      sync.Mutex
	journal Journal
	now     func() time.Time
	design  Design
	assets  map[string]Asset
	// regulations are the data protection regulations by ID, matchers the
	// patterns of the active ones (ADR-0058).
	regulations map[string]Regulation
	matchers    []matcher
	access      Access
	backup      *Backup
	connectors  map[string]Connector
}

// New rebuilds the settings from records and journals later changes.
func New(records [][]byte, j Journal) (*Settings, error) {
	s := &Settings{journal: j, now: time.Now, assets: map[string]Asset{}, regulations: map[string]Regulation{}, connectors: map[string]Connector{}}
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
	case regulationSet:
		if e.Regulation == nil {
			return errors.New("a regulation event without a regulation")
		}
		s.regulations[e.Regulation.ID] = *e.Regulation
		s.matchers = compileMatchers(s.regulations)
	case regulationRemoved:
		delete(s.regulations, e.Asset)
		delete(s.assets, DocumentAsset(e.Asset))
		s.matchers = compileMatchers(s.regulations)
	case accessSet:
		if e.Access == nil {
			return errors.New("an access event without settings")
		}
		s.access = *e.Access
	case backupSet:
		if e.Backup == nil {
			return errors.New("a backup event without settings")
		}
		b := *e.Backup
		s.backup = &b
	case openDataSet:
		if e.OpenData == nil {
			return errors.New("an open data event without settings")
		}
		s.connectors[OpenDataSwiss] = e.OpenData.connector()
	case connectorSet:
		if e.Connector == nil {
			return errors.New("a connector event without a connector")
		}
		s.connectors[e.Connector.ID] = *e.Connector
	case connectorRemoved:
		delete(s.connectors, e.Asset)
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
	if !slices.Contains(Assets, name) && !s.isDocument(name) && !IsSpaceIcon(name) && !IsKanbanIcon(name) {
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
	if !slices.Contains(Assets, name) && !s.isDocument(name) && !IsSpaceIcon(name) && !IsKanbanIcon(name) {
		return fmt.Errorf("%w: asset %q", ErrInvalid, name)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.assets[name]; !ok {
		return nil
	}
	return s.record(event{Kind: assetRemove, Actor: actor, Asset: name})
}

// AccentInk returns the colour of text on a fill of colour c (#rrggbb):
// white or a near black, whichever has the higher contrast ratio (WCAG
// 2.x). The product's own palette sets it per scheme; a corporate accent
// is the same in both, so its text colour follows from the accent alone.
func AccentInk(c string) string {
	l := luminance(c)
	// Contrast with white is 1.05/(l+0.05), with the near black
	// #0b0d12 (luminance about 0.004) (l+0.05)/0.054.
	if 1.05/(l+0.05) >= (l+0.05)/0.054 {
		return "#ffffff"
	}
	return "#0b0d12"
}

// luminance is the relative luminance of #rrggbb (WCAG 2.x).
func luminance(c string) float64 {
	var r, g, b uint8
	if _, err := fmt.Sscanf(c, "#%02x%02x%02x", &r, &g, &b); err != nil {
		return 0
	}
	lin := func(v uint8) float64 {
		x := float64(v) / 255
		if x <= 0.04045 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b)
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
// darkRules finds the rules of a dark scheme in a stylesheet.
var darkRules = regexp.MustCompile(`prefers-color-scheme\s*:\s*dark`)

// DarkMode reports whether the UI offers its dark scheme (ADR-0070): it
// does unless the theme or the installation's own stylesheet replaces the
// look without saying how it looks in the dark, which it says with a
// @media (prefers-color-scheme: dark) rule (ADR-0085).
func (d Design) DarkMode() bool {
	return ThemeDark(d.Theme) && (strings.TrimSpace(d.CSS) == "" || darkRules.MatchString(d.CSS))
}

func (s *Settings) Stylesheet() string {
	s.mu.Lock()
	d := s.design
	_, hasBackground := s.assets[AssetBackground]
	font, hasFont := s.assets[AssetFont]
	s.mu.Unlock()
	var b strings.Builder
	b.WriteString("/* The corporate design of this installation (ADR-0052). */\n")
	dark := d.DarkMode()
	if !dark {
		// The dark scheme is off (ADR-0070): form controls and scroll bars
		// stay light too, and what applies to the light scheme applies
		// always.
		b.WriteString(":root { color-scheme: light; }\n")
	}
	lightOnly := func(rules string) string {
		if !dark {
			return strings.TrimPrefix(strings.ReplaceAll(rules, "\n  ", "\n"), "  ")
		}
		return "@media (prefers-color-scheme: light) {\n" + rules + "}\n"
	}
	if css := ThemeCSS(d.Theme); css != "" {
		fmt.Fprintf(&b, "/* The theme %s (ADR-0085). */\n%s\n", d.Theme, css)
	}
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
			// The text on an accent fill is white or near black, whichever
			// contrasts more, in both schemes; hovering moves the fill away
			// from it, so the contrast only grows.
			ink, away := AccentInk(d.Accent), "black"
			if ink != "#ffffff" {
				away = "white"
			}
			fmt.Fprintf(&b, "  --accent: %s;\n  --accent-hover: color-mix(in srgb, %s 82%%, %s);\n  --accent-ink: %s;\n", d.Accent, d.Accent, away, ink)
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
		b.WriteString(lightOnly(fmt.Sprintf("  :root { --accent-soft: color-mix(in srgb, %s 12%%, white); }\n", d.Accent)))
		// On the dark scheme's cards most corporate colours are too dark to
		// read as text; the scheme gets the accent lightened just enough,
		// as the product's own palette does with its blue.
		if dark := DarkAccent(d.Accent); d.DarkMode() && dark != d.Accent {
			ink, away := AccentInk(dark), "black"
			if ink != "#ffffff" {
				away = "white"
			}
			fmt.Fprintf(&b, "@media (prefers-color-scheme: dark) { :root { --accent: %s; --accent-hover: color-mix(in srgb, %s 82%%, %s); --accent-ink: %s; } }\n", dark, dark, away, ink)
		}
	}
	var light strings.Builder
	if d.Background != "" {
		fmt.Fprintf(&light, "  :root { --bg: %s; }\n  body { background-color: %s; }\n", d.Background, d.Background)
	}
	if d.HeaderBackground != "" {
		fmt.Fprintf(&light, "  .top { background: %s; }\n", d.HeaderBackground)
	}
	if d.HeaderText != "" {
		fmt.Fprintf(&light, "  .top, .top .brand, #nav > a, #nav > button.link, .top .icon-btn, #nav-notifications { color: %s; }\n", d.HeaderText)
	}
	if light.Len() > 0 {
		b.WriteString(lightOnly(light.String()))
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

// isDocument reports whether name is the document asset of a regulation.
func (s *Settings) isDocument(name string) bool {
	id, ok := strings.CutPrefix(name, "regulation-")
	if !ok {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, exists := s.regulations[id]
	return exists
}
