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

package volume

import (
	"context"
	"strings"
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/ddc/base"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// Reviewer harness (PR #6234). Contract test, polarity: contract.
//
// Claim: CreatePersistentVolumeForRuntime must emit PV node affinity
// MatchExpressions sorted by key ascending, deterministically across calls.
// On the PR base (unsorted map range) this test FAILS with multiple distinct
// orderings; on the PR head it PASSES.
func TestHarnessPVNodeAffinityExpressionsSortedDeterministic(t *testing.T) {
	selectors := map[string]string{
		"zeta":  "1",
		"alpha": "2",
		"mu":    "3",
		"beta":  "4",
		"kappa": "5",
		"iota":  "6",
	}
	expected := "alpha,beta,iota,kappa,mu,zeta"

	orderings := map[string]int{}
	sampleOther := ""

	for i := 0; i < 80; i++ {
		scheme := runtime.NewScheme()
		_ = v1.AddToScheme(scheme)
		_ = datav1alpha1.AddToScheme(scheme)
		dataset := &datav1alpha1.Dataset{ObjectMeta: metav1.ObjectMeta{Name: "hbase", Namespace: "fluid"}}
		client := fake.NewFakeClientWithScheme(scheme, dataset)

		runtimeInfo, err := base.BuildRuntimeInfo("hbase", "fluid", "alluxio")
		if err != nil {
			t.Fatalf("iteration %d: fail to build runtimeInfo: %v", i, err)
		}
		runtimeInfo.SetFuseNodeSelector(selectors)

		err = CreatePersistentVolumeForRuntime(context.Background(), client, runtimeInfo, "/mnt", "alluxio", fake.NullLogger())
		if err != nil {
			t.Fatalf("iteration %d: CreatePersistentVolumeForRuntime failed: %v", i, err)
		}

		var list v1.PersistentVolumeList
		if err := client.List(context.TODO(), &list); err != nil {
			t.Fatalf("iteration %d: list PVs failed: %v", i, err)
		}
		if len(list.Items) != 1 {
			t.Fatalf("iteration %d: expected 1 PV, got %d", i, len(list.Items))
		}
		terms := list.Items[0].Spec.NodeAffinity.Required.NodeSelectorTerms
		if len(terms) != 1 {
			t.Fatalf("iteration %d: expected 1 node selector term, got %d", i, len(terms))
		}
		keys := make([]string, 0, len(terms[0].MatchExpressions))
		for _, me := range terms[0].MatchExpressions {
			keys = append(keys, me.Key)
		}
		seq := strings.Join(keys, ",")
		orderings[seq]++
		if seq != expected && sampleOther == "" {
			sampleOther = seq
		}
	}

	if len(orderings) != 1 {
		t.Fatalf("non-deterministic PV node affinity order: %d distinct orderings over 80 PV creations (expected 1). "+
			"expected sorted order %q; sample alternative %q; histogram %v",
			len(orderings), expected, sampleOther, orderings)
	}
	if _, ok := orderings[expected]; !ok {
		for k := range orderings {
			t.Fatalf("deterministic but not sorted: all 80 PVs got %q, expected %q", k, expected)
		}
	}
}
