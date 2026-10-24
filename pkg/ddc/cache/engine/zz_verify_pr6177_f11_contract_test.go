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

// Round-6 CONTRACT tests for finding F11 (PR #6177), INVERTED from the round-4
// canary (zz_verify_pr6177_f11_canary_test.go, retired this round). The canary
// was observed flipped at head a773dfa6 (log line "CANARY FLIPPED: the
// template's container claim no longer propagates to the workload"): the
// round-6 remedy guards the sync. These contracts pin the fixed behavior:
//
//   remedy: validDesiredResources now drops a container claim the workload's
//   pod spec neither declares (pod-level spec.resourceClaims) nor already
//   carries on its first container, emits a RuntimeResourcesConflict Warning
//   naming the claim, and still syncs the rest of resources (sync.go,
//   splitDeclaredClaims in transform_common.go).
//
// Polarity: CONTRACT (red on unfixed code). Test 1 is the differential:
//   - base e0fc4c18: the CacheRuntime's partial list is applied wholesale, the
//     claim vanishes silently and NO warning fires -> RED (missing signal).
//   - previous head ce4f0c4f: the merge carries the claim into the workload
//     with no pod-level backing and no warning -> RED (the F11 defect itself).
//   - head a773dfa6: claim dropped + warned + rest synced + converges -> GREEN.
// Tests 2 and 3 are pass-through pins (green on every ref): the guard must not
// over-block claims the workload declares, and must not roll a workload whose
// container already carries the claim. Compile-safe on all three refs
// (pre-existing symbols only).
import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"

	workloadv1alpha1 "github.com/fluid-cloudnative/advanced-statefulset/api/workload/v1alpha1"
)

func verifyPR6177F11GetWorkerSts(t *testing.T, engine *CacheEngine) *workloadv1alpha1.AdvancedStatefulSet {
	t.Helper()
	sts := &workloadv1alpha1.AdvancedStatefulSet{}
	if err := engine.Client.Get(context.Background(),
		types.NamespacedName{Name: "test-runtime-worker", Namespace: "default"}, sts); err != nil {
		t.Fatalf("get worker sts: %v", err)
	}
	return sts
}

func verifyPR6177F11DrainEvents(recorder *record.FakeRecorder) []string {
	var events []string
	for {
		select {
		case ev := <-recorder.Events:
			events = append(events, ev)
		default:
			return events
		}
	}
}

func TestVerifyPR6177_F11_UndeclaredTemplateClaim_DroppedWarnedConverges(t *testing.T) {
	// Workload as rendered BEFORE the template edit: cpu only, no claims, no
	// pod-level resourceClaims.
	workloadRes := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")},
		Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")},
	}
	// Template AFTER the class edit: gains container claim "gpu-claim" (on a
	// real cluster the edit adds the pod-level entry too; existing workloads
	// were rendered before it).
	template := *workloadRes.DeepCopy()
	template.Claims = []corev1.ResourceClaim{{Name: "gpu-claim"}}
	// The CacheRuntime names a memory limit: the rest of the resources edit
	// must still sync even though the claim is dropped.
	overlay := corev1.ResourceRequirements{
		Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("4Gi")},
	}

	engine, ctx, runtimeObj, runtimeClass := verifyPR6177BuildSyncFixture(workloadRes, &template, &overlay)
	recorder := record.NewFakeRecorder(10)
	engine.Recorder = recorder

	if err := engine.syncRuntimeSpec(ctx, runtimeObj, runtimeClass); err != nil {
		t.Fatalf("syncRuntimeSpec returned %v; a dropped claim must not fail the sync", err)
	}

	got := verifyPR6177WorkerResources(t, engine)
	for _, c := range got.Claims {
		if c.Name == "gpu-claim" {
			t.Errorf("claim gpu-claim was synced onto a workload whose pod spec does not declare it; " +
				"the API server rejects replacement Pods on DRA-enabled clusters (validateResourceClaimNames)")
		}
	}
	if got.Limits.Memory().String() != "4Gi" {
		t.Errorf("limits.memory = %q, want 4Gi: dropping the claim must not hold back the rest of the resources edit",
			got.Limits.Memory().String())
	}
	if got.Requests.Cpu().String() != "1" || got.Limits.Cpu().String() != "2" {
		t.Errorf("cpu requirements = %v/%v, want 1/2 from the template baseline",
			got.Requests.Cpu().String(), got.Limits.Cpu().String())
	}

	events := verifyPR6177F11DrainEvents(recorder)
	warned := false
	for _, ev := range events {
		if strings.Contains(ev, "Warning") && strings.Contains(ev, "RuntimeResourcesConflict") &&
			strings.Contains(ev, "gpu-claim") {
			warned = true
		}
	}
	if !warned {
		t.Errorf("no Warning event naming gpu-claim among %v; the wedge must be signalled on the CacheRuntime", events)
	}

	// Convergence: a second sync with the claim still undeclared must not patch.
	rvBefore := verifyPR6177F11GetWorkerSts(t, engine).ResourceVersion
	if err := engine.syncRuntimeSpec(ctx, runtimeObj, runtimeClass); err != nil {
		t.Fatalf("second syncRuntimeSpec: %v", err)
	}
	if rvAfter := verifyPR6177F11GetWorkerSts(t, engine).ResourceVersion; rvAfter != rvBefore {
		t.Errorf("sync did not converge: resourceVersion %q -> %q with no spec change", rvBefore, rvAfter)
	}
}

