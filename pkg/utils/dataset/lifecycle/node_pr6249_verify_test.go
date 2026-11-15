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

package lifecycle

// Reviewer verification harness for PR #6249 (branch verify/exclude-remove-skip-claude).
// Additive only: production code is untouched.
//
// Claims are exercised through the public entry point SyncScheduleInfoToCacheNodes
// (the same call every engine's SyncScheduleInfoToCacheNodes makes), not just the
// removeScheduleInfoFromNode helper:
//
//   - V1 (CONTRACT, decides premise P0 and finding F1): a node that matches
//     FLUID_SCHEDULE_INFO_EXCLUDE_NODE_SELECTOR keeps its runtime label when the
//     runtime's worker pod leaves it. Intended behavior -> PASSES on the PR head and
//     FAILS on the merge base (base ff99142 has no exclude check in
//     removeScheduleInfoFromNode); red on base is the reproduction of #6246.
//     The non-excluded node in the same run is the control group: it must still
//     be cleaned up, proving the skip does not swallow ordinary removals.
//
//   - V2 (CONTRACT): a selector configured but NOT matching the node must not block
//     the removal (exercises the Matches==false branch of the new check).

import (
	"context"
	"fmt"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/ddc/base"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	"github.com/fluid-cloudnative/fluid/pkg/utils/kubeclient"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PR6249 verification: exclude selector vs schedule info removal", Label("pr6249-verify"), func() {
	var (
		client      client.Client
		runtimeInfo base.RuntimeInfoInterface
		testScheme  *runtime.Scheme
		resources   []runtime.Object
	)

	BeforeEach(func() {
		testScheme = runtime.NewScheme()
		_ = corev1.AddToScheme(testScheme)
		_ = appsv1.AddToScheme(testScheme)
		_ = datav1alpha1.AddToScheme(testScheme)

		nodeExcludeSelector = nil

		var err error
		runtimeInfo, err = base.BuildRuntimeInfo("hbase", "fluid", common.AlluxioRuntime)
		Expect(err).To(BeNil())
		runtimeInfo.SetupWithDataset(&datav1alpha1.Dataset{
			Spec: datav1alpha1.DatasetSpec{PlacementMode: datav1alpha1.ExclusiveMode},
		})

		resources = []runtime.Object{
			workerStatefulSet("hbase"),
			&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-0"}},
			&corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "vk-node",
					Labels: map[string]string{"type": "virtual-kubelet"},
				},
			},
		}
	})

	JustBeforeEach(func() {
		client = fake.NewFakeClientWithScheme(testScheme, resources...)
	})

	AfterEach(func() {
		nodeExcludeSelector = nil
	})

	It("keeps the runtime label on an excluded node when its worker leaves (PR6249 contract)", func() {
		By("labelling both nodes before the operator excludes the vk node")
		Expect(client.Create(context.TODO(), workerPod("hbase", 0, "node-0"))).To(Succeed())
		Expect(client.Create(context.TODO(), workerPod("hbase", 1, "vk-node"))).To(Succeed())
		Expect(SyncScheduleInfoToCacheNodes(runtimeInfo, client)).To(Succeed())

		for _, nodeName := range []string{"node-0", "vk-node"} {
			gotNode, err := kubeclient.GetNode(client, nodeName)
			Expect(err).To(BeNil())
			Expect(gotNode.Labels).To(HaveKeyWithValue(runtimeInfo.GetRuntimeLabelName(), "true"),
				"both nodes should carry the runtime label before exclusion")
		}

		By("excluding the vk node, then scaling the runtime off both nodes")
		nodeExcludeSelector = labels.SelectorFromSet(labels.Set{"type": "virtual-kubelet"})
		Expect(client.Delete(context.TODO(), workerPod("hbase", 0, "node-0"))).To(Succeed())
		Expect(client.Delete(context.TODO(), workerPod("hbase", 1, "vk-node"))).To(Succeed())

		Expect(SyncScheduleInfoToCacheNodes(runtimeInfo, client)).To(Succeed())

		By("the non-excluded node loses the runtime label (control group)")
		gotNode0, err := kubeclient.GetNode(client, "node-0")
		Expect(err).To(BeNil())
		Expect(gotNode0.Labels).NotTo(HaveKey(runtimeInfo.GetRuntimeLabelName()),
			"the removal path must still clean up non-excluded nodes")

		By("the excluded node keeps the runtime label (the contract PR #6249 introduces)")
		gotVKNode, err := kubeclient.GetNode(client, "vk-node")
		Expect(err).To(BeNil())
		Expect(gotVKNode.Labels).To(HaveKeyWithValue(runtimeInfo.GetRuntimeLabelName(), "true"),
			"an excluded node must not be label-patched by the removal path")
	})

	It("still removes the runtime label when the selector does not match the node (PR6249 contract)", func() {
		Expect(client.Create(context.TODO(), workerPod("hbase", 1, "vk-node"))).To(Succeed())
		Expect(SyncScheduleInfoToCacheNodes(runtimeInfo, client)).To(Succeed())

		nodeExcludeSelector = labels.SelectorFromSet(labels.Set{"type": "something-else"})
		Expect(client.Delete(context.TODO(), workerPod("hbase", 1, "vk-node"))).To(Succeed())

		Expect(SyncScheduleInfoToCacheNodes(runtimeInfo, client)).To(Succeed())

		gotVKNode, err := kubeclient.GetNode(client, "vk-node")
		Expect(err).To(BeNil())
		Expect(gotVKNode.Labels).NotTo(HaveKey(runtimeInfo.GetRuntimeLabelName()),
			"a non-matching selector must leave the removal path active")
	})
})

func workerStatefulSet(runtimeName string) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "StatefulSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "fluid",
			Name:      runtimeName + "-worker",
			UID:       "pr6249-verify-uid",
		},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": runtimeName + "-worker"},
			},
		},
	}
}

func workerPod(runtimeName string, ordinal int, nodeName string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-worker-%d", runtimeName, ordinal),
			Namespace: "fluid",
			Labels: map[string]string{
				"app": runtimeName + "-worker",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "apps/v1",
					Kind:       "StatefulSet",
					Name:       runtimeName + "-worker",
					UID:        "pr6249-verify-uid",
					Controller: ptr.To(true),
				},
			},
		},
		Spec: corev1.PodSpec{
			NodeName: nodeName,
		},
	}
}
