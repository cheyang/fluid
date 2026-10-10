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

// Reviewer verification harness for PR #6200 (branch verify/cache-worker-affinity-single-read-claude).
// Additive only: production code untouched. Reuses the package's existing status_test.go helpers so
// the same file grafts onto the BASE merge-base (d8b37f28) as well as the PR head.
//
// Claims (polarity: contract unless noted):
//   P1 (contract, the PR's core claim): each CheckAndUpdateRuntimeStatus cycle performs exactly ONE
//       Get of the worker AdvancedStatefulSet. On base the same spec FAILS with 2 Gets per cycle —
//       that failure is the reproduction of the P0 premise (issue #5879 duplicate read).
//   P2 (contract, regression guard): status.CacheAffinity content equals
//       MergeNodeSelectorAndNodeAffinity(worker podTemplate nodeSelector, affinity) — parity with
//       the removed ComponentManager.GetNodeAffinity.
//   P3 (contract, regression guard): an out-of-band nodeSelector change on the worker workload is
//       reflected in status.CacheAffinity on the very next cycle (zero staleness, no caching).
//   P4 (contract, regression guard): a missing worker workload surfaces as an error, not as a
//       silently-nil affinity.

import (
	"context"

	workloadv1alpha1 "github.com/fluid-cloudnative/advanced-statefulset/api/workload/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	"github.com/fluid-cloudnative/fluid/pkg/utils/kubeclient"
)

