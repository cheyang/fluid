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

// Reviewer verification harness for PR #6177, round 3 (delta e259d738..497bbeaf).
//
// The delta adds the merged-resources validation the round-2 review asked for
// (ledger F6): validateResourceRequirements / validateComponentResources /
// validDesiredResources. These tests pin the parts of the fix the PR's own tests
// cover only in ginkgo or only for the worker-cpu shape, and follow the conflict
// through the whole component lifecycle rather than the new return value alone:
//
//   - D4: on the sync path a conflicting worker merge must leave the workload's
//     resources untouched while replicas still sync (a conflict must not abort
//     the component), and must emit a RuntimeResourcesConflict Warning event.
//   - D5: on the creation path a conflicting pair must fail the transform with
//     an error naming the component and the conflicting keys (setup fails loudly
//     instead of rendering a pod spec the API server will reject at rollout).
//   - D6: the master component shares the same code path but its own call site
//     in syncRuntimeSpec — pin it there too.

import (
	"context"
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
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// v6177r3ConflictReason mirrors common.RuntimeResourcesConflict (added by the
// round-3 delta) but stays a literal so this harness compiles on the pre-fix
// head too — the polarity check runs the same file on both.
const v6177r3ConflictReason = "RuntimeResourcesConflict"

// v6177r3Template builds the round-3 conflicting pair on the memory key (the
// PR's own tests use cpu; a different key keeps this an independent check):
// template requests.memory=2Gi / limits.memory=4Gi, CacheRuntime lowers
// limits.memory to 1Gi. Each side is valid on its own.
var v6177r3Template = corev1.ResourceRequirements{
	Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("2Gi")},
	Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("4Gi")},
}

var v6177r3Overlay = corev1.ResourceRequirements{
	Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")},
}

func v6177r3PodTemplate(resources corev1.ResourceRequirements, name string) corev1.PodTemplateSpec {
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

// D4 (contract): a conflicting worker merge on the sync path must refuse the
// resource update but not the rest of the component sync — replicas still move —
// and must say why via a Warning event on the CacheRuntime.
func TestD4SyncConflictSkipsResourcesButSyncsReplicas(t *testing.T) {
	replicas := int32(1)
	asts := &workloadv1alpha1.AdvancedStatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "test-worker", Namespace: "default"},
		Spec: workloadv1alpha1.AdvancedStatefulSetSpec{
			Replicas: &replicas,
			Template: v6177r3PodTemplate(v6177r3Template, "worker"),
		},
	}
	runtimeObj := &datav1alpha1.CacheRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: datav1alpha1.CacheRuntimeSpec{
			RuntimeClassName: "test-class",
			Worker: datav1alpha1.CacheRuntimeWorkerSpec{
				Replicas:                   3, // the conflict must not block the replica update
				RuntimeComponentCommonSpec: datav1alpha1.RuntimeComponentCommonSpec{Resources: v6177r3Overlay},
			},
		},
	}
	runtimeClass := &datav1alpha1.CacheRuntimeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "test-class"},
		Topology: &datav1alpha1.RuntimeTopology{
			Worker: &datav1alpha1.RuntimeComponentDefinition{Template: v6177r3PodTemplate(v6177r3Template, "worker")},
		},
	}
	engine := v6177r2Engine(t, asts, runtimeObj, runtimeClass)
	recorder := record.NewFakeRecorder(10)
	engine.Recorder = recorder

	if err := engine.syncRuntimeSpec(cruntime.ReconcileRequestContext{Context: context.TODO()}, runtimeObj, runtimeClass); err != nil {
		t.Fatalf("syncRuntimeSpec failed: %v — a conflicting merge must be skipped, not fail the sync", err)
	}

	key := types.NamespacedName{Name: "test-worker", Namespace: "default"}
	synced := &workloadv1alpha1.AdvancedStatefulSet{}
	if err := engine.Client.Get(context.TODO(), key, synced); err != nil {
		t.Fatalf("get ASTS: %v", err)
	}

	// Resources: untouched — the workload keeps the template combination, not
	// the invalid merge (requests.memory 2Gi above limits.memory 1Gi).
	got := synced.Spec.Template.Spec.Containers[0].Resources
	if got.Requests.Memory().String() != "2Gi" || got.Limits.Memory().String() != "4Gi" {
		t.Fatalf("workload resources: got requests.memory=%s limits.memory=%s, want the untouched 2Gi / 4Gi",
			got.Requests.Memory().String(), got.Limits.Memory().String())
	}

	// Replicas: the conflict skips the resource patch, not the component.
	if synced.Spec.Replicas == nil || *synced.Spec.Replicas != 3 {
		t.Fatalf("replicas: got %v, want 3 — the resource conflict must not block the replica sync", synced.Spec.Replicas)
	}

	select {
	case ev := <-recorder.Events:
		if !strings.Contains(ev, v6177r3ConflictReason) {
			t.Errorf("event reason: got %q, want it to contain %q", ev, v6177r3ConflictReason)
		}
		if !strings.Contains(ev, "memory request 2Gi exceeds limit 1Gi") {
			t.Errorf("event body: got %q, want it to name the conflicting pair", ev)
		}
	default:
		t.Fatal("no RuntimeResourcesConflict event was emitted for the conflicting merge")
	}
}

