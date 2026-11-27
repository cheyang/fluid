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

// Verification harness for PR #6219 (reviewer: codex).
//
// P0 premise harness — issue #6192 claims: "Changing spec.worker.tieredStore on an
// existing CacheRuntime updates the runtime ConfigMap but not the worker Pod ...
// and no event, condition or log line says so."
//
// This file deliberately references ONLY symbols that exist on BOTH the base
// commit (86e3adf3) and the PR head (58afa3a9), so the very same file runs
// against either tree:
//
//   - TestVerifyPR6219Premise_ConfigMapFollowsSpecPodDoesNot
//       Contract test on the *split* itself. Expected to PASS on base AND on
//       head: the ConfigMap follows the edited spec while the worker volume
//       keeps its creation-time sizeLimit. (This is why the issue is real and
//       also why the PR's event-based mitigation, not a volume patch, is the
//       right layer.)
//   - TestVerifyPR6219Premise_SilentOnBase
//       BUG CANARY. Asserts the observed base behavior: the split is silent
//       (no *TieredStoreConflict* warning event). PASSES on base (premise
//       reproduced) and FLIPS TO FAIL on the fixed head (fix works). When it
//       flips, invert the assertion or keep it as the historical record.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	workloadv1alpha1 "github.com/fluid-cloudnative/advanced-statefulset/api/workload/v1alpha1"
	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	cruntime "github.com/fluid-cloudnative/fluid/pkg/runtime"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	verifyPR6219RuntimeName = "verify-tsedit"
	verifyPR6219Namespace   = "default"
)

// verifyPR6219Scheme builds the scheme the harness needs. Kept independent of
// CacheEngineTestScheme so the file also compiles against the base tree.
func verifyPR6219Scheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = datav1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = workloadv1alpha1.AddToScheme(scheme)
	return scheme
}

// verifyPR6219Runtime returns the CacheRuntime fixture: a worker with a single
// processMemory tier of 8Gi and a 4Gi memory limit, mirroring the issue #6192
// fixture (worker.resources present, so the base code path reads the pod spec too).
func verifyPR6219Runtime() *datav1alpha1.CacheRuntime {
	return &datav1alpha1.CacheRuntime{
		TypeMeta: metav1.TypeMeta{APIVersion: "data.fluid.io/v1alpha1", Kind: "CacheRuntime"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      verifyPR6219RuntimeName,
			Namespace: verifyPR6219Namespace,
			UID:       "verify-tsedit-uid",
		},
		Spec: datav1alpha1.CacheRuntimeSpec{
			RuntimeClassName: "verify-tsedit-class",
			Master:           datav1alpha1.CacheRuntimeMasterSpec{Replicas: 1},
			Worker: datav1alpha1.CacheRuntimeWorkerSpec{
				RuntimeComponentCommonSpec: datav1alpha1.RuntimeComponentCommonSpec{
					Resources: corev1.ResourceRequirements{
						Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("4Gi")},
						Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("4Gi")},
					},
				},
				Replicas: 1,
				TieredStore: datav1alpha1.RuntimeTieredStore{
					Levels: []datav1alpha1.RuntimeTieredStoreLevel{
						{ProcessMemory: &datav1alpha1.ProcessMemoryMediumSource{Quota: resource.MustParse("8Gi")}},
					},
				},
			},
		},
	}
}

func verifyPR6219RuntimeClass() *datav1alpha1.CacheRuntimeClass {
	return &datav1alpha1.CacheRuntimeClass{
		ObjectMeta:     metav1.ObjectMeta{Name: "verify-tsedit-class"},
		FileSystemType: "verifyfs",
		Topology: &datav1alpha1.RuntimeTopology{
			Master: &datav1alpha1.RuntimeComponentDefinition{
				Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "master", Image: "verify-master:latest"}},
				}},
			},
			Worker: &datav1alpha1.RuntimeComponentDefinition{
				Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "worker", Image: "verify-worker:latest"}},
				}},
			},
			Client: &datav1alpha1.RuntimeComponentDefinition{
				Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "client", Image: "verify-client:latest"}},
				}},
			},
		},
	}
}

