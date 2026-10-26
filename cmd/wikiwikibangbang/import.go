package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/dbuchs7/wikiwikibangbang/internal/auth"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/migrate"
	"github.com/dbuchs7/wikiwikibangbang/internal/storage"
)

// importActor is recorded as the author of everything an import writes.
const importActor = "import"

// importCmd imports Confluence spaces and Jira projects of an Atlassian
// Cloud site into a data directory (ADR-0023), in one run, so page links to
// issues resolve; or it restores spaces from a backup (ADR-0124). It opens
// the data directory itself, so the server must be stopped; a re-run
// updates what changed and creates nothing twice. The source credentials
// and the backup passphrase come from the environment or a file, never
// from flags, which would show in the process list and the shell history.
func importCmd(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlags("import", stderr)
	from := fs.String("from", "", `the source: "atlassian-cloud", or "backup" to restore spaces from a backup`)
	site := fs.String("site", "", "the source site, e.g. https://example.atlassian.net")
	spaces := fs.String("spaces", "", "comma-separated keys of the Confluence spaces to import, or with -from backup of the spaces to restore")
	var backups fileFlags
	fs.Var(&backups, "backup", "with -from backup: a backup file, repeated for a full backup and the incremental ones after it, or a directory or remote destination of automatic backups")
	at := fs.String("at", "", "with -from backup and -backup a directory or destination: the backup to restore from, by file name (default: the newest)")
	passFile := fs.String("passphrase-file", "", "with -from backup: file whose first line is the passphrase (default: $WWBB_BACKUP_PASSPHRASE)")
	projects := fs.String("projects", "", "comma-separated keys of the Jira projects to import")
	dataDir := fs.String("data-dir", defaultDataDir, "directory holding the event log; the server must be stopped")
	usersFile := fs.String("users", "", `file mapping source account IDs to local usernames, one "accountId username" per line`)
	groupsFile := fs.String("groups", "", `file mapping source groups to local groups, one "source group @local-group" per line`)
	var grants grantFlags
	fs.Var(&grants, "grant", `give a role in every imported space, as "username=role" or "@group=role" with role viewer, editor or space-admin; repeatable`)
	dryRun := fs.Bool("dry-run", false, "report what the import would do without writing anything")
	if err := fs.Parse(args); err != nil {
		return parseFailed(err)
	}
	if *from == "backup" {
		return restoreSpaces(backups, *at, *passFile, keyList(*spaces), *dataDir, grants, *dryRun, stdout, stderr)
	}
	if *from != "atlassian-cloud" {
		fmt.Fprintln(stderr, `import: -from must be "atlassian-cloud" or "backup"`)
		return 2
	}
	spaceKeys, projectKeys := keyList(*spaces), keyList(*projects)
	if *site == "" || len(spaceKeys)+len(projectKeys) == 0 {
		fmt.Fprintln(stderr, "import: -site and at least one of -spaces and -projects are required")
		return 2
	}
	if err := checkSite(*site); err != nil {
		fmt.Fprintln(stderr, "import:", err)
		return 2
	}
	email, token := strings.TrimSpace(os.Getenv("WWBB_IMPORT_EMAIL")), strings.TrimSpace(os.Getenv("WWBB_IMPORT_TOKEN"))
	if email == "" || token == "" {
		fmt.Fprintln(stderr, "import: set WWBB_IMPORT_EMAIL and WWBB_IMPORT_TOKEN to an account's e-mail address and API token")
		return 2
	}
	var accounts map[string]string
	if *usersFile != "" {
		var err error
		if accounts, err = readAccountMap(*usersFile); err != nil {
			fmt.Fprintln(stderr, "import:", err)
			return 1
		}
	}

	var groups map[string]string
	if *groupsFile != "" {
		var err error
		if groups, err = readGroupMap(*groupsFile); err != nil {
			fmt.Fprintln(stderr, "import:", err)
			return 1
		}
	}

	db, _, err := storage.Open(*dataDir)
	if err != nil {
		fmt.Fprintln(stderr, "import: cannot open the data directory (is the server still running?):", err)
		return 1
	}
	defer db.Close()
	local, err := localPrincipals(db.Accounts, accounts)
	if err != nil {
		fmt.Fprintln(stderr, "import:", err)
		return 1
	}
	roles, err := grants.resolve(db.Accounts)
	if err != nil {
		fmt.Fprintln(stderr, "import:", err)
		return 1
	}

	snap, problems, err := migrate.ReadCloud(ctx, *site, email, token, nil, spaceKeys, projectKeys)
	if err != nil {
		fmt.Fprintln(stderr, "import:", err)
		return 1
	}
	m := migrate.Derive(snap).MapAccounts(local)
	m.Groups, m.Grants = groups, roles
	var rep migrate.Report
	err = db.Write(func(s *item.Store) error {
		rep, err = migrate.Run(s, snap, m, migrate.Options{Actor: importActor, DryRun: *dryRun})
		return err
	})
	if err != nil {
		fmt.Fprintln(stderr, "import:", err)
		return 1
	}
	what := "imported"
	if *dryRun {
		what = "dry run, nothing written"
	}
	fmt.Fprintf(stdout, "%s: %d pages and issues created, %d updated, %d unchanged; %d roles granted\n", what, rep.Created, rep.Updated, rep.Unchanged, rep.Granted)
	all := append(problems, rep.Problems...)
	if len(all) > 0 {
		fmt.Fprintf(stdout, "%d problems:\n", len(all))
	}
	for _, p := range all {
		fmt.Fprintf(stdout, "  %s: %s\n", p.Source, p.Message)
	}
	return 0
}

