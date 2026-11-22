/*
Verification harness for PR #6276 (reviewer: codex).
Additive test file only — production code is untouched.

Polarity: every test here is a CONTRACT test. It asserts the intended correct
behavior of utils.InjectNodeSelectorRequirements, so it FAILS on the buggy base
(896d21e) and PASSES on the PR head (add9a8fa).
*/

package utils

import (
	"testing"

	v1 "k8s.io/api/core/v1"
)

func hostnameReq() v1.NodeSelectorRequirement {
	return v1.NodeSelectorRequirement{
		Key:      "kubernetes.io/hostname",
		Operator: v1.NodeSelectorOpIn,
		Values:   []string{"node01"},
	}
}

func affinityWithTerms(terms ...v1.NodeSelectorTerm) *v1.Affinity {
	return &v1.Affinity{
		NodeAffinity: &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
				NodeSelectorTerms: terms,
			},
		},
	}
}

func countReq(terms []v1.NodeSelectorTerm, key string) []int {
	counts := make([]int, len(terms))
	for i, t := range terms {
		for _, e := range t.MatchExpressions {
			if e.Key == key {
				counts[i]++
			}
		}
	}
	return counts
}

// Contract: when the affinity already has required terms, the requirement must
// be appended to EVERY term (terms are ORed by the scheduler).
func TestVerifyInjectRequirementsIntoEveryExistingTerm(t *testing.T) {
	in := affinityWithTerms(
		v1.NodeSelectorTerm{MatchExpressions: []v1.NodeSelectorRequirement{
			{Key: "disktype", Operator: v1.NodeSelectorOpIn, Values: []string{"ssd"}},
		}},
		v1.NodeSelectorTerm{MatchExpressions: []v1.NodeSelectorRequirement{
			{Key: "disktype", Operator: v1.NodeSelectorOpIn, Values: []string{"nvme"}},
		}},
	)
	out := InjectNodeSelectorRequirements([]v1.NodeSelectorRequirement{hostnameReq()}, in)
	terms := out.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	if len(terms) != 2 {
		t.Fatalf("expected 2 terms preserved, got %d", len(terms))
	}
	for i, c := range countReq(terms, "kubernetes.io/hostname") {
		if c != 1 {
			t.Errorf("term %d: expected hostname requirement to be present once, got %d (term=%+v)", i, c, terms[i].MatchExpressions)
		}
		if len(terms[i].MatchExpressions) != 2 {
			t.Errorf("term %d: expected 2 match expressions (original + injected), got %d", i, len(terms[i].MatchExpressions))
		}
	}
}

// Contract: appending must work even when the existing MatchExpressions slice
// has spare capacity (append writes into the shared backing array — only an
// index-based write to the term lands in the result).
func TestVerifyInjectRequirementsWithSpareCapacity(t *testing.T) {
	exprs := make([]v1.NodeSelectorRequirement, 1, 4) // len 1, cap 4
	exprs[0] = v1.NodeSelectorRequirement{Key: "disktype", Operator: v1.NodeSelectorOpIn, Values: []string{"ssd"}}
	in := affinityWithTerms(v1.NodeSelectorTerm{MatchExpressions: exprs})
	out := InjectNodeSelectorRequirements([]v1.NodeSelectorRequirement{hostnameReq()}, in)
	terms := out.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	if got := len(terms[0].MatchExpressions); got != 2 {
		t.Fatalf("expected 2 match expressions after injection with spare capacity, got %d", got)
	}
	if terms[0].MatchExpressions[1].Key != "kubernetes.io/hostname" {
		t.Fatalf("expected injected requirement at index 1, got %+v", terms[0].MatchExpressions[1])
	}
}

// Contract: an empty-but-non-nil NodeSelectorTerms slice must be treated like
// nil — the requirement becomes a fresh single term instead of being dropped
// (on base, the empty slice passed the == nil check, the loop ran zero times,
// and the requirement was silently lost).
func TestVerifyInjectRequirementsEmptyNonNilTerms(t *testing.T) {
	in := &v1.Affinity{
		NodeAffinity: &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
				NodeSelectorTerms: []v1.NodeSelectorTerm{}, // empty, non-nil
			},
		},
	}
	if in.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms == nil {
		t.Fatal("precondition failed: expected non-nil empty slice")
	}
	out := InjectNodeSelectorRequirements([]v1.NodeSelectorRequirement{hostnameReq()}, in)
	terms := out.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	if len(terms) != 1 {
		t.Fatalf("expected 1 synthesized term, got %d", len(terms))
	}
	if c := countReq(terms, "kubernetes.io/hostname")[0]; c != 1 {
		t.Fatalf("expected hostname requirement in synthesized term, got count %d", c)
	}
}

// Contract sanity (passes on base too): nil affinity still yields a fresh
// single-term affinity.
func TestVerifyInjectRequirementsNilAffinity(t *testing.T) {
	out := InjectNodeSelectorRequirements([]v1.NodeSelectorRequirement{hostnameReq()}, nil)
	terms := out.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	if len(terms) != 1 || len(terms[0].MatchExpressions) != 1 {
		t.Fatalf("unexpected result for nil affinity: %+v", terms)
	}
}

// Contract sanity (passes on base too): empty requirement input is a no-op.
func TestVerifyInjectRequirementsEmptyInput(t *testing.T) {
	in := affinityWithTerms(v1.NodeSelectorTerm{MatchExpressions: []v1.NodeSelectorRequirement{
		{Key: "disktype", Operator: v1.NodeSelectorOpIn, Values: []string{"ssd"}},
	}})
	out := InjectNodeSelectorRequirements(nil, in)
	if out != in {
		t.Fatalf("expected identity return for empty input")
	}
}
