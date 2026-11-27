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

// Round-2 CONTRACT tests for PR #6219 (reviewer: codex), head b56db670.
// Round 1 ended with three open findings whose fixes landed as wording /
// comparison / docs changes that the round-1 canaries cannot observe:
//
//   - TestVerifyPR6219RemediationGuidanceRecreateCacheRuntime  CONTRACT for F1:
//     the drift event's remediation text must direct the user to delete and
//     recreate the CacheRuntime (the only remedy verified to converge), and
//     must not suggest recreating the worker workload (verified to wedge sync).
//   - TestVerifyPR6219MediumDriftNowDetected  CONTRACT (inverted F2 canary):
//     an emptyDir medium-only edit (disk -> Memory) must emit the warning.
//   - TestVerifyPR6219DocsGuidanceAndScope  CONTRACT for F1+F3 doc sites:
//     en+zh docs scope the warning to spec.worker.tieredStore and never
//     recommend recreating the worker workload.
//
// The round-1 canaries (TestVerifyPR6219RemediationDeleteWorkerWorkloadDoesNotHeal,
// TestVerifyPR6219ClientTieredStoreEditSilent) intentionally still PASS: they pin
// pre-existing controller behavior (no workload self-heal; client edits unwarned)
// that this PR deliberately did not change — the former is now unreferenced by
// any guidance, the latter is tracked in issue #6221.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	workloadv1alpha1 "github.com/fluid-cloudnative/advanced-statefulset/api/workload/v1alpha1"
	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func verifyPR6219SingleConflictEvent(t *testing.T, recorderEvents []string) string {
	t.Helper()
	found := ""
	for _, ev := range recorderEvents {
		if strings.Contains(ev, common.RuntimeTieredStoreConflict) {
			found = ev
		}
	}
	if found == "" {
		t.Fatalf("expected a %s warning event, got events: %v", common.RuntimeTieredStoreConflict, recorderEvents)
	}
	return found
}

// F1 contract: the remediation text in the emitted event.
func TestVerifyPR6219RemediationGuidanceRecreateCacheRuntime(t *testing.T) {
	e, recorder, ctx := verifyPR6219Engine(t)

	verifyPR6219SyncOnce(t, e, ctx)
	verifyPR6219EditQuota(t, e, ctx, "16Gi")
	verifyPR6219SyncOnce(t, e, ctx)

	event := verifyPR6219SingleConflictEvent(t, verifyPR6219DrainEvents(recorder))
	if !strings.Contains(event, "Delete and recreate the CacheRuntime") {
		t.Errorf("remediation guidance must direct to CacheRuntime recreation (the only remedy verified to converge): %s", event)
	}
	if strings.Contains(event, "Recreate the worker") || strings.Contains(event, "recreate the worker") {
		t.Errorf("remediation guidance still points at the worker workload, which wedges sync (F1): %s", event)
	}
}

// F2 contract (inverted canary): medium-only edits are now detected.
func TestVerifyPR6219MediumDriftNowDetected(t *testing.T) {
	e, recorder, ctx := verifyPR6219Engine(t)

	// Reshape the fixture: disk-backed emptyDir level instead of processMemory
	// (same setup as the round-1 F2 canary).
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

	// Edit ONLY the medium, quota untouched — this is the edit round 1 proved invisible.
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

	event := verifyPR6219SingleConflictEvent(t, verifyPR6219DrainEvents(recorder))
	if !strings.Contains(event, "medium") {
		t.Errorf("medium drift event should name the medium divergence: %s", event)
	}
}


func verifyPR6219RepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatalf("repo root (go.mod) not found from package dir")
	return ""
}

// F1+F3 contract: every introduced doc site (en+zh) recommends CacheRuntime
// recreation, never worker-workload recreation, and scopes the drift warning
// to spec.worker.tieredStore.
func TestVerifyPR6219DocsGuidanceAndScope(t *testing.T) {
	root := verifyPR6219RepoRoot(t)
	docs := []string{
		"docs/en/samples/cacheruntime/cacheruntime_spec_update.md",
		"docs/zh/samples/cacheruntime/cacheruntime_spec_update.md",
		"docs/en/userguide/cache_runtime_tieredstore.md",
		"docs/zh/userguide/cache_runtime_tieredstore.md",
	}
	forbidden := []string{
		"Recreate the worker",
		"recreating the worker workload",
		"spec.{worker,client}",
		"重新创建 Worker 工作负载", // "recreate the Worker workload"
	}
	for _, rel := range docs {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		text := string(data)
		for _, bad := range forbidden {
			if strings.Contains(text, bad) {
				t.Errorf("%s still contains %q (F1/F3 regression)", rel, bad)
			}
		}
		if !strings.Contains(text, "spec.worker.tieredStore") {
			t.Errorf("%s no longer scopes the drift warning to spec.worker.tieredStore", rel)
		}
	}
	enSample, _ := os.ReadFile(filepath.Join(root, docs[0]))
	if !strings.Contains(string(enSample), "deleting and recreating the CacheRuntime") {
		t.Errorf("en sample doc lost the CacheRuntime-recreation remedy")
	}
	zhSample, _ := os.ReadFile(filepath.Join(root, docs[1]))
	if !strings.Contains(string(zhSample), "删除并重新创建 CacheRuntime") {
		t.Errorf("zh sample doc lost the CacheRuntime-recreation remedy")
	}
}
