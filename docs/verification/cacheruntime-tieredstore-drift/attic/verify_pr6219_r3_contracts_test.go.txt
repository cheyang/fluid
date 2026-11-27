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

// Round-3 CONTRACT tests for PR #6219 (reviewer: codex), head 6809f62f.
//
// Round 3 (b56db670 -> 6809f62f) changed exactly one production-code behavior:
// the drift-check dispatch in checkWorkerTieredStoreDrift now evaluates
// HostPath BEFORE EmptyDir, matching the ConfigMap extraction precedence in
// extractTieredStoreLevels (cm.go). This closes the round-2 question
// TS-DISPATCH-PRECEDENCE: a level edited to carry BOTH hostPath and emptyDir
// (only reachable post-creation — TransformRuntimeTieredStore rejects
// multi-medium levels at Setup) is reported as hostPath in the ConfigMap,
// so the drift check must validate it as hostPath too, else the split between
// what the cache system reads (hostPath) and what the pod has (the stale
// emptyDir volume) goes unwarned.
//
//   - TestVerifyPR6219DispatchMatchesConfigMapExtraction  CONTRACT: with a
//     both-fields level, the drift warning must be about hostPath (not silent,
//     not emptyDir) AND the ConfigMap extraction must report the hostPath
//     mount path for the same level — the two must agree.
//   - TestVerifyPR6219HostPathSteadyStateNoWarning  CONTRACT: a hostPath level
//     whose pod volume matches produces NO warning — the no-false-positive
//     guard for the new dispatch order.

import (
	"strings"
	"testing"

	workloadv1alpha1 "github.com/fluid-cloudnative/advanced-statefulset/api/workload/v1alpha1"
	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	cruntime "github.com/fluid-cloudnative/fluid/pkg/runtime"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// verifyPR6219ReplaceWorkerVolumes swaps the worker ASTS for one carrying the
// given tiered-store volumes (mirrors how the F2 contract reshapes the fixture).
func verifyPR6219ReplaceWorkerVolumes(t *testing.T, e *CacheEngine, ctx cruntime.ReconcileRequestContext, volumes []corev1.Volume) {
	t.Helper()
	workerKey := types.NamespacedName{
		Name:      common.GetCacheComponentName(verifyPR6219RuntimeName, common.ComponentTypeWorker),
		Namespace: verifyPR6219Namespace,
	}
	old := &workloadv1alpha1.AdvancedStatefulSet{}
	if err := e.Client.Get(ctx.Context, workerKey, old); err != nil {
		t.Fatalf("get worker asts: %v", err)
	}
	if err := e.Client.Delete(ctx.Context, old); err != nil {
		t.Fatalf("delete worker asts: %v", err)
	}
	replicas := int32(1)
	fresh := &workloadv1alpha1.AdvancedStatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: old.Name, Namespace: old.Namespace},
		Spec: workloadv1alpha1.AdvancedStatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "verify-tsedit-worker"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "verify-tsedit-worker"}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "worker",
						Image: "verify-worker:latest",
						Resources: corev1.ResourceRequirements{
							Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("4Gi")},
							Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("4Gi")},
						},
					}},
					Volumes: volumes,
				}},
		},
	}
	if err := e.Client.Create(ctx.Context, fresh); err != nil {
		t.Fatalf("create replacement worker asts: %v", err)
	}
}

// verifyPR6219SetLevels replaces spec.worker.tieredStore.levels.
func verifyPR6219SetLevels(t *testing.T, e *CacheEngine, ctx cruntime.ReconcileRequestContext, levels []datav1alpha1.RuntimeTieredStoreLevel) {
	t.Helper()
	rt, err := e.getRuntime()
	if err != nil {
		t.Fatalf("getRuntime: %v", err)
	}
	rt.Spec.Worker.TieredStore = datav1alpha1.RuntimeTieredStore{Levels: levels}
	if err := e.Client.Update(ctx.Context, rt); err != nil {
		t.Fatalf("update runtime: %v", err)
	}
}

