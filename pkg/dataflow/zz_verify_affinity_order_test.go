/*
Verification harness for fluid-cloudnative/fluid PR #6258 (reviewer: codex).
Additive test file only — production code is NOT modified by this harness.

Polarity: CONTRACT — asserts the intended correct behavior (deterministic,
sorted MatchExpressions). On the pre-fix (base) code this test is expected to
FAIL under repetition because Go map iteration order is randomized; on the
patched code it must PASS on every iteration.
*/
package dataflow

import (
	"reflect"
	"testing"

	"github.com/fluid-cloudnative/fluid/pkg/common"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestVerifyGenerateNodeAffinityDeterministicSortedOrder drives
// GenerateNodeAffinity with several customized affinity annotations plus
// look-alike non-matching annotations, and asserts that the emitted
// MatchExpressions are (a) exactly the prefixed annotations, (b) in sorted
// key order, on every single call.
func TestVerifyGenerateNodeAffinityDeterministicSortedOrder(t *testing.T) {
	newJob := func() *batchv1.Job {
		return &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "verify-job",
				Namespace: "default",
				Annotations: map[string]string{
					// non-matching look-alikes: slash scope form must NOT be
					// treated as a customized affinity label (dot prefix).
					common.AnnotationDataFlowAffinityInject:     "true",
					common.AnnotationDataFlowAffinityLabelsName: "kubernetes.io/hostname",
					// customized affinity labels, adversarial (reverse-sorted) insertion order
					common.AnnotationDataFlowCustomizedAffinityPrefix + "topology.kubernetes.io/zone":   "zone-1",
					common.AnnotationDataFlowCustomizedAffinityPrefix + "topology.kubernetes.io/region": "region-1",
					common.AnnotationDataFlowCustomizedAffinityPrefix + "kubernetes.io/hostname":        "node-1",
					common.AnnotationDataFlowCustomizedAffinityPrefix + "k8s.rack":                      "rack-1",
					common.AnnotationDataFlowCustomizedAffinityPrefix + "a-first":                       "a-1",
					// unrelated annotation that must be ignored
					"kubectl.kubernetes.io/last-applied-configuration": "{}",
				},
			},
		}
	}

	wantExprs := []corev1.NodeSelectorRequirement{
		{Key: "a-first", Operator: corev1.NodeSelectorOpIn, Values: []string{"a-1"}},
		{Key: "k8s.rack", Operator: corev1.NodeSelectorOpIn, Values: []string{"rack-1"}},
		{Key: "kubernetes.io/hostname", Operator: corev1.NodeSelectorOpIn, Values: []string{"node-1"}},
		{Key: "topology.kubernetes.io/region", Operator: corev1.NodeSelectorOpIn, Values: []string{"region-1"}},
		{Key: "topology.kubernetes.io/zone", Operator: corev1.NodeSelectorOpIn, Values: []string{"zone-1"}},
	}

	const iterations = 40
	job := newJob()
	for i := 0; i < iterations; i++ {
		got, err := GenerateNodeAffinity(job)
		if err != nil {
			t.Fatalf("iteration %d: GenerateNodeAffinity() unexpected error: %v", i, err)
		}
		if got == nil || got.RequiredDuringSchedulingIgnoredDuringExecution == nil ||
			len(got.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms) != 1 {
			t.Fatalf("iteration %d: unexpected affinity shape: %+v", i, got)
		}
		exprs := got.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions
		if !reflect.DeepEqual(exprs, wantExprs) {
			t.Fatalf("iteration %d: MatchExpressions not in deterministic sorted order:\n got: %+v\nwant: %+v", i, exprs, wantExprs)
		}
	}
}

// TestVerifyGenerateNodeAffinityPrefixCollision guards the boundary between
// the slash-form scope annotations (affinity.dataflow.fluid.io/inject|labels)
// and the dot-form customized prefix (affinity.dataflow.fluid.io.): neither
// slash-form key may leak into MatchExpressions.
func TestVerifyGenerateNodeAffinityPrefixCollision(t *testing.T) {
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				common.AnnotationDataFlowAffinityInject:                        "true",
				common.AnnotationDataFlowAffinityLabelsName:                    "x",
				common.AnnotationDataFlowCustomizedAffinityPrefix + "only-one": "v",
			},
		},
	}
	got, err := GenerateNodeAffinity(job)
	if err != nil {
		t.Fatalf("GenerateNodeAffinity() unexpected error: %v", err)
	}
	exprs := got.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions
	want := []corev1.NodeSelectorRequirement{
		{Key: "only-one", Operator: corev1.NodeSelectorOpIn, Values: []string{"v"}},
	}
	if !reflect.DeepEqual(exprs, want) {
		t.Fatalf("slash-form scope annotations leaked into MatchExpressions:\n got: %+v\nwant: %+v", exprs, want)
	}
}
