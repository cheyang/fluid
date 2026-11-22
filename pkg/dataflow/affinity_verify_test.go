/*
Verification harness for PR #6276 (reviewer: codex).
Additive test file only — production code is untouched.

Polarity: CONTRACT tests. They assert the intended behavior of
InjectAffinityByRunAfterOp with policy=Require when the current operation
already carries its own required node affinity. FAILS on base (896d21e),
PASSES on the PR head (add9a8fa).
*/

package dataflow

import (
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func verifyTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = v1.AddToScheme(s)
	_ = datav1alpha1.AddToScheme(s)
	return s
}

func verifyCompletedDataLoad(name, ns string) *datav1alpha1.DataLoad {
	return &datav1alpha1.DataLoad{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Status: datav1alpha1.OperationStatus{
			Phase: common.PhaseComplete,
			NodeAffinity: &v1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
					NodeSelectorTerms: []v1.NodeSelectorTerm{
						{MatchExpressions: []v1.NodeSelectorRequirement{
							{Key: common.K8sNodeNameLabelKey, Operator: v1.NodeSelectorOpIn, Values: []string{"node01"}},
						}},
					},
				},
			},
		},
	}
}

func verifyRunAfterRequire(name string) *datav1alpha1.OperationRef {
	return &datav1alpha1.OperationRef{
		ObjectRef: datav1alpha1.ObjectRef{Kind: "DataLoad", Name: name},
		AffinityStrategy: datav1alpha1.AffinityStrategy{
			Policy: datav1alpha1.RequireAffinityStrategy,
		},
	}
}

func verifyHostnameCount(terms []v1.NodeSelectorTerm) []int {
	counts := make([]int, len(terms))
	for i, t := range terms {
		for _, e := range t.MatchExpressions {
			if e.Key == common.K8sNodeNameLabelKey {
				counts[i]++
			}
		}
	}
	return counts
}

// Issue #6275 through the real entry point: Require policy + the current op
// already has its own required node affinity with two terms. The preceding
// op's hostname must land in BOTH terms.
func TestVerifyInjectAffinityByRunAfterOpRequireMergesExistingTerms(t *testing.T) {
	objs := []runtime.Object{verifyCompletedDataLoad("prev-op", "default")}
	c := fake.NewFakeClientWithScheme(verifyTestScheme(), objs...)

	current := &v1.Affinity{
		NodeAffinity: &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
				NodeSelectorTerms: []v1.NodeSelectorTerm{
					{MatchExpressions: []v1.NodeSelectorRequirement{
						{Key: "node.kubernetes.io/instance-type", Operator: v1.NodeSelectorOpIn, Values: []string{"GPU"}},
					}},
					{MatchExpressions: []v1.NodeSelectorRequirement{
						{Key: "topology.kubernetes.io/zone", Operator: v1.NodeSelectorOpIn, Values: []string{"zone-a"}},
					}},
				},
			},
		},
	}

	got, err := InjectAffinityByRunAfterOp(c, verifyRunAfterRequire("prev-op"), "default", current)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	terms := got.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	if len(terms) != 2 {
		t.Fatalf("expected the 2 existing terms to be preserved, got %d: %+v", len(terms), terms)
	}
	for i, cnt := range verifyHostnameCount(terms) {
		if cnt != 1 {
			t.Errorf("term %d: expected preceding op hostname requirement injected once, got %d (term=%+v)", i, cnt, terms[i].MatchExpressions)
		}
		if len(terms[i].MatchExpressions) != 2 {
			t.Errorf("term %d: expected original + injected expressions, got %d", i, len(terms[i].MatchExpressions))
		}
	}
}

// Empty-but-non-nil existing terms: base dropped the requirement and left an
// (API-invalid) empty NodeSelector; the fix must synthesize a fresh term.
func TestVerifyInjectAffinityByRunAfterOpRequireEmptyTerms(t *testing.T) {
	objs := []runtime.Object{verifyCompletedDataLoad("prev-op", "default")}
	c := fake.NewFakeClientWithScheme(verifyTestScheme(), objs...)

	current := &v1.Affinity{
		NodeAffinity: &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
				NodeSelectorTerms: []v1.NodeSelectorTerm{},
			},
		},
	}

	got, err := InjectAffinityByRunAfterOp(c, verifyRunAfterRequire("prev-op"), "default", current)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	terms := got.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	if len(terms) != 1 {
		t.Fatalf("expected 1 synthesized term, got %d", len(terms))
	}
	if verifyHostnameCount(terms)[0] != 1 {
		t.Fatalf("expected hostname requirement in synthesized term, got %+v", terms[0].MatchExpressions)
	}
}