// verifyPR6219WorkerASTS reproduces the worker AdvancedStatefulSet as the
// creation path (TransformRuntimeTieredStore) leaves it for the fixture above:
// tmpfs volume tiered-store-level-0-memory sized 8Gi and a container memory
// limit of 4Gi (baseline) + 8Gi (charged quota) = 12Gi.
func verifyPR6219WorkerASTS(quota string) *workloadv1alpha1.AdvancedStatefulSet {
	replicas := int32(1)
	q := resource.MustParse(quota)
	return &workloadv1alpha1.AdvancedStatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      common.GetCacheComponentName(verifyPR6219RuntimeName, common.ComponentTypeWorker),
			Namespace: verifyPR6219Namespace,
		},
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
							Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("12Gi")},
							Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("12Gi")},
						},
					}},
					Volumes: []corev1.Volume{{
						Name: "tiered-store-level-0-memory",
						VolumeSource: corev1.VolumeSource{
							EmptyDir: &corev1.EmptyDirVolumeSource{
								Medium:    corev1.StorageMediumMemory,
								SizeLimit: &q,
							},
						},
					}},
				},
			},
		},
	}
}

func verifyPR6219MasterASTS() *workloadv1alpha1.AdvancedStatefulSet {
	replicas := int32(1)
	return &workloadv1alpha1.AdvancedStatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      common.GetCacheComponentName(verifyPR6219RuntimeName, common.ComponentTypeMaster),
			Namespace: verifyPR6219Namespace,
		},
		Spec: workloadv1alpha1.AdvancedStatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "verify-tsedit-master"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "verify-tsedit-master"}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "master", Image: "verify-master:latest"}}},
			},
		},
	}
}

// verifyPR6219Engine wires a CacheEngine against a fake client seeded with the
// steady state (spec 8Gi, workload 8Gi) and returns the engine plus the
// FakeRecorder that captures every event the engine emits.
func verifyPR6219Engine(t *testing.T) (*CacheEngine, *record.FakeRecorder, cruntime.ReconcileRequestContext) {
	t.Helper()
	dataset := &datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: verifyPR6219RuntimeName, Namespace: verifyPR6219Namespace, UID: "verify-dataset-uid"},
		Spec:       datav1alpha1.DatasetSpec{},
	}
	rt := verifyPR6219Runtime()
	class := verifyPR6219RuntimeClass()
	c := fake.NewClientBuilder().WithScheme(verifyPR6219Scheme()).
		WithObjects(dataset, rt, class, verifyPR6219MasterASTS(), verifyPR6219WorkerASTS("8Gi")).
		Build()
	recorder := record.NewFakeRecorder(100)
	e := &CacheEngine{
		name:      verifyPR6219RuntimeName,
		namespace: verifyPR6219Namespace,
		Client:    c,
		Log:       ctrl.Log.WithName("verify-pr6219"),
		Recorder:  recorder,
	}
	ctx := cruntime.ReconcileRequestContext{
		Client:         c,
		Context:        context.Background(),
		Log:            ctrl.Log.WithName("verify-pr6219"),
		RuntimeType:    "cache",
		NamespacedName: types.NamespacedName{Name: verifyPR6219RuntimeName, Namespace: verifyPR6219Namespace},
	}
	return e, recorder, ctx
}

// verifyPR6219SyncOnce runs the two sync steps involved in the premise: the
// ConfigMap rebuild and the runtime spec sync.
func verifyPR6219SyncOnce(t *testing.T, e *CacheEngine, ctx cruntime.ReconcileRequestContext) {
	t.Helper()
	rt, err := e.getRuntime()
	if err != nil {
		t.Fatalf("getRuntime: %v", err)
	}
	class, err := e.getRuntimeClass(rt.Spec.RuntimeClassName)
	if err != nil {
		t.Fatalf("getRuntimeClass: %v", err)
	}
	if err := e.syncRuntimeValueConfigMap(ctx, rt); err != nil {
		t.Fatalf("syncRuntimeValueConfigMap: %v", err)
	}
	if err := e.syncRuntimeSpec(ctx, rt, class); err != nil {
		t.Fatalf("syncRuntimeSpec: %v", err)
	}
}

// verifyPR6219EditQuota patches spec.worker.tieredStore level 0 to newQuota,
// mimicking the user's in-place edit from issue #6192.
func verifyPR6219EditQuota(t *testing.T, e *CacheEngine, ctx cruntime.ReconcileRequestContext, newQuota string) {
	t.Helper()
	rt, err := e.getRuntime()
	if err != nil {
		t.Fatalf("getRuntime for edit: %v", err)
	}
	rt.Spec.Worker.TieredStore.Levels[0].ProcessMemory.Quota = resource.MustParse(newQuota)
	if err := e.Client.Update(ctx.Context, rt); err != nil {
		t.Fatalf("update runtime: %v", err)
	}
}

