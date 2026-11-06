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

// Verification harness for fluid PR #6088 (reviewer: codex).
// ADDITIVE ONLY — this file is part of the review harness on branch
// verify/pr6088-gofmt-comment-codex and is not proposed for merge.

package alluxio

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestVerifyPR6088TouchedFilesGofmtClean is a CONTRACT test for finding F1:
// every Go file touched by PR #6088 must be gofmt-clean, because the lint job
// of .github/workflows/project-check.yml runs
//
//	gofmt -l ./pkg ./cmd ./api | grep . && ... && exit 1
//
// so any listed file fails CI. On the PR head (e23c7a04) this test FAILS:
// the patch deletes the blank line between getMasterPodInfo's closing brace
// and the new getMasterName doc comment, and gofmt re-inserts that blank line
// (verified with the CI toolchain go1.24.12 and with go1.27.0).
// Once the author runs `make fmt` (restores the blank line), this test PASSES.
func TestVerifyPR6088TouchedFilesGofmtClean(t *testing.T) {
	gofmt := filepath.Join(runtime.GOROOT(), "bin", "gofmt")
	if _, err := exec.LookPath(gofmt); err != nil {
		t.Skipf("gofmt not found at %s", gofmt)
	}

	// Files touched by PR #6088 (go test runs with cwd = package dir).
	touched := []string{"utils.go"}

	out, err := exec.Command(gofmt, append([]string{"-l"}, touched...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("gofmt -l failed to run: %v, output: %s", err, string(out))
	}
	flagged := strings.TrimSpace(string(out))
	if flagged != "" {
		t.Fatalf("F1: files touched by PR #6088 are not gofmt-clean (CI 'Code fmt check' fails):\n%s\nrun `make fmt` to fix", flagged)
	}
}
