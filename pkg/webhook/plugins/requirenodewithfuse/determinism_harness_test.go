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

package requirenodewithfuse

import (
	"fmt"
	"strings"
	"testing"

	"github.com/fluid-cloudnative/fluid/pkg/ddc/base"
)

// Reviewer harness (PR #6234). Contract test, polarity: contract.
//
// Claim: under a multi-label fuse node selector, getRequiredSchedulingTerm must
// emit MatchExpressions sorted by key ascending, deterministically across calls.
// On the PR base (unsorted map range) this test FAILS with multiple distinct
// orderings; on the PR head it PASSES.
func TestHarnessFuseSelectorExpressionsSortedDeterministic(t *testing.T) {
	selectors := map[string]string{
		"zeta":  "1",
		"alpha": "2",
		"mu":    "3",
		"beta":  "4",
		"kappa": "5",
		"iota":  "6",
	}
	expected := "alpha,beta,iota,kappa,mu,zeta"

	runtimeInfo, err := base.BuildRuntimeInfo("harness", "fluid", "alluxio")
	if err != nil {
		t.Fatalf("fail to build runtimeInfo: %v", err)
	}
	runtimeInfo.SetFuseNodeSelector(selectors)

	orderings := map[string]int{}
	var sampleOther string
	for i := 0; i < 500; i++ {
		term, err := getRequiredSchedulingTerm(runtimeInfo)
		if err != nil {
			t.Fatalf("iteration %d: unexpected error: %v", i, err)
		}
		keys := make([]string, 0, len(term.MatchExpressions))
		for _, me := range term.MatchExpressions {
			keys = append(keys, me.Key)
		}
		seq := strings.Join(keys, ",")
		orderings[seq]++
		if seq != expected && sampleOther == "" {
			sampleOther = seq
		}
	}

	if len(orderings) != 1 {
		t.Fatalf("non-deterministic MatchExpressions order: observed %d distinct orderings over 500 calls (expected exactly 1). "+
			"expected sorted order %q; sample alternative order %q; histogram %v",
			len(orderings), expected, sampleOther, orderings)
	}
	if got, ok := orderings[expected]; !ok || got != 500 {
		t.Fatalf("deterministic but not sorted: all 500 calls returned %q, expected %q", sampleOf(orderings), expected)
	}
}

func sampleOf(m map[string]int) string {
	for k := range m {
		return k
	}
	return fmt.Sprintf("%v", m)
}
