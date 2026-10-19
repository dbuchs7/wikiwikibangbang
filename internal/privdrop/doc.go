// Package privdrop lets the container image start as root, hand the data
// directory to an unprivileged account and give root up for good before
// the server does anything else (ADR-0038). Only an account named in
// WWBB_RUN_AS is switched to; without it nothing changes.
package privdrop

import (
	"fmt"
	"strconv"
	"strings"
)

// Account is a user and group by number.
type Account struct{ UID, GID int }

// Parse reads "uid:gid", e.g. "65532:65532". Root (0) is refused for
// either: switching to it would drop nothing.
func Parse(s string) (Account, error) {
	u, g, ok := strings.Cut(strings.TrimSpace(s), ":")
	uid, err1 := strconv.Atoi(u)
	gid, err2 := strconv.Atoi(g)
	if !ok || err1 != nil || err2 != nil || uid <= 0 || gid <= 0 {
		return Account{}, fmt.Errorf("%q is not uid:gid of an unprivileged account, e.g. 65532:65532", s)
	}
	return Account{UID: uid, GID: gid}, nil
}
