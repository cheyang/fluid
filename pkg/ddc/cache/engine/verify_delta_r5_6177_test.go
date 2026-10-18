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

// Reviewer verification harness for PR #6177, round 5 (delta 3adc547d..ce4f0c4f).
//
// The delta closes three carried ledger items:
//   - F7 (94121e27): syncRuntimeSpec guards against a class with a nil Topology
//     instead of dereferencing it. D9 pins that: a topology-less class must sync
//     nothing (no panic) and leave the workloads untouched. Expected to FAIL
//     (panic) on the previous head 3adc547d and PASS on ce4f0c4f.
//   - F8 (44e6c90a + ce4f0c4f): the resources equivalence table renders
//     tiered-store workloads and pins the quota each row charges. D10 mirrors
//     the new rows at the pure layer: the creation render charges the tiered
//     store quota on the merged memory, and the sync resolution plus the same
//     quota equals the creation render. A regression pin (both heads pass) —
//     the PR's own new specs are the coverage fix; D10 keeps the invariant
//     pinned in this harness.
//   - F10 (71d94397 + 4a8668f9): the creation path routes validation through
//     checkComponentResources, which emits the same RuntimeResourcesConflict
//     Warning event the sync path emits. D11 drives transformWorker with a
//     conflicting merge and asserts the error AND the event. Expected to FAIL
//     on 3adc547d (error, no event) and PASS on ce4f0c4f.

import (
	"context"
	"strings"
	"testing"

	workloadv1alpha1 "github.com/fluid-cloudnative/advanced-statefulset/api/workload/v1alpha1"
	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	cruntime "github.com/fluid-cloudnative/fluid/pkg/runtime"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// v6177r5Full is the #6173 template baseline: a full set of resources.
var v6177r5Full = corev1.ResourceRequirements{
	Requests: corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("1"),
		corev1.ResourceMemory: resource.MustParse("2Gi"),
	},
	Limits: corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("2"),
		corev1.ResourceMemory: resource.MustParse("4Gi"),
	},
}

func v6177r5PodTemplate(resources corev1.ResourceRequirements, name string) corev1.PodTemplateSpec {
	return corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:      name,
				Image:     "fluid/cache:v1",
				Resources: *resources.DeepCopy(),
			}},
		},
	}
}

// D9 (contract, ledger F7 — fixed by 94121e27): a CacheRuntimeClass without a
// topology must not panic syncRuntimeSpec. getRuntimeClass rejects such a class
// before syncRuntimeSpec is reached, so this is defense in depth for direct
// callers; the guard must leave both workloads untouched.
func TestD9SyncWithTopologylessClassIsANoOp(t *testing.T) {
	replicas := int32(1)
	asts := &workloadv1alpha1.AdvancedStatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "test-worker", Namespace: "default"},
		Spec: workloadv1alpha1.AdvancedStatefulSetSpec{
			Replicas: &replicas,
			Template: v6177r5PodTemplate(v6177r5Full, "worker"),
		},
	}
	runtimeObj := &datav1alpha1.CacheRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: datav1alpha1.CacheRuntimeSpec{
			RuntimeClassName: "test-class",
			Worker: datav1alpha1.CacheRuntimeWorkerSpec{
				Replicas: 2,
				RuntimeComponentCommonSpec: datav1alpha1.RuntimeComponentCommonSpec{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("8Gi")},
					},
				},
			},
		},
	}
	runtimeClass := &datav1alpha1.CacheRuntimeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "test-class"},
		// The condition under review: no topology at all.
		Topology: nil,
	}

	engine := v6177r2Engine(t, asts, runtimeObj, runtimeClass)

	panicked := true
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("syncRuntimeSpec panicked on a topology-less class: %v — the guard must return before the first dereference", r)
			}
		}()
		if err := engine.syncRuntimeSpec(cruntime.ReconcileRequestContext{Context: context.TODO()}, runtimeObj, runtimeClass); err != nil {
			t.Fatalf("syncRuntimeSpec returned %v — a topology-less class has no component to sync, so it is a no-op, not an error", err)
		}
		panicked = false
	}()
	if panicked {
		return // the deferred check already reported the panic
	}

	key := types.NamespacedName{Name: "test-worker", Namespace: "default"}
	synced := &workloadv1alpha1.AdvancedStatefulSet{}
	if err := engine.Client.Get(context.TODO(), key, synced); err != nil {
		t.Fatalf("get ASTS: %v", err)
	}
	got := synced.Spec.Template.Spec.Containers[0].Resources
	if got.Limits.Memory().String() != "4Gi" || got.Requests.Memory().String() != "2Gi" {
		t.Errorf("workload resources: got requests.memory=%s limits.memory=%s, want the untouched template 2Gi/4Gi",
			got.Requests.Memory().String(), got.Limits.Memory().String())
	}
	if synced.Spec.Replicas == nil || *synced.Spec.Replicas != 1 {
		t.Errorf("replicas: got %v, want the untouched 1", synced.Spec.Replicas)
	}
}

