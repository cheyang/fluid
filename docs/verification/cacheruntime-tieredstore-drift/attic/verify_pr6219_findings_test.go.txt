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

// Verification harness for PR #6219 findings (reviewer: codex). Head-only:
// these tests may reference symbols introduced by the PR.
//
//   - TestVerifyPR6219Fix_DriftEmitsWarningOnHead      CONTRACT (green on head)
//   - TestVerifyPR6219RemediationDeleteWorkerWorkloadDoesNotHeal  (F1, contract on controller behavior)
//   - TestVerifyPR6219MediumOnlyEditInvisibleToDriftCheck         (F2, BUG CANARY: passes while the blind spot exists)
//   - TestVerifyPR6219ClientTieredStoreEditSilent                 (F3, BUG CANARY: passes while client edits are unwarned)

import (
	"encoding/json"
	"strings"
	"testing"

	workloadv1alpha1 "github.com/fluid-cloudnative/advanced-statefulset/api/workload/v1alpha1"
	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// CONTRACT: on the PR head, editing spec.worker.tieredStore produces a Warning
// event carrying reason RuntimeTieredStoreConflict and the level detail.
func TestVerifyPR6219Fix_DriftEmitsWarningOnHead(t *testing.T) {
	e, recorder, ctx := verifyPR6219Engine(t)

	verifyPR6219SyncOnce(t, e, ctx)
	verifyPR6219EditQuota(t, e, ctx, "16Gi")
	verifyPR6219SyncOnce(t, e, ctx)

	events := verifyPR6219DrainEvents(recorder)
	found := ""
	for _, ev := range events {
		if strings.Contains(ev, common.RuntimeTieredStoreConflict) {
			found = ev
		}
	}
	if found == "" {
		t.Fatalf("expected a %s warning event after in-place tieredStore edit, got events: %v",
			common.RuntimeTieredStoreConflict, events)
	}
	if !strings.Contains(found, string(corev1.EventTypeWarning)) {
		t.Errorf("event is not a Warning: %s", found)
	}
	if !strings.Contains(found, "16Gi") || !strings.Contains(found, "8Gi") {
		t.Errorf("event does not name both quotas (spec 16Gi vs volume 8Gi): %s", found)
	}
}

// F1 — remediation guidance. The event message and the docs tell the user to
// "recreate the worker" / "delete and recreate the worker workload (or the
// CacheRuntime)". This test follows the workload reading: the user deletes the
// worker AdvancedStatefulSet to force recreation with the new tieredStore.
//
// CONTRACT on controller behavior (green iff the runtime keeps failing and the
// workload is NOT recreated): after setup, nothing re-creates a deleted worker
// workload — SetupWorkerComponent is gated on Status.Worker.Phase == None and
// only runs from Setup, so syncRuntimeSpec fails on GetPodSpec (NotFound)
// forever. Only recreating the CacheRuntime (new UID, Phase reset) works, so
// the guidance is only half right.
func TestVerifyPR6219RemediationDeleteWorkerWorkloadDoesNotHeal(t *testing.T) {
	e, recorder, ctx := verifyPR6219Engine(t)

	verifyPR6219SyncOnce(t, e, ctx)
	verifyPR6219EditQuota(t, e, ctx, "16Gi")
	verifyPR6219SyncOnce(t, e, ctx) // drift warning emitted here

	workerKey := types.NamespacedName{
		Name:      common.GetCacheComponentName(verifyPR6219RuntimeName, common.ComponentTypeWorker),
		Namespace: verifyPR6219Namespace,
	}

	// User follows the guidance literally: delete the worker workload.
	sts := &workloadv1alpha1.AdvancedStatefulSet{}
	if err := e.Client.Get(ctx.Context, workerKey, sts); err != nil {
		t.Fatalf("get worker asts before delete: %v", err)
	}
	if err := e.Client.Delete(ctx.Context, sts); err != nil {
		t.Fatalf("delete worker asts: %v", err)
	}

	// Sync three times: if anything re-created the workload, later syncs succeed.
	for i := 0; i < 3; i++ {
		rt, err := e.getRuntime()
		if err != nil {
			t.Fatalf("getRuntime: %v", err)
		}
		class, err := e.getRuntimeClass(rt.Spec.RuntimeClassName)
		if err != nil {
			t.Fatalf("getRuntimeClass: %v", err)
		}
		err = e.syncRuntimeSpec(ctx, rt, class)
		if err == nil {
			t.Fatalf("sync %d: expected error after worker workload deletion, got nil", i)
		}
		if !apierrors.IsNotFound(err) {
			t.Logf("sync %d: error is not NotFound (%v) — sync still fails, finding stands", i, err)
		}
		if err := e.Client.Get(ctx.Context, workerKey, &workloadv1alpha1.AdvancedStatefulSet{}); !apierrors.IsNotFound(err) {
			t.Fatalf("sync %d: worker workload was recreated (err=%v) — guidance would be fine, finding refuted", i, err)
		}
	}
	t.Log("worker workload is never re-created after deletion; sync fails until the CacheRuntime itself is recreated")

	// Control: recreating the workload with the NEW quota (what recreating the
	// CacheRuntime achieves) converges the runtime and stops the drift warnings.
	if err := e.Client.Create(ctx.Context, verifyPR6219WorkerASTS("16Gi")); err != nil {
		t.Fatalf("recreate worker asts: %v", err)
	}
	verifyPR6219DrainEvents(recorder)
	verifyPR6219SyncOnce(t, e, ctx)
	for _, ev := range verifyPR6219DrainEvents(recorder) {
		if strings.Contains(ev, common.RuntimeTieredStoreConflict) {
			t.Fatalf("drift warning still emitted after recreation with matching quota: %s", ev)
		}
	}
}

// F2 — blind spot. The drift check compares quotas (emptyDir/processMemory) and
// paths (hostPath), but NOT the emptyDir medium. Editing only
// level.EmptyDir.Medium ("" -> "Memory") flips the ConfigMap's mediumType from
// HDD to MEM while the pod volume stays disk-backed — the same class of silent
// split the PR exists to surface.
//
// BUG CANARY: PASSES while the blind spot exists. FLIPS TO FAIL if the check is
// extended to medium; invert the assertion then.
func TestVerifyPR6219MediumOnlyEditInvisibleToDriftCheck(t *testing.T) {
	e, recorder, ctx := verifyPR6219Engine(t)

	// Reshape the fixture: disk-backed emptyDir level instead of processMemory.
	rt, err := e.getRuntime()
	if err != nil {
		t.Fatalf("getRuntime: %v", err)
	}
	rt.Spec.Worker.TieredStore = datav1alpha1.RuntimeTieredStore{
		Levels: []datav1alpha1.RuntimeTieredStoreLevel{
			{EmptyDir: &datav1alpha1.EmptyDirMediumSource{Quota: resource.MustParse("50Gi")}}, // Medium "" (disk)
		},
	}
	if err := e.Client.Update(ctx.Context, rt); err != nil {
		t.Fatalf("update runtime: %v", err)
	}
	// Recreate the worker workload as the creation path would have left it.
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
	q := resource.MustParse("50Gi")
	diskASTS := &workloadv1alpha1.AdvancedStatefulSet{
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
					Volumes: []corev1.Volume{{
						Name:         "tiered-store-level-0-index-0",
						VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{Medium: "", SizeLimit: &q}},
					}},
				}},
		},
	}
	if err := e.Client.Create(ctx.Context, diskASTS); err != nil {
		t.Fatalf("create disk-backed worker asts: %v", err)
	}

	verifyPR6219SyncOnce(t, e, ctx)

	// User edits ONLY the medium, quota untouched.
	rt, err = e.getRuntime()
	if err != nil {
		t.Fatalf("getRuntime for medium edit: %v", err)
	}
	rt.Spec.Worker.TieredStore.Levels[0].EmptyDir.Medium = corev1.StorageMediumMemory
	if err := e.Client.Update(ctx.Context, rt); err != nil {
		t.Fatalf("update runtime medium: %v", err)
	}
	verifyPR6219DrainEvents(recorder)
	verifyPR6219SyncOnce(t, e, ctx)

	// The ConfigMap now claims the tier is memory-backed ...
	cm := &corev1.ConfigMap{}
	if err := e.Client.Get(ctx.Context, types.NamespacedName{
		Name:      common.GetCacheRuntimeConfigConfigMapName(verifyPR6219RuntimeName),
		Namespace: verifyPR6219Namespace,
	}, cm); err != nil {
		t.Fatalf("get configmap: %v", err)
	}
	var cfg common.CacheRuntimeConfig
	if err := json.Unmarshal([]byte(cm.Data["runtime.json"]), &cfg); err != nil {
		t.Fatalf("unmarshal runtime.json: %v", err)
	}
	if cfg.Worker == nil || len(cfg.Worker.TieredStoreLevels) == 0 {
		t.Fatalf("runtime.json has no worker tieredStoreLevels")
	}
	if cfg.Worker.TieredStoreLevels[0].MediumType != common.Memory {
		t.Fatalf("expected configmap mediumType MEM after edit, got %q", cfg.Worker.TieredStoreLevels[0].MediumType)
	}

	// ... the pod volume is still disk-backed ...
	sts := &workloadv1alpha1.AdvancedStatefulSet{}
	if err := e.Client.Get(ctx.Context, workerKey, sts); err != nil {
		t.Fatalf("get worker asts after edit: %v", err)
	}
	if got := sts.Spec.Template.Spec.Volumes[0].EmptyDir.Medium; got != corev1.StorageMedium("") {
		t.Fatalf("expected pod volume medium to stay disk-backed, got %q", got)
	}

	// ... and nothing reports the split (canary: flips when medium is compared).
	for _, ev := range verifyPR6219DrainEvents(recorder) {
		if strings.Contains(ev, common.RuntimeTieredStoreConflict) {
			t.Fatalf("CANARY FLIPPED (blind spot fixed): %s", ev)
		}
	}
	t.Log("medium-only edit changed configmap mediumType HDD->MEM with pod volume unchanged and no warning")
}

