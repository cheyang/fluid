// Verification harness for fluid-cloudnative/fluid PR #6234 (reviewer: codex).
// Additive test file; no production code is modified.
//
// CONTRACT test for the premise (issue #6220 site 1):
// getTieredLocalityPreferredSchedulingTerms and getTieredLocalityNodeSelectorTerms
// iterate the preferredRuntimes/runtimeInfos maps; the emitted scheduling terms
// must appear in deterministic (runtime-name sorted) order. FAILS on base
// (unsorted map iteration), PASSES on the PR head.

package nodeaffinitywithcache

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/ddc/base"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
)

func TestVerifyPR6234TieredLocalityDeterministic(t *testing.T) {
	mkRuntime := func(name string) *datav1alpha1.AlluxioRuntime {
		return &datav1alpha1.AlluxioRuntime{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "fluid-test"},
		}
	}
	cl := fake.NewFakeClientWithScheme(testScheme,
		mkRuntime("runtime-c"), mkRuntime("runtime-a"), mkRuntime("runtime-b"))

	plugin, err := NewPlugin(cl, simpleTieredLocality)
	if err != nil {
		t.Fatalf("NewPlugin: %v", err)
	}
	p := plugin.(*NodeAffinityWithCache)

	mkInfo := func(name string) base.RuntimeInfoInterface {
		info, err := base.BuildRuntimeInfo(name, "fluid-test", "alluxio")
		if err != nil {
			t.Fatalf("BuildRuntimeInfo: %v", err)
		}
		return info
	}
	infoA, infoB, infoC := mkInfo("runtime-a"), mkInfo("runtime-b"), mkInfo("runtime-c")
	runtimeMap := map[string]base.RuntimeInfoInterface{
		"runtime-c": infoC,
		"runtime-a": infoA,
		"runtime-b": infoB,
	}
	// runtime-name sorted order of the per-runtime label keys
	wantPreferredKeys := []string{infoA.GetCommonLabelName(), infoB.GetCommonLabelName(), infoC.GetCommonLabelName()}
	wantRequiredKeys := wantPreferredKeys

	preferredLocality := map[string]int32{"fluid.io/node": 100}
	requiredLocality := []string{"fluid.io/node"}

	for i := 0; i < 100; i++ {
		preferred, err := p.getTieredLocalityPreferredSchedulingTerms(runtimeMap, preferredLocality)
		if err != nil {
			t.Fatalf("iteration %d: preferred: %v", i, err)
		}
		if len(preferred) != 3 {
			t.Fatalf("iteration %d: expected 3 preferred terms, got %d", i, len(preferred))
		}
		for j, term := range preferred {
			got := term.Preference.MatchExpressions[0].Key
			if got != wantPreferredKeys[j] {
				t.Fatalf("iteration %d: preferred term order non-deterministic: term[%d].key=%s, want %s (full want %v)", i, j, got, wantPreferredKeys[j], wantPreferredKeys)
			}
		}

		required, err := p.getTieredLocalityNodeSelectorTerms(runtimeMap, requiredLocality)
		if err != nil {
			t.Fatalf("iteration %d: required: %v", i, err)
		}
		if len(required) != 3 {
			t.Fatalf("iteration %d: expected 3 required terms, got %d", i, len(required))
		}
		for j, term := range required {
			got := term.MatchExpressions[0].Key
			if got != wantRequiredKeys[j] {
				t.Fatalf("iteration %d: required term order non-deterministic: term[%d].key=%s, want %s", i, j, got, wantRequiredKeys[j])
			}
		}
	}
}
