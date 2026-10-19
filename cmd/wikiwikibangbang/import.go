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

// importCmd imports spaces of a Confluence Cloud site into a data directory
// (ADR-0023). It opens the data directory itself, so the server must be
// stopped; a re-run updates what changed and creates nothing twice. The
// source credentials come from the environment, never from flags, which
// would show in the process list and the shell history.
func importCmd(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlags("import", stderr)
	from := fs.String("from", "", `the source; only "confluence-cloud" so far`)
	site := fs.String("site", "", "the source site, e.g. https://example.atlassian.net")
	spaces := fs.String("spaces", "", "comma-separated keys of the spaces to import")
	dataDir := fs.String("data-dir", defaultDataDir, "directory holding the event log; the server must be stopped")
	usersFile := fs.String("users", "", `file mapping source account IDs to local usernames, one "accountId username" per line`)
	dryRun := fs.Bool("dry-run", false, "report what the import would do without writing anything")
	if err := fs.Parse(args); err != nil {
		return parseFailed(err)
	}
	if *from != "confluence-cloud" {
		fmt.Fprintln(stderr, `import: -from must be "confluence-cloud"`)
		return 2
	}
	var keys []string
	for k := range strings.SplitSeq(*spaces, ",") {
		if k = strings.TrimSpace(k); k != "" {
			keys = append(keys, k)
		}
	}
	if *site == "" || len(keys) == 0 {
		fmt.Fprintln(stderr, "import: -site and -spaces are required")
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

	src := migrate.ConfluenceCloud{Site: *site, Email: email, Token: token}
	snap, problems, err := src.Read(ctx, keys)
	if err != nil {
		fmt.Fprintln(stderr, "import:", err)
		return 1
	}
	m := migrate.Derive(snap).MapAccounts(local)
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
	fmt.Fprintf(stdout, "%s: %d pages created, %d updated, %d unchanged\n", what, rep.Created, rep.Updated, rep.Unchanged)
	all := append(problems, rep.Problems...)
	if len(all) > 0 {
		fmt.Fprintf(stdout, "%d problems:\n", len(all))
	}
	for _, p := range all {
		fmt.Fprintf(stdout, "  %s: %s\n", p.Source, p.Message)
	}
	return 0
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
	ids := map[string]auth.UserID{}
	for _, u := range accounts.Users() {
		ids[strings.ToLower(u.Username)] = u.ID
	}
	out := make(map[string]string, len(byName))
	for src, name := range byName {
		id, ok := ids[strings.ToLower(name)]
		if !ok {
			return nil, fmt.Errorf("no local account %q (mapped from %s); create it first or fix the users file", name, src)
		}
		out[src] = string(id)
	}
	return out, nil
}
