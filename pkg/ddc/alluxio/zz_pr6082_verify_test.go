// Verification harness for PR #6082 (review deliverable, additive only).
// These tests wrap the shell checks in
// docs/verification/pr6082-ufs-comments/scripts/ so that
// docs/verification/pr6082-ufs-comments/scripts/re-verify.sh (gotest layer)
// can re-run them against any future head of the PR.
package alluxio

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func pr6082RepoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test file")
	}
	// <root>/pkg/ddc/alluxio/<this file> -> <root>
	return filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(thisFile))))
}

func pr6082MergeBase(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "merge-base", "origin/master", "HEAD").Output()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		// fall back to the merge-base recorded at review time
		return "8cda1f984627e75d1706e732723b1f9dc4d3c455"
	}
	return strings.TrimSpace(string(out))
}

func pr6082RunScript(t *testing.T, script string, args ...string) (string, error) {
	t.Helper()
	root := pr6082RepoRoot(t)
	full := filepath.Join(root, "docs", "verification", "pr6082-ufs-comments", "scripts", script)
	cmd := exec.Command("bash", append([]string{full}, args...)...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// Contract: the PR diff on pkg/ddc/alluxio/ufs_internal.go is comment-only
// (every added line is a Go line comment; no line is removed).
func TestPR6082CommentOnlyDiff(t *testing.T) {
	out, err := pr6082RunScript(t, "check_comment_only_diff.sh", pr6082MergeBase(t), "HEAD")
	t.Log(out)
	if err != nil {
		t.Fatalf("comment-only contract violated: %v\n%s", err, out)
	}
}

// Contract: every commit of the PR carries a Signed-off-by trailer (DCO).
// Reproduces the failing DCO check on the current head (fails until the
// author re-pushes with `git commit --amend -s`).
func TestPR6082DCOSignoff(t *testing.T) {
	out, err := pr6082RunScript(t, "check_dco.sh", pr6082MergeBase(t), "HEAD")
	t.Log(out)
	if err != nil {
		t.Fatalf("DCO sign-off missing: %v\n%s", err, out)
	}
}
