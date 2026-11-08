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

package nodeaffinitywithcache

import (
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/ddc/base"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Reviewer harness (PR #6234). Contract tests, polarity: contract.
//
// Claim: for a pod bound to multiple runtimes, the tiered-locality preferred
// scheduling terms and required node selector terms must be emitted in runtime
// name (map key) sorted order, deterministically across calls. On the PR base
// (unsorted map range) these tests FAIL with multiple distinct orders; on the
// PR head they PASS.

func harnessTieredLocalitySetup(t *testing.T) (client.Client, map[string]base.RuntimeInfoInterface, string) {
	t.Helper()

	runtimeNames := []string{"runtime-m", "runtime-a", "runtime-z", "runtime-b"}

	rtA := &datav1alpha1.AlluxioRuntime{ObjectMeta: metav1.ObjectMeta{Name: "runtime-a", Namespace: "fluid-test"}}
	rtB := &datav1alpha1.AlluxioRuntime{ObjectMeta: metav1.ObjectMeta{Name: "runtime-b", Namespace: "fluid-test"}}
	rtM := &datav1alpha1.AlluxioRuntime{ObjectMeta: metav1.ObjectMeta{Name: "runtime-m", Namespace: "fluid-test"}}
	rtZ := &datav1alpha1.AlluxioRuntime{ObjectMeta: metav1.ObjectMeta{Name: "runtime-z", Namespace: "fluid-test"}}

	cl := fake.NewFakeClientWithScheme(testScheme, rtA, rtB, rtM, rtZ)

	runtimeMap := map[string]base.RuntimeInfoInterface{}
	for _, name := range runtimeNames {
		info, err := base.BuildRuntimeInfo(name, "fluid-test", "alluxio")
		require.NoError(t, err)
		runtimeMap[name] = info
	}

	// expected term order: runtime names sorted ascending, terms keyed by common label name
	expected := runtimeMap["runtime-a"].GetCommonLabelName() + "," +
		runtimeMap["runtime-b"].GetCommonLabelName() + "," +
		runtimeMap["runtime-m"].GetCommonLabelName() + "," +
		runtimeMap["runtime-z"].GetCommonLabelName()

	return cl, runtimeMap, expected
}

func TestHarnessTieredLocalityPreferredOrderSortedDeterministic(t *testing.T) {
	cl, runtimeMap, expected := harnessTieredLocalitySetup(t)

	plugin, err := NewPlugin(cl, simpleTieredLocality)
	require.NoError(t, err)
	nodeAffinityPlugin := plugin.(*NodeAffinityWithCache)

	preferredLocality := map[string]int32{"fluid.io/node": 100}

	orders := map[string]int{}
	sampleOther := ""
	for i := 0; i < 300; i++ {
		terms, err := nodeAffinityPlugin.getTieredLocalityPreferredSchedulingTerms(runtimeMap, preferredLocality)
		require.NoError(t, err)
		require.Len(t, terms, 4)
		seq := ""
		for idx, term := range terms {
			if idx > 0 {
				seq += ","
			}
			seq += term.Preference.MatchExpressions[0].Key
		}
		orders[seq]++
		if seq != expected && sampleOther == "" {
			sampleOther = seq
		}
	}

	if len(orders) != 1 {
		t.Fatalf("non-deterministic preferred scheduling term order: %d distinct orders over 300 calls (expected 1). "+
			"expected %q; sample alternative %q; histogram %v", len(orders), expected, sampleOther, orders)
	}
	assert.Equal(t, expected, harnessFirstKey(orders), "deterministic but not sorted by runtime name")
}

func TestHarnessTieredLocalityRequiredOrderSortedDeterministic(t *testing.T) {
	cl, runtimeMap, expected := harnessTieredLocalitySetup(t)

	plugin, err := NewPlugin(cl, simpleTieredLocality)
	require.NoError(t, err)
	nodeAffinityPlugin := plugin.(*NodeAffinityWithCache)

	requireLocality := []string{"fluid.io/node"}

	orders := map[string]int{}
	sampleOther := ""
	for i := 0; i < 300; i++ {
		terms, err := nodeAffinityPlugin.getTieredLocalityNodeSelectorTerms(runtimeMap, requireLocality)
		require.NoError(t, err)
		require.Len(t, terms, 4)
		seq := ""
		for idx, term := range terms {
			if idx > 0 {
				seq += ","
			}
			seq += term.MatchExpressions[0].Key
		}
		orders[seq]++
		if seq != expected && sampleOther == "" {
			sampleOther = seq
		}
	}

	if len(orders) != 1 {
		t.Fatalf("non-deterministic node selector term order: %d distinct orders over 300 calls (expected 1). "+
			"expected %q; sample alternative %q; histogram %v", len(orders), expected, sampleOther, orders)
	}
	assert.Equal(t, expected, harnessFirstKey(orders), "deterministic but not sorted by runtime name")
}

func harnessFirstKey(m map[string]int) string {
	for k := range m {
		return k
	}
	return ""
}