// D10 (contract, ledger F8 — coverage fixed by 44e6c90a + ce4f0c4f): a
// tiered-store workload's memory must agree between the creation render and the
// sync resolution. The creation path charges the tiered store quota (2Gi
// emptyDir medium=Memory) on top of the merged memory; the sync path must
// resolve the same merged baseline and add the same quota read back from the
// rendered workload — otherwise the first reconcile after creation would patch
// the workload it just rendered.
func TestD10TieredStoreQuotaAgreesAcrossPaths(t *testing.T) {
	e := &CacheEngine{name: "test", namespace: "default"}
	runtimeSpec := datav1alpha1.CacheRuntimeSpec{
		RuntimeClassName: "test-class",
		Worker: datav1alpha1.CacheRuntimeWorkerSpec{
			Replicas: 1,
			// The #6173 partial override: only limits.memory is named.
			RuntimeComponentCommonSpec: datav1alpha1.RuntimeComponentCommonSpec{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("8Gi")},
				},
			},
		},
	}
	definition := &datav1alpha1.RuntimeComponentDefinition{Template: v6177r5PodTemplate(v6177r5Full, "worker")}

	value, err := e.initComponentValue(common.ComponentTypeWorker, definition, nil, 1)
	if err != nil {
		t.Fatalf("initComponentValue: %v", err)
	}
	e.transformComponentPodTemplate(runtimeSpec, runtimeSpec.Worker.RuntimeComponentCommonSpec,
		&datav1alpha1.Dataset{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"}}, value)
	tieredStore := &datav1alpha1.RuntimeTieredStore{
		Levels: []datav1alpha1.RuntimeTieredStoreLevel{{
			EmptyDir: &datav1alpha1.EmptyDirMediumSource{
				Quota:  resource.MustParse("2Gi"),
				Medium: corev1.StorageMediumMemory,
			},
		}},
	}
	if err := e.TransformRuntimeTieredStore(tieredStore, &value.PodTemplateSpec.Spec); err != nil {
		t.Fatalf("TransformRuntimeTieredStore: %v", err)
	}

	rendered := value.PodTemplateSpec.Spec.Containers[0].Resources
	quota := chargedTieredStoreMemoryQuota(&value.PodTemplateSpec.Spec)
	if quota.Cmp(resource.MustParse("2Gi")) != 0 {
		t.Fatalf("charged quota: got %s, want 2Gi — the row cannot pass with the quota silently dropped", quota.String())
	}
	if got := rendered.Requests.Memory().String(); got != "4Gi" {
		t.Errorf("creation requests.memory: got %s, want 4Gi (2Gi merged baseline + 2Gi quota)", got)
	}
	if got := rendered.Limits.Memory().String(); got != "10Gi" {
		t.Errorf("creation limits.memory: got %s, want 10Gi (8Gi merged override + 2Gi quota)", got)
	}

	// The sync side: merged baseline plus the quota read back from the workload.
	merged := desiredComponentResources(runtimeSpec.Worker.Resources, definition)
	desired := withTieredStoreMemoryQuota(*merged, quota)
	if !equality.Semantic.DeepEqual(rendered, desired) {
		t.Errorf("paths disagree: creation rendered %+v, sync wants %+v — a freshly rendered workload would be patched on its first sync", rendered, desired)
	}
}

// D11 (contract, ledger F10 — fixed by 71d94397 + 4a8668f9): the creation path
// must surface a conflicting merge as a RuntimeResourcesConflict Warning event
// on the CacheRuntime, not only as the generic setup error. The worker call
// site is driven here; master and client share checkComponentResources, and
// the PR's own specs cover all three.
func TestD11CreationConflictEmitsRuntimeResourcesConflictEvent(t *testing.T) {
	dataset := &datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: "test-dataset", Namespace: "default", UID: "test-dataset-uid"},
	}
	runtimeObj := &datav1alpha1.CacheRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: "test-runtime", Namespace: "default"},
		Spec: datav1alpha1.CacheRuntimeSpec{
			RuntimeClassName: "test-class",
			Worker: datav1alpha1.CacheRuntimeWorkerSpec{
				RuntimeComponentCommonSpec: datav1alpha1.RuntimeComponentCommonSpec{Resources: v6177r3Overlay},
			},
		},
	}
	runtimeClass := &datav1alpha1.CacheRuntimeClass{
		ObjectMeta:     metav1.ObjectMeta{Name: "test-class"},
		FileSystemType: "test-fs",
		Topology: &datav1alpha1.RuntimeTopology{
			Worker: &datav1alpha1.RuntimeComponentDefinition{Template: v6177r3PodTemplate(v6177r3Template, "worker")},
		},
	}
	config := &CacheRuntimeComponentCommonConfig{
		Owner: &common.OwnerReference{
			APIVersion: "data.fluid.io/v1alpha1",
			Kind:       "CacheRuntime",
			Name:       "test-runtime",
			UID:        "test-uid",
		},
		RuntimeConfigs: &RuntimeConfigVolumeConfig{
			RuntimeConfigVolume: corev1.Volume{
				Name: "runtime-config",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: "test-runtime-config"},
					},
				},
			},
			RuntimeConfigVolumeMount: corev1.VolumeMount{
				Name:      "runtime-config",
				MountPath: "/etc/fluid/config",
			},
			ExtraConfigMapNames: make(map[string]bool),
		},
	}
	value := &common.CacheRuntimeValue{}

	engine := &CacheEngine{
		name:      "test-runtime",
		namespace: "default",
		Client: fake.NewClientBuilder().
			WithScheme(CacheEngineTestScheme).
			WithObjects(dataset, runtimeObj).
			Build(),
		Log: ctrl.Log.WithName("verify-delta-r5-6177"),
	}
	recorder := record.NewFakeRecorder(10)
	engine.Recorder = recorder

	tErr := engine.transformWorker(dataset, runtimeObj, runtimeClass, config, value)
	if tErr == nil {
		t.Fatal("transformWorker accepted a conflicting pair: the creation path must fail loudly")
	}
	if !strings.Contains(tErr.Error(), "invalid worker resources") || !strings.Contains(tErr.Error(), "memory request 2Gi exceeds limit 1Gi") {
		t.Errorf("error: got %q, want it to name the worker component and the conflicting pair", tErr.Error())
	}

	select {
	case ev := <-recorder.Events:
		if !strings.Contains(ev, v6177r3ConflictReason) {
			t.Errorf("event: got %q, want the RuntimeResourcesConflict reason", ev)
		}
		if !strings.Contains(ev, "memory request 2Gi exceeds limit 1Gi") {
			t.Errorf("event: got %q, want it to name the conflicting pair", ev)
		}
	default:
		t.Fatal("no RuntimeResourcesConflict event was emitted on the creation path — the conflict is only visible in the generic setup failure")
	}
	select {
	case ev := <-recorder.Events:
		t.Errorf("unexpected second event: %q", ev)
	default:
	}
}
