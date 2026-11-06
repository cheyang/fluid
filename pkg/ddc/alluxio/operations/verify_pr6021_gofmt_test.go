/*
Verification harness for PR #6021 review (Reviewer B / Codex).
ADDITIVE ONLY: this file does not modify any production or existing test code.

Polarity:
  - TestVerifyPR6021_BaseTestFileGofmtClean   [contract] — FAILS on the PR head
    (reproduces finding F1: the PR leaves base_test.go not gofmt-clean, which
    fails the mandatory "Code fmt check" step of the lint job in
    .github/workflows/project-check.yml). Must PASS once the author runs
    `gofmt -w pkg/ddc/alluxio/operations/base_test.go` (or `make fmt`).
  - TestVerifyPR6021_ReportSummaryCommentPresent [contract] — PASSES already on
    the PR head; documents that the PR's stated intent (a doc comment on
    TestAlluxioFileUtils_ReportSummary) is present, so a correct fix keeps it
    while restoring gofmt cleanliness.
*/

package operations

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func gofmtBinary(t *testing.T) string {
	t.Helper()
	if p, err := exec.LookPath("gofmt"); err == nil {
		return p
	}
	p := filepath.Join(runtime.GOROOT(), "bin", "gofmt")
	if _, err := exec.LookPath(p); err == nil {
		return p
	}
	t.Skip("gofmt binary not found on PATH or in GOROOT/bin")
	return ""
}

// TestVerifyPR6021_BaseTestFileGofmtClean asserts the intended repo invariant
// enforced by CI (.github/workflows/project-check.yml, lint job, "Code fmt
// check" step: `gofmt -l ./pkg ./cmd ./api` must be empty). Tests in this
// package run with the working directory set to the package directory.
func TestVerifyPR6021_BaseTestFileGofmtClean(t *testing.T) {
	gofmt := gofmtBinary(t)
	out, err := exec.Command(gofmt, "-l", "base_test.go").CombinedOutput()
	if err != nil {
		t.Fatalf("running gofmt -l failed: %v (output: %s)", err, string(out))
	}
	if files := strings.TrimSpace(string(out)); files != "" {
		t.Errorf("gofmt -l lists %q: base_test.go is not gofmt-clean; "+
			"the lint job's 'Code fmt check' step exits 1 on this. "+
			"Fix: `gofmt -w pkg/ddc/alluxio/operations/base_test.go` "+
			"(de-indent the new comment block and the func line to column 0)", files)
	}
}

// TestVerifyPR6021_ReportSummaryCommentPresent documents the PR's intent so a
// fix that simply deletes the comment is also caught.
func TestVerifyPR6021_ReportSummaryCommentPresent(t *testing.T) {
	out, err := exec.Command("grep", "-c",
		"TestAlluxioFileUtils_ReportSummary tests the AlluxioFileUtils.ReportSummary method",
		"base_test.go").CombinedOutput()
	if err != nil {
		t.Fatalf("doc comment for TestAlluxioFileUtils_ReportSummary missing: %v (%s)", err, string(out))
	}
}