func TestVerifyPR6219DispatchMatchesConfigMapExtraction(t *testing.T) {
	e, recorder, ctx := verifyPR6219Engine(t)

	// Steady state: disk-backed emptyDir level 0 (2Gi), pod carries the matching
	// emptyDir volume tiered-store-level-0-index-0.
	q := resource.MustParse("2Gi")
	verifyPR6219SetLevels(t, e, ctx, []datav1alpha1.RuntimeTieredStoreLevel{
		{EmptyDir: &datav1alpha1.EmptyDirMediumSource{Quota: q}},
	})
	verifyPR6219ReplaceWorkerVolumes(t, e, ctx, []corev1.Volume{{
		Name: "tiered-store-level-0-index-0",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{Medium: "", SizeLimit: &q},
		},
	}})
	verifyPR6219SyncOnce(t, e, ctx)
	for _, ev := range verifyPR6219DrainEvents(recorder) {
		if strings.Contains(ev, common.RuntimeTieredStoreConflict) {
			t.Fatalf("steady state must not warn, got: %s", ev)
		}
	}

	// Post-creation edit: the level now carries BOTH hostPath and emptyDir.
	// (Creation would reject this — mediaCount>1 — but the CRD has no one-of
	// validation, so an in-place edit can produce it; that is the exact shape
	// round-2 question TS-DISPATCH-PRECEDENCE was about.)
	verifyPR6219SetLevels(t, e, ctx, []datav1alpha1.RuntimeTieredStoreLevel{{
		EmptyDir: &datav1alpha1.EmptyDirMediumSource{Quota: q},
		HostPath: &datav1alpha1.HostPathMediumSource{
			Paths:  []string{"/mnt/cache"},
			Quotas: []resource.Quantity{q},
		},
	}})

	// (a) ConfigMap extraction must treat the level as hostPath — this is what
	// the cache system will actually read on restart.
	rt, err := e.getRuntime()
	if err != nil {
		t.Fatalf("getRuntime for extraction check: %v", err)
	}
	levels := e.extractTieredStoreLevels(&rt.Spec.Worker.TieredStore)
	if len(levels) != 1 {
		t.Fatalf("expected 1 extracted level, got %d", len(levels))
	}
	wantMount := GetHostPathTieredStoreMountPath(0, 0)
	if len(levels[0].MountPaths) != 1 || levels[0].MountPaths[0] != wantMount {
		t.Errorf("ConfigMap extraction must report the hostPath mount path %q for a both-fields level, got %v", wantMount, levels[0].MountPaths)
	}

	// (b) The drift check must validate the SAME interpretation: with the pod
	// holding only the stale emptyDir volume, the warning must name hostPath.
	// Under the round-2 dispatch (emptyDir first) this level produced NO
	// warning — the emptyDir half still matched the pod volume — while the
	// ConfigMap already told the cache system to use hostPath.
	verifyPR6219SyncOnce(t, e, ctx)
	event := verifyPR6219SingleConflictEvent(t, verifyPR6219DrainEvents(recorder))
	if !strings.Contains(event, "hostPath") {
		t.Errorf("both-fields level must be drift-checked as hostPath (matching ConfigMap extraction), got: %s", event)
	}
	if strings.Contains(event, "(emptyDir)") {
		t.Errorf("drift message must not be about the emptyDir half — the ConfigMap reports hostPath: %s", event)
	}
}

func TestVerifyPR6219HostPathSteadyStateNoWarning(t *testing.T) {
	e, recorder, ctx := verifyPR6219Engine(t)

	// hostPath level 0 (/mnt/cache, 2Gi), pod carries the matching hostPath
	// volume — the state the creation path would have produced for this spec.
	q := resource.MustParse("2Gi")
	verifyPR6219SetLevels(t, e, ctx, []datav1alpha1.RuntimeTieredStoreLevel{
		{HostPath: &datav1alpha1.HostPathMediumSource{
			Paths:  []string{"/mnt/cache"},
			Quotas: []resource.Quantity{q},
		}},
	})
	verifyPR6219ReplaceWorkerVolumes(t, e, ctx, []corev1.Volume{{
		Name: "tiered-store-level-0-index-0",
		VolumeSource: corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{Path: "/mnt/cache"},
		},
	}})

	verifyPR6219SyncOnce(t, e, ctx)
	for _, ev := range verifyPR6219DrainEvents(recorder) {
		if strings.Contains(ev, common.RuntimeTieredStoreConflict) {
			t.Errorf("hostPath steady state must not warn under the hostPath-first dispatch, got: %s", ev)
		}
	}
}
