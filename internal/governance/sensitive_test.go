package governance

import (
	"strings"
	"testing"
)

func kinds(fs []Finding) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Kind)
	}
	return out
}

func TestSensitiveFindsWhatLeaks(t *testing.T) {
	for text, want := range map[string]string{
		"VPN password: Sommer2026!":                    "password",
		"Passwort = hunter2#x":                         "password",
		"pwd=Tr0ub4dor&3":                              "password",
		"Kennwort: correct-horse-battery-staple-1":     "password",
		"-----BEGIN OPENSSH PRIVATE KEY-----\nb3Bl":    "private-key",
		"-----BEGIN PRIVATE KEY-----":                  "private-key",
		"key AKIAIOSFODNN7EXAMPLE in the script":       "access-token",
		"ghp_abcdefghijklmnopqrstuvwxyz0123456789":     "access-token",
		"token wwbb_3q2+7w9xkZpL0aaaaaaaaaaaaaaaaaaaa": "access-token",
		"xoxb-123456789012-abcdefABCDEF":               "access-token",
		"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U": "access-token",
		"IBAN CH93 0076 2011 6238 5295 7": "iban",
		"Konto DE89370400440532013000":    "iban",
		"Karte 4111 1111 1111 1111":       "card-number",
		"card 5500-0000-0000-0004":        "card-number",
	} {
		got := Sensitive(text)
		if len(got) != 1 || got[0].Kind != want {
			t.Errorf("%q: %v, want one %s", text, kinds(got), want)
		}
	}
}

func TestSensitiveLeavesOrdinaryTextAlone(t *testing.T) {
	for _, text := range []string{
		"Reset your password in the account settings.",
		"password: required",                       // a word, not a secret
		"password: ********",                       // already masked
		"Passwort: siehe Tresor",                   // a pointer, not a secret
		"The order number is 4111 1111 1111 1112.", // fails Luhn
		"IBAN CH93 0076 2011 6238 5295 8",          // fails the check digits
		"Call 044 123 45 67 or 079 123 45 67.",
		"Version 1.2.3, build 20261019",
		"a JWT starts with eyJ",
		"",
	} {
		if got := Sensitive(text); len(got) != 0 {
			t.Errorf("%q: %v, want nothing", text, kinds(got))
		}
	}
}

// A finding is shown masked: the report must not become the leak.
func TestFindingsAreMasked(t *testing.T) {
	for text, secret := range map[string]string{
		"VPN password: Sommer2026!":                "Sommer2026!",
		"ghp_abcdefghijklmnopqrstuvwxyz0123456789": "abcdefghijklmnopqrstuvwxyz0123456789",
		"Karte 4111 1111 1111 1111":                "1111 1111 1111",
	} {
		f := Sensitive(text)[0]
		if strings.Contains(f.Masked, secret) || f.Masked == "" {
			t.Errorf("%q masked as %q", text, f.Masked)
		}
		if text[f.Start:f.End] == "" {
			t.Errorf("%q: empty span", text)
		}
	}
}

func TestFingerprintsTellNewFindingsFromOld(t *testing.T) {
	old := Fingerprints("password: Sommer2026!")
	same := Fingerprints("note\npassword: Sommer2026!")
	other := Fingerprints("password: Winter2027?")
	if len(old) != 1 || len(same) != 1 || len(other) != 1 {
		t.Fatalf("old %v same %v other %v", old, same, other)
	}
	for fp := range same {
		if !old[fp] {
			t.Error("the same secret elsewhere in the text counts as new")
		}
	}
	for fp := range other {
		if old[fp] {
			t.Error("another secret counts as old")
		}
	}
	for fp := range old {
		if strings.Contains(fp, "Sommer") {
			t.Error("a fingerprint shows the secret")
		}
	}
}
