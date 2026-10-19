// Package governance finds problems in content (ADR-0040): sensitive data
// such as passwords, keys and account numbers; and, over an item's
// metadata, staleness and implausible values. It holds no state and
// stores nothing; a finding names where the problem is, never the secret.
package governance

import (
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// Finding is one piece of sensitive data in a text.
type Finding struct {
	// Kind is password, private-key, access-token, iban or card-number.
	Kind string
	// Start and End are byte offsets of the match in the text.
	Start, End int
	// Masked shows the match with its secret part hidden.
	Masked string
}

type detector struct {
	kind string
	re   *regexp.Regexp
	// value is the submatch holding the secret; 0 is the whole match.
	value int
	// valid confirms a candidate the pattern alone cannot judge.
	valid func(string) bool
}

var detectors = []detector{
	{kind: "password", value: 2, valid: plausiblePassword,
		re: regexp.MustCompile(`(?i)\b(passwor[dt]|pwd|passcode|kennwort)\s*[:=]\s*(\S+)`)},
	{kind: "private-key", re: regexp.MustCompile(`-----BEGIN (?:[A-Z0-9]+ )*PRIVATE KEY-----`)},
	{kind: "access-token", re: regexp.MustCompile(
		`\bAKIA[0-9A-Z]{16}\b` + // cloud access key ID
			`|\bgh[pousr]_[A-Za-z0-9]{36,}` + // repository host tokens
			`|\bgithub_pat_[A-Za-z0-9_]{50,}` +
			`|\bxox[abprs]-[A-Za-z0-9-]{10,}` + // chat bot tokens
			`|\bsk-[A-Za-z0-9_-]{20,}` + // API secret keys
			`|\bwwbb_[A-Za-z0-9+/_=-]{20,}` + // this server's own API tokens
			`|\beyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`)}, // signed tokens
	{kind: "iban", valid: validIBAN,
		re: regexp.MustCompile(`\b[A-Z]{2}\d{2}(?: ?[A-Z0-9]){11,30}\b`)},
	{kind: "card-number", valid: validCard,
		re: regexp.MustCompile(`\b\d(?:[ -]?\d){12,18}\b`)},
}

// Sensitive returns the sensitive data in text, in text order.
func Sensitive(text string) []Finding {
	var out []Finding
	for _, d := range detectors {
		for _, m := range d.re.FindAllStringSubmatchIndex(text, -1) {
			start, end := m[2*d.value], m[2*d.value+1]
			if d.valid != nil && !d.valid(text[start:end]) {
				continue
			}
			out = append(out, Finding{Kind: d.kind, Start: m[0], End: m[1],
				Masked: text[m[0]:start] + mask(text[start:end]) + text[end:m[1]]})
		}
	}
	slices.SortFunc(out, func(a, b Finding) int { return a.Start - b.Start })
	return out
}

// Fingerprints identifies the sensitive data in text without holding it,
// so that a change can tell secrets it adds from secrets already there.
func Fingerprints(text string) map[string]bool {
	out := map[string]bool{}
	for _, f := range Sensitive(text) {
		sum := sha256.Sum256([]byte(f.Kind + "\x00" + text[f.Start:f.End]))
		out[hex.EncodeToString(sum[:8])] = true
	}
	return out
}

// mask keeps two characters at each end of a long value and hides the
// rest; a short value is hidden entirely.
func mask(v string) string {
	r := []rune(v)
	if len(r) < 12 {
		return strings.Repeat("•", 6)
	}
	return string(r[:2]) + "…" + string(r[len(r)-2:])
}

// plausiblePassword tells a value from a word or a placeholder: it is not
// masked already, and it has a digit or a symbol, or is long.
func plausiblePassword(v string) bool {
	if strings.Trim(v, "*•x.") == "" || len(v) < 6 {
		return false
	}
	if len(v) >= 16 {
		return true
	}
	return strings.IndexFunc(v, func(r rune) bool { return unicode.IsDigit(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) }) >= 0
}

// validIBAN checks the ISO 13616 check digits (mod 97).
func validIBAN(s string) bool {
	s = strings.ReplaceAll(s, " ", "")
	if len(s) < 15 || len(s) > 34 {
		return false
	}
	var digits strings.Builder
	for _, r := range s[4:] + s[:4] {
		switch {
		case r >= '0' && r <= '9':
			digits.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			digits.WriteString(big.NewInt(int64(r - 'A' + 10)).String())
		default:
			return false
		}
	}
	n, ok := new(big.Int).SetString(digits.String(), 10)
	return ok && new(big.Int).Mod(n, big.NewInt(97)).Int64() == 1
}

// validCard checks the Luhn digit of 13 to 19 digits that are not all the
// same.
func validCard(s string) bool {
	var d []int
	for _, r := range s {
		if r >= '0' && r <= '9' {
			d = append(d, int(r-'0'))
		}
	}
	if len(d) < 13 || len(d) > 19 || strings.Count(s, string(s[0])) == len(d) {
		return false
	}
	sum := 0
	for i := range d {
		v := d[len(d)-1-i]
		if i%2 == 1 {
			if v *= 2; v > 9 {
				v -= 9
			}
		}
		sum += v
	}
	return sum%10 == 0
}
