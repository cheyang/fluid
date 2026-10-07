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

// Verification harness for PR #6181 (issue #6174), reviewer harness only.
// Additive: it touches no production code.
//
// Claims under test:
//
//	V1 [contract] getRuntimeInfo must feed spec.worker.tieredStore into RuntimeInfo.
//	    On the PR head this passes; grafted onto the merge-base it fails with an
//	    empty storage map, which is the reproduction of the issue's premise (P0).
//	V2 [contract, integration] The full label chain CacheRuntime spec ->
//	    getRuntimeInfo -> lifecycle.SyncScheduleInfoToCacheNodes -> node labels
//	    must write the disk capacity label and a correct total.
//	    On the merge-base this fails with total=0B and no disk label, the exact
//	    symptom reported in issue #6174.
//	V3 [contract, integration] A processMemory level must surface as the memory
//	    capacity label, not the disk one.
//	V4 [bug-canary] Documents the limitation the PR discloses: a node that already
//	    carries the runtime label is skipped by addScheduleInfoToNode, so a stale
//	    total=0B label from before the fix is NOT healed on upgrade. Passes on both
//	    base and PR head; flips to red if label healing is ever implemented (then
//	    invert it into a contract test).
package engine

import (
	workloadv1alpha1 "github.com/fluid-cloudnative/advanced-statefulset/api/workload/v1alpha1"
	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/utils"
	"github.com/fluid-cloudnative/fluid/pkg/utils/dataset/lifecycle"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	"github.com/fluid-cloudnative/fluid/pkg/utils/kubeclient"
	"github.com/fluid-cloudnative/fluid/pkg/utils/tieredstore"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("PR6181 verify: RuntimeInfo built from worker tiered store", Label("verify-pr6181"), func() {
	const (
		runtimeName = "mooncake-demo"
		namespace   = "default"
		nodeName    = "worker-node"
	)

	// newVerifyEngine returns a CacheEngine backed by a fake client that holds the
	// given CacheRuntime, mirroring how Build() wires the reconciler's engine.
	newVerifyEngine := func(objs ...runtime.Object) (*CacheEngine, client.Client) {
		fakeClient := fake.NewFakeClientWithScheme(CacheEngineTestScheme, objs...)
		return &CacheEngine{
			Client:      fakeClient,
			Log:         fake.NullLogger(),
			name:        runtimeName,
			namespace:   namespace,
			runtimeType: common.CacheRuntime,
		}, fakeClient
	}

	cacheRuntimeWithTieredStore := func(levels []datav1alpha1.RuntimeTieredStoreLevel) *datav1alpha1.CacheRuntime {
		return &datav1alpha1.CacheRuntime{
			ObjectMeta: metav1.ObjectMeta{Name: runtimeName, Namespace: namespace},
			Spec: datav1alpha1.CacheRuntimeSpec{
				Worker: datav1alpha1.CacheRuntimeWorkerSpec{
					Replicas: 1,
					TieredStore: datav1alpha1.RuntimeTieredStore{
						Levels: levels,
					},
				},
			},
		}
	}

	// workerPodOnNode creates the AdvancedStatefulSet + one scheduled worker pod +
	// the node itself, so that getDesiredNodesWithScheduleInfo resolves nodeName.
	workerPodOnNode := func() []runtime.Object {
		workerName := common.GetCacheComponentName(runtimeName, common.ComponentTypeWorker)
		selectorLabels := map[string]string{
			common.LabelCacheRuntimeName:          runtimeName,
			common.LabelCacheRuntimeComponentName: workerName,
		}
		trueVal := true
		sts := &workloadv1alpha1.AdvancedStatefulSet{
			TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "StatefulSet"},
			ObjectMeta: metav1.ObjectMeta{
				Name:      workerName,
				Namespace: namespace,
				UID:       "verify-worker-uid",
			},
			Spec: workloadv1alpha1.AdvancedStatefulSetSpec{
				Selector: &metav1.LabelSelector{MatchLabels: selectorLabels},
			},
		}
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      workerName + "-0",
				Namespace: namespace,
				Labels:    selectorLabels,
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         "apps/v1",
					Kind:               "StatefulSet",
					Name:               workerName,
					UID:                "verify-worker-uid",
					Controller:         &trueVal,
					BlockOwnerDeletion: &trueVal,
				}},
			},
			Spec: corev1.PodSpec{
				NodeName:   nodeName,
				Containers: []corev1.Container{{Name: "worker", Image: "img"}},
			},
		}
		return []runtime.Object{sts, pod}
	}

	diskLabel := func() string { return "fluid.io/s-h-cache-d-" + namespace + "-" + runtimeName }
	memLabel := func() string { return "fluid.io/s-h-cache-m-" + namespace + "-" + runtimeName }
	totalLabel := func() string { return "fluid.io/s-h-cache-t-" + namespace + "-" + runtimeName }
	runtimeLabel := func() string { return "fluid.io/s-cache-" + namespace + "-" + runtimeName }

	Context("V1 [contract] getRuntimeInfo carries the worker tiered store", func() {
		It("exposes the emptyDir quota through GetLevelStorageMap", func() {
			engine, _ := newVerifyEngine(cacheRuntimeWithTieredStore([]datav1alpha1.RuntimeTieredStoreLevel{
				{
					EmptyDir: &datav1alpha1.EmptyDirMediumSource{Quota: resource.MustParse("1Gi")},
					High:     "0.8",
					Low:      "0.5",
				},
			}))

			info, err := engine.getRuntimeInfo()
			Expect(err).NotTo(HaveOccurred())

			storage := tieredstore.GetLevelStorageMap(info)
			// On the merge-base this map is empty: WithTieredStore was called with
			// a zero TieredStore. That empty map is the premise of issue #6174.
			Expect(storage).To(HaveKey(common.DiskCacheStore),
				"RuntimeInfo must expose the worker tiered store (empty on base = premise of #6174)")
			Expect(storage[common.DiskCacheStore].String()).To(Equal("1Gi"))
			Expect(storage).NotTo(HaveKey(common.MemoryCacheStore))
		})
	})

	Context("V2 [contract] node capacity labels reflect the worker tiered store", func() {
		It("writes the disk and total capacity labels onto the worker's node", func() {
			objs := append(workerPodOnNode(),
				cacheRuntimeWithTieredStore([]datav1alpha1.RuntimeTieredStoreLevel{
					{EmptyDir: &datav1alpha1.EmptyDirMediumSource{Quota: resource.MustParse("1Gi")}},
				}),
				&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}},
			)
			engine, fakeClient := newVerifyEngine(objs...)

			info, err := engine.getRuntimeInfo()
			Expect(err).NotTo(HaveOccurred())
			Expect(lifecycle.SyncScheduleInfoToCacheNodes(info, fakeClient)).To(Succeed())

			node, err := kubeclient.GetNode(fakeClient, nodeName)
			Expect(err).NotTo(HaveOccurred())
			// On the merge-base the total label is written as 0B and the disk label
			// is absent: the exact symptom shown in issue #6174.
			Expect(node.Labels).To(HaveKeyWithValue(diskLabel(), "1GiB"))
			Expect(node.Labels).To(HaveKeyWithValue(totalLabel(), "1GiB"))
			Expect(node.Labels).NotTo(HaveKey(memLabel()))
			Expect(node.Labels).To(HaveKeyWithValue(runtimeLabel(), "true"))
		})
	})

	Context("V3 [contract] process memory levels surface as the memory capacity label", func() {
		It("writes the memory and total capacity labels onto the worker's node", func() {
			objs := append(workerPodOnNode(),
				cacheRuntimeWithTieredStore([]datav1alpha1.RuntimeTieredStoreLevel{
					{ProcessMemory: &datav1alpha1.ProcessMemoryMediumSource{Quota: resource.MustParse("2Gi")}},
				}),
				&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}},
			)
			engine, fakeClient := newVerifyEngine(objs...)

			info, err := engine.getRuntimeInfo()
			Expect(err).NotTo(HaveOccurred())
			Expect(lifecycle.SyncScheduleInfoToCacheNodes(info, fakeClient)).To(Succeed())

			node, err := kubeclient.GetNode(fakeClient, nodeName)
			Expect(err).NotTo(HaveOccurred())
			Expect(node.Labels).To(HaveKeyWithValue(memLabel(), "2GiB"))
			Expect(node.Labels).To(HaveKeyWithValue(totalLabel(), "2GiB"))
			Expect(node.Labels).NotTo(HaveKey(diskLabel()))
		})
	})

	Context("V4 [bug-canary] stale 0B labels on pre-existing nodes are not healed", func() {
		It("keeps the stale total label because the node already carries the runtime label", func() {
			staleNode := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
				Name: nodeName,
				Labels: map[string]string{
					runtimeLabel(): "true",
					totalLabel():   "0B",
				},
			}}
			objs := append(workerPodOnNode(),
				cacheRuntimeWithTieredStore([]datav1alpha1.RuntimeTieredStoreLevel{
					{EmptyDir: &datav1alpha1.EmptyDirMediumSource{Quota: resource.MustParse("1Gi")}},
				}),
				staleNode,
			)
			engine, fakeClient := newVerifyEngine(objs...)

			info, err := engine.getRuntimeInfo()
			Expect(err).NotTo(HaveOccurred())
			Expect(lifecycle.SyncScheduleInfoToCacheNodes(info, fakeClient)).To(Succeed())

			node, err := kubeclient.GetNode(fakeClient, nodeName)
			Expect(err).NotTo(HaveOccurred())
			// Canary: asserts the limitation the PR body discloses ("capacity labels
			// are written once"). If a follow-up starts healing labels on upgrade,
			// this flips to red and must be inverted into a contract test.
			Expect(node.Labels).To(HaveKeyWithValue(totalLabel(), "0B"),
				"pre-existing nodes keep stale labels until the worker is recreated")
			Expect(node.Labels).NotTo(HaveKey(diskLabel()))
			Expect(utils.ContainsAll(node.Labels, []string{totalLabel()})).To(BeTrue())
		})
	})
})
