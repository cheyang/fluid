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

// Reviewer verification harness for PR #6258
// (https://github.com/fluid-cloudnative/fluid/pull/6258).
//
// Additive only: production code is untouched. See
// docs/verification/dataflow-affinity-sort/ for the manifest, results and
// re-verify script.
//
// Polarity:
//   - TestVerifyF1_MatchExpressionsSortedAndStable  (contract: PASS when the
//     fix is present; FAIL on unpatched base code, which is the reproduction).
//   - TestVerifyF2_NonPrefixAndEdgePathsUnchanged   (contract: must PASS on
//     both base and head; guards against collateral behavior change).
//   - TestVerifyP0_BaseMapIterationNondeterministic (bug-canary for the
//     premise; runs ONLY when FLUID_VERIFY_PREMISE=1, against the BASE branch:
//     PASS there means the nondeterminism symptom is real. On the patched head
//     it flips to FAIL — that flip is the fix being confirmed by F1 instead.)

import (
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/fluid-cloudnative/fluid/pkg/common"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const verifyAffinityPrefix = common.AnnotationDataFlowCustomizedAffinityPrefix

func verifyAffinityJob(annotations map[string]string) *batchv1.Job {
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "verify-affinity-job",
			Namespace:   "default",
			Annotations: annotations,
		},
	}
}

func verifyExpectedAffinity(keysValues [][2]string) *v1.NodeAffinity {
	terms := v1.NodeSelectorTerm{MatchExpressions: []v1.NodeSelectorRequirement{}}
	for _, kv := range keysValues {
		terms.MatchExpressions = append(terms.MatchExpressions,
			v1.NodeSelectorRequirement{
				Key:      kv[0],
				Operator: v1.NodeSelectorOpIn,
				Values:   []string{kv[1]},
			})
	}
	return &v1.NodeAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
			NodeSelectorTerms: []v1.NodeSelectorTerm{terms},
		},
	}
}