func keyList(s string) []string {
	var keys []string
	for k := range strings.SplitSeq(s, ",") {
		if k = strings.TrimSpace(k); k != "" {
			keys = append(keys, k)
		}
	}
	return keys
}

// checkSite refuses a site the token would travel to in the clear: plain
// http only to the local machine, which a test or a tunnel uses.
func checkSite(site string) error {
	u, err := url.Parse(site)
	if err != nil || u.Host == "" {
		return fmt.Errorf("%q is not a site address", site)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if ip := net.ParseIP(u.Hostname()); ip != nil && ip.IsLoopback() || u.Hostname() == "localhost" {
			return nil
		}
	}
	return fmt.Errorf("%q: the API token is sent only over https", site)
}

// readAccountMap reads "accountId username" lines; blank lines and lines
// starting with # are skipped.
func readAccountMap(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, fmt.Errorf("%s:%d: want \"accountId username\", got %q", path, n, line)
		}
		if _, dup := out[fields[0]]; dup {
			return nil, fmt.Errorf("%s:%d: account %s is mapped twice", path, n, fields[0])
		}
		out[fields[0]] = fields[1]
	}
	return out, sc.Err()
}

// localPrincipals turns a map of source accounts to usernames into one to
// the principals the store knows, user IDs. A username without an account is
// an error: mapping it to a placeholder would silently lock its owner out.
func localPrincipals(accounts *auth.Accounts, byName map[string]string) (map[string]string, error) {
	return migrate.LocalAccounts(byName, idOf(accounts))
}

// idOf finds a username's user ID, ignoring case.
func idOf(accounts *auth.Accounts) func(string) (string, bool) {
	ids := map[string]string{}
	for _, u := range accounts.Users() {
		ids[strings.ToLower(u.Username)] = string(u.ID)
	}
	return func(name string) (string, bool) {
		id, ok := ids[strings.ToLower(name)]
		return id, ok
	}
}

// grantFlags collects -grant values.
type grantFlags []string

func (g *grantFlags) String() string { return strings.Join(*g, ",") }

func (g *grantFlags) Set(v string) error {
	if _, _, err := migrate.ParseGrant(v); err != nil {
		return err
	}
	*g = append(*g, v)
	return nil
}

// resolve turns usernames into the user IDs the store knows; a group is
// kept as it is. A username without an account is an error.
func (g grantFlags) resolve(accounts *auth.Accounts) ([]migrate.Grant, error) {
	return migrate.LocalGrants(g, idOf(accounts))
}

// readGroupMap reads "source group @local-group" lines; a source group's
// name may hold spaces, the local group is the last word. Blank lines and
// lines starting with # are skipped.
func readGroupMap(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		i := strings.LastIndexAny(line, " \t")
		if i < 0 || !strings.HasPrefix(line[i+1:], "@") || len(line[i+1:]) < 2 {
			return nil, fmt.Errorf("%s:%d: want \"source group @local-group\", got %q", path, n, line)
		}
		src := strings.TrimSpace(line[:i])
		if _, dup := out[src]; dup {
			return nil, fmt.Errorf("%s:%d: group %q is mapped twice", path, n, src)
		}
		out[src] = line[i+1:]
	}
	return out, sc.Err()
}
