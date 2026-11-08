// Verification harness for fluid-cloudnative/fluid PR #6234 (reviewer: codex).
// Additive test file; no production code is modified.
//
// CONTRACT test for the premise (issue #6220 site 3):
// CreatePersistentVolumeForRuntime builds the PV node affinity from the
// runtime's fuse node selector map; the MatchExpressions must be sorted by key.
// FAILS on base (unsorted map iteration), PASSES on the PR head.

package volume

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/ddc/base"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
)

func TestVerifyPR6234PVNodeAffinitySorted(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = datav1alpha1.AddToScheme(scheme)

	runtimeInfo, err := base.BuildRuntimeInfo("hbase", "fluid", "alluxio")
	if err != nil {
		t.Fatalf("BuildRuntimeInfo: %v", err)
	}
	runtimeInfo.SetFuseNodeSelector(map[string]string{
		"zone":   "us-east-1a",
		"arch":   "amd64",
		"region": "us-east-1",
	})
	want := []string{"arch", "region", "zone"}

	log := fake.NullLogger()
	for i := 0; i < 100; i++ {
		// Fresh fake client per iteration: the PV create path short-circuits
		// when the PV already exists.
		dataset := &datav1alpha1.Dataset{ObjectMeta: metav1.ObjectMeta{Name: "hbase", Namespace: "fluid"}}
		client := fake.NewFakeClientWithScheme(scheme, dataset)

		if err := CreatePersistentVolumeForRuntime(context.Background(), client, runtimeInfo, "/mnt", "alluxio", log); err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
		var list v1.PersistentVolumeList
		if err := client.List(context.TODO(), &list); err != nil {
			t.Fatalf("iteration %d: list: %v", i, err)
		}
		if len(list.Items) != 1 {
			t.Fatalf("iteration %d: expected 1 PV, got %d", i, len(list.Items))
		}
		pv := list.Items[0]
		if pv.Spec.NodeAffinity == nil || pv.Spec.NodeAffinity.Required == nil ||
			len(pv.Spec.NodeAffinity.Required.NodeSelectorTerms) != 1 {
			t.Fatalf("iteration %d: expected 1 node selector term, got %+v", i, pv.Spec.NodeAffinity)
		}
		exprs := pv.Spec.NodeAffinity.Required.NodeSelectorTerms[0].MatchExpressions
		var gotKeys []string
		for _, e := range exprs {
			gotKeys = append(gotKeys, e.Key)
		}
		if len(gotKeys) != len(want) {
			t.Fatalf("iteration %d: expected %d expressions, got %v", i, len(want), gotKeys)
		}
		for j := range want {
			if gotKeys[j] != want[j] {
				t.Fatalf("iteration %d: PV MatchExpressions not sorted/deterministic: got %v, want %v", i, gotKeys, want)
			}
		}
	}
}