// F1 (contract): with several customized-affinity annotations present,
// GenerateNodeAffinity must emit MatchExpressions in lexicographic order of
// the (full) annotation keys — equivalently of the trimmed expression keys —
// and the whole returned affinity must be identical across repeated calls.
//
// The key set deliberately includes pairs whose full-key order differs from
// any naive human ordering (e.g. "k8s.rack" sorts BEFORE
// "kubernetes.io/hostname" because '8' < 'e').
//
// On the merge-base (unpatched) code this test FAILS: `for key, value := range
// annotations` iterates the map in randomized order.
func TestVerifyF1_MatchExpressionsSortedAndStable(t *testing.T) {
	annotations := map[string]string{
		common.AnnotationDataFlowAffinityInject:              "true",
		verifyAffinityPrefix + "z-label":                     "z-value",
		verifyAffinityPrefix + "a-label":                     "a-value",
		verifyAffinityPrefix + "k8s.rack":                    "rack-value",
		verifyAffinityPrefix + "kubernetes.io/hostname":      "host-value",
		verifyAffinityPrefix + "m-label":                     "m-value",
		verifyAffinityPrefix + "topology.kubernetes.io/zone": "zone-value",
	}
	want := verifyExpectedAffinity([][2]string{
		{"a-label", "a-value"},
		{"k8s.rack", "rack-value"},
		{"kubernetes.io/hostname", "host-value"},
		{"m-label", "m-value"},
		{"topology.kubernetes.io/zone", "zone-value"},
		{"z-label", "z-value"},
	})

	job := verifyAffinityJob(annotations)
	for i := 0; i < 50; i++ {
		got, err := GenerateNodeAffinity(job)
		if err != nil {
			t.Fatalf("iteration %d: GenerateNodeAffinity() unexpected error: %v", i, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("iteration %d: GenerateNodeAffinity() ordering/stability mismatch.\ngot:  %s\nwant: %s",
				i, verifyFormatAffinity(got), verifyFormatAffinity(want))
		}
	}
}

// F2 (contract, behavior-preservation): the sort must not change any of the
// surrounding behavior — gate on the inject annotation, key trimming, error
// when no customized label is set, and — a subtle boundary — annotations from
// the same annotation family that use '/' instead of '.' after the common
// "affinity.dataflow.fluid.io" stem (e.g. the inject key and the
// ".../labels" key) must NOT be collected as affinity expressions.
func TestVerifyF2_NonPrefixAndEdgePathsUnchanged(t *testing.T) {
	t.Run("nil job returns nil nil", func(t *testing.T) {
		got, err := GenerateNodeAffinity(nil)
		if got != nil || err != nil {
			t.Errorf("GenerateNodeAffinity(nil) = (%v, %v), want (nil, nil)", got, err)
		}
	})

	t.Run("inject annotation absent returns nil nil", func(t *testing.T) {
		got, err := GenerateNodeAffinity(verifyAffinityJob(map[string]string{
			verifyAffinityPrefix + "a-label": "a-value",
		}))
		if got != nil || err != nil {
			t.Errorf("got (%v, %v), want (nil, nil)", got, err)
		}
	})

	t.Run("inject annotation not true returns nil nil", func(t *testing.T) {
		got, err := GenerateNodeAffinity(verifyAffinityJob(map[string]string{
			common.AnnotationDataFlowAffinityInject: "false",
			verifyAffinityPrefix + "a-label":        "a-value",
		}))
		if got != nil || err != nil {
			t.Errorf("got (%v, %v), want (nil, nil)", got, err)
		}
	})

	t.Run("slash-form sibling annotations are not collected", func(t *testing.T) {
		// Only keys with the dotted prefix count. The inject key and the
		// labels key use "affinity.dataflow.fluid.io/" and must be skipped,
		// leaving no affinity label -> the documented error path.
		got, err := GenerateNodeAffinity(verifyAffinityJob(map[string]string{
			common.AnnotationDataFlowAffinityInject:            "true",
			common.AnnotationDataFlowAffinityLabelsName:        "z-label",
			verifyAffinityPrefix[:len(verifyAffinityPrefix)-1]: "bare-stem-no-trailing-dot", // "affinity.dataflow.fluid.io"
		}))
		if got != nil {
			t.Errorf("got affinity %v, want nil", got)
		}
		if err == nil {
			t.Errorf("got nil error, want the 'affinity label is not set' error")
		}
	})

	t.Run("single customized label", func(t *testing.T) {
		got, err := GenerateNodeAffinity(verifyAffinityJob(map[string]string{
			common.AnnotationDataFlowAffinityInject: "true",
			verifyAffinityPrefix + "only-label":     "only-value",
		}))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := verifyExpectedAffinity([][2]string{{"only-label", "only-value"}})
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("empty value annotation still emitted", func(t *testing.T) {
		got, err := GenerateNodeAffinity(verifyAffinityJob(map[string]string{
			common.AnnotationDataFlowAffinityInject: "true",
			verifyAffinityPrefix + "empty-label":    "",
		}))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := verifyExpectedAffinity([][2]string{{"empty-label", ""}})
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
}

// P0 (bug-canary, premise check — run against the BASE branch with
// FLUID_VERIFY_PREMISE=1): the merge-base code iterates the annotation map in
// randomized order, so multiple customized annotations produce varying
// MatchExpressions orderings across calls. PASS on base = symptom confirmed.
// On the patched head this canary FAILS (only one ordering is ever produced),
// which is the expected flip; TestVerifyF1 is the contract that replaces it.
func TestVerifyP0_BaseMapIterationNondeterministic(t *testing.T) {
	if os.Getenv("FLUID_VERIFY_PREMISE") != "1" {
		t.Skip("premise canary: set FLUID_VERIFY_PREMISE=1 to run against base code")
	}

	annotations := map[string]string{
		common.AnnotationDataFlowAffinityInject: "true",
		verifyAffinityPrefix + "z-label":        "z-value",
		verifyAffinityPrefix + "a-label":        "a-value",
		verifyAffinityPrefix + "m-label":        "m-value",
		verifyAffinityPrefix + "k8s.rack":       "rack-value",
	}
	job := verifyAffinityJob(annotations)

	seen := map[string]int{}
	for i := 0; i < 200; i++ {
		got, err := GenerateNodeAffinity(job)
		if err != nil {
			t.Fatalf("iteration %d: unexpected error: %v", i, err)
		}
		seen[verifyFormatAffinity(got)]++
	}

	t.Logf("distinct MatchExpressions orderings observed in 200 calls: %d", len(seen))
	if len(seen) < 2 {
		t.Errorf("expected randomized map iteration to yield >= 2 distinct orderings on base code, got %d — premise NOT reproduced", len(seen))
	}
}

func verifyFormatAffinity(a *v1.NodeAffinity) string {
	if a == nil || a.RequiredDuringSchedulingIgnoredDuringExecution == nil ||
		len(a.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms) == 0 {
		return "<nil>"
	}
	out := ""
	for _, e := range a.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions {
		out += fmt.Sprintf("{%s In %v} ", e.Key, e.Values)
	}
	return out
}
