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

package alluxio

// Reviewer-A (Claude) verification harness for PR #6061
// (https://github.com/fluid-cloudnative/fluid/pull/6061).
//
// Additive tests only — no production code is modified on this branch.
// Deliberately gomonkey-free so the harness runs on any platform.
//
// Claims covered (see docs/verification/alluxio-graceful-scaledown-claude/):
//   - G1 (contract): GracefulWorkerScaleDown is registered on the global
//     feature gate and settable through the same --feature-gates flag the
//     alluxioruntime-controller already exposes (cmd/alluxio calls
//     utilfeature.DefaultMutableFeatureGate.AddFlag). Proves the PR-body
//     claim that the gate is reachable in a real deployment (although via
//     the import chain, not via any cmd/ change in this diff).
//     Negative control: an unregistered gate name must fail flag parsing.
//   - G2 (contract): workerWebPortFromPod returns the web port the
//     alluxio-worker container actually exposes (the fix for the earlier
//     "decommission targets spec/default port 30000 while host-network
//     workers bind a dynamically allocated port" finding).
//   - G3 (contract): the e2e script makes the post-scale-down read
//     cache-only by tearing the UFS (minio) down between the two read
//     jobs (the fix for the earlier "read_after can pass via UFS" finding).
//   - G4 (canary): the e2e script still has NO failure-enforcing assertion
//     that the graceful decommission path actually ran (as opposed to the
//     10-minute deadline fallback). PASSES while the gap is present; must
//     be inverted once the test asserts the WorkerDecommissioning
//     condition / drain actually happened.

import (
	"os"
	"strings"
	"testing"

	"github.com/fluid-cloudnative/fluid/pkg/features"
	utilfeature "github.com/fluid-cloudnative/fluid/pkg/utils/feature"
	"github.com/spf13/pflag"
	corev1 "k8s.io/api/core/v1"
)

// G1: the gate must be registered on the shared global gate and reachable
// through --feature-gates, exactly the flag the alluxioruntime-controller
// binary already wires up (cmd/alluxio/app/alluxio.go: AddFlag). The gate's
// registration reaches that binary through the import chain
// cmd/alluxio -> ... -> pkg/ddc/alluxio -> pkg/features (verified via
// `go list -deps ./cmd/alluxio`).
func TestVerifyClaudeG1GracefulWorkerScaleDownGateRegisteredAndFlagParseable(t *testing.T) {
	// Default must be off (Alpha).
	if utilfeature.DefaultFeatureGate.Enabled(features.GracefulWorkerScaleDown) {
		t.Fatalf("GracefulWorkerScaleDown should default to disabled (Alpha gate)")
	}

	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	utilfeature.DefaultMutableFeatureGate.AddFlag(fs)
	if err := fs.Parse([]string{"--feature-gates=GracefulWorkerScaleDown=true"}); err != nil {
		t.Fatalf("failed to parse --feature-gates=GracefulWorkerScaleDown=true (gate not registered?): %v", err)
	}
	if !utilfeature.DefaultFeatureGate.Enabled(features.GracefulWorkerScaleDown) {
		t.Fatalf("gate should be enabled after parsing --feature-gates=GracefulWorkerScaleDown=true")
	}

	// Negative control (proves the test bites): an unregistered gate name
	// must be rejected by the same parse path.
	fs2 := pflag.NewFlagSet("test-neg", pflag.ContinueOnError)
	utilfeature.DefaultMutableFeatureGate.AddFlag(fs2)
	if err := fs2.Parse([]string{"--feature-gates=ClaudeNoSuchGate=true"}); err == nil {
		t.Fatalf("parsing an unregistered feature gate should have failed; the negative control does not bite")
	}

	// Restore so other tests in this package see the default state.
	_ = utilfeature.DefaultMutableFeatureGate.Set("GracefulWorkerScaleDown=false")
}

