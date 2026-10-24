/*
Verification harness for fluid PR #6217 / issue #6216.
Additive reviewer test file (stage 2 verify). Production code untouched.

Claim P0 (premise, runs against BASE): with a multi-label nodeSelector map,
MergeNodeSelectorAndNodeAffinity emits MatchExpressions in nondeterministic
(map iteration) order, so a controller that rebuilds status.CacheAffinity every
reconcile and compares with reflect.DeepEqual (pkg/ddc/alluxio/status.go:121,
pkg/ddc/cache/engine/status.go:150, jindo/juicefs/thin/jindofsx/jindocache
equivalents) observes spurious diffs and issues a status update on nearly every
cycle.

Polarity: CONTRACT tests. They FAIL on the base commit (proving the bug) and
PASS on the PR head (proving the fix).
*/

package kubeclient

import (
	"reflect"
	"sort"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

// 6 distinct labels -> 720 possible orderings on unfixed code.
var verifySelector = map[string]string{
	"kubernetes.io/hostname":           "node-1",
	"topology.kubernetes.io/zone":      "zone-a",
	"fluid.io/dataset":                 "my-dataset",
	"node.kubernetes.io/instance-type": "m5.large",
	"beta.kubernetes.io/arch":          "amd64",
	"app.kubernetes.io/name":           "worker",
}

func verifyExpressions(t *testing.T, a *corev1.NodeAffinity) []corev1.NodeSelectorRequirement {
	t.Helper()
	if a == nil || a.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		t.Fatalf("expected non-nil required node affinity")
	}
	terms := a.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	if len(terms) != 1 {
		t.Fatalf("expected exactly 1 node selector term, got %d", len(terms))
	}
	return terms[0].MatchExpressions
}

// TestVerifyMergeNodeSelectorDeterministic asserts the output is sorted and
// byte-identical across 300 invocations. On base, the probability of 300
// consecutive identical random orderings of 6 keys is (1/720)^300 ~ 0.
func TestVerifyMergeNodeSelectorDeterministic(t *testing.T) {
	first := verifyExpressions(t, MergeNodeSelectorAndNodeAffinity(verifySelector, nil))
	if len(first) != len(verifySelector) {
		t.Fatalf("expected %d expressions, got %d", len(verifySelector), len(first))
	}
	if !sort.SliceIsSorted(first, func(i, j int) bool { return first[i].Key < first[j].Key }) {
		t.Fatalf("match expressions are not sorted by key: %v", first)
	}
	for i := 0; i < 300; i++ {
		cur := verifyExpressions(t, MergeNodeSelectorAndNodeAffinity(verifySelector, nil))
		if !reflect.DeepEqual(cur, first) {
			t.Fatalf("iteration %d: expression order changed between calls\nfirst: %v\ncur:   %v", i, first, cur)
		}
	}
}

// TestVerifyStatusCompareStability simulates the reconcile-loop comparison in
// pkg/ddc/alluxio/status.go:121: the persisted status (computed once) is
// compared with reflect.DeepEqual against a freshly recomputed affinity each
// cycle. Counts how many of 200 cycles would trigger a spurious status update.
// Expect 0 on fixed code; expect ~200 * (1 - 1/720) on base.
func TestVerifyStatusCompareStability(t *testing.T) {
	persisted := MergeNodeSelectorAndNodeAffinity(verifySelector, nil)
	spurious := 0
	for i := 0; i < 200; i++ {
		recomputed := MergeNodeSelectorAndNodeAffinity(verifySelector, nil)
		if !reflect.DeepEqual(recomputed, persisted) {
			spurious++
		}
	}
	if spurious > 0 {
		t.Fatalf("status comparison flapped: %d/200 reconcile cycles would issue a spurious status update", spurious)
	}
}

// TestVerifyMergeIntoExistingTermsDeterministic covers the append path:
// nodeSelector expressions are merged into every existing NodeSelectorTerm of
// a user-supplied pod affinity. Output must stay deterministic there too.
func TestVerifyMergeIntoExistingTermsDeterministic(t *testing.T) {
	affinity := &corev1.Affinity{
		NodeAffinity: &corev1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{
					{MatchExpressions: []corev1.NodeSelectorRequirement{
						{Key: "preset", Operator: corev1.NodeSelectorOpIn, Values: []string{"x"}},
					}},
				},
			},
		},
	}
	first := MergeNodeSelectorAndNodeAffinity(verifySelector, affinity)
	for i := 0; i < 200; i++ {
		cur := MergeNodeSelectorAndNodeAffinity(verifySelector, affinity)
		if !reflect.DeepEqual(cur, first) {
			t.Fatalf("iteration %d: merged affinity changed between calls\nfirst: %v\ncur:   %v", i, first, cur)
		}
	}
}
