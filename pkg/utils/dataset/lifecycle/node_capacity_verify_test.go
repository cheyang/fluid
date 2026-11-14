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
Verification harness for the review of PR #6244
(https://github.com/fluid-cloudnative/fluid/pull/6244, head e0cba66e).

Additive only — production code is untouched on this branch.

Claims (test polarity noted per claim):

  P0  (premise, runs against BASE ff99142f as well):
      "an existing CacheRuntime still shows fluid.io/s-h-cache-t-*: 0B with no
      mem/disk labels" because addScheduleInfoToNode skips nodes that already
      have the runtime label (issue #6204). Reproduced by C1/C2 failing on base.

  C1  [contract] SyncScheduleInfoToCacheNodes must refresh outdated capacity
      labels on a node that already has the runtime label.
      FAILS on base (premise), PASSES on head (fix works, wiring correct).

  C2  [contract] SyncScheduleInfoToCacheNodes must remove the memory/disk
      capacity label of a tier the runtime no longer has.
      FAILS on base, PASSES on head.

  C3  [contract] Nodes matching nodeExcludeSelector
      (FLUID_SCHEDULE_INFO_EXCLUDE_NODE_SELECTOR, e.g. type=virtual-kubelet per
      charts/fluid/fluid/values.yaml) must not be touched by schedule-info
      syncing, including the new capacity-label refresh.
      PASSES on base (already-labelled nodes were never touched), FAILS on head
      => finding F1: the new syncCapacityInfoOnNode path bypasses the
      operator's exclude selector.

  C4  [contract] Steady state: a node whose capacity labels already match the
      runtime's tiered store must not be patched (PR claims "just a cached Get
      per node"). PASSES on head.
*/

package lifecycle

import (
	"context"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/ddc/base"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	"github.com/fluid-cloudnative/fluid/pkg/utils/kubeclient"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// patchCountingClient counts Patch calls issued through it, so tests can
// assert "no write happens on a steady cluster".
type patchCountingClient struct {
	client.Client
	patchCalls int
}

func (c *patchCountingClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	c.patchCalls++
	return c.Client.Patch(ctx, obj, patch, opts...)
}

var _ = Describe("Verify PR #6244: capacity label refresh on already-labelled nodes", func() {
	const (
		nodeName    = "cache-node"
		runtimeName = "spark"
		namespace   = "fluid"
	)

	// runtimeInfo with mem 1GiB + ssd 2GiB by default
	newRuntimeInfo := func(levels ...datav1alpha1.Level) base.RuntimeInfoInterface {
		if len(levels) == 0 {
			levels = []datav1alpha1.Level{
				{MediumType: common.Memory, Quota: resource.NewQuantity(1<<30, resource.BinarySI)},
				{MediumType: common.SSD, Quota: resource.NewQuantity(2<<30, resource.BinarySI)},
			}
		}
		info, err := base.BuildRuntimeInfo(runtimeName, namespace, common.AlluxioRuntime, base.WithTieredStore(datav1alpha1.TieredStore{Levels: levels}))
		Expect(err).To(BeNil())
		return info
	}

	// workerPod + statefulset fixture, mirroring Test SyncScheduleInfoToCacheNodes
	newWorkerFixture := func() []runtime.Object {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "spark-worker-0",
				Namespace: namespace,
				Labels:    map[string]string{"app": "spark-worker"},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: "apps/v1",
					Kind:       "StatefulSet",
					Name:       "spark-worker",
					UID:        "verify-uid",
					Controller: ptr.To(true),
				}},
			},
			Spec: corev1.PodSpec{NodeName: nodeName},
		}
		sts := &appsv1.StatefulSet{
			TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "StatefulSet"},
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      "spark-worker",
				UID:       "verify-uid",
			},
			Spec: appsv1.StatefulSetSpec{
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "spark-worker"}},
			},
		}
		return []runtime.Object{pod, sts}
	}

	newNode := func(labels map[string]string) *corev1.Node {
		return &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName, Labels: labels}}
	}

	getNodeLabels := func(cli client.Client) map[string]string {
		node, err := kubeclient.GetNode(cli, nodeName)
		Expect(err).To(BeNil())
		return node.Labels
	}

	newScheme := func() *runtime.Scheme {
		scheme := runtime.NewScheme()
		_ = corev1.AddToScheme(scheme)
		_ = appsv1.AddToScheme(scheme)
		_ = datav1alpha1.AddToScheme(scheme)
		return scheme
	}

	Describe("C1/P0: entry point refreshes outdated capacity labels on an already-labelled node", func() {
		It("should refresh outdated capacity labels on a node that already has the runtime label", func() {
			runtimeInfo := newRuntimeInfo()
			scheme := newScheme()

			// node labelled before #6181 from an empty tiered store:
			// runtime label + total 0B, no mem/disk labels
			stale := newNode(map[string]string{
				runtimeInfo.GetRuntimeLabelName():  "true",
				runtimeInfo.GetLabelNameForTotal(): "0B",
				"unrelated":                        "keep",
			})
			cli := fake.NewFakeClientWithScheme(scheme, append(newWorkerFixture(), stale)...)

			Expect(SyncScheduleInfoToCacheNodes(runtimeInfo, cli)).To(Succeed())

			labels := getNodeLabels(cli)
			Expect(labels).To(HaveKeyWithValue(runtimeInfo.GetLabelNameForMemory(), "1GiB"))
			Expect(labels).To(HaveKeyWithValue(runtimeInfo.GetLabelNameForDisk(), "2GiB"))
			Expect(labels).To(HaveKeyWithValue(runtimeInfo.GetLabelNameForTotal(), "3GiB"))
			Expect(labels).To(HaveKeyWithValue(runtimeInfo.GetRuntimeLabelName(), "true"))
			Expect(labels).To(HaveKeyWithValue("unrelated", "keep"))
		})
	})

	Describe("C2: entry point drops the capacity label of a removed tier", func() {
		It("should remove the capacity label of a tier the runtime no longer has", func() {
			// runtime now has only the SSD tier
			runtimeInfo := newRuntimeInfo(datav1alpha1.Level{
				MediumType: common.SSD, Quota: resource.NewQuantity(2<<30, resource.BinarySI),
			})
			scheme := newScheme()

			// node still carries the memory label from when the runtime had a MEM tier
			stale := newNode(map[string]string{
				runtimeInfo.GetRuntimeLabelName():   "true",
				runtimeInfo.GetLabelNameForMemory(): "1GiB",
				runtimeInfo.GetLabelNameForDisk():   "2GiB",
				runtimeInfo.GetLabelNameForTotal():  "3GiB",
			})
			cli := fake.NewFakeClientWithScheme(scheme, append(newWorkerFixture(), stale)...)

			Expect(SyncScheduleInfoToCacheNodes(runtimeInfo, cli)).To(Succeed())

			labels := getNodeLabels(cli)
			Expect(labels).NotTo(HaveKey(runtimeInfo.GetLabelNameForMemory()))
			Expect(labels).To(HaveKeyWithValue(runtimeInfo.GetLabelNameForDisk(), "2GiB"))
			Expect(labels).To(HaveKeyWithValue(runtimeInfo.GetLabelNameForTotal(), "2GiB"))
		})
	})

	Describe("C3: entry point must honor nodeExcludeSelector", func() {
		It("should leave nodes matching nodeExcludeSelector untouched", func() {
			runtimeInfo := newRuntimeInfo()
			scheme := newScheme()

			// node matches the exclude selector (e.g. a virtual-kubelet node,
			// see charts/fluid/fluid/values.yaml) and already carries the
			// runtime label with stale capacity labels
			excluded := newNode(map[string]string{
				"type":                             "virtual-kubelet",
				runtimeInfo.GetRuntimeLabelName():  "true",
				runtimeInfo.GetLabelNameForTotal(): "0B",
			})
			cli := fake.NewFakeClientWithScheme(scheme, append(newWorkerFixture(), excluded)...)

			selector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
				MatchLabels: map[string]string{"type": "virtual-kubelet"},
			})
			Expect(err).To(BeNil())
			orig := nodeExcludeSelector
			nodeExcludeSelector = selector
			DeferCleanup(func() { nodeExcludeSelector = orig })

			Expect(SyncScheduleInfoToCacheNodes(runtimeInfo, cli)).To(Succeed())

			// exclusion contract: fluid does not write schedule info (incl.
			// capacity labels) to nodes matching the selector
			labels := getNodeLabels(cli)
			Expect(labels).To(HaveKeyWithValue(runtimeInfo.GetLabelNameForTotal(), "0B"))
			Expect(labels).NotTo(HaveKey(runtimeInfo.GetLabelNameForMemory()))
			Expect(labels).NotTo(HaveKey(runtimeInfo.GetLabelNameForDisk()))
		})
	})

	Describe("C4: steady state must not write", func() {
		It("should not patch a node whose capacity labels already match", func() {
			runtimeInfo := newRuntimeInfo()
			scheme := newScheme()

			// node labels exactly what labelNodeWithCapacityInfo computes today
			fresh := newNode(map[string]string{
				runtimeInfo.GetRuntimeLabelName():   "true",
				runtimeInfo.GetLabelNameForMemory(): "1GiB",
				runtimeInfo.GetLabelNameForDisk():   "2GiB",
				runtimeInfo.GetLabelNameForTotal():  "3GiB",
			})
			counting := &patchCountingClient{Client: fake.NewFakeClientWithScheme(scheme, append(newWorkerFixture(), fresh)...)}

			Expect(SyncScheduleInfoToCacheNodes(runtimeInfo, counting)).To(Succeed())

			Expect(counting.patchCalls).To(Equal(0), "steady-state reconcile should not patch the node (PR claims 'just a cached Get per node')")
			labels := getNodeLabels(counting)
			Expect(labels).To(HaveKeyWithValue(runtimeInfo.GetLabelNameForTotal(), "3GiB"))
		})
	})

	// guard against the counting wrapper itself being broken (harness-bites
	// for C4): the same wrapper must observe the patch issued by a refresh.
	Describe("C4b: harness-bites check for the counting client", func() {
		It("should observe a patch when labels are outdated", func() {
			runtimeInfo := newRuntimeInfo()
			scheme := newScheme()

			stale := newNode(map[string]string{
				runtimeInfo.GetRuntimeLabelName():  "true",
				runtimeInfo.GetLabelNameForTotal(): "0B",
			})
			counting := &patchCountingClient{Client: fake.NewFakeClientWithScheme(scheme, append(newWorkerFixture(), stale)...)}

			Expect(SyncScheduleInfoToCacheNodes(runtimeInfo, counting)).To(Succeed())
			Expect(counting.patchCalls).To(BeNumerically(">=", 1), "counting client must see the refresh patch, else C4 proves nothing")
			Expect(getNodeLabels(counting)).To(HaveKeyWithValue(runtimeInfo.GetLabelNameForTotal(), "3GiB"))
		})
	})
})
