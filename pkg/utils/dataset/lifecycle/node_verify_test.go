/*
Verification harness for PR #6244 (reviewer: codex). Additive test file only;
touches no production code. It deliberately uses ONLY symbols that exist on both
the base branch (ff99142) and the PR head (e0cba66) so the same file can be
grafted onto either side:

  - SyncScheduleInfoToCacheNodes (public entry the PR extends)

Polarity: every spec here is a CONTRACT test (asserts intended behavior).
  - On base  (ff99142): P0 specs FAIL  -> proves the premise of issue #6204
    (capacity labels written from an outdated tiered store are never fixed).
  - On head  (e0cba66): P0 specs PASS  -> the fix works through the public path.
  - Control specs PASS on both sides (regression guards).
*/

package lifecycle

import (
	"context"
	"fmt"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/ddc/base"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	"github.com/fluid-cloudnative/fluid/pkg/utils/kubeclient"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// verifyFixture holds the objects every spec in this container needs: one worker
// StatefulSet, one worker pod bound to the node under test, and the node itself.
type verifyFixture struct {
	client      client.Client
	runtimeInfo base.RuntimeInfoInterface
	nodeName    string
}

// newVerifyFixture builds a fake client holding a worker sts+pod pair so that
// nodeName is "desired" (carries a worker pod) for a runtime with the given
// tiered store, plus the node itself pre-populated with nodeLabels.
func newVerifyFixture(nodeName string, nodeLabels map[string]string, tieredStore datav1alpha1.TieredStore) *verifyFixture {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = datav1alpha1.AddToScheme(scheme)

	runtimeInfo, err := base.BuildRuntimeInfo("spark", "fluid", "alluxio", base.WithTieredStore(tieredStore))
	Expect(err).To(BeNil())

	sts := &appsv1.StatefulSet{
		TypeMeta:   metav1.TypeMeta{APIVersion: "apps/v1", Kind: "StatefulSet"},
		ObjectMeta: metav1.ObjectMeta{Namespace: "fluid", Name: "spark-worker", UID: "verify-uid"},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "spark-worker"}},
		},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "spark-worker-0",
			Namespace: "fluid",
			Labels:    map[string]string{"app": "spark-worker"},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "apps/v1", Kind: "StatefulSet", Name: "spark-worker",
				UID: "verify-uid", Controller: ptr.To(true),
			}},
		},
		Spec: corev1.PodSpec{NodeName: nodeName},
	}
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName, Labels: nodeLabels}}

	return &verifyFixture{
		client:      fake.NewFakeClientWithScheme(scheme, sts, pod, node),
		runtimeInfo: runtimeInfo,
		nodeName:    nodeName,
	}
}

func memThenDiskTieredStore() datav1alpha1.TieredStore {
	return datav1alpha1.TieredStore{Levels: []datav1alpha1.Level{
		{MediumType: common.Memory, Quota: resource.NewQuantity(1<<30, resource.BinarySI)},
		{MediumType: common.SSD, Quota: resource.NewQuantity(2<<30, resource.BinarySI)},
	}}
}

