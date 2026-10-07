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

// Verification harness for PR #6183 (docs: CacheRuntime in-place field updates).
// These specs prove claim P0 from docs/verification/cacheruntime-replicas-inplace-doc:
// the CacheRuntime reconcile path syncs spec.{master,worker}.replicas IN PLACE to the
// AdvancedStatefulSet on every sync, contrary to what the docs claimed before the PR.
// Polarity: contract tests — they PASS as long as the documented behavior exists,
// and FAIL if the sync path stops propagating replicas.

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	workloadv1alpha1 "github.com/fluid-cloudnative/advanced-statefulset/api/workload/v1alpha1"
	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	cruntime "github.com/fluid-cloudnative/fluid/pkg/runtime"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	cclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newVerifyAsts(name, namespace, containerName string, replicas int32) *workloadv1alpha1.AdvancedStatefulSet {
	r := replicas
	return &workloadv1alpha1.AdvancedStatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: workloadv1alpha1.AdvancedStatefulSetSpec{
			Replicas: &r,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: containerName, Image: "test-" + containerName + ":latest"}},
				},
			},
		},
	}
}

func newVerifyRuntimeClass() *datav1alpha1.CacheRuntimeClass {
	return &datav1alpha1.CacheRuntimeClass{
		ObjectMeta:     metav1.ObjectMeta{Name: "test-class"},
		FileSystemType: "test-fs",
		Topology: &datav1alpha1.RuntimeTopology{
			Master: &datav1alpha1.RuntimeComponentDefinition{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "master", Image: "test-master:latest"}},
					},
				},
			},
			Worker: &datav1alpha1.RuntimeComponentDefinition{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "worker", Image: "test-worker:latest"}},
					},
				},
			},
		},
	}
}

func newVerifyRuntime(masterReplicas, workerReplicas int32) *datav1alpha1.CacheRuntime {
	return &datav1alpha1.CacheRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: "test-runtime", Namespace: "default"},
		Spec: datav1alpha1.CacheRuntimeSpec{
			RuntimeClassName: "test-class",
			Master:           datav1alpha1.CacheRuntimeMasterSpec{Replicas: masterReplicas},
			Worker:           datav1alpha1.CacheRuntimeWorkerSpec{Replicas: workerReplicas},
		},
	}
}

func getVerifyAsts(c cclient.Client, name string) *workloadv1alpha1.AdvancedStatefulSet {
	asts := &workloadv1alpha1.AdvancedStatefulSet{}
	ExpectWithOffset(1, c.Get(context.Background(), types.NamespacedName{Name: name, Namespace: "default"}, asts)).To(Succeed())
	return asts
}

var _ = Describe("P0 verification: CacheRuntime replicas are synced in place (PR #6183 premise)", Label("verify-p0-cacheruntime-replicas"), func() {
	var (
		engine     *CacheEngine
		fakeClient cclient.Client
		ctx        cruntime.ReconcileRequestContext
	)

	newEngine := func(objs ...cclient.Object) {
		fakeClient = fake.NewClientBuilder().WithScheme(CacheEngineTestScheme).WithObjects(objs...).Build()
		engine = &CacheEngine{
			name:      "test-runtime",
			namespace: "default",
			Client:    fakeClient,
			Log:       ctrl.Log.WithName("verify-p0"),
		}
		ctx = cruntime.ReconcileRequestContext{
			Client:         fakeClient,
			Context:        context.Background(),
			Log:            ctrl.Log.WithName("verify-p0"),
			RuntimeType:    "cache",
			NamespacedName: types.NamespacedName{Name: "test-runtime", Namespace: "default"},
		}
	}

	It("P0: syncs an increased spec.worker.replicas to the worker AdvancedStatefulSet in place", func() {
		newEngine(newVerifyAsts("test-runtime-master", "default", "master", 1),
			newVerifyAsts("test-runtime-worker", "default", "worker", 2))

		err := engine.syncRuntimeSpec(ctx, newVerifyRuntime(1, 3), newVerifyRuntimeClass())
		Expect(err).NotTo(HaveOccurred())

		worker := getVerifyAsts(fakeClient, "test-runtime-worker")
		Expect(*worker.Spec.Replicas).To(Equal(int32(3)),
			"worker AdvancedStatefulSet spec.replicas must be patched in place to the new spec.worker.replicas")
	})

	It("P0: syncs a decreased spec.master.replicas to the master AdvancedStatefulSet in place", func() {
		newEngine(newVerifyAsts("test-runtime-master", "default", "master", 3),
			newVerifyAsts("test-runtime-worker", "default", "worker", 2))

		err := engine.syncRuntimeSpec(ctx, newVerifyRuntime(1, 2), newVerifyRuntimeClass())
		Expect(err).NotTo(HaveOccurred())

		master := getVerifyAsts(fakeClient, "test-runtime-master")
		Expect(*master.Spec.Replicas).To(Equal(int32(1)),
			"master AdvancedStatefulSet spec.replicas must be patched in place to the new spec.master.replicas")
	})

	It("P0: does not write the AdvancedStatefulSet when replicas already match (value-driven, no periodic overwrite)", func() {
		newEngine(newVerifyAsts("test-runtime-master", "default", "master", 1),
			newVerifyAsts("test-runtime-worker", "default", "worker", 2))

		masterBefore := getVerifyAsts(fakeClient, "test-runtime-master")
		workerBefore := getVerifyAsts(fakeClient, "test-runtime-worker")

		err := engine.syncRuntimeSpec(ctx, newVerifyRuntime(1, 2), newVerifyRuntimeClass())
		Expect(err).NotTo(HaveOccurred())

		masterAfter := getVerifyAsts(fakeClient, "test-runtime-master")
		workerAfter := getVerifyAsts(fakeClient, "test-runtime-worker")
		Expect(masterAfter.ResourceVersion).To(Equal(masterBefore.ResourceVersion),
			"master AdvancedStatefulSet must not be rewritten when nothing changed")
		Expect(workerAfter.ResourceVersion).To(Equal(workerBefore.ResourceVersion),
			"worker AdvancedStatefulSet must not be rewritten when nothing changed")
	})
})
