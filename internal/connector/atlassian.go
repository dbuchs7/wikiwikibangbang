package connector

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/dbuchs7/wikiwikibangbang/internal/auth"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/migrate"
	"github.com/dbuchs7/wikiwikibangbang/internal/site"
	"github.com/dbuchs7/wikiwikibangbang/internal/storage"
)

// atlassianReader imports the spaces and projects of an Atlassian Cloud
// site with the import of ADR-0023, on a schedule and with a dry run
// (ADR-0128). Unlike the other kinds it writes what it read itself: the
// import decides the spaces, types, keys, restrictions and roles, and its
// items carry the same origin as those of the import command, so a site
// imported by the command is kept in step by a connector, and the other
// way round.
type atlassianReader struct{}

// ErrNotManaging is a cut-over from a connector that does not manage its
// items; only an Atlassian import does.
var ErrNotManaging = errors.New("connector: only an Atlassian import manages its items")

// ErrNoCredential is a connector whose credential is not set.
var ErrNoCredential = errors.New("connector: the credential is not set")

func (atlassianReader) Read(ctx context.Context, c site.Connector, env Env) (reading, error) {
	var rd reading
	if env.Email == "" || env.Token == "" {
		return rd, fmt.Errorf("%w: set %s and %s to an account's e-mail address and API token, in the server's environment, as files or on the administration page",
			ErrNoCredential, EmailVar(c.ID), TokenVar(c.ID))
	}
	a := c.Atlassian
	snap, problems, err := migrate.ReadCloud(ctx, a.Site, env.Email, env.Token, env.HTTP, a.Spaces, a.Projects)
	for _, p := range problems {
		rd.problem("%s: %s", p.Source, p.Message)
	}
	if err != nil {
		return rd, err
	}
	rd.datasets = len(snap.Pages) + len(snap.Issues)
	rd.write = func(db *storage.DB, dry bool) (Result, error) { return writeAtlassian(db, c, snap, dry) }
	return rd, nil
}

// writeAtlassian runs the import of a snapshot. Its items are managed by
// the connector: people read them, but change them only after the
// cut-over (ReleaseAtlassian), as a run would overwrite what
// they changed (ADR-0023). A dry run imports into a copy of the store.
func writeAtlassian(db *storage.DB, c site.Connector, snap migrate.Snapshot, dry bool) (Result, error) {
	res := Result{Connector: c.ID, DryRun: dry, Problems: []string{}, Examples: []Change{}}
	m, err := atlassianMapping(db.Accounts, c, snap)
	if err != nil {
		return res, err
	}
	opt := migrate.Options{Actor: Actor(c), DryRun: dry, Managed: Actor(c)}
	var rep migrate.Report
	if dry {
		db.Read(func(s *item.Store) { rep, err = migrate.Run(s, snap, m, opt) })
	} else {
		err = db.Write(func(s *item.Store) error {
			rep, err = migrate.Run(s, snap, m, opt)
			return err
		})
	}
	if err != nil {
		return res, err
	}
	res.Entries, res.New, res.Changed, res.Unchanged = rep.Created+rep.Updated+rep.Unchanged, rep.Created, rep.Updated, rep.Unchanged
	for _, p := range rep.Problems {
		res.problem("%s: %s", p.Source, p.Message)
	}
	if more := len(rep.Problems) - problemsShown; more > 0 {
		res.Problems[len(res.Problems)-1] = fmt.Sprintf("and %d more problems", more+1)
	}
	return res, nil
}

// atlassianMapping is the import's mapping (migrate.Derive) with the
// connector's accounts, groups and roles, usernames resolved to the
// accounts they name now.
func atlassianMapping(accounts *auth.Accounts, c site.Connector, snap migrate.Snapshot) (migrate.Mapping, error) {
	idOf := userIDs(accounts)
	a := c.Atlassian
	local, err := migrate.LocalAccounts(a.Users, idOf)
	if err != nil {
		return migrate.Mapping{}, err
	}
	grants, err := migrate.LocalGrants(a.Grants, idOf)
	if err != nil {
		return migrate.Mapping{}, err
	}
	m := migrate.Derive(snap).MapAccounts(local)
	m.Groups, m.Grants = a.Groups, grants
	return m, nil
}

// userIDs finds a username's user ID, ignoring case.
func userIDs(accounts *auth.Accounts) func(string) (string, bool) {
	ids := map[string]string{}
	for _, u := range accounts.Users() {
		ids[strings.ToLower(u.Username)] = string(u.ID)
	}
	return func(name string) (string, bool) {
		id, ok := ids[strings.ToLower(name)]
		return id, ok
	}
}

// CheckAccounts checks that the usernames an Atlassian connector names
// have accounts, so a mistake is found when it is saved, not at its next
// run.
func CheckAccounts(accounts *auth.Accounts, c site.Connector) error {
	if c.Atlassian == nil {
		return nil
	}
	idOf := userIDs(accounts)
	if _, err := migrate.LocalAccounts(c.Atlassian.Users, idOf); err != nil {
		return err
	}
	_, err := migrate.LocalGrants(c.Atlassian.Grants, idOf)
	return err
}

// ReleaseAtlassian is the cut-over (ADR-0023, ADR-0128): the connector's
// items become ordinary items that people change, in every space it wrote
// to, and its schedule is turned off, as a further run would take the
// items back under the connector. It returns the spaces released.
func (r *Runner) ReleaseAtlassian(id, actor string) ([]string, error) {
	c, err := r.begin(id)
	if err != nil {
		return nil, err
	}
	defer func() {
		r.mu.Lock()
		r.of(id).running = false
		r.mu.Unlock()
	}()
	if c.Kind != site.KindAtlassian {
		return nil, fmt.Errorf("%w: %s is not an Atlassian import", ErrNotManaging, id)
	}
	var spaces []string
	err = r.db.Write(func(s *item.Store) error {
		for _, it := range s.Find(item.Query{}) {
			if it.Managed == Actor(c) && !slices.Contains(spaces, it.Space) {
				spaces = append(spaces, it.Space)
			}
		}
		slices.Sort(spaces)
		for _, sp := range spaces {
			if err := s.Release(actor, Actor(c), sp); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	c.Enabled = false
	if _, err := r.db.Site.SetConnector(actor, c); err != nil {
		return spaces, err
	}
	return spaces, nil
}
