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

// kanbanID is a kanban's ID as the item store makes it.
var kanbanID = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)

// KanbanIcon names the asset holding a kanban's picture or logo, shown on
// its board and tile (ADR-0087).
func KanbanIcon(space, id string) string { return "kanban-" + space + "-" + id }

// IsKanbanIcon reports whether an asset name is a kanban's icon.
func IsKanbanIcon(name string) bool {
	rest, ok := strings.CutPrefix(name, "kanban-")
	if !ok {
		return false
	}
	space, id, ok := strings.Cut(rest, "-")
	return ok && spaceKey.MatchString(space) && kanbanID.MatchString(id)
}

// KanbanIcons lists the assets of the kanbans of a space, so they go with
// it.
func (s *Settings) KanbanIcons(space string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for name := range s.assets {
		if strings.HasPrefix(name, "kanban-"+space+"-") && IsKanbanIcon(name) {
			out = append(out, name)
		}
	}
	return out
}
