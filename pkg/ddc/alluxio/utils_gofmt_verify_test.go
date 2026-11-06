package alluxio

import (
	"os/exec"
	"strings"
	"testing"
)

// TestVerifyGofmtCleanAlluxio is the F1 contract test for PR #6088 review.
//
// Contract: every .go file in pkg/ddc/alluxio must be gofmt-clean, mirroring the
// CI "lint" job's "Code fmt check" step (`gofmt -l ./pkg ./cmd ./api | grep .`
// -> exit 1). PR #6088 removed the blank line between getMasterPodInfo and the
// new doc comment on getMasterName, which gofmt (any version >= CI's 1.24.12)
// flags. Polarity: CONTRACT — FAILS on the buggy head, PASSES once the blank
// line is restored (or `gofmt -w` is applied).
func TestVerifyGofmtCleanAlluxio(t *testing.T) {
	gofmt, err := exec.LookPath("gofmt")
	if err != nil {
		t.Skipf("gofmt not found in PATH: %v", err)
	}
	out, err := exec.Command(gofmt, "-l", ".").CombinedOutput()
	if err != nil {
		t.Fatalf("gofmt -l failed: %v\n%s", err, out)
	}
	dirty := strings.TrimSpace(string(out))
	if dirty != "" {
		t.Errorf("gofmt reports unformatted files (CI 'Code fmt check' step will fail the lint job):\n%s\n"+
			"fix: restore the blank line before the getMasterName doc comment, or run `gofmt -w pkg/ddc/alluxio`", dirty)
	}
}
