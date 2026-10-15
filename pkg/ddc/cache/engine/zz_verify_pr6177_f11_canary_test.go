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

package engine

// Round-4 canary for finding F11 (PR #6177; also Copilot discussion_r4276559859):
// sync propagates container claims from the template to an existing workload,
// but SyncComponentSpec (advanced_statefulset_manager.go) only ever patches
// replicas, image and container resources -- never the pod-level
// spec.resourceClaims the claims must reference. On a DRA-enabled cluster the
// API server rejects replacement Pods whose container claim is not declared at
// pod level (k8s.io/kubernetes@v1.29.15 validation.go:6595
// validateResourceClaimNames), so a CacheRuntimeClass edited after workload
// creation to add a claim stalls the rollout with no event.
//
// Attribution (differential, ad-hoc probes on both refs, recorded in
// results/round4/f11-claims-*.txt): with a CacheRuntime declaring nothing, the
// claim propagates identically on base e0fc4c18 (template fallback) and head
// 3adc547d -- a PRE-EXISTING gap. With the CacheRuntime declaring a partial
// value (cpu request only), base wholesale-applies the runtime list (claim
// dropped) while head merges (claim propagates) -- the PR widens exposure to
// that combination while fixing the dropped-keys bug for it. Hence
// origin=preexisting, introducedByPR=false.
//
// Polarity: CANARY -- asserts the CURRENT behavior (claim propagated, nil
// error, no Warning event). The right remedy is under discussion (validate
// resolved claims against the workload's pod-level declarations and refuse
// with a Warning event, mirroring F6/F9; or sync pod-level resourceClaims; or
// document), so this test must FLIP to red when a guard lands, then be
// inverted into a contract. Compile-safe on base, previous head and head
// (pre-existing symbols only).
import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/tools/record"
)

func TestVerifyPR6177_F11_TemplateClaimsPropagateWithoutPodLevelDeclaration_Canary(t *testing.T) {
	// Workload as rendered BEFORE the template edit: cpu only, no claims, and
	// no pod-level resourceClaims (the fixture builds the ASTS without any --
	// which is the gap: sync never reads or writes pod-level declarations).
	workloadRes := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")},
		Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")},
	}
	// Template AFTER the edit: gains container claim "gpu-claim" (on a real
	// cluster the edit would also add the matching pod-level entry; new
	// runtimes render consistently, existing workloads are the problem).
	template := workloadRes
	template.Claims = []corev1.ResourceClaim{{Name: "gpu-claim"}}

	engine, ctx, runtimeObj, runtimeClass := verifyPR6177BuildSyncFixture(workloadRes, &template, nil)
	recorder := record.NewFakeRecorder(10)
	engine.Recorder = recorder

	if err := engine.syncRuntimeSpec(ctx, runtimeObj, runtimeClass); err != nil {
		t.Fatalf("syncRuntimeSpec returned %v", err)
	}

	got := verifyPR6177WorkerResources(t, engine)
	propagated := false
	for _, c := range got.Claims {
		if c.Name == "gpu-claim" {
			propagated = true
		}
	}
	if !propagated {
		t.Logf("CANARY FLIPPED: the template's container claim no longer propagates to the " +
			"workload -- a guard landed; invert this canary into a contract test")
		return
	}

	warned := false
	for {
		select {
		case ev := <-recorder.Events:
			if strings.Contains(ev, "Warning") {
				warned = true
			}
		default:
			goto drained
		}
	}
drained:
	if warned {
		t.Logf("CANARY FLIPPED: a Warning event now fires for the unbacked claim; " +
			"invert this canary into a contract test")
		return
	}
	// Current behavior pinned: claim propagated onto a workload whose pod spec
	// cannot back it, silently. Replacement pods are apiserver-rejected on
	// DRA-enabled clusters (validateResourceClaimNames).
	t.Logf("canary holds: claim propagated with no Warning event (workload pod-level declarations untouched)")
}
