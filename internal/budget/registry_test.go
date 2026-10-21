package budget

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// ceilingCalls are the calls that bound how much input the process reads,
// with the index of the argument that carries the bound.
var ceilingCalls = map[string]int{
	"io.LimitReader":      1,
	"io.CopyN":            2,
	"http.MaxBytesReader": 2,
}

// classified are the ceilings that are deliberately not budgets, each with
// its reason. A key is "<package dir>.<name>" for a package-level constant
// or variable, or "<file>:<bound as printed>" for a ceiling call.
var classified = map[string]string{
	"internal/export.maxEmbedDepth": "the shape of an export (ADR-0071): how deep embedded items are written inside each other, " +
		"like a heading level; what an export gathers is bounded by PageItems",
	"internal/site.MaxBackupVersions": "a retention policy (ADR-0073): how many backup versions an administrator may keep " +
		"at the destination, not something the server holds in memory",
	"internal/backup/dest.maxListing": "a backup destination's listing or error answer (ADR-0073), read and parsed; " +
		"not a caller's input to this server, it guards against a destination that misbehaves",
	"internal/backup/dest/remote.go:2048": "the start of a destination's error answer, quoted in the error message (ADR-0073)",
	"internal/item.maxURL":                "the shape of a url field (ADR-0074), like the length of a username; what a request carries is bounded by RequestBody",
	"internal/opendata.maxAnswer": "the answer of the catalog source the synchronisation reads (ADR-0075), not a caller's input to this server; " +
		"it guards the synchronisation against a source that misbehaves",
	"internal/opendata/ckan.go:maxAnswer": "the same bound, where the answer is read",
	"internal/opendata.maxDatasets": "the size of one synchronisation run (ADR-0075), so a misbehaving source cannot make it endless; " +
		"not memory a caller of this server can claim",
	"internal/auth.MaxFailures":      "a security policy (ADR-0013): failed logins before the throttle, not a memory setting",
	"internal/auth.MaxTokenLifetime": "a security policy (ADR-0027): how long an API token may live",
	"internal/auth.MaxReasonLength":  "the shape of an account request (ADR-0061), like the length of a username; the waiting requests are bounded by AccountRequests",
	"internal/wal.MaxRecord": "part of the event log format: a server must read every record a server wrote, " +
		"so the bound may not differ between them",
	"internal/backup.maxIterations": "part of the backup format: the work factor a header may demand " +
		"must not depend on the restoring server's environment",
	"internal/backup.maxManifest": "part of the backup format: the manifest of a backup, which lists every file, " +
		"read by the restore command from its own format",
	"internal/backup/backup.go:maxManifest": "the same bound, where the manifest is read",
	"cmd/settingscheck/main.go:4 << 20":     "a command-line client reading GitHub's answer, not this server's input",
	"internal/mcp.maxAnswer": "the MCP adapter reading an answer of the API it calls; the API's own budgets bound " +
		"what it sends, this only guards the adapter against a server that misbehaves",
	"internal/mcp/client.go:maxAnswer": "the same bound, where the adapter reads",
	"internal/migrate.maxSourceAnswer": "the import reading an answer of the source site (ADR-0023), " +
		"not a caller's input to this server; it guards the import against a source that misbehaves",
	"internal/notify.maxHookAnswer": "the answer of a webhook a message was posted to (ADR-0064), read and dropped; " +
		"not a caller's input to this server",
	"internal/notify/hooks.go:maxHookAnswer":    "the same bound, where the answer is dropped",
	"internal/migrate/cloud.go:maxSourceAnswer": "the same bound, where the import reads (ADR-0023)",
}

// TestNoCeilingWithoutAName keeps the budgets complete (ADR-0015). Every
// package-level constant or variable named max… and every bound passed to a
// reading ceiling is either read from Budgets or classified above with the
// reason. A new limit therefore cannot hide in a package: it becomes a
// budget, or its exemption is written down.
func TestNoCeilingWithoutAName(t *testing.T) {
	root := filepath.Join("..", "..")
	fset := token.NewFileSet()
	seen := map[string]bool{}
	var unnamed []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "testdata", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		pkg := filepath.ToSlash(filepath.Dir(rel))
		consts := map[string]bool{}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || (gd.Tok != token.CONST && gd.Tok != token.VAR) {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, name := range vs.Names {
					consts[name.Name] = true
					if strings.HasPrefix(strings.ToLower(name.Name), "max") {
						key := pkg + "." + name.Name
						seen[key] = true
						if classified[key] == "" {
							unnamed = append(unnamed, key)
						}
					}
				}
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			x, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			arg, ok := ceilingCalls[x.Name+"."+sel.Sel.Name]
			if !ok || arg >= len(call.Args) || !fixed(call.Args[arg], consts) {
				return true
			}
			var b strings.Builder
			printer.Fprint(&b, fset, call.Args[arg])
			key := rel + ":" + b.String()
			seen[key] = true
			if classified[key] == "" {
				unnamed = append(unnamed, key)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(unnamed)
	for _, key := range unnamed {
		t.Errorf("%s is a ceiling without a name: make it a field of Budgets, or classify it with the reason", key)
	}
	for key := range classified {
		if !seen[key] {
			t.Errorf("classified %s no longer exists; remove it from the list", key)
		}
	}
}

// fixed reports whether a bound is chosen in the file itself — a literal,
// or a package-level name — rather than read from Budgets or passed in.
func fixed(e ast.Expr, consts map[string]bool) bool {
	switch e := e.(type) {
	case *ast.BasicLit:
		return true
	case *ast.Ident:
		return consts[e.Name]
	case *ast.BinaryExpr:
		return fixed(e.X, consts) && fixed(e.Y, consts)
	case *ast.ParenExpr:
		return fixed(e.X, consts)
	}
	return false
}