// G2: decommission addresses must be derived from the port the worker pod's
// own alluxio-worker container exposes, not the runtime spec / default.
func TestVerifyClaudeG2WorkerWebPortComesFromPodContainerSpec(t *testing.T) {
	cases := []struct {
		name string
		pod  *corev1.Pod
		want int
		ok   bool
	}{
		{
			name: "dynamically allocated host-network port",
			pod: &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{
				{Name: "alluxio-worker", Ports: []corev1.ContainerPort{{Name: "web", ContainerPort: 20481}}},
			}}},
			want: 20481,
			ok:   true,
		},
		{
			name: "job-worker's differently numbered web port is ignored",
			pod: &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{
				{Name: "alluxio-job-worker", Ports: []corev1.ContainerPort{{Name: "web", ContainerPort: 21000}}},
				{Name: "alluxio-worker", Ports: []corev1.ContainerPort{{Name: "web", ContainerPort: 20482}}},
			}}},
			want: 20482,
			ok:   true,
		},
		{
			name: "no web port on alluxio-worker",
			pod: &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{
				{Name: "alluxio-job-worker", Ports: []corev1.ContainerPort{{Name: "web", ContainerPort: 21000}}},
			}}},
			want: 0,
			ok:   false,
		},
	}
	for _, c := range cases {
		got, ok := workerWebPortFromPod(c.pod)
		if ok != c.ok || got != c.want {
			t.Errorf("%s: workerWebPortFromPod = (%d, %v), want (%d, %v)", c.name, got, ok, c.want, c.ok)
		}
	}
}

// G3 (contract): the e2e must make the post-scale-down read provable only
// from cache by removing the UFS between the two reads.
func TestVerifyClaudeG3E2eTearsDownUFSAroundScaleDownReads(t *testing.T) {
	// Anchor on the main()-sequence call sites, not the cleanup function,
	// which also references the job yamls earlier in the file.
	sh := readE2eScript(t)
	readBefore := strings.Index(sh, "create_job test/gha-e2e/alluxio-scaledown/read_before_job.yaml")
	teardown := strings.Index(sh, "\n    teardown_minio\n")
	scaleDown := strings.Index(sh, "\n    scale_down\n")
	readAfter := strings.Index(sh, "create_job test/gha-e2e/alluxio-scaledown/read_after_job.yaml")
	if readBefore < 0 || teardown < 0 || scaleDown < 0 || readAfter < 0 {
		t.Fatalf("e2e script is missing one of the expected steps (read_before=%d teardown_minio=%d scale_down=%d read_after=%d)", readBefore, teardown, scaleDown, readAfter)
	}
	if !(readBefore < teardown && teardown < scaleDown && scaleDown < readAfter) {
		t.Errorf("expected order read_before < teardown_minio < scale_down < read_after; got %d < %d < %d < %d", readBefore, teardown, scaleDown, readAfter)
	}
}

// G4 (BUG-CANARY, polarity: canary): asserts the CURRENT state of the e2e —
// it observes the WorkerDecommissioning condition but never fails when the
// graceful path did not run. Passes while the gap is present. When the e2e
// gains a failure-enforcing assertion on the graceful path (panic/exit on a
// missing WorkerDecommissioning condition, or equivalent), this canary
// FLIPS TO RED and must be inverted (or replaced by a contract test
// asserting the enforcement).
func TestVerifyClaudeG4E2eDoesNotAssertGracefulPathRan(t *testing.T) {
	sh := readE2eScript(t)
	for _, line := range strings.Split(sh, "\n") {
		l := strings.ToLower(line)
		if strings.Contains(l, "decommission") && (strings.Contains(l, "panic") || strings.Contains(l, "exit 1")) {
			t.Fatalf("e2e now appears to enforce the decommission path (line: %q) — invert this canary into a contract test", line)
		}
	}
}

func readE2eScript(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../../../test/gha-e2e/alluxio-scaledown/test.sh")
	if err != nil {
		t.Fatalf("cannot read e2e script: %v", err)
	}
	return string(b)
}
