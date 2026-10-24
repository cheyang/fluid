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

import (
	"context"
	"encoding/json"
	"testing"

	workloadv1alpha1 "github.com/fluid-cloudnative/advanced-statefulset/api/workload/v1alpha1"
	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	cruntime "github.com/fluid-cloudnative/fluid/pkg/runtime"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestPremiseTieredStoreSplitWorker reproduces the premise of PR #6219 (issue #6192)
// on any revision: after the worker workload is created, editing
// spec.worker.tieredStore updates the runtime ConfigMap (recomputed from spec on
// every sync) but leaves the worker volume sizeLimit at its creation-time value.
//
// This is a symptom-demonstration test: it asserts the *split* itself, so it is
// expected to pass on the base branch (where the problem lives) and on the PR head
// (which warns about the split but does not remove it). The recorder is attached and
// its events are only logged, not asserted, so the same run also records whether the
// revision under test reports the split in any way.
func TestPremiseTieredStoreSplitWorker(t *testing.T) {
	const (
		name      = "test-runtime"
		namespace = "default"
	)

	scheme := CacheEngineTestScheme

	dataset := &datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: "test-dataset-uid"},
		Spec:       datav1alpha1.DatasetSpec{},
	}
	runtimeObj := &datav1alpha1.CacheRuntime{
		TypeMeta: metav1.TypeMeta{APIVersion: "data.fluid.io/v1alpha1", Kind: "CacheRuntime"},
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: namespace, UID: "test-runtime-uid",
		},
		Spec: datav1alpha1.CacheRuntimeSpec{
			RuntimeClassName: "test-class",
			Master:           datav1alpha1.CacheRuntimeMasterSpec{Replicas: 1},
			Worker: datav1alpha1.CacheRuntimeWorkerSpec{
				Replicas: 2,
				TieredStore: datav1alpha1.RuntimeTieredStore{
					Levels: []datav1alpha1.RuntimeTieredStoreLevel{
						{ProcessMemory: &datav1alpha1.ProcessMemoryMediumSource{Quota: resource.MustParse("8Gi")}},
					},
				},
			},
			Client: datav1alpha1.CacheRuntimeClientSpec{},
		},
	}
	runtimeClass := &datav1alpha1.CacheRuntimeClass{
		ObjectMeta:     metav1.ObjectMeta{Name: "test-class"},
		FileSystemType: "test-fs",
		Topology: &datav1alpha1.RuntimeTopology{
			Master: &datav1alpha1.RuntimeComponentDefinition{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "master", Image: "test-master:latest"}}},
				},
			},
			Worker: &datav1alpha1.RuntimeComponentDefinition{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "worker", Image: "test-worker:latest"}}},
				},
			},
			Client: &datav1alpha1.RuntimeComponentDefinition{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "client", Image: "test-client:latest"}}},
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(dataset, runtimeObj, runtimeClass).
		WithStatusSubresource(dataset, runtimeObj).
		Build()

	recorder := record.NewFakeRecorder(100)
	engine := &CacheEngine{
		name:      name,
		namespace: namespace,
		Client:    fakeClient,
		Recorder:  recorder,
		Log:       ctrl.Log.WithName("premise-test"),
	}
	ctx := cruntime.ReconcileRequestContext{
		Client:         fakeClient,
		Context:        context.Background(),
		Log:            ctrl.Log.WithName("premise-test"),
		RuntimeType:    "cache",
		NamespacedName: types.NamespacedName{Name: name, Namespace: namespace},
	}

	// Creation: build the workloads the same way Setup does, so the worker
	// volumes carry the creation-time tieredStore.
	rt, err := engine.getRuntime()
	if err != nil {
		t.Fatalf("getRuntime: %v", err)
	}
	value, err := engine.transform(dataset, rt, runtimeClass)
	if err != nil {
		t.Fatalf("transform: %v", err)
	}
	if _, err = engine.SetupMasterComponent(value.Master); err != nil {
		t.Fatalf("SetupMasterComponent: %v", err)
	}
	if _, err = engine.SetupWorkerComponent(value.Worker); err != nil {
		t.Fatalf("SetupWorkerComponent: %v", err)
	}

	// Sanity: creation-time volume carries the 8Gi quota.
	mustFindVolumeSizeLimit := func(t *testing.T) resource.Quantity {
		t.Helper()
		asts := &workloadv1alpha1.AdvancedStatefulSet{}
		if err := fakeClient.Get(ctx.Context, types.NamespacedName{Name: name + "-worker", Namespace: namespace}, asts); err != nil {
			t.Fatalf("get worker asts: %v", err)
		}
		for _, vol := range asts.Spec.Template.Spec.Volumes {
			if vol.Name == getMemoryTieredStoreVolumeName(0) {
				if vol.EmptyDir == nil || vol.EmptyDir.SizeLimit == nil {
					t.Fatalf("volume %s has no sizeLimit", vol.Name)
				}
				return *vol.EmptyDir.SizeLimit
			}
		}
		t.Fatalf("volume %s not found on worker asts", getMemoryTieredStoreVolumeName(0))
		return resource.Quantity{}
	}
	if got := mustFindVolumeSizeLimit(t); got.Cmp(resource.MustParse("8Gi")) != 0 {
		t.Fatalf("creation-time sizeLimit = %s, want 8Gi", got.String())
	}

	// Edit the spec quota, like `kubectl patch`, then run the sync path.
	edited, err := engine.getRuntime()
	if err != nil {
		t.Fatalf("getRuntime (edited): %v", err)
	}
	edited.Spec.Worker.TieredStore.Levels[0].ProcessMemory.Quota = resource.MustParse("16Gi")
	if err := fakeClient.Update(ctx.Context, edited); err != nil {
		t.Fatalf("update runtime: %v", err)
	}

	syncRuntime, err := engine.getRuntime()
	if err != nil {
		t.Fatalf("getRuntime (sync): %v", err)
	}
	if err := engine.syncRuntimeValueConfigMap(ctx, syncRuntime); err != nil {
		t.Fatalf("syncRuntimeValueConfigMap: %v", err)
	}
	if err := engine.syncRuntimeSpec(ctx, syncRuntime, runtimeClass); err != nil {
		t.Fatalf("syncRuntimeSpec: %v", err)
	}

	// The ConfigMap is recomputed from the edited spec...
	cm := &corev1.ConfigMap{}
	if err := fakeClient.Get(ctx.Context, types.NamespacedName{Name: "fluid-runtime-config-" + name, Namespace: namespace}, cm); err != nil {
		t.Fatalf("get configmap: %v", err)
	}
	raw, ok := cm.Data["runtime.json"]
	if !ok {
		t.Fatalf("runtime.json missing from configmap data: %v", cm.Data)
	}
	var cfg struct {
		Worker struct {
			TieredStoreLevels []struct {
				Quotas []string `json:"quotas"`
			} `json:"tieredStoreLevels"`
		} `json:"worker"`
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("unmarshal runtime.json: %v\n%s", err, raw)
	}
	if len(cfg.Worker.TieredStoreLevels) != 1 || len(cfg.Worker.TieredStoreLevels[0].Quotas) != 1 {
		t.Fatalf("unexpected worker tieredStoreLevels in runtime.json: %s", raw)
	}
	if got := cfg.Worker.TieredStoreLevels[0].Quotas[0]; got != "16Gi" {
		t.Fatalf("ConfigMap worker quota = %q, want %q (ConfigMap must follow the edited spec)", got, "16Gi")
	}

	// ...while the worker volume keeps its creation-time size.
	if got := mustFindVolumeSizeLimit(t); got.Cmp(resource.MustParse("8Gi")) != 0 {
		t.Fatalf("worker volume sizeLimit after sync = %s, want 8Gi (unchanged)", got.String())
	}

	// The split is real: ConfigMap says 16Gi, the volume says 8Gi. Log (not
	// assert) whatever the recorder saw, so the same run shows whether the
	// revision reports the split.
	var events []string
	for {
		select {
		case e := <-recorder.Events:
			events = append(events, e)
		default:
			if len(events) > 0 {
				t.Logf("recorder saw %d event(s) during sync: %v", len(events), events)
			} else {
				t.Log("recorder saw no events during sync: the split is not reported by this revision")
			}
			return
		}
	}
}
