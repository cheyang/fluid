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

// Reviewer verification harness for PR #6276 (issue #6275), entry-point layer.
//
// Polarity: CONTRACT test — asserts the intended correct behavior. On the
// buggy (base) code it FAILS; on the fixed code it PASSES.
//
// Layer: integration with a fake API server (the same layer the existing
// package tests use). InjectAffinityByRunAfterOp is the entry point used by
// the DataLoad/DataMigrate/DataProcess engines.
func TestHarnessRequirePolicyWithExistingOwnAffinity(t *testing.T) {
	testScheme := runtime.NewScheme()
	_ = v1.AddToScheme(testScheme)
	_ = datav1alpha1.AddToScheme(testScheme)

	precedingOp := &datav1alpha1.DataLoad{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "prev-dataload",
			Namespace: "default",
		},
		Status: datav1alpha1.OperationStatus{
			Phase: common.PhaseComplete,
			NodeAffinity: &v1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
					NodeSelectorTerms: []v1.NodeSelectorTerm{
						{
							MatchExpressions: []v1.NodeSelectorRequirement{
								{
									Key:      common.K8sNodeNameLabelKey,
									Operator: v1.NodeSelectorOpIn,
									Values:   []string{"node01"},
								},
							},
						},
					},
				},
			},
		},
	}

	// The current operation pins itself to GPU nodes with two ORed terms.
	currentAffinity := &v1.Affinity{
		NodeAffinity: &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
				NodeSelectorTerms: []v1.NodeSelectorTerm{
					{
						MatchExpressions: []v1.NodeSelectorRequirement{
							{Key: "node.kubernetes.io/instance-type", Operator: v1.NodeSelectorOpIn, Values: []string{"gpu-a"}},
						},
					},
					{
						MatchExpressions: []v1.NodeSelectorRequirement{
							{Key: "node.kubernetes.io/instance-type", Operator: v1.NodeSelectorOpIn, Values: []string{"gpu-b"}},
						},
					},
				},
			},
		},
	}

	c := fake.NewFakeClientWithScheme(testScheme, precedingOp)
	got, err := InjectAffinityByRunAfterOp(c, &datav1alpha1.OperationRef{
		ObjectRef: datav1alpha1.ObjectRef{
			Kind: "DataLoad",
			Name: "prev-dataload",
		},
		AffinityStrategy: datav1alpha1.AffinityStrategy{
			Policy: datav1alpha1.RequireAffinityStrategy,
		},
	}, "default", currentAffinity)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	terms := got.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	if len(terms) != 2 {
		t.Fatalf("expected 2 terms, got %d", len(terms))
	}
	for i, instanceType := range []string{"gpu-a", "gpu-b"} {
		exprs := terms[i].MatchExpressions
		if len(exprs) != 2 {
			t.Fatalf("term %d: expected 2 matchExpressions (instance-type + hostname), got %d — the Require requirement was not injected", i, len(exprs))
		}
		if exprs[0].Key != "node.kubernetes.io/instance-type" || exprs[0].Values[0] != instanceType {
			t.Errorf("term %d: first expression changed: %v", i, exprs[0])
		}
		last := exprs[len(exprs)-1]
		if last.Key != common.K8sNodeNameLabelKey || last.Operator != v1.NodeSelectorOpIn || len(last.Values) != 1 || last.Values[0] != "node01" {
			t.Errorf("term %d: hostname requirement missing or wrong: %v", i, last)
		}
	}
}
