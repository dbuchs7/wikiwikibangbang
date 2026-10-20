package item

import (
	"slices"
	"strings"
)

// Who holds which role, and taking it back (ADR-0048). Granting is in
// permissions.go; these list the grants for administration and revoke
// them.

// Grant is a space role given to a user ID or "@group" directly.
type Grant struct {
	Principal string
	Role      Role
}

// SpaceGrants lists the roles granted in a space, by principal.
func (s *Store) SpaceGrants(space string) []Grant {
	var out []Grant
	for p, r := range s.spaceRoles[space] {
		out = append(out, Grant{Principal: p, Role: r})
	}
	slices.SortFunc(out, func(a, b Grant) int { return strings.Compare(a.Principal, b.Principal) })
	return out
}

// Groups lists every group with its members, both sorted.
func (s *Store) Groups() map[string][]string {
	out := map[string][]string{}
	for g, members := range s.groups {
		if len(members) == 0 {
			continue
		}
		list := make([]string, 0, len(members))
		for m := range members {
			list = append(list, m)
		}
		slices.Sort(list)
		out[g] = list
	}
	return out
}

// SystemRoles lists the principals holding a system role, with their roles
// sorted.
func (s *Store) SystemRoles() map[string][]string {
	out := map[string][]string{}
	for p, roles := range s.sysRoles {
		for r, held := range roles {
			if held {
				out[p] = append(out[p], r)
			}
		}
		slices.Sort(out[p])
	}
	return out
}

// RevokeRole takes back the role granted to a principal in a space. What
// the principal holds through a group or as an administrator stays.
func (s *Store) RevokeRole(actor, space, principal string) error {
	if _, ok := s.spaceRoles[space][principal]; !ok {
		return notFound("no role of %s in %s", principal, space)
	}
	return s.record(actor, Event{Kind: RoleRevoked, Space: space, Principal: principal})
}

// RemoveFromGroup takes a member out of a group.
func (s *Store) RemoveFromGroup(actor, group, member string) error {
	if !s.groups[group][member] {
		return notFound("%s is not a member of %s", member, group)
	}
	return s.record(actor, Event{Kind: GroupMemberRemoved, Group: group, Principal: member})
}

// RevokeSystemRole takes back a system role. The last administrator keeps
// theirs: without one, nobody could grant it again.
func (s *Store) RevokeSystemRole(actor, principal, role string) error {
	if !s.sysRoles[principal][role] {
		return notFound("%s does not hold the system role %s", principal, role)
	}
	if role == RoleAdmin {
		others := 0
		for p, roles := range s.sysRoles {
			if p != principal && roles[RoleAdmin] {
				others++
			}
		}
		if others == 0 {
			return invalid("%s is the last administrator", principal)
		}
	}
	return s.record(actor, Event{Kind: SystemRoleRevoked, Principal: principal, SystemRole: role})
}

// applyRevocation applies the events that take access back.
func (s *Store) applyRevocation(e Event) {
	switch e.Kind {
	case RoleRevoked:
		delete(s.spaceRoles[e.Space], e.Principal)
	case GroupMemberRemoved:
		delete(s.groups[e.Group], e.Principal)
	case SystemRoleRevoked:
		delete(s.sysRoles[e.Principal], e.SystemRole)
	}
}

// --- sessions ---------------------------------------------------------------

// SpaceGrants lists the roles granted in a space the principal administers.
func (x *Session) SpaceGrants(space string) ([]Grant, error) {
	if _, ok := x.s.spaces[space]; !ok || x.s.spaceRole(x.who, space) < Viewer {
		return nil, notFound("space %q", space)
	}
	if x.s.spaceRole(x.who, space) < SpaceAdmin {
		return nil, forbidden("%s may not see the roles of %s", x.who, space)
	}
	return x.s.SpaceGrants(space), nil
}

// Revoke takes back a role in a space the principal administers.
func (x *Session) Revoke(space, principal string) error {
	if x.s.spaceRole(x.who, space) < SpaceAdmin {
		return forbidden("%s may not revoke roles in %s", x.who, space)
	}
	return x.s.RevokeRole(x.who, space, principal)
}

// EditsASpace reports whether the principal may edit in some space, and
// so may look up the people to assign work to.
func (x *Session) EditsASpace() bool {
	if x.s.hasSystemRole(x.who, RoleAdmin) {
		return true
	}
	for key := range x.s.spaces {
		if x.s.spaceRole(x.who, key) >= Editor {
			return true
		}
	}
	return false
}

// AdministersASpace reports whether the principal is an administrator or
// a space administrator somewhere, and so may look up the people to grant
// roles to.
func (x *Session) AdministersASpace() bool {
	if x.s.hasSystemRole(x.who, RoleAdmin) {
		return true
	}
	for key := range x.s.spaces {
		if x.s.spaceRole(x.who, key) >= SpaceAdmin {
			return true
		}
	}
	return false
}
