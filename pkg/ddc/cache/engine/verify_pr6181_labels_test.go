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

// Verification harness for PR #6181 (reviewer: Claude, round 2026-10-10).
// Layer: L2 integration (fake client). Compiles against both the merge-base
// (f2785f8) and the PR head, so the same file doubles as the P0 premise
// reproduction on base and as the fix check on head.
//
// Claim mapping (see docs/verification/cache-tieredstore-labels-claude/):
//   P0/V1     [contract] emptyDir{quota:1Gi} (default medium) on a CacheRuntime
//             must label its worker node with total=1GiB and disk=1GiB and no
//             memory label. RED on base (total=0B, no disk label) - that red
//             run is the premise reproduction; GREEN on the PR head.
//   V1-mem    [contract] processMemory{quota:4Gi} must label memory=4GiB,
//             total=4GiB, no disk label. RED on base, GREEN on head.
//   V1-hp     [contract] hostPath with two paths 1Gi+3Gi must label disk=4GiB
//             (per-path quotas preserved, not averaged). RED on base, GREEN
//             on head.
//   V1-2lvl   [contract] processMemory + emptyDir across two levels must sum
//             into memory=4GiB and disk=1GiB. RED on base, GREEN on head.
//   V2-stale  [BUG-CANARY] a node that was already labelled by the pre-fix
//             code (total=0B, no m/d labels, runtime label present) keeps its
//             stale labels after SyncScheduleInfoToCacheNodes, because
//             calculateNodeDifferences never revisits already-labelled nodes
//             and addScheduleInfoToNode skips nodes carrying the runtime
//             label. PASSES on base AND head - it documents the upgrade-path
//             limitation the PR author acknowledged. If label healing is
//             implemented later, this canary flips to FAIL and must then be
//             inverted into a contract test.

