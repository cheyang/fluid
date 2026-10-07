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

/*
Verification harness for PR #6183 (docs: replicas can be updated in place).

Claims under test (polarity: contract):
  - P0/F1: changing spec.worker.replicas / spec.master.replicas on a CacheRuntime
    is propagated by syncRuntimeSpec to the AdvancedStatefulSet's spec.replicas
    in place, without touching the pod template (no redeploy).
  - P0 (value-driven): an unchanged replicas value produces no patch at all.
  - boundary: a disabled component is not synced, even when replicas differ.
*/

package engine

import (
	"context"

	workloadv1alpha1 "github.com/fluid-cloudnative/advanced-statefulset/api/workload/v1alpha1"
	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	cruntime "github.com/fluid-cloudnative/fluid/pkg/runtime"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// corev1PodTemplate builds a single-container pod template for the fixtures.
func corev1PodTemplate(containerName, image string) corev1.PodTemplateSpec {
	return corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: containerName, Image: image}},
		},
	}
}

// newTestClientDaemonSet builds the client DaemonSet fixture.
func newTestClientDaemonSet(name string) *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: appsv1.DaemonSetSpec{
			Template: corev1PodTemplate("client", "test-client:latest"),
		},
	}
}

// patchCountingClient counts Patch calls so the harness can assert that a sync
// with no spec change issues no patch at all (value-driven sync).
type patchCountingClient struct {
	client.Client
	patchCount int
}

func (c *patchCountingClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	c.patchCount++
	return c.Client.Patch(ctx, obj, patch, opts...)
}

var _ = Describe("CacheEngine replicas in-place sync (verification harness for PR #6183)", Label("pkg.ddc.cache.engine.sync_replicas_verify_test.go"), func() {
	var (
		engine       *CacheEngine
		runtimeObj   *datav1alpha1.CacheRuntime
		runtimeClass *datav1alpha1.CacheRuntimeClass
		ctx          cruntime.ReconcileRequestContext
		counting     *patchCountingClient
	)

	BeforeEach(func() {
		runtimeObj = &datav1alpha1.CacheRuntime{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "data.fluid.io/v1alpha1",
				Kind:       "CacheRuntime",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-runtime",
				Namespace: "default",
				UID:       "test-runtime-uid",
			},
			Spec: datav1alpha1.CacheRuntimeSpec{
				RuntimeClassName: "test-class",
				Master:           datav1alpha1.CacheRuntimeMasterSpec{Replicas: 1},
				Worker:           datav1alpha1.CacheRuntimeWorkerSpec{Replicas: 2},
				Client:           datav1alpha1.CacheRuntimeClientSpec{},
			},
		}

		runtimeClass = &datav1alpha1.CacheRuntimeClass{
			ObjectMeta:     metav1.ObjectMeta{Name: "test-class"},
			FileSystemType: "test-fs",
			Topology: &datav1alpha1.RuntimeTopology{
				Master: &datav1alpha1.RuntimeComponentDefinition{
					Template: corev1PodTemplate("master", "test-master:latest"),
				},
				Worker: &datav1alpha1.RuntimeComponentDefinition{
					Template: corev1PodTemplate("worker", "test-worker:latest"),
				},
				Client: &datav1alpha1.RuntimeComponentDefinition{
					Template: corev1PodTemplate("client", "test-client:latest"),
				},
			},
		}

		masterReplicas := int32(1)
		masterSts := &workloadv1alpha1.AdvancedStatefulSet{
			ObjectMeta: metav1.ObjectMeta{Name: "test-runtime-master", Namespace: "default"},
			Spec: workloadv1alpha1.AdvancedStatefulSetSpec{
				Replicas: &masterReplicas,
				Template: corev1PodTemplate("master", "test-master:latest"),
			},
		}

		workerReplicas := int32(2)
		workerSts := &workloadv1alpha1.AdvancedStatefulSet{
			ObjectMeta: metav1.ObjectMeta{Name: "test-runtime-worker", Namespace: "default"},
			Spec: workloadv1alpha1.AdvancedStatefulSetSpec{
				Replicas: &workerReplicas,
				Template: corev1PodTemplate("worker", "test-worker:latest"),
			},
		}

		clientDs := newTestClientDaemonSet("test-runtime-client")

		counting = &patchCountingClient{
			Client: fake.NewClientBuilder().
				WithScheme(CacheEngineTestScheme).
				WithObjects(runtimeObj, runtimeClass, masterSts, workerSts, clientDs).
				WithStatusSubresource(runtimeObj).
				Build(),
		}

		engine = &CacheEngine{
			name:      "test-runtime",
			namespace: "default",
			Client:    counting,
			Log:       ctrl.Log.WithName("verify-test"),
		}

		ctx = cruntime.ReconcileRequestContext{
			Client:         counting,
			Context:        context.Background(),
			Log:            ctrl.Log.WithName("verify-test"),
			RuntimeType:    "cache",
			NamespacedName: types.NamespacedName{Name: "test-runtime", Namespace: "default"},
		}
	})

	fetchSts := func(name string) *workloadv1alpha1.AdvancedStatefulSet {
		sts := &workloadv1alpha1.AdvancedStatefulSet{}
		key := types.NamespacedName{Name: name, Namespace: "default"}
		ExpectWithOffset(1, counting.Get(ctx.Context, key, sts)).To(Succeed())
		return sts
	}

	// P0/F1: worker replicas 2 -> 3 reaches the ASTS spec.replicas, pod template untouched.
	It("should propagate spec.worker.replicas changes to the worker AdvancedStatefulSet in place", func() {
		runtimeObj.Spec.Worker.Replicas = 3

		Expect(engine.syncRuntimeSpec(ctx, runtimeObj, runtimeClass)).To(Succeed())

		worker := fetchSts("test-runtime-worker")
		Expect(*worker.Spec.Replicas).To(Equal(int32(3)))
		// in place: the pod template is not rewritten (image/containers unchanged)
		Expect(equality.Semantic.DeepEqual(worker.Spec.Template, corev1PodTemplate("worker", "test-worker:latest"))).To(BeTrue())
		// one patch for the worker ASTS; the master ASTS is untouched
		Expect(counting.patchCount).To(Equal(1))
	})

	// P0/F1: master replicas 1 -> 2 reaches the master ASTS.
	It("should propagate spec.master.replicas changes to the master AdvancedStatefulSet in place", func() {
		runtimeObj.Spec.Master.Replicas = 2

		Expect(engine.syncRuntimeSpec(ctx, runtimeObj, runtimeClass)).To(Succeed())

		master := fetchSts("test-runtime-master")
		Expect(*master.Spec.Replicas).To(Equal(int32(2)))
		Expect(counting.patchCount).To(Equal(1))
	})

	// P0 (value-driven): unchanged replicas must issue no patch.
	It("should not patch anything when replicas are unchanged", func() {
		Expect(engine.syncRuntimeSpec(ctx, runtimeObj, runtimeClass)).To(Succeed())
		Expect(counting.patchCount).To(Equal(0))
	})

	// Boundary: a disabled component is skipped entirely.
	It("should not sync replicas for a disabled worker component", func() {
		runtimeObj.Spec.Worker.Replicas = 5
		runtimeObj.Spec.Worker.Disabled = true

		Expect(engine.syncRuntimeSpec(ctx, runtimeObj, runtimeClass)).To(Succeed())

		worker := fetchSts("test-runtime-worker")
		Expect(*worker.Spec.Replicas).To(Equal(int32(2)))
	})
})
