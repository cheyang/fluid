// Package verify is the review harness for PR #6229
// (chore(deps): bump container-storage-interface/spec 1.8.0 -> 1.13.0).
// Additive review artifact: wraps the shell contracts in scripts/ so
// re-verify.sh (gotest layer) can drive them.
// Run from the repository root: go test ./docs/verification/csi-spec-1-13-bump/
package verify

import (
	"os/exec"
	"testing"
)

func runScript(t *testing.T, script string, args ...string) {
	t.Helper()
	full := append([]string{"scripts/" + script}, args...)
	cmd := exec.Command("bash", full...)
	out, err := cmd.CombinedOutput()
	t.Logf("%s output:\n%s", script, out)
	if err != nil {
		t.Fatalf("%s %v failed: %v", script, args, err)
	}
}

// C1: fluid CSI plugin chain compiles (go build ./pkg/csi/... ./cmd/csi/...).
func TestVerifyBuildCSI(t *testing.T) { runScript(t, "check-build.sh") }

// C2: go vet over CSI packages passes (CI `make vet` equivalent subset).
func TestVerifyVetCSI(t *testing.T) { runScript(t, "check-vet.sh") }

// C3: interface-satisfaction contract test in pkg/csi/plugins compiles and passes.
func TestVerifyInterfaceContract(t *testing.T) { runScript(t, "check-interface-contract.sh") }

// C4: existing CSI unit tests compile and pass.
func TestVerifyCSIUnitTests(t *testing.T) { runScript(t, "check-unit-tests.sh") }