func TestVerifyPR6177_F11_WorkloadDeclaresClaim_Syncs(t *testing.T) {
	// Same template edit, but the workload's pod spec DOES declare the claim
	// (e.g. the workload was recreated): the claim must sync, with no warning.
	workloadRes := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")},
		Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")},
	}
	template := *workloadRes.DeepCopy()
	template.Claims = []corev1.ResourceClaim{{Name: "gpu-claim"}}

	engine, ctx, runtimeObj, runtimeClass := verifyPR6177BuildSyncFixture(workloadRes, &template, nil)
	recorder := record.NewFakeRecorder(10)
	engine.Recorder = recorder

	sts := verifyPR6177F11GetWorkerSts(t, engine)
	sts.Spec.Template.Spec.ResourceClaims = []corev1.PodResourceClaim{{Name: "gpu-claim"}}
	if err := engine.Client.Update(context.Background(), sts); err != nil {
		t.Fatalf("declare pod-level claim: %v", err)
	}

	if err := engine.syncRuntimeSpec(ctx, runtimeObj, runtimeClass); err != nil {
		t.Fatalf("syncRuntimeSpec returned %v", err)
	}

	got := verifyPR6177WorkerResources(t, engine)
	found := false
	for _, c := range got.Claims {
		if c.Name == "gpu-claim" {
			found = true
		}
	}
	if !found {
		t.Errorf("claim gpu-claim missing from %+v; a claim the workload declares must sync", got.Claims)
	}
	if events := verifyPR6177F11DrainEvents(recorder); len(events) != 0 {
		t.Errorf("unexpected events %v; a declared claim is not a conflict", events)
	}
}

func TestVerifyPR6177_F11_ContainerAlreadyCarriesClaim_KeptNoRoll(t *testing.T) {
	// The container already carries the claim (e.g. synced before the guard
	// existed) while the pod spec never declared it: dropping it now would
	// patch the workload and roll Pods for no change in what they get, so the
	// sync keeps it and stays convergent.
	workloadRes := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")},
		Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")},
		Claims:   []corev1.ResourceClaim{{Name: "gpu-claim"}},
	}
	template := *workloadRes.DeepCopy()

	engine, ctx, runtimeObj, runtimeClass := verifyPR6177BuildSyncFixture(workloadRes, &template, nil)
	recorder := record.NewFakeRecorder(10)
	engine.Recorder = recorder

	rvBefore := verifyPR6177F11GetWorkerSts(t, engine).ResourceVersion
	if err := engine.syncRuntimeSpec(ctx, runtimeObj, runtimeClass); err != nil {
		t.Fatalf("syncRuntimeSpec returned %v", err)
	}

	got := verifyPR6177WorkerResources(t, engine)
	found := false
	for _, c := range got.Claims {
		if c.Name == "gpu-claim" {
			found = true
		}
	}
	if !found {
		t.Errorf("claim gpu-claim was dropped from %+v; the container already carries it, "+
			"dropping would roll the workload for no Pod-visible change", got.Claims)
	}
	if rvAfter := verifyPR6177F11GetWorkerSts(t, engine).ResourceVersion; rvAfter != rvBefore {
		t.Errorf("workload was patched (resourceVersion %q -> %q) although nothing changed", rvBefore, rvAfter)
	}
	if events := verifyPR6177F11DrainEvents(recorder); len(events) != 0 {
		t.Errorf("unexpected events %v; keeping an already-carried claim is not a conflict", events)
	}
}
