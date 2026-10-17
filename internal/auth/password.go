package auth

import (
	"crypto/pbkdf2"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

// DefaultIterations is the PBKDF2-HMAC-SHA256 work factor for new password
// hashes, the OWASP recommendation for that function (ADR-0027).
const DefaultIterations = 600_000

// hashPassword returns "pbkdf2-sha256$<iterations>$<salt>$<key>", base64
// without padding. The iteration count is stored, so it can be raised later
// without invalidating existing hashes.
func hashPassword(password string, iter int, random func([]byte) error) (string, error) {
	salt := make([]byte, 16)
	if err := random(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, iter, 32)
	if err != nil {
		return "", err
	}
	enc := base64.RawStdEncoding
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", iter, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

// checkPassword compares in constant time. A malformed hash never matches.
func checkPassword(hash, password string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < 1 {
		return false
	}
	enc := base64.RawStdEncoding
	salt, err1 := enc.DecodeString(parts[2])
	want, err2 := enc.DecodeString(parts[3])
	if err1 != nil || err2 != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iter, len(want))
	if err != nil {
		return false
	}
	return equalStrings(string(got), string(want))
}

// dummyHash is checked when a username does not exist, so a login for an
// unknown user costs as much as one for a known user. Its key is all zero
// bytes, which no password is known to derive to.
func dummyHash(iter int) string {
	return "pbkdf2-sha256$" + strconv.Itoa(iter) + "$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
}
