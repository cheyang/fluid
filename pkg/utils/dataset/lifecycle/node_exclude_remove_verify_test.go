/*
Copyright 2023 The Fluid Authors.

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

// Verification harness for PR #6249 (fixes #6246), reviewer: Codex (Reviewer B).
// Polarity: BOTH specs are CONTRACT tests - they assert the intended behavior.
//   - On the base branch (ff99142f, without the patch) C1 FAILS, which
//     reproduces the reported bug (P0 premise Confirmed).
//   - On the PR head (145a2c13) both specs PASS, which proves the fix.
// C2 guards against an over-broad skip: a node that does NOT match the
// selector must still get its fluid labels removed.

import (
	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/ddc/base"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	"github.com/fluid-cloudnative/fluid/pkg/utils/kubeclient"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
)

var _ = Describe("PR6249ExcludeSelectorRemoveHarness", Label("verify-pr-6249"), func() {
	var (
		runtimeInfo     base.RuntimeInfoInterface
		origSelector    labels.Selector
		cacheNodeLabels map[string]string
		testScheme      *runtime.Scheme
	)

	BeforeEach(func() {
		testScheme = runtime.NewScheme()
		_ = corev1.AddToScheme(testScheme)
		_ = datav1alpha1.AddToScheme(testScheme)

		var err error
		runtimeInfo, err = base.BuildRuntimeInfo("hbase", "fluid", common.AlluxioRuntime)
		Expect(err).To(BeNil())
		runtimeInfo.SetupWithDataset(&datav1alpha1.Dataset{
			Spec: datav1alpha1.DatasetSpec{PlacementMode: datav1alpha1.ExclusiveMode},
		})

		origSelector = nodeExcludeSelector
		cacheNodeLabels = map[string]string{
			runtimeInfo.GetRuntimeLabelName(): "true",
			"type":                            "virtual-kubelet",
		}
	})

	AfterEach(func() {
		nodeExcludeSelector = origSelector
	})

	// C1 (contract): an excluded node must keep every label when its worker leaves.
	It("C1: excluded node keeps all labels after removeScheduleInfoFromNode", func() {
		nodeExcludeSelector = labels.SelectorFromSet(labels.Set{"type": "virtual-kubelet"})

		c := fake.NewFakeClientWithScheme(testScheme, &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "cache-node", Labels: cacheNodeLabels},
		})

		Expect(removeScheduleInfoFromNode("cache-node", runtimeInfo, c)).To(Succeed())

		gotNode, err := kubeclient.GetNode(c, "cache-node")
		Expect(err).To(BeNil())
		Expect(gotNode.Labels).To(Equal(cacheNodeLabels),
			"excluded node must be left completely untouched")
	})

	// C2 (contract): a node that does NOT match the selector still loses its fluid labels.
	It("C2: non-excluded node still loses the runtime label", func() {
		nodeExcludeSelector = labels.SelectorFromSet(labels.Set{"type": "real-node"})

		c := fake.NewFakeClientWithScheme(testScheme, &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "cache-node", Labels: cacheNodeLabels},
		})

		Expect(removeScheduleInfoFromNode("cache-node", runtimeInfo, c)).To(Succeed())

		gotNode, err := kubeclient.GetNode(c, "cache-node")
		Expect(err).To(BeNil())
		Expect(gotNode.Labels).NotTo(HaveKey(runtimeInfo.GetRuntimeLabelName()),
			"non-excluded node must still get its runtime label removed")
		Expect(gotNode.Labels).To(HaveKeyWithValue("type", "virtual-kubelet"),
			"user-owned labels must survive removal")
	})
})