func verifyPR6219ConfigMapQuota(t *testing.T, e *CacheEngine, ctx cruntime.ReconcileRequestContext) string {
	t.Helper()
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
	if cfg.Worker == nil || len(cfg.Worker.TieredStoreLevels) == 0 || len(cfg.Worker.TieredStoreLevels[0].Quotas) == 0 {
		t.Fatalf("runtime.json has no worker tieredStoreLevels quota: %s", cm.Data["runtime.json"])
	}
	return cfg.Worker.TieredStoreLevels[0].Quotas[0]
}

func verifyPR6219WorkerVolumeSize(t *testing.T, e *CacheEngine, ctx cruntime.ReconcileRequestContext) string {
	t.Helper()
	sts := &workloadv1alpha1.AdvancedStatefulSet{}
	if err := e.Client.Get(ctx.Context, types.NamespacedName{
		Name:      common.GetCacheComponentName(verifyPR6219RuntimeName, common.ComponentTypeWorker),
		Namespace: verifyPR6219Namespace,
	}, sts); err != nil {
		t.Fatalf("get worker asts: %v", err)
	}
	for _, v := range sts.Spec.Template.Spec.Volumes {
		if v.Name == "tiered-store-level-0-memory" && v.EmptyDir != nil && v.EmptyDir.SizeLimit != nil {
			return v.EmptyDir.SizeLimit.String()
		}
	}
	t.Fatalf("tiered-store-level-0-memory volume not found on worker asts")
	return ""
}

func verifyPR6219DrainEvents(recorder *record.FakeRecorder) []string {
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

// CONTRACT (both trees): after the in-place quota edit the runtime ConfigMap
// follows the spec while the worker pod volume keeps its creation-time size.
// This is the split issue #6192 reports; it must hold on base (reproduction)
// and still hold on head (the PR deliberately does not patch volumes).
func TestVerifyPR6219Premise_ConfigMapFollowsSpecPodDoesNot(t *testing.T) {
	e, _, ctx := verifyPR6219Engine(t)

	// steady state: spec 8Gi, CM 8Gi, volume 8Gi
	verifyPR6219SyncOnce(t, e, ctx)
	if got := verifyPR6219ConfigMapQuota(t, e, ctx); got != "8Gi" {
		t.Fatalf("steady-state configmap quota = %s, want 8Gi", got)
	}
	if got := verifyPR6219WorkerVolumeSize(t, e, ctx); got != "8Gi" {
		t.Fatalf("steady-state worker volume sizeLimit = %s, want 8Gi", got)
	}

	// user edit: 8Gi -> 16Gi
	verifyPR6219EditQuota(t, e, ctx, "16Gi")
	verifyPR6219SyncOnce(t, e, ctx)

	if got := verifyPR6219ConfigMapQuota(t, e, ctx); got != "16Gi" {
		t.Errorf("SPLIT NOT REPRODUCED (configmap side): configmap quota = %s, want 16Gi (configmap follows spec)", got)
	}
	if got := verifyPR6219WorkerVolumeSize(t, e, ctx); got != "8Gi" {
		t.Errorf("SPLIT NOT REPRODUCED (pod side): worker volume sizeLimit = %s, want 8Gi (pod keeps creation-time size)", got)
	}
}

// BUG CANARY: passes on base (the split is silent — premise reproduced), flips
// to FAIL on the fixed head (a RuntimeTieredStoreConflict warning is emitted).
// String match on the reason is deliberate so the file compiles against base.
func TestVerifyPR6219Premise_SilentOnBase(t *testing.T) {
	e, recorder, ctx := verifyPR6219Engine(t)

	verifyPR6219SyncOnce(t, e, ctx)
	verifyPR6219EditQuota(t, e, ctx, "16Gi")
	verifyPR6219SyncOnce(t, e, ctx)

	events := verifyPR6219DrainEvents(recorder)
	for _, ev := range events {
		fmt.Printf("[premise-canary] observed event: %s\n", ev)
	}
	for _, ev := range events {
		if strings.Contains(ev, "TieredStoreConflict") {
			t.Fatalf("CANARY FLIPPED (this is the fix working on head): unexpected TieredStoreConflict event: %s", ev)
		}
	}
	t.Logf("split stayed silent: %d events, none about tieredStore (expected on base)", len(events))
}
