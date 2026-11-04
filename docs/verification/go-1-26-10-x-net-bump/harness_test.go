// Package verify harness for PR #6227 (Go 1.26.10 / x/net v0.62.0 bump).
// Additive review artifact: wraps the shell contracts in scripts/ so the
// review-finding-verifier re-verify.sh (gotest layer) can drive them.
// Run from the repository root: go test ./docs/verification/go-1-26-10-x-net-bump/
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

// C1: Dockerfile golang pins match the live multi-arch index digest of their tag.
func TestVerifyImageDigest(t *testing.T) { runScript(t, "check-image-digest.sh") }

// C2: build + vet + go mod verify clean under the pinned toolchain.
func TestVerifyBuildVet(t *testing.T) { runScript(t, "check-build-vet.sh") }

// C5: govulncheck head contract — zero reachable stdlib / golang.org/x/net findings.
func TestVerifyGovulncheckHead(t *testing.T) { runScript(t, "check-govulncheck.sh", "head") }

// C6: urlstrictcolons behavior delta pinned; url.Parse sites confined to juicefs data_migrate.go.
func TestVerifyURLStrictColons(t *testing.T) { runScript(t, "check-urlstrictcolons.sh") }

// C3 (integration layer, long): CI-equivalent unit suite with gomonkey gcflags.
func TestVerifyUnitTests(t *testing.T) { runScript(t, "check-unit-tests.sh") }

// C4 (integration layer): make update-crd / gen-openapi diff-free.
func TestVerifyCodegenDiff(t *testing.T) { runScript(t, "check-codegen-diff.sh") }
