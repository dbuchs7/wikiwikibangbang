package site

import (
	"fmt"
	"math"
	"strings"
)

// The product's own colours a corporate design is read against: the light
// scheme's text, cards and header, and the dark scheme's cards, where the
// accent applies too (style.css).
const (
	productText        = "#14161a"
	productSurface     = "#ffffff"
	productDarkSurface = "#171a20"
)

// MinContrast is the contrast ratio WCAG 2.x level AA asks of normal text.
const MinContrast = 4.5

// Finding is a pair of the design's colours whose text would be hard to
// read: Pair names it, Ratio is its contrast ratio, below MinContrast.
type Finding struct {
	Pair       string  `json:"pair"`
	Foreground string  `json:"foreground"`
	Background string  `json:"background"`
	Ratio      float64 `json:"ratio"`
}

// Contrast checks the text colours of a design against the backgrounds
// they are shown on, in both schemes where they apply (ADR-0052). It warns;
// it does not refuse: an organisation's colours are its decision, and a
// stylesheet may change what the pairs are.
func (d Design) Contrast() []Finding {
	or := func(c, fallback string) string {
		if c != "" {
			return c
		}
		return fallback
	}
	type pair struct{ name, fg, bg string }
	var pairs []pair
	if d.HeaderText != "" || d.HeaderBackground != "" {
		pairs = append(pairs, pair{"headerText", or(d.HeaderText, productText), or(d.HeaderBackground, productSurface)})
	}
	if d.Background != "" {
		pairs = append(pairs, pair{"textOnBackground", productText, d.Background})
	}
	if d.Accent != "" {
		pairs = append(pairs,
			pair{"accentInk", AccentInk(d.Accent), d.Accent},
			pair{"accentOnSurface", d.Accent, productSurface},
			pair{"accentOnDarkSurface", DarkAccent(d.Accent), productDarkSurface})
		if d.Background != "" {
			pairs = append(pairs, pair{"accentOnBackground", d.Accent, d.Background})
		}
	}
	var out []Finding
	for _, p := range pairs {
		if r := ContrastRatio(p.fg, p.bg); r < MinContrast {
			out = append(out, Finding{Pair: p.name, Foreground: p.fg, Background: p.bg, Ratio: float64(int(r*100)) / 100})
		}
	}
	return out
}

// ContrastRatio is the contrast ratio of two colours #rrggbb, from 1 to 21
// (WCAG 2.x).
func ContrastRatio(a, b string) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// DarkAccent returns the accent for the dark scheme: the accent itself if
// it reads on the dark scheme's cards, else the accent mixed with white in
// steps of 5 % until it does. The hue stays; only the lightness changes.
func DarkAccent(c string) string {
	for p := 0; p <= 100; p += 5 {
		m := mix(c, "#ffffff", float64(p)/100)
		if ContrastRatio(m, productDarkSurface) >= MinContrast {
			return m
		}
	}
	return "#ffffff"
}

// mix blends a with b by the share w of b, per channel in sRGB, as CSS
// color-mix(in srgb, …) does.
func mix(a, b string, w float64) string {
	var ar, ag, ab, br, bg, bb uint8
	if _, err := fmt.Sscanf(a, "#%02x%02x%02x", &ar, &ag, &ab); err != nil {
		return a
	}
	if _, err := fmt.Sscanf(b, "#%02x%02x%02x", &br, &bg, &bb); err != nil {
		return a
	}
	ch := func(x, y uint8) uint8 { return uint8(math.Round(float64(x)*(1-w) + float64(y)*w)) }
	if w == 0 {
		return strings.ToLower(a)
	}
	return fmt.Sprintf("#%02x%02x%02x", ch(ar, br), ch(ag, bg), ch(ab, bb))
}
