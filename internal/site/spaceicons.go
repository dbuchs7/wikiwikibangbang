package site

import (
	"regexp"
	"strings"
)

// spaceKey is a space's key as the item store accepts it.
var spaceKey = regexp.MustCompile(`^[A-Z][A-Z0-9]*$`)

// SpaceIcon names the asset holding a space's icon or logo, shown on its
// tile (ADR-0063). Whether the space exists is the caller's concern.
func SpaceIcon(key string) string { return "space-" + key }

// IsSpaceIcon reports whether an asset name is a space's icon.
func IsSpaceIcon(name string) bool {
	key, ok := strings.CutPrefix(name, "space-")
	return ok && spaceKey.MatchString(key)
}
