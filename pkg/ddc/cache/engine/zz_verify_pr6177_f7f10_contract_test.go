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

// Round-5 contract tests for carried findings F7 and F10 (PR #6177), remediated
// by the 3adc547d..ce4f0c4f delta:
//
//	F7  (94121e27): syncRuntimeSpec now returns nil for a class without a
//	    topology instead of dereferencing runtimeClass.Topology.Master.
//	    Pre-existing (getRuntimeClass rejects such classes in production), so
//	    this pins the defense-in-depth guard for direct callers.
//	F10 (71d94397): the creation path now emits the specific
//	    RuntimeResourcesConflict Warning event when the merged resources are
//	    rejected, on top of the reconciler's generic ErrorProcessRuntime event,
//	    for all three components (worker, master, client).
//
// Polarity: CONTRACT tests. Only symbols that exist on base e0fc4c18, previous
// head 3adc547d and head ce4f0c4f are referenced (transformWorker /
// transformClient / syncRuntimeSpec), so the file compiles on every ref:
// RED on base (no validation at all), RED on 3adc547d (rejection without the
// event / nil-Topology panic, recovered into a clean failure), GREEN at head.

import (
	"context"
	"strings"
	"testing"

	workloadv1alpha1 "github.com/fluid-cloudnative/advanced-statefulset/api/workload/v1alpha1"
	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// F7 contract, integration layer (fake client): a CacheRuntimeClass without a
// topology must make syncRuntimeSpec a no-op (nil error, workloads untouched),
// not a nil-pointer panic. RED on previous head 3adc547d (panics at
// runtimeClass.Topology.Master; recovered here into a clean failure), GREEN at
// head ce4f0c4f.
func TestVerifyPR6177_F7_NilTopology_SyncIsNoop(t *testing.T) {
	template := verifyPR6177FullTemplateResources()
	engine, ctx, runtimeObj, runtimeClass := verifyPR6177BuildSyncFixture(template, &template, nil)
	runtimeClass.Topology = nil

	before := verifyPR6177WorkerResources(t, engine)

	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("syncRuntimeSpec panicked on a class without a topology: %v; "+
					"a missing topology must leave no component to sync", r)
			}
		}()
		err = engine.syncRuntimeSpec(ctx, runtimeObj, runtimeClass)
	}()
	if err != nil {
		t.Fatalf("syncRuntimeSpec returned %v for a topology-less class; want nil (nothing to sync)", err)
	}

	if got := verifyPR6177WorkerResources(t, engine); !resourceListEqual(got, before) {
		t.Errorf("topology-less sync mutated the worker workload: before %+v, after %+v; want untouched", before, got)
	}
	masterSts := &workloadv1alpha1.AdvancedStatefulSet{}
	if err := engine.Client.Get(context.Background(),
		types.NamespacedName{Name: "test-runtime-master", Namespace: "default"}, masterSts); err != nil {
		t.Fatalf("get master sts: %v", err)
	}
	if got := masterSts.Spec.Template.Spec.Containers[0].Resources; len(got.Requests) != 0 || len(got.Limits) != 0 {
		t.Errorf("topology-less sync mutated the master workload resources: %+v; want untouched (empty)", got)
	}
}

func resourceListEqual(a, b corev1.ResourceRequirements) bool {
	return a.Requests.Cpu().Cmp(*b.Requests.Cpu()) == 0 &&
		a.Requests.Memory().Cmp(*b.Requests.Memory()) == 0 &&
		a.Limits.Cpu().Cmp(*b.Limits.Cpu()) == 0 &&
		a.Limits.Memory().Cmp(*b.Limits.Memory()) == 0
}

