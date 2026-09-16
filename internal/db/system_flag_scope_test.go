package db

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// systemFlagName is the privileged session flag the tenants
// system_iteration_read policy (0027, AD-024) checks.
const systemFlagName = "app.is_system"

// systemFlagOwners are the only non-test files allowed to mention
// systemFlagName: the Go helper that sets it and the migration that
// defines the policy reading it. Paths are relative to the repository
// root.
var systemFlagOwners = []string{
	"internal/db/migrations/0027_poller_tenant_enumeration.up.sql",
	"internal/db/system_tenant_lister.go",
}

// TestSystemFlag_OnlySetByItsDedicatedOwner is the structural half of
// AD-024's guarantee: the policy is only as safe as the rule that no HTTP
// request can influence app.is_system. That rule is a code-organization
// property, so it is asserted structurally - if any production file
// besides the dedicated owner ever mentions the flag (an API handler, a
// middleware, the router, the TLS manager, a CLI command), this fails and
// the decision gets revisited instead of silently eroding.
//
// Test files are exempt: they cannot run in production, and the
// integration tests asserting the flag's absence on real request paths
// have to name it to assert on it.
func TestSystemFlag_OnlySetByItsDedicatedOwner(t *testing.T) {
	root := repoRoot(t)

	var found []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if info.IsDir() {
			switch rel {
			case ".git", "web", "node_modules", ".specs":
				return filepath.SkipDir
			}
			return nil
		}

		ext := filepath.Ext(rel)
		if ext != ".go" && ext != ".sql" {
			return nil
		}
		if strings.HasSuffix(rel, "_test.go") {
			return nil
		}

		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if mentionsOutsideComments(string(content), systemFlagName) {
			found = append(found, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking repository returned unexpected error: %v", err)
	}

	sort.Strings(found)
	want := append([]string(nil), systemFlagOwners...)
	sort.Strings(want)

	if strings.Join(found, ",") != strings.Join(want, ",") {
		t.Errorf("production files mentioning %s = %v, want exactly %v - the flag must never be reachable from an HTTP-facing path (AD-024)",
			systemFlagName, found, want)
	}
}

// mentionsOutsideComments reports whether name appears in executable
// content rather than only in prose about it. Line comments ("//" for Go,
// "--" for SQL) are stripped first, so a doc comment explaining the flag
// (serve.go, tenant_repository.go) does not count as reaching it. This is
// a deliberate approximation: neither language's block comments nor
// string literals containing a comment marker occur in the files it
// scans, and a false positive here fails loudly rather than silently.
func mentionsOutsideComments(content, name string) bool {
	for _, line := range strings.Split(content, "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		if i := strings.Index(line, "--"); i >= 0 {
			line = line[:i]
		}
		if strings.Contains(line, name) {
			return true
		}
	}
	return false
}

// repoRoot walks up from the package directory until it finds go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() returned unexpected error: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate repository root (no go.mod above %s)", dir)
		}
		dir = parent
	}
}
