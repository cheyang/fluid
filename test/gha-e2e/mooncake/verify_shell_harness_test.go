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

package mooncake

// Wrappers that run the shell harnesses of
// docs/verification/mooncake-client-less-e2e/scripts/ as go tests, so the
// re-verify pipeline can consume them through the gotest layer.

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func verifyScript(t *testing.T, name string) (string, error) {
	t.Helper()
	script := filepath.Join(repoRoot(t), "docs", "verification", "mooncake-client-less-e2e", "scripts", name)
	out, err := exec.Command("bash", script).CombinedOutput()
	return string(out), err
}

// Contract: reportSummary.sh must parse the master metrics summary into the
// JSON shape Fluid expects.
func TestReportSummaryParsingHarness_Verify(t *testing.T) {
	out, err := verifyScript(t, "verify-report-summary.sh")
	t.Log(out)
	if err != nil {
		t.Fatalf("reportSummary harness failed: %v", err)
	}
}

// Contract: custom-entrypoint.sh (worker role) must turn the controller's
// runtime.json into the right mooncake_client invocation.
func TestWorkerEntrypointHarness_Verify(t *testing.T) {
	out, err := verifyScript(t, "verify-entrypoint-worker.sh")
	t.Log(out)
	if err != nil {
		t.Fatalf("entrypoint harness failed: %v", err)
	}
}

// Canary for F4: passes while wait_runtime_deleted reports GC success even
// when every kubectl call fails; flips to fail once the scripts stop
// swallowing kubectl errors.
func TestWaitRuntimeDeletedVacuousPass_Canary(t *testing.T) {
	out, err := verifyScript(t, "verify-wait-runtime-deleted-canary.sh")
	t.Log(out)
	if err != nil {
		t.Fatalf("canary flipped (finding fixed): %v", err)
	}
}

// Canary for F2: passes while check_pvc_not_mountable's FailedMount query is
// not scoped to the pod's UID; flips to fail once it is.
func TestFailedMountEventQueryUIDScoped_Canary(t *testing.T) {
	out, err := verifyScript(t, "verify-failedmount-uid-scope.sh")
	t.Log(out)
	if err != nil {
		t.Fatalf("canary flipped (finding fixed): %v", err)
	}
}