// F3 — scope. The issue suspects spec.client.tieredStore splits the same way
// (client DaemonSet is never updated after creation; the ConfigMap rebuilds
// client levels from spec on every sync). The PR implements the worker check
// only. This test pins the current behavior so the gap is explicit.
//
// BUG CANARY: PASSES while client edits are unwarned. FLIPS TO FAIL if client
// coverage is added; invert then.
func TestVerifyPR6219ClientTieredStoreEditSilent(t *testing.T) {
	e, recorder, ctx := verifyPR6219Engine(t)

	rt, err := e.getRuntime()
	if err != nil {
		t.Fatalf("getRuntime: %v", err)
	}
	rt.Spec.Client.TieredStore = datav1alpha1.RuntimeTieredStore{
		Levels: []datav1alpha1.RuntimeTieredStoreLevel{
			{ProcessMemory: &datav1alpha1.ProcessMemoryMediumSource{Quota: resource.MustParse("4Gi")}},
		},
	}
	if err := e.Client.Update(ctx.Context, rt); err != nil {
		t.Fatalf("update runtime with client tieredStore: %v", err)
	}
	verifyPR6219SyncOnce(t, e, ctx)

	// Edit the client quota in place.
	rt, err = e.getRuntime()
	if err != nil {
		t.Fatalf("getRuntime for client edit: %v", err)
	}
	rt.Spec.Client.TieredStore.Levels[0].ProcessMemory.Quota = resource.MustParse("8Gi")
	if err := e.Client.Update(ctx.Context, rt); err != nil {
		t.Fatalf("update client quota: %v", err)
	}
	verifyPR6219DrainEvents(recorder)
	verifyPR6219SyncOnce(t, e, ctx)

	// ConfigMap client levels follow the edit ...
	cm := &corev1.ConfigMap{}
	if err := e.Client.Get(ctx.Context, types.NamespacedName{
		Name:      common.GetCacheRuntimeConfigConfigMapName(verifyPR6219RuntimeName),
		Namespace: verifyPR6219Namespace,
	}, cm); err != nil {
		t.Fatalf("get configmap: %v", err)
	}
	var cfg common.CacheRuntimeConfig
	if err := json.Unmarshal([]byte(cm.Data["runtime.json"]), &cfg); err != nil {
		t.Fatalf("unmarshal runtime.json: %v", err)
	}
	if cfg.Client == nil || len(cfg.Client.TieredStoreLevels) == 0 || cfg.Client.TieredStoreLevels[0].Quotas[0] != "8Gi" {
		t.Fatalf("configmap client tieredStoreLevels did not follow the edit: %+v", cfg.Client)
	}

	// ... while no warning is emitted for the client split (canary).
	for _, ev := range verifyPR6219DrainEvents(recorder) {
		if strings.Contains(ev, common.RuntimeTieredStoreConflict) {
			t.Fatalf("CANARY FLIPPED (client coverage added): %s", ev)
		}
	}
	t.Log("client tieredStore edit updates the configmap with no warning event (worker-only coverage)")
}
