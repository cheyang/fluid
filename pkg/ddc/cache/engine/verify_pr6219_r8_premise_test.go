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

// Round-8 PREMISE re-verification for PR #6219 (reviewer: codex), against the
// moved base b8ff60e3 (round-8 merge-base; the round-7 premise was confirmed
// on 253f6bb8). This file deliberately uses only symbols that exist on BOTH
// the base and the head trees, so it compiles against either; the silence
// canary matches on strings for the same reason.
//
// Premise (issue #6192, OPEN, re-checked 2026-11-27): editing
// spec.worker.tieredStore on an existing CacheRuntime updates the runtime
// ConfigMap but not the worker pod volumes, and nothing reports the split.
//
//   - TestVerifyPR6219R8Premise_ConfigMapFollowsSpecPodDoesNot  CONTRACT on
//     both trees: ConfigMap follows the edited spec, pod volume keeps the
//     creation-time size. On base: reproduction. On head: still the PR's
//     design (the PR warns; it does not patch volumes; #6299 follows up the
//     ConfigMap side).
//   - TestVerifyPR6219R8Premise_SilentOnBase  BUG CANARY: passes on base (no
//     event and no status condition mention tieredStore drift), flips to FAIL
//     on the head where the PR reports the drift (condition TieredStoreSynced
//     + RuntimeTieredStoreConflict event).

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
	verifyPR6219R8PremiseName      = "verify-tsedit-r8"
	verifyPR6219R8PremiseNamespace = "default"
)

func verifyPR6219R8PremiseScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = datav1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = workloadv1alpha1.AddToScheme(scheme)
	return scheme
}

func verifyPR6219R8PremiseRuntime() *datav1alpha1.CacheRuntime {
	return &datav1alpha1.CacheRuntime{
		TypeMeta: metav1.TypeMeta{APIVersion: "data.fluid.io/v1alpha1", Kind: "CacheRuntime"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      verifyPR6219R8PremiseName,
			Namespace: verifyPR6219R8PremiseNamespace,
			UID:       "verify-tsedit-r8-uid",
		},
		Spec: datav1alpha1.CacheRuntimeSpec{
			RuntimeClassName: "verify-tsedit-r8-class",
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

func verifyPR6219R8PremiseClass() *datav1alpha1.CacheRuntimeClass {
	return &datav1alpha1.CacheRuntimeClass{
		ObjectMeta:     metav1.ObjectMeta{Name: "verify-tsedit-r8-class"},
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

func verifyPR6219R8PremiseWorkerASTS(quota string) *workloadv1alpha1.AdvancedStatefulSet {
	replicas := int32(1)
	q := resource.MustParse(quota)
	return &workloadv1alpha1.AdvancedStatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      common.GetCacheComponentName(verifyPR6219R8PremiseName, common.ComponentTypeWorker),
			Namespace: verifyPR6219R8PremiseNamespace,
		},
		Spec: workloadv1alpha1.AdvancedStatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "verify-tsedit-r8-worker"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "verify-tsedit-r8-worker"}},
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
		Status: workloadv1alpha1.AdvancedStatefulSetStatus{
			ReadyReplicas:     1,
			CurrentReplicas:   1,
			AvailableReplicas: 1,
		},
	}
}

func verifyPR6219R8PremiseMasterASTS() *workloadv1alpha1.AdvancedStatefulSet {
	replicas := int32(1)
	return &workloadv1alpha1.AdvancedStatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      common.GetCacheComponentName(verifyPR6219R8PremiseName, common.ComponentTypeMaster),
			Namespace: verifyPR6219R8PremiseNamespace,
		},
		Spec: workloadv1alpha1.AdvancedStatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "verify-tsedit-r8-master"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "verify-tsedit-r8-master"}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "master", Image: "verify-master:latest"}}},
			},
		},
		Status: workloadv1alpha1.AdvancedStatefulSetStatus{
			ReadyReplicas:     1,
			CurrentReplicas:   1,
			AvailableReplicas: 1,
		},
	}
}

// verifyPR6219R8PremiseClientDS is the client DaemonSet the full Sync path
// reads for component readiness (0 desired / 0 ready = fully ready).
func verifyPR6219R8PremiseClientDS() *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      common.GetCacheComponentName(verifyPR6219R8PremiseName, common.ComponentTypeClient),
			Namespace: verifyPR6219R8PremiseNamespace,
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "verify-tsedit-r8-client"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "verify-tsedit-r8-client"}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "client", Image: "verify-client:latest"}}},
			},
		},
		Status: appsv1.DaemonSetStatus{NumberReady: 0, DesiredNumberScheduled: 0},
	}
}

func verifyPR6219R8PremiseEngine(t *testing.T) (*CacheEngine, *record.FakeRecorder, cruntime.ReconcileRequestContext) {
	t.Helper()
	dataset := &datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: verifyPR6219R8PremiseName, Namespace: verifyPR6219R8PremiseNamespace, UID: "verify-dataset-r8-uid"},
		Spec:       datav1alpha1.DatasetSpec{},
	}
	rt := verifyPR6219R8PremiseRuntime()
	datasetForStatus := dataset
	c := fake.NewClientBuilder().WithScheme(verifyPR6219R8PremiseScheme()).
		WithObjects(datasetForStatus, rt, verifyPR6219R8PremiseClass(),
			verifyPR6219R8PremiseMasterASTS(), verifyPR6219R8PremiseWorkerASTS("8Gi"),
			verifyPR6219R8PremiseClientDS()).
		WithStatusSubresource(datasetForStatus, rt).
		Build()
	recorder := record.NewFakeRecorder(100)
	e := &CacheEngine{
		name:      verifyPR6219R8PremiseName,
		namespace: verifyPR6219R8PremiseNamespace,
		Client:    c,
		Log:       ctrl.Log.WithName("verify-pr6219-r8-premise"),
		Recorder:  recorder,
	}
	ctx := cruntime.ReconcileRequestContext{
		Client:         c,
		Context:        context.Background(),
		Log:            ctrl.Log.WithName("verify-pr6219-r8-premise"),
		RuntimeType:    "cache",
		NamespacedName: types.NamespacedName{Name: verifyPR6219R8PremiseName, Namespace: verifyPR6219R8PremiseNamespace},
	}
	return e, recorder, ctx
}

