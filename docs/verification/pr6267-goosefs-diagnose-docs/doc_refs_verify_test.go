// Verification harness for fluid PR #6267 (docs: drop the removed GooseFS
// diagnose script from troubleshooting). Additive only — no production code.
//
// Polarity: both tests are CONTRACT tests. They FAIL on the base branch
// (dcb76167, where the docs still point at the removed
// tools/diagnose-fluid-goosefs.sh) and PASS on the PR head.
package verify

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// repoRoot resolves the repository root from this file's location
// (docs/verification/pr6267-goosefs-diagnose-docs).
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("cannot locate repo root from %s: %v", wd, err)
	}
	return root
}

var troubleshootingDocs = []string{
	"docs/en/userguide/troubleshooting.md",
	"docs/zh/userguide/troubleshooting.md",
}

var diagScriptRef = regexp.MustCompile(`tools/diagnose-fluid-[a-z0-9-]*\.sh`)

// TestTroubleshootingDocsReferenceExistingScripts is the premise/fix contract:
// every diagnose-fluid-*.sh URL referenced by the troubleshooting docs must
// resolve to a script that actually ships in tools/. On the base branch the
// docs reference tools/diagnose-fluid-goosefs.sh, which was removed by
// #5703, so the documented `wget` returns 404 and this test FAILS there.
func TestTroubleshootingDocsReferenceExistingScripts(t *testing.T) {
	root := repoRoot(t)
	seen := map[string]bool{}
	for _, doc := range troubleshootingDocs {
		content, err := os.ReadFile(filepath.Join(root, doc))
		if err != nil {
			t.Fatalf("read %s: %v", doc, err)
		}
		refs := diagScriptRef.FindAllString(string(content), -1)
		if len(refs) == 0 {
			t.Fatalf("%s: no diagnose-fluid-*.sh references found; harness broken?", doc)
		}
		for _, ref := range refs {
			if seen[doc+"|"+ref] {
				continue
			}
			seen[doc+"|"+ref] = true
			if _, err := os.Stat(filepath.Join(root, ref)); err != nil {
				t.Errorf("%s references %s, but that script does not exist in the repo (wget would 404)", doc, ref)
			}
		}
	}
}

// TestNoStaleGooseFSDiagnoseReference pins the actual PR change: after the
// GooseFS runtime was deprecated (#5703) and its diagnose script removed, no
// Fluid doc may still tell users to download diagnose-fluid-goosefs.sh.
func TestNoStaleGooseFSDiagnoseReference(t *testing.T) {
	root := repoRoot(t)
	for _, doc := range troubleshootingDocs {
		content, err := os.ReadFile(filepath.Join(root, doc))
		if err != nil {
			t.Fatalf("read %s: %v", doc, err)
		}
		if strings.Contains(string(content), "diagnose-fluid-goosefs") {
			t.Errorf("%s still references the removed diagnose-fluid-goosefs.sh", doc)
		}
	}
}
