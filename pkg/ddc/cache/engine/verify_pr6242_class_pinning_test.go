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

// Verification harness for PR #6242 (reviewer: codex). Additive only; production code untouched.
//
// TestVerifyPR6242ClassEditDoesNotReachExistingRuntime is the P0 premise check and the fix
// contract in one:
//   - CONTRACT on the PR head: after a CacheRuntimeClass edit, a Sync leaves the existing
//     master/worker AdvancedStatefulSets and the runtime config ConfigMap untouched.
//   - On the BASE (merge parent 956d78a8f, i.e. #6177 without the snapshot), the same test
//     FAILS: the worker ASTS resources and master image follow the live class edit. That red
//     run is the reproduction of the premise "editing a CacheRuntimeClass changes existing
//     CacheRuntimes on their next reconcile".
//
// The file compiles against both base and head: it only uses Sync(ctx) and fixture types that
// exist in both.

import (
	"context"
	"testing"

	workloadv1alpha1 "github.com/fluid-cloudnative/advanced-statefulset/api/workload/v1alpha1"
	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	cruntime "github.com/fluid-cloudnative/fluid/pkg/runtime"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func verifyPR6242Component(name, image string, resources corev1.ResourceRequirements) *datav1alpha1.RuntimeComponentDefinition {
	return &datav1alpha1.RuntimeComponentDefinition{
		Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: name, Image: image, Resources: resources}},
		}},
	}
}

func verifyPR6242Workload(name, container, image string, replicas int32, memory string) *workloadv1alpha1.AdvancedStatefulSet {
	return &workloadv1alpha1.AdvancedStatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: workloadv1alpha1.AdvancedStatefulSetSpec{
			Replicas: ptr.To(replicas),
			Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name:  container,
					Image: image,
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse(memory)},
					},
				}},
			}},
		},
		Status: workloadv1alpha1.AdvancedStatefulSetStatus{
			ReadyReplicas: replicas, CurrentReplicas: replicas, AvailableReplicas: replicas,
		},
	}
}

func TestVerifyPR6242ClassEditDoesNotReachExistingRuntime(t *testing.T) {
	const (
		runtimeName = "test-runtime"
		namespace   = "default"
		masterSts   = runtimeName + "-master"
		workerSts   = runtimeName + "-worker"
	)
	dataset := &datav1alpha1.Dataset{ObjectMeta: metav1.ObjectMeta{Name: runtimeName, Namespace: namespace, UID: "dataset-uid"}}
	runtimeObj := &datav1alpha1.CacheRuntime{
		TypeMeta:   metav1.TypeMeta{APIVersion: datav1alpha1.GroupVersion.String(), Kind: datav1alpha1.CacheRuntimeKind},
		ObjectMeta: metav1.ObjectMeta{Name: runtimeName, Namespace: namespace, UID: "runtime-uid"},
		Spec: datav1alpha1.CacheRuntimeSpec{
			RuntimeClassName: "test-class",
			Master:           datav1alpha1.CacheRuntimeMasterSpec{Replicas: 1},
			Worker:           datav1alpha1.CacheRuntimeWorkerSpec{Replicas: 2},
		},
	}
	runtimeClass := &datav1alpha1.CacheRuntimeClass{
		ObjectMeta:     metav1.ObjectMeta{Name: "test-class"},
		FileSystemType: "test-fs",
		Topology: &datav1alpha1.RuntimeTopology{
			Master: verifyPR6242Component("master", "test-master:v1", corev1.ResourceRequirements{
				Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("2Gi")}}),
			Worker: verifyPR6242Component("worker", "test-worker:v1", corev1.ResourceRequirements{
				Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("4Gi")}}),
			Client: verifyPR6242Component("client", "test-client:v1", corev1.ResourceRequirements{}),
		},
	}
	runtimeClass.Topology.Master.ExecutionEntries = &datav1alpha1.ExecutionEntries{
		ReportSummary: &datav1alpha1.ExecutionCommonEntry{Command: []string{"summary-v1"}, TimeoutSeconds: 10},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(CacheEngineTestScheme).
		WithObjects(
			dataset, runtimeObj, runtimeClass,
			verifyPR6242Workload(masterSts, "master", "test-master:v1", 1, "2Gi"),
			verifyPR6242Workload(workerSts, "worker", "test-worker:v1", 2, "4Gi"),
			&appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{Name: runtimeName + "-client", Namespace: namespace},
				Spec: appsv1.DaemonSetSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "client", Image: "test-client:v1"}},
				}}},
			},
		).
		WithStatusSubresource(dataset, runtimeObj).
		Build()

	engine := &CacheEngine{
		name:      runtimeName,
		namespace: namespace,
		Client:    fakeClient,
		Log:       ctrl.Log.WithName("verify-pr6242"),
		Recorder:  record.NewFakeRecorder(20),
	}
	ctx := cruntime.ReconcileRequestContext{
		Context:        context.Background(),
		Client:         fakeClient,
		Log:            ctrl.Log.WithName("verify-pr6242"),
		NamespacedName: types.NamespacedName{Name: runtimeName, Namespace: namespace},
	}

	getRV := func(name string, obj client.Object) string {
		if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: name, Namespace: namespace}, obj); err != nil {
			t.Fatalf("get %s: %v", name, err)
		}
		return obj.GetResourceVersion()
	}

	// First Sync creates the runtime config ConfigMap (and, on the head, binds the snapshot).
	if err := engine.Sync(ctx); err != nil {
		t.Fatalf("first Sync: %v", err)
	}
	masterRV := getRV(masterSts, &workloadv1alpha1.AdvancedStatefulSet{})
	workerRV := getRV(workerSts, &workloadv1alpha1.AdvancedStatefulSet{})
	configRV := getRV(common.GetCacheRuntimeConfigConfigMapName(runtimeName), &corev1.ConfigMap{})

	// An administrator edits the live class: worker resources, master image and options.
	live := &datav1alpha1.CacheRuntimeClass{}
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: "test-class"}, live); err != nil {
		t.Fatalf("get class: %v", err)
	}
	live.Topology.Worker.Template.Spec.Containers[0].Resources.Limits = corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("16Gi")}
	live.Topology.Master.Template.Spec.Containers[0].Image = "test-master:v2"
	live.Topology.Master.Options = map[string]string{"new": "option"}
	if err := fakeClient.Update(context.Background(), live); err != nil {
		t.Fatalf("edit class: %v", err)
	}

	if err := engine.Sync(ctx); err != nil {
		t.Fatalf("second Sync: %v", err)
	}

	if got := getRV(workerSts, &workloadv1alpha1.AdvancedStatefulSet{}); got != workerRV {
		t.Errorf("worker AdvancedStatefulSet changed after a CacheRuntimeClass edit (rv %s -> %s): the class edit reached an existing runtime", workerRV, got)
	}
	if got := getRV(masterSts, &workloadv1alpha1.AdvancedStatefulSet{}); got != masterRV {
		t.Errorf("master AdvancedStatefulSet changed after a CacheRuntimeClass edit (rv %s -> %s): the class edit reached an existing runtime", masterRV, got)
	}
	if got := getRV(common.GetCacheRuntimeConfigConfigMapName(runtimeName), &corev1.ConfigMap{}); got != configRV {
		t.Errorf("runtime config ConfigMap changed after a CacheRuntimeClass edit (rv %s -> %s)", configRV, got)
	}
}
