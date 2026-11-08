// Verification harness for fluid-cloudnative/fluid PR #6234 (reviewer: codex).
// Additive test file; no production code is modified.
//
// TestVerifyPR6234FuseSelectorRequirementsSorted is a CONTRACT test for the
// premise (issue #6220 site 2): getRequiredSchedulingTerm must emit
// MatchExpressions sorted by key. FAILS on base (unsorted map iteration),
// PASSES on the PR head.
//
// TestVerifyPR6234MutateTermOrderDeterministic is a CONTRACT test for finding
// F1: RequireNodeWithFuse.Mutate iterates the runtimeInfos map without sorting,
// so with a pod bound to multiple runtimes the ORDER of the injected
// NodeSelectorTerms is still non-deterministic even at the PR head. FAILS on
// both base and head until the outer loop is also ordered.

package requirenodewithfuse

import (
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/fluid-cloudnative/fluid/pkg/ddc/base"
)

func TestVerifyPR6234FuseSelectorRequirementsSorted(t *testing.T) {
	runtimeInfo, err := base.BuildRuntimeInfo("test", "fluid", "alluxio")
	if err != nil {
		t.Fatalf("BuildRuntimeInfo: %v", err)
	}
	runtimeInfo.SetFuseNodeSelector(map[string]string{
		"zone":   "us-east-1a",
		"arch":   "amd64",
		"region": "us-east-1",
	})
	want := []string{"arch", "region", "zone"}

	for i := 0; i < 200; i++ {
		term, err := getRequiredSchedulingTerm(runtimeInfo)
		if err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
		var gotKeys []string
		for _, expr := range term.MatchExpressions {
			gotKeys = append(gotKeys, expr.Key)
		}
		if len(gotKeys) != len(want) {
			t.Fatalf("iteration %d: expected %d expressions, got %v", i, len(want), gotKeys)
		}
		for j := range want {
			if gotKeys[j] != want[j] {
				t.Fatalf("iteration %d: MatchExpressions not sorted/deterministic: got %v, want %v", i, gotKeys, want)
			}
		}
	}
}

func TestVerifyPR6234MutateTermOrderDeterministic(t *testing.T) {
	plugin := &RequireNodeWithFuse{}

	mkInfo := func(name, selectorKey string) base.RuntimeInfoInterface {
		info, err := base.BuildRuntimeInfo(name, "fluid", "alluxio")
		if err != nil {
			t.Fatalf("BuildRuntimeInfo: %v", err)
		}
		info.SetFuseNodeSelector(map[string]string{selectorKey: "true"})
		return info
	}

	// Three runtimes; each contributes one NodeSelectorTerm keyed by a
	// per-runtime selector key so the term order is observable.
	runtimeInfos := map[string]base.RuntimeInfoInterface{
		"zzz-runtime": mkInfo("zzz-runtime", "sel-zzz"),
		"aaa-runtime": mkInfo("aaa-runtime", "sel-aaa"),
		"mmm-runtime": mkInfo("mmm-runtime", "sel-mmm"),
	}
	// Contract consistent with the sibling plugin nodeaffinitywithcache:
	// terms appear in runtime-name sorted order, deterministically.
	wantFirstKeys := []string{"sel-aaa", "sel-mmm", "sel-zzz"}

	distinct := map[string]int{}
	for i := 0; i < 200; i++ {
		pod := &corev1.Pod{}
		shouldStop, err := plugin.Mutate(pod, runtimeInfos)
		if err != nil {
			t.Fatalf("iteration %d: Mutate: %v", i, err)
		}
		if shouldStop {
			t.Fatalf("iteration %d: Mutate returned shouldStop=true", i)
		}
		na := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution
		if na == nil || len(na.NodeSelectorTerms) != 3 {
			t.Fatalf("iteration %d: expected 3 node selector terms, got %+v", i, pod.Spec.Affinity)
		}
		var gotFirstKeys []string
		for _, term := range na.NodeSelectorTerms {
			if len(term.MatchExpressions) != 1 {
				t.Fatalf("iteration %d: expected 1 expression per term, got %v", i, term.MatchExpressions)
			}
			gotFirstKeys = append(gotFirstKeys, term.MatchExpressions[0].Key)
		}
		key := gotFirstKeys[0] + "," + gotFirstKeys[1] + "," + gotFirstKeys[2]
		distinct[key]++
		for j := range wantFirstKeys {
			if gotFirstKeys[j] != wantFirstKeys[j] {
				t.Fatalf("iteration %d: NodeSelectorTerm order is non-deterministic across runtimeInfos map iterations: got %v, want sorted %v (distinct orders seen so far: %v)", i, gotFirstKeys, wantFirstKeys, distinct)
			}
		}
	}
}
