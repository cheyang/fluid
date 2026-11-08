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

package efc

import (
	"context"
	"strings"
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/ddc/base"
	"github.com/fluid-cloudnative/fluid/pkg/utils"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Reviewer harness (PR #6234). Contract test, polarity: contract.
//
// Claim: the EFC engine's createFusePersistentVolume must emit PV node affinity
// MatchExpressions sorted by key ascending, deterministically across calls.
// On the PR base (unsorted map range) this test FAILS with multiple distinct
// orderings; on the PR head it PASSES.
func TestHarnessEFCPVNodeAffinityExpressionsSortedDeterministic(t *testing.T) {
	t.Setenv(utils.MountRoot, "/runtime-mnt")

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

	for i := 0; i < 60; i++ {
		runtimeInput := &datav1alpha1.EFCRuntime{
			ObjectMeta: metav1.ObjectMeta{Name: "hbase", Namespace: "fluid"},
		}
		datasetInput := &datav1alpha1.Dataset{
			ObjectMeta: metav1.ObjectMeta{Name: "hbase", Namespace: "fluid"},
		}
		client := fake.NewFakeClientWithScheme(testScheme, runtimeInput, datasetInput)
		engine := &EFCEngine{
			Client:    client,
			Log:       fake.NullLogger(),
			namespace: "fluid",
			name:      "hbase",
		}

		runtimeInfo, err := base.BuildRuntimeInfo("hbase", "fluid", common.EFCRuntime)
		if err != nil {
			t.Fatalf("iteration %d: fail to build runtimeInfo: %v", i, err)
		}
		runtimeInfo.SetFuseNodeSelector(selectors)
		engine.runtimeInfo = runtimeInfo

		if err := engine.createFusePersistentVolume(context.Background()); err != nil {
			t.Fatalf("iteration %d: createFusePersistentVolume failed: %v", i, err)
		}

		var pvs v1.PersistentVolumeList
		if err := client.List(context.TODO(), &pvs); err != nil {
			t.Fatalf("iteration %d: list PVs failed: %v", i, err)
		}
		if len(pvs.Items) != 1 {
			t.Fatalf("iteration %d: expected 1 PV, got %d", i, len(pvs.Items))
		}
		pv := pvs.Items[0]
		if pv.Spec.NodeAffinity == nil || pv.Spec.NodeAffinity.Required == nil || len(pv.Spec.NodeAffinity.Required.NodeSelectorTerms) != 1 {
			t.Fatalf("iteration %d: unexpected node affinity: %v", i, pv.Spec.NodeAffinity)
		}
		keys := make([]string, 0, len(pv.Spec.NodeAffinity.Required.NodeSelectorTerms[0].MatchExpressions))
		for _, me := range pv.Spec.NodeAffinity.Required.NodeSelectorTerms[0].MatchExpressions {
			keys = append(keys, me.Key)
		}
		seq := strings.Join(keys, ",")
		orderings[seq]++
		if seq != expected && sampleOther == "" {
			sampleOther = seq
		}
	}

	if len(orderings) != 1 {
		t.Fatalf("non-deterministic EFC PV node affinity order: %d distinct orderings over 60 PV creations (expected 1). "+
			"expected sorted order %q; sample alternative %q; histogram %v",
			len(orderings), expected, sampleOther, orderings)
	}
	if _, ok := orderings[expected]; !ok {
		for k := range orderings {
			t.Fatalf("deterministic but not sorted: all 60 PVs got %q, expected %q", k, expected)
		}
	}
}
