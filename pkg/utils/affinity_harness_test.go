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

package utils

import (
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
)

// Reviewer verification harness for PR #6276 (issue #6275).
//
// Polarity: ALL tests below are CONTRACT tests — they assert the intended
// correct behavior. On the buggy (base) code they FAIL; on the fixed code they
// PASS. A red result is the reproduction of the finding.
//
// Layer: deterministic unit (pure logic, no dependencies).

var harnessHostnameReq = v1.NodeSelectorRequirement{
	Key:      "kubernetes.io/hostname",
	Operator: v1.NodeSelectorOpIn,
	Values:   []string{"node01"},
}

func harnessTerm(key, value string) v1.NodeSelectorTerm {
	return v1.NodeSelectorTerm{
		MatchExpressions: []v1.NodeSelectorRequirement{
			{
				Key:      key,
				Operator: v1.NodeSelectorOpIn,
				Values:   []string{value},
			},
		},
	}
}

// Contract: with existing terms, the requirement must be appended to EVERY
// term (node selector terms are ORed, so it must be present in each).
// Reproduces issue #6275: on base the loop variable is a copy and the
// appended requirement is discarded.
func TestHarnessInjectAppendsToEveryExistingTerm(t *testing.T) {
	affinity := &v1.Affinity{
		NodeAffinity: &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
				NodeSelectorTerms: []v1.NodeSelectorTerm{
					harnessTerm("disktype", "ssd"),
					harnessTerm("disktype", "nvme"),
					harnessTerm("topology.kubernetes.io/zone", "zone-a"),
				},
			},
		},
	}

	got := InjectNodeSelectorRequirements([]v1.NodeSelectorRequirement{harnessHostnameReq}, affinity)

	terms := got.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	if len(terms) != 3 {
		t.Fatalf("expected 3 terms, got %d", len(terms))
	}
	want := []v1.NodeSelectorRequirement{
		{Key: "disktype", Operator: v1.NodeSelectorOpIn, Values: []string{"ssd"}},
		harnessHostnameReq,
	}
	for i, tt := range []string{"ssd", "nvme", "zone-a"} {
		want[0].Values = []string{tt}
		if i == 2 {
			want[0].Key = "topology.kubernetes.io/zone"
		} else {
			want[0].Key = "disktype"
		}
		if !reflect.DeepEqual(terms[i].MatchExpressions, want) {
			t.Errorf("term %d: got %v, want %v (requirement must be appended to every term)", i, terms[i].MatchExpressions, want)
		}
	}
}

// Contract: an empty but non-nil NodeSelectorTerms must behave like nil —
// the requirement becomes a new term instead of being silently dropped.
// On base, the non-nil-empty case fell through to the (broken) loop and the
// requirement was lost.
func TestHarnessInjectEmptyNonNilTermsCreatesTerm(t *testing.T) {
	affinity := &v1.Affinity{
		NodeAffinity: &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
				NodeSelectorTerms: []v1.NodeSelectorTerm{},
			},
		},
	}

	got := InjectNodeSelectorRequirements([]v1.NodeSelectorRequirement{harnessHostnameReq}, affinity)

	terms := got.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	want := []v1.NodeSelectorTerm{{MatchExpressions: []v1.NodeSelectorRequirement{harnessHostnameReq}}}
	if !reflect.DeepEqual(terms, want) {
		t.Errorf("got %v, want %v", terms, want)
	}
}

// Guard (already worked on base): nil terms -> single new term holding only
// the injected requirements, and the input affinity is mutated in place /
// returned by pointer.
func TestHarnessInjectNilTermsUnchangedBehavior(t *testing.T) {
	affinity := &v1.Affinity{}
	got := InjectNodeSelectorRequirements([]v1.NodeSelectorRequirement{harnessHostnameReq}, affinity)
	if got != affinity {
		t.Errorf("expected the same affinity pointer to be returned, got a different object")
	}
	terms := got.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	want := []v1.NodeSelectorTerm{{MatchExpressions: []v1.NodeSelectorRequirement{harnessHostnameReq}}}
	if !reflect.DeepEqual(terms, want) {
		t.Errorf("got %v, want %v", terms, want)
	}
}

// Guard: empty matchExpressions is a no-op (also on base).
func TestHarnessInjectNoRequirementsIsNoop(t *testing.T) {
	affinity := &v1.Affinity{
		NodeAffinity: &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
				NodeSelectorTerms: []v1.NodeSelectorTerm{harnessTerm("disktype", "ssd")},
			},
		},
	}
	got := InjectNodeSelectorRequirements(nil, affinity)
	if !reflect.DeepEqual(got, affinity) {
		t.Errorf("expected no change, got %v", got)
	}
}
