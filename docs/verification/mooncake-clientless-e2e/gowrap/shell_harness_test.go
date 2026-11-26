// Verification harness for PR #6286 (reviewer: codex). ADDITIVE ONLY.
// Wraps ../scripts/run-shell-harness.sh so the stock re-verify.sh (which only
// speaks gotest/ginkgo) can drive the shell layer: each PASS:/FAIL: case line
// becomes a Go subtest named after its case ID (A1, B, C, D, E, F1..F6, G1..G3).
package gowrap

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestShellHarness(t *testing.T) {
	script, err := filepath.Abs("../scripts/run-shell-harness.sh")
	if err != nil {
		t.Fatal(err)
	}
	out, runErr := exec.Command("bash", script).CombinedOutput()
	t.Logf("shell harness output:\n%s", out)
	ran := 0
	for _, line := range strings.Split(string(out), "\n") {
		var caseOK bool
		var rest string
		if strings.HasPrefix(line, "PASS: ") {
			caseOK, rest = true, strings.TrimPrefix(line, "PASS: ")
		} else if strings.HasPrefix(line, "FAIL: ") {
			caseOK, rest = false, strings.TrimPrefix(line, "FAIL: ")
		} else {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		id := fields[0]
		name := strings.NewReplacer(" ", "_", "/", "_").Replace(rest)
		ran++
		t.Run(id+"_"+name, func(t *testing.T) {
			if !caseOK {
				t.Fatalf("shell case %s failed", id)
			}
		})
	}
	if ran == 0 {
		t.Fatal("shell harness produced no PASS/FAIL case lines")
	}
	if runErr != nil {
		t.Fatalf("shell harness exited non-zero: %v", runErr)
	}
}
