// Verification harness for fluid-cloudnative/fluid PR #6234 (reviewer: codex).
// Additive test file; no production code is modified.
//
// CONTROL test: MergeNodeSelectorAndNodeAffinity was already sorted by #6217 on
// the base branch, so this contract test must PASS on BOTH base and head. If it
// fails on base, the harness itself is broken; if it fails only on head, the PR
// regressed the refactor.

package kubeclient

import (
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/fluid-cloudnative/fluid/pkg/utils"
)

func TestVerifyPR6234MergeNodeSelectorSortedControl(t *testing.T) {
	selector := map[string]string{
		"zone":   "us-east-1a",
		"arch":   "amd64",
		"region": "us-east-1",
		"inst":   "large",
	}
	want := []string{"arch", "inst", "region", "zone"}

	for i := 0; i < 200; i++ {
		na := MergeNodeSelectorAndNodeAffinity(selector, &corev1.Affinity{})
		terms := na.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
		if len(terms) != 1 {
			t.Fatalf("iteration %d: expected 1 node selector term, got %d", i, len(terms))
		}
		var gotKeys []string
		for _, expr := range terms[0].MatchExpressions {
			gotKeys = append(gotKeys, expr.Key)
		}
		if len(gotKeys) != len(want) {
			t.Fatalf("iteration %d: expected %d expressions, got %d", i, len(want), len(gotKeys))
		}
		for j := range want {
			if gotKeys[j] != want[j] {
				t.Fatalf("iteration %d: MatchExpressions not sorted/deterministic: got keys %v, want %v", i, gotKeys, want)
			}
		}
	}
	t.Logf("control: 200 iterations produced sorted order %v (helper utils.OrderedKeys present: %v)", want, len(utils.OrderedKeys(selector)) == len(want))
}
