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

// Verification harness for the review of PR #6199 (Reviewer B / Codex).
// Additive only: no production code is modified by this file.
//
// Claim C1 (contract): datasetControllerOwnerReference must produce a complete,
// scheme-consistent ownerReference for every TypeMeta shape a typed client can
// hand back, including the group-only apiVersion ("data.fluid.io/") whose empty
// version segment must be recovered from the scheme.
// On the base branch (pre-#6199) the group-only case keeps "data.fluid.io/"
// verbatim, so this test FAILS there and PASSES on the PR head.
//
// Claim C2 (contract / convergence invariant): for every TypeMeta shape,
// datasetControllerOwnerReference must agree with the shared helper
// transformer.GenerateOwnerReferenceFromObject on Kind and APIVersion.

package utils

import (
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/utils/transformer"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func verifyPR6199TypeMetaShapes() map[string]metav1.TypeMeta {
	return map[string]metav1.TypeMeta{
		"empty TypeMeta":              {},
		"kind only":                   {Kind: "Dataset"},
		"apiVersion only":             {APIVersion: "data.fluid.io/v1alpha1"},
		"complete TypeMeta":           {Kind: "Dataset", APIVersion: "data.fluid.io/v1alpha1"},
		"group-only apiVersion":       {Kind: "Dataset", APIVersion: "data.fluid.io/"},
		"custom version":              {Kind: "Dataset", APIVersion: "data.fluid.io/v1beta1"},
		"foreign kind, empty version": {Kind: "WrongKind"},
	}
}

func TestVerifyPR6199DatasetControllerOwnerReferenceCompleteGVK(t *testing.T) {
	for name, tm := range verifyPR6199TypeMetaShapes() {
		t.Run(name, func(t *testing.T) {
			dataset := &datav1alpha1.Dataset{
				TypeMeta:   tm,
				ObjectMeta: metav1.ObjectMeta{Name: "verify-dataset", Namespace: "default", UID: "uid-verify"},
			}
			ref := datasetControllerOwnerReference(dataset)
			if ref.Kind == "" {
				t.Errorf("C1: Kind must never be empty, got %q (apiVersion %q)", ref.Kind, ref.APIVersion)
			}
			if ref.APIVersion == "" {
				t.Errorf("C1: APIVersion must never be empty, got %q (kind %q)", ref.APIVersion, ref.Kind)
			}
			// An apiVersion with an empty version segment is just as unresolvable
			// for an owner-based watch as an empty one.
			if gv := ref.APIVersion; gv == "data.fluid.io/" {
				t.Errorf("C1: APIVersion %q has an empty version segment and must have been recovered from the scheme", gv)
			}
		})
	}
}

func TestVerifyPR6199GroupOnlyAPIVersionRecovered(t *testing.T) {
	// This is the exact divergent shape: the pre-#6199 per-field fallback kept any
	// non-empty apiVersion verbatim, so "data.fluid.io/" survived into the ref.
	dataset := &datav1alpha1.Dataset{
		TypeMeta:   metav1.TypeMeta{Kind: "Dataset", APIVersion: "data.fluid.io/"},
		ObjectMeta: metav1.ObjectMeta{Name: "verify-group-only", Namespace: "default", UID: "uid-group-only"},
	}
	ref := datasetControllerOwnerReference(dataset)
	if ref.APIVersion != datav1alpha1.GroupVersion.String() {
		t.Errorf("C1: expected group-only apiVersion to be repaired to %q, got %q",
			datav1alpha1.GroupVersion.String(), ref.APIVersion)
	}
	if ref.Kind != datav1alpha1.Datasetkind {
		t.Errorf("C1: expected kind %q, got %q", datav1alpha1.Datasetkind, ref.Kind)
	}
}

func TestVerifyPR6199ConvergesWithTransformerHelper(t *testing.T) {
	for name, tm := range verifyPR6199TypeMetaShapes() {
		t.Run(name, func(t *testing.T) {
			dataset := &datav1alpha1.Dataset{
				TypeMeta:   tm,
				ObjectMeta: metav1.ObjectMeta{Name: "verify-converge", Namespace: "default", UID: "uid-converge"},
			}
			ref := datasetControllerOwnerReference(dataset)
			shared := transformer.GenerateOwnerReferenceFromObject(dataset)
			if ref.Kind != shared.Kind {
				t.Errorf("C2: kind divergence: datasetControllerOwnerReference=%q, GenerateOwnerReferenceFromObject=%q", ref.Kind, shared.Kind)
			}
			if ref.APIVersion != shared.APIVersion {
				t.Errorf("C2: apiVersion divergence: datasetControllerOwnerReference=%q, GenerateOwnerReferenceFromObject=%q", ref.APIVersion, shared.APIVersion)
			}
		})
	}
}
