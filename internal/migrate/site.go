package migrate

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

// ReadCloud reads spaces and projects of one Atlassian Cloud site into one
// snapshot, so page links to issues resolve (ADR-0023). client sends the
// requests; nil means http.DefaultClient. The import command and the
// connector of ADR-0128 read through it.
func ReadCloud(ctx context.Context, site, email, token string, client *http.Client, spaces, projects []string) (Snapshot, []Problem, error) {
	var snap Snapshot
	var problems []Problem
	if len(spaces) > 0 {
		var err error
		snap, problems, err = ConfluenceCloud{Site: site, Email: email, Token: token, Client: client}.Read(ctx, spaces)
		if err != nil {
			return snap, problems, err
		}
	}
	if len(projects) > 0 {
		jira, more, err := JiraCloud{Site: site, Email: email, Token: token, Client: client}.Read(ctx, projects)
		problems = append(problems, more...)
		if err != nil {
			return snap, problems, err
		}
		snap.Instance = jira.Instance
		snap.Projects, snap.IssueTypes, snap.Issues = jira.Projects, jira.IssueTypes, jira.Issues
		for _, u := range jira.Users {
			if !slices.ContainsFunc(snap.Users, func(v SrcUser) bool { return v.ID == u.ID }) {
				snap.Users = append(snap.Users, u)
			}
		}
	}
	return snap, problems, nil
}

// Roles are the roles a grant names, by their names.
var Roles = map[string]item.Role{"viewer": item.Viewer, "editor": item.Editor, "space-admin": item.SpaceAdmin}

// ParseGrant reads a grant as "username=role" or "@group=role".
func ParseGrant(v string) (who string, role item.Role, err error) {
	who, name, ok := strings.Cut(v, "=")
	who = strings.TrimSpace(who)
	if !ok || who == "" || who == "@" {
		return "", 0, fmt.Errorf(`want "username=role" or "@group=role", got %q`, v)
	}
	role, ok = Roles[strings.TrimSpace(name)]
	if !ok {
		return "", 0, fmt.Errorf("unknown role %q; use viewer, editor or space-admin", name)
	}
	return who, role, nil
}

// LocalAccounts maps source accounts to the principals the store knows,
// user IDs, from a map of source accounts to usernames; idOf finds a
// username's ID, ignoring case. A username without an account is an
// error: mapping it to a placeholder would silently lock its owner out.
func LocalAccounts(byName map[string]string, idOf func(username string) (string, bool)) (map[string]string, error) {
	out := make(map[string]string, len(byName))
	for _, src := range slices.Sorted(maps.Keys(byName)) {
		id, ok := idOf(byName[src])
		if !ok {
			return nil, fmt.Errorf("no local account %q (named for %s); create it first or correct the name", byName[src], src)
		}
		out[src] = id
	}
	return out, nil
}

// LocalGrants resolves grants, "username=role" or "@group=role": a
// username becomes its user ID, a group is kept as it is.
func LocalGrants(grants []string, idOf func(username string) (string, bool)) ([]Grant, error) {
	var out []Grant
	for _, v := range grants {
		who, role, err := ParseGrant(v)
		if err != nil {
			return nil, err
		}
		if !strings.HasPrefix(who, "@") {
			id, ok := idOf(who)
			if !ok {
				return nil, fmt.Errorf("no local account %q (named in a grant); create it first or correct the name", who)
			}
			who = id
		}
		out = append(out, Grant{Principal: who, Role: role})
	}
	return out, nil
}
