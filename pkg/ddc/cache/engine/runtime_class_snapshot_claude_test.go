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

// Reviewer harness (Claude) for PR #6242 — head-only contracts of the snapshot binding.
//
// AC2 (rebind): deleting the snapshot configmap is the documented way to move a CacheRuntime to
// its current CacheRuntimeClass. The next Sync must bind the runtime to the *edited* class and
// apply it, rather than wedging or silently keeping the old values.
//
// AC3 (data operations wait): while a runtime is unbound, the read-only path used by the
// dataset controller must return an error instead of falling back to the live class, so a
// DataLoad retries instead of running commands from a class the components do not run.

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	workloadv1alpha1 "github.com/fluid-cloudnative/advanced-statefulset/api/workload/v1alpha1"
	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
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

var _ = Describe("CacheRuntimeClass snapshot lifecycle (claude harness)", Label("pkg.ddc.cache.engine.runtime_class_snapshot_claude_test.go"), func() {
	const (
		masterSts    = "test-runtime-master"
		workerSts    = "test-runtime-worker"
		snapshotName = "fluid-runtime-class-snapshot-test-runtime"
	)

	var (
		engine       *CacheEngine
		fakeClient   client.Client
		dataset      *datav1alpha1.Dataset
		runtimeObj   *datav1alpha1.CacheRuntime
		runtimeClass *datav1alpha1.CacheRuntimeClass
		ctx          cruntime.ReconcileRequestContext
	)

	resources := func(cpu, memory string) corev1.ResourceRequirements {
		return corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(cpu),
				corev1.ResourceMemory: resource.MustParse(memory),
			},
		}
	}

	BeforeEach(func() {
		dataset = &datav1alpha1.Dataset{
			ObjectMeta: metav1.ObjectMeta{Name: "test-runtime", Namespace: "default", UID: "dataset-uid"},
		}
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
						Containers: []corev1.Container{{Name: "master", Image: "test-master:v1", Resources: resources("1", "4Gi")}},
					}},
				},
				Worker: &datav1alpha1.RuntimeComponentDefinition{
					Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "worker", Image: "test-worker:v1", Resources: resources("1", "4Gi")}},
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
						Containers: []corev1.Container{{Name: container, Image: image, Resources: resources("1", "4Gi")}},
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

	workerResources := func() corev1.ResourceRequirements {
		sts := &workloadv1alpha1.AdvancedStatefulSet{}
		Expect(fakeClient.Get(context.Background(), types.NamespacedName{Name: workerSts, Namespace: "default"}, sts)).To(Succeed())
		return sts.Spec.Template.Spec.Containers[0].Resources
	}

	getSnapshot := func() *corev1.ConfigMap {
		snapshot := &corev1.ConfigMap{}
		Expect(fakeClient.Get(context.Background(), types.NamespacedName{Name: snapshotName, Namespace: "default"}, snapshot)).To(Succeed())
		return snapshot
	}

	It("rebinds to the current class when the snapshot is deleted, and applies it", func() {
		// Settle: bind to the class as declared and sync.
		Expect(engine.Sync(ctx)).To(Succeed())
		Expect(workerResources()).To(Equal(resources("1", "4Gi")))

		// The admin edits the class and deletes the snapshot to rebind the runtime.
		live := &datav1alpha1.CacheRuntimeClass{}
		Expect(fakeClient.Get(context.Background(), types.NamespacedName{Name: "test-class"}, live)).To(Succeed())
		live.Topology.Worker.Template.Spec.Containers[0].Resources = resources("2", "8Gi")
		Expect(fakeClient.Update(context.Background(), live)).To(Succeed())
		Expect(fakeClient.Delete(context.Background(), getSnapshot())).To(Succeed())

		// The next Sync must bind a fresh snapshot of the edited class and converge onto it.
		Expect(engine.Sync(ctx)).To(Succeed())

		bound, err := engine.getBoundRuntimeClass(context.Background(), runtimeObj)
		Expect(err).NotTo(HaveOccurred())
		Expect(bound.Topology.Worker.Template.Spec.Containers[0].Resources).To(Equal(resources("2", "8Gi")),
			"the rebound snapshot must hold the edited class")
		Expect(workerResources()).To(Equal(resources("2", "8Gi")),
			"the rebind must sync the new baseline to the workload")
		Expect(getSnapshot().Annotations).To(HaveKeyWithValue(common.AnnotationCacheRuntimeClassName, "test-class"))
	})

	It("makes data operations wait instead of reading the live class while unbound", func() {
		Expect(engine.Sync(ctx)).To(Succeed())

		// The rebind window: the snapshot is gone but the runtime still exists.
		Expect(fakeClient.Delete(context.Background(), getSnapshot())).To(Succeed())

		_, err := engine.getBoundRuntimeClassReadOnly(context.Background(), runtimeObj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("not bound"), "got %v", err)

		// Rebinding through the runtime controller restores the read-only path.
		Expect(engine.Sync(ctx)).To(Succeed())
		bound, err := engine.getBoundRuntimeClassReadOnly(context.Background(), runtimeObj)
		Expect(err).NotTo(HaveOccurred())
		Expect(bound.Name).To(Equal("test-class"))
	})
})