var _ = Describe("Verification: worker affinity single-read (PR #6200)", func() {
	It("P1: performs exactly one worker AdvancedStatefulSet Get per status cycle", func() {
		baseClient := fake.NewFakeClientWithScheme(
			CacheEngineTestScheme,
			newStatusTestRuntime(),
			newAdvancedStatefulSetComponent(testStatusMaster, testStatusNamespace, 1, 1),
			newAdvancedStatefulSetComponent(testStatusWorker, testStatusNamespace, 1, 1),
		)
		counting := &verifyAffinityCountingClient{Client: baseClient}
		engine, _ := newStatusTestEngineWithClient(counting)

		_, err := engine.CheckAndUpdateRuntimeStatus(newStatusTestRuntimeValue(false))
		Expect(err).NotTo(HaveOccurred())
		Expect(counting.workerGets).To(Equal(1), "cycle 1: expected exactly 1 worker Get, got %d", counting.workerGets)

		_, err = engine.CheckAndUpdateRuntimeStatus(newStatusTestRuntimeValue(false))
		Expect(err).NotTo(HaveOccurred())
		Expect(counting.workerGets).To(Equal(2), "cycle 2: expected exactly 1 worker Get per cycle (total 2), got %d", counting.workerGets)
	})

	It("P2: status.CacheAffinity equals MergeNodeSelectorAndNodeAffinity of the worker pod template", func() {
		worker := newAdvancedStatefulSetComponent(testStatusWorker, testStatusNamespace, 1, 1)
		worker.Spec.Template.Spec.NodeSelector = map[string]string{"disktype": "ssd"}
		worker.Spec.Template.Spec.Affinity = &corev1.Affinity{
			NodeAffinity: &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{{
						MatchExpressions: []corev1.NodeSelectorRequirement{{
							Key: "zone", Operator: corev1.NodeSelectorOpIn, Values: []string{"zone-a"},
						}},
					}},
				},
			},
		}

		baseClient := fake.NewFakeClientWithScheme(
			CacheEngineTestScheme,
			newStatusTestRuntime(),
			newAdvancedStatefulSetComponent(testStatusMaster, testStatusNamespace, 1, 1),
			worker,
		)
		engine, client := newStatusTestEngineWithClient(baseClient)

		_, err := engine.CheckAndUpdateRuntimeStatus(newStatusTestRuntimeValue(false))
		Expect(err).NotTo(HaveOccurred())

		expected := kubeclient.MergeNodeSelectorAndNodeAffinity(
			worker.Spec.Template.Spec.NodeSelector, worker.Spec.Template.Spec.Affinity)
		Expect(getUpdatedRuntime(client).Status.CacheAffinity).To(Equal(expected))
	})

	It("P3: out-of-band worker nodeSelector change is reflected on the next cycle (zero staleness)", func() {
		worker := newAdvancedStatefulSetComponent(testStatusWorker, testStatusNamespace, 1, 1)
		worker.Spec.Template.Spec.NodeSelector = map[string]string{"disktype": "ssd"}

		baseClient := fake.NewFakeClientWithScheme(
			CacheEngineTestScheme,
			newStatusTestRuntime(),
			newAdvancedStatefulSetComponent(testStatusMaster, testStatusNamespace, 1, 1),
			worker,
		)
		engine, client := newStatusTestEngineWithClient(baseClient)

		_, err := engine.CheckAndUpdateRuntimeStatus(newStatusTestRuntimeValue(false))
		Expect(err).NotTo(HaveOccurred())

		// Mutate the workload out-of-band (simulating any writer other than the engine).
		fetched := &workloadv1alpha1.AdvancedStatefulSet{}
		Expect(baseClient.Get(context.TODO(), types.NamespacedName{Name: testStatusWorker, Namespace: testStatusNamespace}, fetched)).To(Succeed())
		fetched.Spec.Template.Spec.NodeSelector = map[string]string{"disktype": "nvme"}
		Expect(baseClient.Update(context.TODO(), fetched)).To(Succeed())

		_, err = engine.CheckAndUpdateRuntimeStatus(newStatusTestRuntimeValue(false))
		Expect(err).NotTo(HaveOccurred())

		affinity := getUpdatedRuntime(client).Status.CacheAffinity
		Expect(affinity).NotTo(BeNil())
		terms := affinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
		Expect(terms).To(HaveLen(1))
		Expect(terms[0].MatchExpressions).To(ContainElement(corev1.NodeSelectorRequirement{
			Key: "disktype", Operator: corev1.NodeSelectorOpIn, Values: []string{"nvme"},
		}))
	})

	It("P4: missing worker workload surfaces as an error", func() {
		baseClient := fake.NewFakeClientWithScheme(
			CacheEngineTestScheme,
			newStatusTestRuntime(),
			newAdvancedStatefulSetComponent(testStatusMaster, testStatusNamespace, 1, 1),
			// worker deliberately absent
		)
		engine, _ := newStatusTestEngineWithClient(baseClient)

		ready, err := engine.CheckAndUpdateRuntimeStatus(newStatusTestRuntimeValue(false))
		Expect(err).To(HaveOccurred())
		Expect(ready).To(BeFalse())
	})
})

// verifyAffinityCountingClient counts Get calls targeting the worker AdvancedStatefulSet.
type verifyAffinityCountingClient struct {
	ctrlclient.Client
	workerGets int
}

func (c *verifyAffinityCountingClient) Get(ctx context.Context, key types.NamespacedName, obj ctrlclient.Object, opts ...ctrlclient.GetOption) error {
	if _, ok := obj.(*workloadv1alpha1.AdvancedStatefulSet); ok && key.Name == testStatusWorker {
		c.workerGets++
	}
	return c.Client.Get(ctx, key, obj, opts...)
}

var _ = Describe("Verification: worker affinity helpers sanity (PR #6200)", func() {
	It("newStatusTestRuntimeValue(false) keeps the worker component enabled", func() {
		value := newStatusTestRuntimeValue(false)
		Expect(value.Master.Enabled).To(BeTrue())
		Expect(value.Worker.Enabled).To(BeTrue())
		Expect(value.Client.Enabled).To(BeFalse())
		Expect(value.Worker.Name).To(Equal(testStatusWorker))
		Expect(value.Worker.Namespace).To(Equal(testStatusNamespace))
	})
})
