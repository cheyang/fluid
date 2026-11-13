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

// Reviewer harness (Claude) for PR #6242 — contract P0/AC1.
//
// A CacheRuntimeClass edit must not reach the workloads of an existing CacheRuntime.
// This file deliberately uses only symbols that exist on the PR base branch as well, so the
// same test can run on the base (where it FAILS — the premise of issue #6213) and on the PR
// head (where it PASSES). It is the premise reproduction and the headline regression test.

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	workloadv1alpha1 "github.com/fluid-cloudnative/advanced-statefulset/api/workload/v1alpha1"
	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	cruntime "github.com/fluid-cloudnative/fluid/pkg/runtime"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("CacheRuntimeClass edit isolation (claude harness)", Label("pkg.ddc.cache.engine.runtime_class_edit_claude_test.go"), func() {
	const (
		masterSts = "test-runtime-master"
		workerSts = "test-runtime-worker"
	)

	var (
		engine       *CacheEngine
		fakeClient   client.Client
		dataset      *datav1alpha1.Dataset
		runtimeObj   *datav1alpha1.CacheRuntime
		runtimeClass *datav1alpha1.CacheRuntimeClass
		ctx          cruntime.ReconcileRequestContext
	)

	classResources := func() corev1.ResourceRequirements {
		return corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("4Gi"),
			},
		}
	}

	BeforeEach(func() {
		dataset = &datav1alpha1.Dataset{
			ObjectMeta: metav1.ObjectMeta{Name: "test-runtime", Namespace: "default", UID: "dataset-uid"},
		}
		// The CacheRuntime declares no resources of its own: every key comes from the class
		// template, which is the shape an edit reaches most easily.
		runtimeObj = &datav1alpha1.CacheRuntime{
			TypeMeta:   metav1.TypeMeta{APIVersion: datav1alpha1.GroupVersion.String(), Kind: datav1alpha1.CacheRuntimeKind},
			ObjectMeta: metav1.ObjectMeta{Name: "test-runtime", Namespace: "default", UID: "runtime-uid"},
			Spec: datav1alpha1.CacheRuntimeSpec{
				RuntimeClassName: "test-class",
				Master:           datav1alpha1.CacheRuntimeMasterSpec{Replicas: 1},
				Worker:           datav1alpha1.CacheRuntimeWorkerSpec{Replicas: 2},
			},
		}
		runtimeClass = &datav1alpha1.CacheRuntimeClass{
			ObjectMeta:     metav1.ObjectMeta{Name: "test-class"},
			FileSystemType: "test-fs",
			Topology: &datav1alpha1.RuntimeTopology{
				Master: &datav1alpha1.RuntimeComponentDefinition{
					Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "master", Image: "test-master:v1", Resources: classResources()}},
					}},
				},
				Worker: &datav1alpha1.RuntimeComponentDefinition{
					Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "worker", Image: "test-worker:v1", Resources: classResources()}},
					}},
				},
				Client: &datav1alpha1.RuntimeComponentDefinition{
					Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "client", Image: "test-client:v1"}},
					}},
				},
			},
		}
		newWorkload := func(name, container, image string, replicas int32) *workloadv1alpha1.AdvancedStatefulSet {
			rep := replicas
			return &workloadv1alpha1.AdvancedStatefulSet{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
				Spec: workloadv1alpha1.AdvancedStatefulSetSpec{
					Replicas: &rep,
					Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: container, Image: image, Resources: classResources()}},
					}},
				},
				Status: workloadv1alpha1.AdvancedStatefulSetStatus{
					ReadyReplicas: replicas, CurrentReplicas: replicas, AvailableReplicas: replicas,
				},
			}
		}

		fakeClient = fake.NewClientBuilder().
			WithScheme(CacheEngineTestScheme).
			WithObjects(dataset, runtimeObj, runtimeClass,
				newWorkload(masterSts, "master", "test-master:v1", 1),
				newWorkload(workerSts, "worker", "test-worker:v1", 2),
				&appsv1.DaemonSet{
					ObjectMeta: metav1.ObjectMeta{Name: "test-runtime-client", Namespace: "default"},
					Spec: appsv1.DaemonSetSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "client", Image: "test-client:v1"}},
					}}},
				}).
			WithStatusSubresource(dataset, runtimeObj).
			Build()

		engine = &CacheEngine{
			name:      "test-runtime",
			namespace: "default",
			Client:    fakeClient,
			Log:       ctrl.Log.WithName("claude-harness"),
		}
		ctx = cruntime.ReconcileRequestContext{
			Context:        context.Background(),
			Client:         fakeClient,
			Log:            ctrl.Log.WithName("claude-harness"),
			NamespacedName: types.NamespacedName{Name: "test-runtime", Namespace: "default"},
		}
	})

	getWorkloadResources := func(name string) corev1.ResourceRequirements {
		sts := &workloadv1alpha1.AdvancedStatefulSet{}
		Expect(fakeClient.Get(context.Background(), types.NamespacedName{Name: name, Namespace: "default"}, sts)).To(Succeed())
		return sts.Spec.Template.Spec.Containers[0].Resources
	}

	getWorkloadImage := func(name string) string {
		sts := &workloadv1alpha1.AdvancedStatefulSet{}
		Expect(fakeClient.Get(context.Background(), types.NamespacedName{Name: name, Namespace: "default"}, sts)).To(Succeed())
		return sts.Spec.Template.Spec.Containers[0].Image
	}

	// A first reconcile settles the runtime. On the PR head this is also the reconcile that
	// binds the runtime to a snapshot of the class as it is now.
	settle := func() {
		Expect(engine.Sync(ctx)).To(Succeed())
	}

	editClass := func() {
		live := &datav1alpha1.CacheRuntimeClass{}
		Expect(fakeClient.Get(context.Background(), types.NamespacedName{Name: "test-class"}, live)).To(Succeed())
		live.Topology.Worker.Template.Spec.Containers[0].Resources = corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("16Gi"),
			},
		}
		live.Topology.Master.Template.Spec.Containers[0].Image = "test-master:v2"
		Expect(fakeClient.Update(context.Background(), live)).To(Succeed())
	}

	It("must not change an existing workload after a CacheRuntimeClass edit", func() {
		settle()
		before := getWorkloadResources(workerSts)
		editClass()

		// A reconcile must still run and succeed after the edit.
		Expect(engine.Sync(ctx)).To(Succeed())

		Expect(getWorkloadResources(workerSts)).To(Equal(before),
			"a CacheRuntimeClass edit reached the existing worker workload")
		Expect(getWorkloadImage(masterSts)).To(Equal("test-master:v1"),
			"a CacheRuntimeClass edit reached the existing master workload")
	})
})