// verifyPR6177TransformFixture builds the minimal transform-path fixture the
// F6 creation test established, parameterized by component so the F10 event
// contract can drive transformWorker and transformClient identically.
func verifyPR6177TransformFixture(t *testing.T, component string) (*CacheEngine, *datav1alpha1.Dataset, *datav1alpha1.CacheRuntime, *datav1alpha1.CacheRuntimeClass, *CacheRuntimeComponentCommonConfig, *common.CacheRuntimeValue, *record.FakeRecorder) {
	t.Helper()
	conflicting := corev1.ResourceRequirements{
		Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m")},
	}
	dataset := &datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: "test-dataset", Namespace: "default", UID: "test-dataset-uid"},
	}
	runtimeObj := &datav1alpha1.CacheRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: "test-runtime", Namespace: "default"},
		Spec:       datav1alpha1.CacheRuntimeSpec{RuntimeClassName: "test-class"},
	}
	template := corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name:      component,
			Image:     "test-" + component + ":latest",
			Resources: verifyPR6177FullTemplateResources(), // requests.cpu=1 vs overridden limits.cpu=500m
		}}},
	}
	topology := &datav1alpha1.RuntimeTopology{}
	switch component {
	case string(common.ComponentTypeWorker):
		runtimeObj.Spec.Worker.Resources = conflicting
		topology.Worker = &datav1alpha1.RuntimeComponentDefinition{Template: template}
	case string(common.ComponentTypeClient):
		runtimeObj.Spec.Client.Resources = conflicting
		topology.Client = &datav1alpha1.RuntimeComponentDefinition{Template: template}
	default:
		t.Fatalf("unsupported component %q", component)
	}
	runtimeClass := &datav1alpha1.CacheRuntimeClass{
		ObjectMeta:     metav1.ObjectMeta{Name: "test-class"},
		FileSystemType: "test-fs",
		Topology:       topology,
	}

	fakeClient := fake.NewClientBuilder().WithScheme(CacheEngineTestScheme).WithObjects(dataset, runtimeObj).Build()
	recorder := record.NewFakeRecorder(10)
	engine := &CacheEngine{
		name:      "test-runtime",
		namespace: "default",
		Client:    fakeClient,
		Log:       ctrl.Log.WithName("test"),
		Recorder:  recorder,
	}
	config := &CacheRuntimeComponentCommonConfig{
		Owner: &common.OwnerReference{
			APIVersion: "data.fluid.io/v1alpha1",
			Kind:       "CacheRuntime",
			Name:       "test-runtime",
			UID:        "test-uid",
		},
		// Needed so old heads (which lack the validation and proceed into
		// transformVolumes) fail cleanly instead of panicking.
		RuntimeConfigs: &RuntimeConfigVolumeConfig{
			RuntimeConfigVolume: corev1.Volume{
				Name: "runtime-config",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: "test-runtime-config"},
					},
				},
			},
			RuntimeConfigVolumeMount: corev1.VolumeMount{Name: "runtime-config", MountPath: "/etc/fluid/config"},
			ExtraConfigMapNames:      make(map[string]bool),
		},
	}
	return engine, dataset, runtimeObj, runtimeClass, config, &common.CacheRuntimeValue{}, recorder
}

// verifyPR6177CheckConflictEvent asserts the F10 contract shared by every
// creation-path component: the transform must fail, and it must emit exactly
// one Warning event with the specific RuntimeResourcesConflict reason naming
// the conflict -- not only the reconciler's generic ErrorProcessRuntime.
func verifyPR6177CheckConflictEvent(t *testing.T, component string, err error, recorder *record.FakeRecorder) {
	t.Helper()
	if err == nil {
		t.Fatalf("transform<%s> rendered a pod spec with requests.cpu=1 above limits.cpu=500m without an error",
			component)
	}
	if !strings.Contains(err.Error(), "exceeds limit") {
		t.Fatalf("transform<%s> error = %q, want it to name the conflicting key (\"exceeds limit\")", component, err)
	}
	select {
	case event := <-recorder.Events:
		if !strings.Contains(event, "Warning") || !strings.Contains(event, "RuntimeResourcesConflict") {
			t.Errorf("event = %q, want a Warning event with the specific reason RuntimeResourcesConflict "+
				"(the generic ErrorProcessRuntime from the reconciler does not say why)", event)
		}
		if !strings.Contains(event, "cpu request 1 exceeds limit 500m") {
			t.Errorf("event = %q, want it to carry the validator's message naming the conflict", event)
		}
	default:
		t.Errorf("no event emitted on the creation-path rejection: the conflict reason was visible only as " +
			"a generic setup failure (F10)")
	}
	select {
	case extra := <-recorder.Events:
		t.Errorf("unexpected second event %q: exactly one RuntimeResourcesConflict event is wanted", extra)
	default:
	}
}

// F10 contract, unit layer, worker creation path.
// RED on base e0fc4c18 (no validation, no error) and on previous head 3adc547d
// (error without any event), GREEN at head ce4f0c4f.
func TestVerifyPR6177_F10_WorkerCreationRefusal_EmitsConflictEvent(t *testing.T) {
	engine, dataset, runtimeObj, runtimeClass, config, value, recorder :=
		verifyPR6177TransformFixture(t, string(common.ComponentTypeWorker))
	verifyPR6177CheckConflictEvent(t, string(common.ComponentTypeWorker),
		engine.transformWorker(dataset, runtimeObj, runtimeClass, config, value), recorder)
}

// F10 contract, unit layer, client creation path -- the component whose
// creation-rejection spec was still missing when F10 was filed.
// RED on base e0fc4c18 and on previous head 3adc547d, GREEN at head ce4f0c4f.
func TestVerifyPR6177_F10_ClientCreationRefusal_EmitsConflictEvent(t *testing.T) {
	engine, dataset, runtimeObj, runtimeClass, config, value, recorder :=
		verifyPR6177TransformFixture(t, string(common.ComponentTypeClient))
	verifyPR6177CheckConflictEvent(t, string(common.ComponentTypeClient),
		engine.transformClient(dataset, runtimeObj, runtimeClass, config, value), recorder)
}