import (
	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/utils"
	"github.com/fluid-cloudnative/fluid/pkg/utils/dataset/lifecycle"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	"github.com/fluid-cloudnative/fluid/pkg/utils/kubeclient"
	"github.com/go-logr/logr"
	workloadv1alpha1 "github.com/fluid-cloudnative/advanced-statefulset/api/workload/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Verification harness: PR #6181 tiered store -> node capacity labels", Label("pr-6181-verification"), func() {

	const (
		runtimeName = "demo"
		namespace   = "default"
		nodeName    = "node-0"
	)

	// setupCluster builds a fake cluster with one node, an AdvancedStatefulSet
	// worker and one worker pod scheduled on the node, plus a CacheRuntime
	// carrying the given tiered store. When preLabelStale is true, the node
	// starts out already labelled the way the pre-fix code left it.
	setupCluster := func(levels []datav1alpha1.RuntimeTieredStoreLevel, preLabelStale bool) *CacheEngine {
		runtime := &datav1alpha1.CacheRuntime{
			ObjectMeta: metav1.ObjectMeta{Name: runtimeName, Namespace: namespace},
			Spec: datav1alpha1.CacheRuntimeSpec{
				Worker: datav1alpha1.CacheRuntimeWorkerSpec{
					TieredStore: datav1alpha1.RuntimeTieredStore{Levels: levels},
				},
			},
		}

		workerName := common.GetCacheComponentName(runtimeName, common.ComponentTypeWorker)

		nodeLabels := map[string]string{}
		if preLabelStale {
			// labels as the pre-fix controller writes them: runtime label set,
			// total pinned to 0B, no memory/disk labels at all.
			nodeLabels = map[string]string{
				utils.GetRuntimeLabelName(common.CacheRuntime, namespace, runtimeName, ""): "true",
				utils.GetLabelNameForTotal(common.CacheRuntime, namespace, runtimeName, ""): "0B",
			}
		}
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: nodeName, Labels: nodeLabels},
		}

		advancedSts := &workloadv1alpha1.AdvancedStatefulSet{
			TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "StatefulSet"},
			ObjectMeta: metav1.ObjectMeta{
				Name:      workerName,
				Namespace: namespace,
				UID:       "test-worker-uid",
			},
			Spec: workloadv1alpha1.AdvancedStatefulSetSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						common.LabelCacheRuntimeName:          runtimeName,
						common.LabelCacheRuntimeComponentName: workerName,
					},
				},
			},
		}

		workerPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      workerName + "-0",
				Namespace: namespace,
				Labels: map[string]string{
					common.LabelCacheRuntimeName:          runtimeName,
					common.LabelCacheRuntimeComponentName: workerName,
				},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: "apps/v1",
					Kind:       "StatefulSet",
					Name:       workerName,
					UID:        "test-worker-uid",
					Controller: func() *bool { b := true; return &b }(),
				}},
			},
			Spec: corev1.PodSpec{NodeName: nodeName},
		}

		client := fake.NewFakeClientWithScheme(CacheEngineTestScheme, runtime, node, advancedSts, workerPod)

		return &CacheEngine{
			Client:      client,
			Log:         logr.Discard(),
			name:        runtimeName,
			namespace:   namespace,
			runtimeType: common.CacheRuntime,
		}
	}

	// syncAndGetNode drives the exact production path used by sync.go:
	// getRuntimeInfo -> lifecycle.SyncScheduleInfoToCacheNodes, then returns
	// the node after labelling.
	syncAndGetNode := func(engine *CacheEngine) *corev1.Node {
		runtimeInfo, err := engine.getRuntimeInfo()
		Expect(err).NotTo(HaveOccurred())

		err = lifecycle.SyncScheduleInfoToCacheNodes(runtimeInfo, engine.Client)
		Expect(err).NotTo(HaveOccurred())

		gotNode, err := kubeclient.GetNode(engine.Client, nodeName)
		Expect(err).NotTo(HaveOccurred())
		return gotNode
	}

	Context("P0/V1: worker tiered store emptyDir (default medium)", func() {
		It("contract: node gets total=1GiB and disk=1GiB labels, no memory label", func() {
			engine := setupCluster([]datav1alpha1.RuntimeTieredStoreLevel{
				{
					EmptyDir: &datav1alpha1.EmptyDirMediumSource{Quota: resource.MustParse("1Gi")},
					High:     "0.8",
					Low:      "0.5",
				},
			}, false)

			runtimeInfo, err := engine.getRuntimeInfo()
			Expect(err).NotTo(HaveOccurred())

			node := syncAndGetNode(engine)

			Expect(node.Labels).To(HaveKeyWithValue(runtimeInfo.GetRuntimeLabelName(), "true"))
			// the exact reproduction from issue #6174: total was pinned to 0B
			// and the m/d labels were missing entirely.
			Expect(node.Labels).To(HaveKeyWithValue(runtimeInfo.GetLabelNameForTotal(), "1GiB"))
			Expect(node.Labels).To(HaveKeyWithValue(runtimeInfo.GetLabelNameForDisk(), "1GiB"))
			Expect(node.Labels).NotTo(HaveKey(runtimeInfo.GetLabelNameForMemory()))
		})
	})

	Context("V1-mem: processMemory medium", func() {
		It("contract: node gets total=4GiB and memory=4GiB labels, no disk label", func() {
			engine := setupCluster([]datav1alpha1.RuntimeTieredStoreLevel{
				{
					ProcessMemory: &datav1alpha1.ProcessMemoryMediumSource{Quota: resource.MustParse("4Gi")},
				},
			}, false)

			runtimeInfo, err := engine.getRuntimeInfo()
			Expect(err).NotTo(HaveOccurred())

			node := syncAndGetNode(engine)

			Expect(node.Labels).To(HaveKeyWithValue(runtimeInfo.GetLabelNameForTotal(), "4GiB"))
			Expect(node.Labels).To(HaveKeyWithValue(runtimeInfo.GetLabelNameForMemory(), "4GiB"))
			Expect(node.Labels).NotTo(HaveKey(runtimeInfo.GetLabelNameForDisk()))
		})
	})

	Context("V1-hp: hostPath medium with per-path quotas", func() {
		It("contract: node disk label sums per-path quotas (4GiB), not the average (2GiB)", func() {
			engine := setupCluster([]datav1alpha1.RuntimeTieredStoreLevel{
				{
					HostPath: &datav1alpha1.HostPathMediumSource{
						Paths:  []string{"/mnt/cache1", "/mnt/cache2"},
						Quotas: []resource.Quantity{resource.MustParse("1Gi"), resource.MustParse("3Gi")},
					},
				},
			}, false)

			runtimeInfo, err := engine.getRuntimeInfo()
			Expect(err).NotTo(HaveOccurred())

			node := syncAndGetNode(engine)

			Expect(node.Labels).To(HaveKeyWithValue(runtimeInfo.GetLabelNameForTotal(), "4GiB"))
			Expect(node.Labels).To(HaveKeyWithValue(runtimeInfo.GetLabelNameForDisk(), "4GiB"))
			Expect(node.Labels).NotTo(HaveKey(runtimeInfo.GetLabelNameForMemory()))
		})
	})

	Context("V1-2lvl: processMemory and emptyDir across two levels", func() {
		It("contract: memory and disk labels each carry their level's quota", func() {
			engine := setupCluster([]datav1alpha1.RuntimeTieredStoreLevel{
				{
					ProcessMemory: &datav1alpha1.ProcessMemoryMediumSource{Quota: resource.MustParse("4Gi")},
				},
				{
					EmptyDir: &datav1alpha1.EmptyDirMediumSource{Quota: resource.MustParse("1Gi")},
				},
			}, false)

			runtimeInfo, err := engine.getRuntimeInfo()
			Expect(err).NotTo(HaveOccurred())

			node := syncAndGetNode(engine)

			Expect(node.Labels).To(HaveKeyWithValue(runtimeInfo.GetLabelNameForTotal(), "5GiB"))
			Expect(node.Labels).To(HaveKeyWithValue(runtimeInfo.GetLabelNameForMemory(), "4GiB"))
			Expect(node.Labels).To(HaveKeyWithValue(runtimeInfo.GetLabelNameForDisk(), "1GiB"))
		})
	})

	Context("V2-stale: node already labelled by the pre-fix code (BUG-CANARY)", func() {
		It("canary: stale 0B capacity labels are NOT healed by SyncScheduleInfoToCacheNodes", func() {
			engine := setupCluster([]datav1alpha1.RuntimeTieredStoreLevel{
				{
					EmptyDir: &datav1alpha1.EmptyDirMediumSource{Quota: resource.MustParse("1Gi")},
				},
			}, true)

			runtimeInfo, err := engine.getRuntimeInfo()
			Expect(err).NotTo(HaveOccurred())

			node := syncAndGetNode(engine)

			// documents current behavior: capacity labels are written once,
			// when the node first enters the cache node set. If this ever
			// starts to fail, label healing was implemented - invert this
			// canary into a contract test.
			Expect(node.Labels).To(HaveKeyWithValue(runtimeInfo.GetLabelNameForTotal(), "0B"))
			Expect(node.Labels).NotTo(HaveKey(runtimeInfo.GetLabelNameForDisk()))
			Expect(node.Labels).NotTo(HaveKey(runtimeInfo.GetLabelNameForMemory()))
		})
	})
})
