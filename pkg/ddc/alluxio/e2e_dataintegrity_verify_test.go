/*
Copyright 2024 The Fluid Authors.

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

// Additive verification harness for the review of
// https://github.com/fluid-cloudnative/fluid/pull/6061. No production code is
// modified. See docs/verification/alluxio-scaledown-e2e/.

package alluxio

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func e2eRepoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve caller path")
	}
	// pkg/ddc/alluxio/<file> -> up 3 dirs to repo root
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
}

func readE2EFile(t *testing.T, rel string) string {
	t.Helper()
	p := filepath.Join(e2eRepoRoot(t), rel)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("cannot read %s: %v", p, err)
	}
	return string(b)
}

// FINDING F1 — canary (PASSES while the gap exists; revise when a real
// data-integrity check is added).
//
// The e2e is meant to prove graceful scale-down doesn't lose data (that was the
// stated reason PR #5805 was reverted in #6059). But both read jobs just
// `cat /data/scaledown/fixture.txt` over a dataset mounted from an S3/minio UFS.
// On a cache miss Alluxio transparently re-fetches from the UFS, so the
// after-scaledown read succeeds whether or not graceful decommission preserved
// any cached block. An ungraceful scale-down (or the F2 bug degrading to one)
// would pass the identical test. This test encodes that gap: the two read jobs
// are functionally identical UFS-backed reads, the dataset is UFS-backed, and
// nothing in test.sh makes the UFS unavailable between the two reads — so the
// e2e cannot distinguish graceful from ungraceful scale-down.
func TestVerifyF1E2ECannotDetectDataLoss(t *testing.T) {
	base := "test/gha-e2e/alluxio-scaledown"
	dataset := readE2EFile(t, base+"/dataset.yaml")
	before := readE2EFile(t, base+"/read_before_job.yaml")
	after := readE2EFile(t, base+"/read_after_job.yaml")
	testsh := readE2EFile(t, base+"/test.sh")

	// 1. The dataset is UFS(S3)-backed, so any cached data is always
	//    recoverable from the UFS regardless of worker loss.
	if !strings.Contains(dataset, "s3://") {
		t.Fatalf("assumption broke: dataset no longer S3/UFS-backed; F1 reasoning needs revisiting")
	}

	// 2. Extract each read job's shell command and confirm both are the same
	//    plain `cat` + string-compare over the mounted UFS path (i.e. the
	//    after-read exercises nothing the before-read didn't).
	readCmd := "cat /data/scaledown/fixture.txt"
	if !strings.Contains(before, readCmd) || !strings.Contains(after, readCmd) {
		t.Fatalf("assumption broke: read jobs no longer both a plain cat of the fixture; F1 reasoning needs revisiting")
	}
	// Neither job forces a cache-only read or checks a cache-hit metric; they
	// only assert the file content, which the UFS can always satisfy.
	for _, marker := range []string{"cache", "metric", "MUST_CACHE", "hit"} {
		if strings.Contains(strings.ToLower(after), strings.ToLower(marker)) {
			t.Errorf("F1 canary flipped: read_after now references %q — it may actually test cache preservation; revise F1", marker)
		}
	}

	// 3. Nothing between the two reads makes the UFS unavailable (which is the
	//    only way a plain re-read could distinguish real cache preservation
	//    from UFS fallback). If the test ever deletes/scales minio before the
	//    after-read, this canary should flip and F1 be revised.
	afterReadIdx := strings.Index(testsh, "read_after_job.yaml")
	scaleDownIdx := strings.Index(testsh, "scale_down")
	if afterReadIdx < 0 || scaleDownIdx < 0 {
		t.Fatalf("assumption broke: test.sh structure changed; F1 reasoning needs revisiting")
	}
	between := testsh[scaleDownIdx:afterReadIdx]
	for _, ufsKill := range []string{"delete -f test/gha-e2e/alluxio-scaledown/minio.yaml", "scale deployment/scaledown-minio", "delete deployment/scaledown-minio"} {
		if strings.Contains(between, ufsKill) {
			t.Errorf("F1 canary flipped: test.sh now disrupts the UFS (%q) before the after-read — it may genuinely test data integrity; revise F1", ufsKill)
		}
	}

	t.Log("F1 CONFIRMED: the e2e reads UFS-backed data identically before/after scale-down with the UFS " +
		"always available, so a passing read_after cannot prove graceful decommission preserved any block — " +
		"an ungraceful scale-down would pass the same test. Combined with F2 (drain no-ops) and the " +
		"deadline-forced ungraceful fallback, the e2e provides false confidence in the reverted-for concern.")
}
