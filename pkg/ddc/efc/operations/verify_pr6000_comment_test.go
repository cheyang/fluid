/*
Copyright 2026 The Fluid Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Verification harness for the review of
// https://github.com/fluid-cloudnative/fluid/pull/6000 (adds a doc comment to
// TestOperations in this package). Additive only: it does not change any
// production or test behavior of the package under review.
//
// Polarity: all tests here are CONTRACT tests. They must PASS on the PR head;
// a failure means the PR's deliverable regressed (comment removed/altered) or
// the PR started introducing new gofmt violations.
package operations

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot walks up from the package test working directory until it finds
// go.mod, so the differential gofmt check can run from the repository root
// exactly like the CI "Code fmt check" step does.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("harness error: cannot get cwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("harness error: go.mod not found above %s", dir)
		}
		dir = parent
	}
}

// docCommentAbove returns the contiguous // comment block immediately
// preceding the line that starts with the given func signature.
func docCommentAbove(t *testing.T, srcFile, funcSig string) string {
	t.Helper()
	data, err := os.ReadFile(srcFile)
	if err != nil {
		t.Fatalf("harness error: cannot read %s: %v", srcFile, err)
	}
	lines := strings.Split(string(data), "\n")
	funcLine := -1
	for i, l := range lines {
		if strings.HasPrefix(l, funcSig) {
			funcLine = i
			break
		}
	}
	if funcLine < 0 {
		t.Fatalf("harness error: %q not found in %s", funcSig, srcFile)
	}
	var block []string
	for i := funcLine - 1; i >= 0 && strings.HasPrefix(strings.TrimSpace(lines[i]), "//"); i-- {
		block = append([]string{lines[i]}, block...)
	}
	return strings.Join(block, "\n")
}

// TestPR6000DocCommentPresent is a contract test for the PR's deliverable:
// issue #5999 asks for a doc comment on TestOperations describing that it is
// the Ginkgo suite entry point, registers the Gomega fail handler, and runs
// the specs under the suite name "Operations Suite". If a future revision of
// the PR drops or guts the comment, this test fails.
func TestPR6000DocCommentPresent(t *testing.T) {
	comment := docCommentAbove(t, "operations_suite_test.go", "func TestOperations(")
	if comment == "" {
		t.Fatalf("TestOperations has no doc comment; PR #6000's deliverable is missing")
	}
	for _, want := range []string{
		"TestOperations is the entry point for the Ginkgo test suite",
		"registers the Gomega fail handler",
		`"Operations Suite"`,
	} {
		if !strings.Contains(comment, want) {
			t.Errorf("doc comment above TestOperations lacks %q.\nActual comment block:\n%s", want, comment)
		}
	}
}

// TestPR6000ChangedFileGofmtClean is a contract test for CI attribution F1:
// the only file the PR changes must itself be gofmt-clean, replicating the
// CI "Code fmt check" step (`gofmt -l`) restricted to the changed file.
func TestPR6000ChangedFileGofmtClean(t *testing.T) {
	if _, err := exec.LookPath("gofmt"); err != nil {
		t.Fatalf("harness error: gofmt not found in PATH: %v", err)
	}
	out, err := exec.Command("gofmt", "-l", "operations_suite_test.go").CombinedOutput()
	if err != nil {
		t.Fatalf("harness error: gofmt run failed: %v (%s)", err, out)
	}
	if flagged := strings.TrimSpace(string(out)); flagged != "" {
		t.Errorf("PR-changed file is flagged by gofmt -l: %s", flagged)
	}
}

// gofmtFlagged runs `gofmt -l ./pkg ./cmd ./api` from the repo root (the exact
// CI invocation) and returns the set of flagged files.
func gofmtFlagged(t *testing.T, root string) map[string]bool {
	t.Helper()
	cmd := exec.Command("gofmt", "-l", "./pkg", "./cmd", "./api")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("harness error: gofmt -l failed: %v (%s)", err, out)
	}
	flagged := map[string]bool{}
	for _, f := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f = strings.TrimSpace(f)
		if f != "" {
			flagged[filepath.ToSlash(f)] = true
		}
	}
	return flagged
}

// baselineGofmtFailures is the set of files `gofmt -l ./pkg ./cmd ./api`
// flags at the PR's merge-base da69206da6388e46a8e9e32834d6b22e53fe14a9,
// captured with the exact CI Go toolchain (go1.24.12) on 2026-11-06. All
// three are doc comments with trailing whitespace introduced by earlier
// merged comment PRs (#5959/#5960/#5962) — they are the pre-existing reason
// the lint job's "Code fmt check" step fails on this PR. Refresh with
// docs/verification/pr6000-efc-operations-suite-comment/scripts/check-gofmt-attribution.sh.
var baselineGofmtFailures = []string{
	"pkg/ddc/alluxio/ufs_internal.go",
	"pkg/ddc/base/runtime.go",
	"pkg/ddc/thin/transform_fuse.go",
}

// TestPR6000NoNewGofmtFailuresVsBase is the differential contract test behind
// finding F1: the PR must not add any NEW gofmt-flagged file beyond the
// pre-existing baseline set. It passes on both base and head of the round-1
// review (head set == base set), which is the evidence that the red lint CI
// job is baseline-inherited, not PR-caused. If the PR is rebased onto a
// gofmt-clean master, the flagged set shrinks and the test still passes;
// if a future revision adds an unformatted file, the test fails and names it.
func TestPR6000NoNewGofmtFailuresVsBase(t *testing.T) {
	if _, err := exec.LookPath("gofmt"); err != nil {
		t.Fatalf("harness error: gofmt not found in PATH: %v", err)
	}
	flagged := gofmtFlagged(t, repoRoot(t))
	allowed := map[string]bool{}
	for _, f := range baselineGofmtFailures {
		allowed[f] = true
	}
	for f := range flagged {
		if !allowed[f] {
			t.Errorf("NEW gofmt failure not present at merge-base da69206: %s", f)
		}
		if f == "pkg/ddc/efc/operations/operations_suite_test.go" {
			t.Errorf("the file this PR changes must never be gofmt-flagged: %s", f)
		}
	}
	t.Logf("gofmt-flagged files at HEAD (all pre-existing at base): %v", flagged)
}