// D5 (contract): the creation path must refuse to render a conflicting pair —
// transformWorker fails with an error naming the component and the keys, so a
// fresh CacheRuntime never reaches a rollout the API server would reject.
func TestD5CreationRejectsConflictingPair(t *testing.T) {
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
		Log: ctrl.Log.WithName("verify-delta-r3-6177"),
	}

	tErr := engine.transformWorker(dataset, runtimeObj, runtimeClass, config, value)
	if tErr == nil {
		t.Fatal("transformWorker accepted a conflicting pair: the creation path must fail loudly")
	}
	if !strings.Contains(tErr.Error(), "invalid worker resources") || !strings.Contains(tErr.Error(), "memory request 2Gi exceeds limit 1Gi") {
		t.Errorf("error: got %q, want it to name the worker component and the conflicting pair", tErr.Error())
	}
}

// D6 (contract): the master component's own call site in syncRuntimeSpec must
// refuse a conflicting merge the same way the worker does.
func TestD6MasterSyncConflictSkipsResources(t *testing.T) {
	replicas := int32(1)
	asts := &workloadv1alpha1.AdvancedStatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "test-master", Namespace: "default"},
		Spec: workloadv1alpha1.AdvancedStatefulSetSpec{
			Replicas: &replicas,
			Template: v6177r3PodTemplate(v6177r3Template, "master"),
		},
	}
	runtimeObj := &datav1alpha1.CacheRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: datav1alpha1.CacheRuntimeSpec{
			RuntimeClassName: "test-class",
			Master: datav1alpha1.CacheRuntimeMasterSpec{
				Replicas:                   2,
				RuntimeComponentCommonSpec: datav1alpha1.RuntimeComponentCommonSpec{Resources: v6177r3Overlay},
			},
		},
	}
	runtimeClass := &datav1alpha1.CacheRuntimeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "test-class"},
		Topology: &datav1alpha1.RuntimeTopology{
			Master: &datav1alpha1.RuntimeComponentDefinition{Template: v6177r3PodTemplate(v6177r3Template, "master")},
		},
	}
	engine := v6177r2Engine(t, asts, runtimeObj, runtimeClass)
	recorder := record.NewFakeRecorder(10)
	engine.Recorder = recorder

	if err := engine.syncRuntimeSpec(cruntime.ReconcileRequestContext{Context: context.TODO()}, runtimeObj, runtimeClass); err != nil {
		t.Fatalf("syncRuntimeSpec failed: %v — a conflicting master merge must be skipped, not fail the sync", err)
	}

	key := types.NamespacedName{Name: "test-master", Namespace: "default"}
	synced := &workloadv1alpha1.AdvancedStatefulSet{}
	if err := engine.Client.Get(context.TODO(), key, synced); err != nil {
		t.Fatalf("get ASTS: %v", err)
	}

	got := synced.Spec.Template.Spec.Containers[0].Resources
	if got.Requests.Memory().String() != "2Gi" || got.Limits.Memory().String() != "4Gi" {
		t.Fatalf("master resources: got requests.memory=%s limits.memory=%s, want the untouched 2Gi / 4Gi",
			got.Requests.Memory().String(), got.Limits.Memory().String())
	}
	if synced.Spec.Replicas == nil || *synced.Spec.Replicas != 2 {
		t.Fatalf("replicas: got %v, want 2 — the resource conflict must not block the master replica sync", synced.Spec.Replicas)
	}

	select {
	case ev := <-recorder.Events:
		if !strings.Contains(ev, v6177r3ConflictReason) || !strings.Contains(ev, "memory request 2Gi exceeds limit 1Gi") {
			t.Errorf("event: got %q, want RuntimeResourcesConflict naming the conflicting pair", ev)
		}
	default:
		t.Fatal("no RuntimeResourcesConflict event was emitted for the master conflict")
	}
}