func verifyPR6219R8PremiseSyncOnce(t *testing.T, e *CacheEngine, ctx cruntime.ReconcileRequestContext) {
	t.Helper()
	rt, err := e.getRuntime()
	if err != nil {
		t.Fatalf("getRuntime: %v", err)
	}
	class, err := e.getRuntimeClass(ctx.Context, rt.Spec.RuntimeClassName)
	if err != nil {
		t.Fatalf("getRuntimeClass: %v", err)
	}
	if err := e.syncRuntimeValueConfigMap(ctx, rt, class); err != nil {
		t.Fatalf("syncRuntimeValueConfigMap: %v", err)
	}
	if _, err := e.syncRuntimeSpec(ctx, rt, class); err != nil {
		t.Fatalf("syncRuntimeSpec: %v", err)
	}
}

func verifyPR6219R8PremiseEditQuota(t *testing.T, e *CacheEngine, ctx cruntime.ReconcileRequestContext, newQuota string) {
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

func verifyPR6219R8PremiseConfigMapQuota(t *testing.T, e *CacheEngine, ctx cruntime.ReconcileRequestContext) string {
	t.Helper()
	cm := &corev1.ConfigMap{}
	if err := e.Client.Get(ctx.Context, types.NamespacedName{
		Name:      common.GetCacheRuntimeConfigConfigMapName(verifyPR6219R8PremiseName),
		Namespace: verifyPR6219R8PremiseNamespace,
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

func verifyPR6219R8PremiseWorkerVolumeSize(t *testing.T, e *CacheEngine, ctx cruntime.ReconcileRequestContext) string {
	t.Helper()
	sts := &workloadv1alpha1.AdvancedStatefulSet{}
	if err := e.Client.Get(ctx.Context, types.NamespacedName{
		Name:      common.GetCacheComponentName(verifyPR6219R8PremiseName, common.ComponentTypeWorker),
		Namespace: verifyPR6219R8PremiseNamespace,
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

func verifyPR6219R8PremiseDrainEvents(recorder *record.FakeRecorder) []string {
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
func TestVerifyPR6219R8Premise_ConfigMapFollowsSpecPodDoesNot(t *testing.T) {
	e, _, ctx := verifyPR6219R8PremiseEngine(t)

	verifyPR6219R8PremiseSyncOnce(t, e, ctx)
	if got := verifyPR6219R8PremiseConfigMapQuota(t, e, ctx); got != "8Gi" {
		t.Fatalf("steady-state configmap quota = %s, want 8Gi", got)
	}
	if got := verifyPR6219R8PremiseWorkerVolumeSize(t, e, ctx); got != "8Gi" {
		t.Fatalf("steady-state worker volume sizeLimit = %s, want 8Gi", got)
	}

	verifyPR6219R8PremiseEditQuota(t, e, ctx, "16Gi")
	verifyPR6219R8PremiseSyncOnce(t, e, ctx)

	if got := verifyPR6219R8PremiseConfigMapQuota(t, e, ctx); got != "16Gi" {
		t.Errorf("SPLIT NOT REPRODUCED (configmap side): configmap quota = %s, want 16Gi (configmap follows spec)", got)
	}
	if got := verifyPR6219R8PremiseWorkerVolumeSize(t, e, ctx); got != "8Gi" {
		t.Errorf("SPLIT NOT REPRODUCED (pod side): worker volume sizeLimit = %s, want 8Gi (pod keeps creation-time size)", got)
	}
}

// BUG CANARY: passes on base (the split is silent — premise reproduced), flips
// to FAIL on the fixed head (a RuntimeTieredStoreConflict event and/or a
// TieredStoreSynced=False status condition appears). String matches are
// deliberate so the file compiles against base.
func TestVerifyPR6219R8Premise_SilentOnBase(t *testing.T) {
	e, recorder, ctx := verifyPR6219R8PremiseEngine(t)

	if err := e.Sync(ctx); err != nil {
		t.Fatalf("steady-state full Sync: %v", err)
	}
	verifyPR6219R8PremiseEditQuota(t, e, ctx, "16Gi")
	if err := e.Sync(ctx); err != nil {
		t.Fatalf("post-edit full Sync: %v", err)
	}

	events := verifyPR6219R8PremiseDrainEvents(recorder)
	for _, ev := range events {
		fmt.Printf("[premise-canary] observed event: %s\n", ev)
	}
	for _, ev := range events {
		if strings.Contains(ev, "TieredStoreConflict") || strings.Contains(ev, "TieredStoreInvalid") {
			t.Fatalf("CANARY FLIPPED (this is the fix working on head): unexpected tieredStore event: %s", ev)
		}
	}

	rt, err := e.getRuntime()
	if err != nil {
		t.Fatalf("getRuntime: %v", err)
	}
	for _, cond := range rt.Status.Conditions {
		if strings.Contains(string(cond.Type), "TieredStoreSynced") {
			t.Fatalf("CANARY FLIPPED (this is the fix working on head): unexpected TieredStoreSynced condition: %+v", cond)
		}
	}
	t.Logf("split stayed silent: %d events, no tieredStore condition (expected on base)", len(events))
}