var _ = Describe("PR6244 verification (codex)", Label("verify-pr-6244"), func() {
	const (
		runtimeLabel = "fluid.io/s-alluxio-fluid-spark"
		commonLabel  = "fluid.io/s-fluid-spark"
		memLabel     = "fluid.io/s-h-alluxio-m-fluid-spark"
		diskLabel    = "fluid.io/s-h-alluxio-d-fluid-spark"
		totalLabel   = "fluid.io/s-h-alluxio-t-fluid-spark"
	)

	// P0a: the issue #6204 symptom - a node labelled before #6181 keeps t=0B and
	// never gains mem/disk labels even though the runtime has a real tiered store.
	It("P0a-stale-capacity-labels-get-refreshed", func() {
		fx := newVerifyFixture("stale-node", map[string]string{
			runtimeLabel: "true",
			commonLabel:  "true",
			totalLabel:   "0B", // what pre-#6181 CacheRuntime wrote
		}, memThenDiskTieredStore())

		Expect(SyncScheduleInfoToCacheNodes(fx.runtimeInfo, fx.client)).To(Succeed())

		node, err := kubeclient.GetNode(fx.client, "stale-node")
		Expect(err).To(BeNil())
		Expect(node.Labels).To(HaveKeyWithValue(memLabel, "1GiB"))
		Expect(node.Labels).To(HaveKeyWithValue(diskLabel, "2GiB"))
		Expect(node.Labels).To(HaveKeyWithValue(totalLabel, "3GiB"))
	})

	// P0b: a label for a tier the runtime no longer has must be removed, and the
	// total recomputed, through the public sync path.
	It("P0b-removed-tier-label-gets-deleted", func() {
		diskOnly := datav1alpha1.TieredStore{Levels: []datav1alpha1.Level{
			{MediumType: common.SSD, Quota: resource.NewQuantity(2<<30, resource.BinarySI)},
		}}
		fx := newVerifyFixture("shrink-node", map[string]string{
			runtimeLabel: "true",
			commonLabel:  "true",
			memLabel:     "1GiB",
			diskLabel:    "2GiB",
			totalLabel:   "3GiB",
		}, diskOnly)

		Expect(SyncScheduleInfoToCacheNodes(fx.runtimeInfo, fx.client)).To(Succeed())

		node, err := kubeclient.GetNode(fx.client, "shrink-node")
		Expect(err).To(BeNil())
		Expect(node.Labels).NotTo(HaveKey(memLabel))
		Expect(node.Labels).To(HaveKeyWithValue(diskLabel, "2GiB"))
		Expect(node.Labels).To(HaveKeyWithValue(totalLabel, "2GiB"))
	})

	// C1 (control, passes on both sides): a node whose labels already match must
	// be left byte-identical - in particular no relabel storm on a steady cluster.
	It("C1-matching-labels-are-not-rewritten", func() {
		fx := newVerifyFixture("steady-node", map[string]string{
			runtimeLabel: "true",
			commonLabel:  "true",
			memLabel:     "1GiB",
			diskLabel:    "2GiB",
			totalLabel:   "3GiB",
			"other":      "keep",
		}, memThenDiskTieredStore())

		before, err := kubeclient.GetNode(fx.client, "steady-node")
		Expect(err).To(BeNil())

		Expect(SyncScheduleInfoToCacheNodes(fx.runtimeInfo, fx.client)).To(Succeed())

		after, err := kubeclient.GetNode(fx.client, "steady-node")
		Expect(err).To(BeNil())
		Expect(after.Labels).To(Equal(before.Labels))
		// the fake client bumps resourceVersion on any write; no bump == no patch
		Expect(after.ResourceVersion).To(Equal(before.ResourceVersion),
			"a steady node must not be re-patched on every reconcile")
	})

	// C2 (control, passes on both sides): capacity labels of a DIFFERENT runtime
	// on the same node are never touched by this runtime's sync, and vice versa a
	// node whose spark labels already match is left alone.
	It("C2-foreign-runtime-labels-are-untouched", func() {
		fx := newVerifyFixture("shared-node", map[string]string{
			runtimeLabel:                         "true",
			commonLabel:                          "true",
			memLabel:                             "1GiB",
			diskLabel:                            "2GiB",
			totalLabel:                           "3GiB",
			"fluid.io/s-h-alluxio-m-fluid-other": "9GiB",
			"fluid.io/s-h-alluxio-t-fluid-other": "9GiB",
		}, memThenDiskTieredStore())

		Expect(SyncScheduleInfoToCacheNodes(fx.runtimeInfo, fx.client)).To(Succeed())

		node, err := kubeclient.GetNode(fx.client, "shared-node")
		Expect(err).To(BeNil())
		Expect(node.Labels).To(HaveKeyWithValue("fluid.io/s-h-alluxio-m-fluid-other", "9GiB"))
		Expect(node.Labels).To(HaveKeyWithValue("fluid.io/s-h-alluxio-t-fluid-other", "9GiB"))
		Expect(node.Labels).To(HaveKeyWithValue(memLabel, "1GiB"))
		Expect(node.Labels).To(HaveKeyWithValue(totalLabel, "3GiB"))
	})

	// C3 (control, passes on both sides): a worker node that never had the
	// runtime label still gets fully labelled by the add path (pre-existing
	// behavior the refresh must not regress).
	It("C3-new-node-still-gets-full-label-set", func() {
		fx := newVerifyFixture("fresh-node", map[string]string{}, memThenDiskTieredStore())

		Expect(SyncScheduleInfoToCacheNodes(fx.runtimeInfo, fx.client)).To(Succeed())

		node, err := kubeclient.GetNode(fx.client, "fresh-node")
		Expect(err).To(BeNil())
		Expect(node.Labels).To(HaveKeyWithValue(runtimeLabel, "true"))
		Expect(node.Labels).To(HaveKeyWithValue(memLabel, "1GiB"))
		Expect(node.Labels).To(HaveKeyWithValue(diskLabel, "2GiB"))
		Expect(node.Labels).To(HaveKeyWithValue(totalLabel, "3GiB"))
	})

	// C4 (control, passes on both sides): a worker that leaves the node set still
	// gets every fluid label removed; the refresh must not resurrect capacity
	// labels on a node whose pod is gone.
	It("C4-removed-node-loses-all-runtime-labels", func() {
		fx := newVerifyFixture("gone-node", map[string]string{
			runtimeLabel: "true",
			commonLabel:  "true",
			memLabel:     "1GiB",
			diskLabel:    "2GiB",
			totalLabel:   "3GiB",
		}, memThenDiskTieredStore())

		// remove the only worker pod -> node leaves the desired set
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "spark-worker-0", Namespace: "fluid"}}
		Expect(fx.client.Delete(context.TODO(), pod)).To(Succeed())

		Expect(SyncScheduleInfoToCacheNodes(fx.runtimeInfo, fx.client)).To(Succeed())

		node, err := kubeclient.GetNode(fx.client, "gone-node")
		Expect(err).To(BeNil())
		for _, k := range []string{runtimeLabel, commonLabel, memLabel, diskLabel, totalLabel} {
			Expect(node.Labels).NotTo(HaveKey(k), fmt.Sprintf("label %s should be gone", k))
		}
	})

	// E1 (contract, consensus finding F1): a node matching
	// FLUID_SCHEDULE_INFO_EXCLUDE_NODE_SELECTOR must be left byte-identical by the
	// capacity refresh - parity with addScheduleInfoToNode (#5285, documented in
	// charts/fluid/fluid/values.yaml, canonical example type=virtual-kubelet).
	// Isolates ONLY the exclusion behavior so the base/head differential is clean:
	//   base ff99142:      PASS (no refresh exists, excluded node trivially untouched)
	//   previous e0cba66:  FAIL (refresh patches the excluded node - the F1 bug)
	//   fixed head 7de8800: PASS (guard mirrors addScheduleInfoToNode)
	It("E1-excluded-node-is-not-refreshed", func() {
		nodeExcludeSelector = labels.SelectorFromSet(labels.Set{"type": "virtual-kubelet"})
		defer func() { nodeExcludeSelector = nil }()

		fx := newVerifyFixture("excluded-node", map[string]string{
			runtimeLabel: "true",
			commonLabel:  "true",
			totalLabel:   "0B", // pre-#6181 stale label
			"type":       "virtual-kubelet",
		}, memThenDiskTieredStore())

		before, err := kubeclient.GetNode(fx.client, "excluded-node")
		Expect(err).To(BeNil())

		Expect(SyncScheduleInfoToCacheNodes(fx.runtimeInfo, fx.client)).To(Succeed())

		excluded, err := kubeclient.GetNode(fx.client, "excluded-node")
		Expect(err).To(BeNil())
		Expect(excluded.Labels).To(Equal(before.Labels))
		Expect(excluded.ResourceVersion).To(Equal(before.ResourceVersion),
			"a node matching nodeExcludeSelector must not be patched by the capacity refresh")
	})

	// E2 (contract): with the exclude selector active, a NON-matching node in the
	// same sync must still be refreshed - guards against an over-broad fix that
	// skips the refresh entirely. Same polarity family as P0a:
	//   base ff99142:      FAIL (no refresh exists at all)
	//   previous e0cba66:  PASS (refresh ignores the selector, sibling is refreshed)
	//   fixed head 7de8800: PASS (guard skips only the excluded node)
	It("E2-exclude-guard-does-not-skip-normal-nodes", func() {
		nodeExcludeSelector = labels.SelectorFromSet(labels.Set{"type": "virtual-kubelet"})
		defer func() { nodeExcludeSelector = nil }()

		fx := newVerifyFixture("excluded-node", map[string]string{
			runtimeLabel: "true",
			commonLabel:  "true",
			totalLabel:   "0B",
			"type":       "virtual-kubelet",
		}, memThenDiskTieredStore())

		// a second worker node, same runtime, NOT matching the exclude selector
		sibling := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "sibling-node", Labels: map[string]string{
			runtimeLabel: "true",
			commonLabel:  "true",
			totalLabel:   "0B",
			"type":       "real-node",
		}}}
		Expect(fx.client.Create(context.TODO(), sibling)).To(Succeed())
		siblingPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "spark-worker-1",
				Namespace: "fluid",
				Labels:    map[string]string{"app": "spark-worker"},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: "apps/v1", Kind: "StatefulSet", Name: "spark-worker",
					UID: "verify-uid", Controller: ptr.To(true),
				}},
			},
			Spec: corev1.PodSpec{NodeName: "sibling-node"},
		}
		Expect(fx.client.Create(context.TODO(), siblingPod)).To(Succeed())

		Expect(SyncScheduleInfoToCacheNodes(fx.runtimeInfo, fx.client)).To(Succeed())

		refreshed, err := kubeclient.GetNode(fx.client, "sibling-node")
		Expect(err).To(BeNil())
		Expect(refreshed.Labels).To(HaveKeyWithValue(memLabel, "1GiB"))
		Expect(refreshed.Labels).To(HaveKeyWithValue(diskLabel, "2GiB"))
		Expect(refreshed.Labels).To(HaveKeyWithValue(totalLabel, "3GiB"))
	})
})
