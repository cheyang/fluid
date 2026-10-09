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

// Reviewer verification harness for PR #6199 (branch verify/ownerref-helper-converge-claude).
// Additive only — production code is untouched on this branch.
//
// B1 (contract, unit): datasetControllerOwnerReference delegates to
// transformer.GenerateOwnerReferenceFromObject and is behavior-preserving for every
// well-formed TypeMeta shape (matrix below). The only intended deltas are the malformed
// apiVersion shapes ("data.fluid.io/", "a/b/c", "/") which the legacy implementation
// passed through verbatim and which are now recovered from the scheme. On the pre-PR
// base (1bad02a9^) this test is RED: the delegation is absent, so the malformed-shape
// assertions fail.
//
// B2 (contract, unit): the well-known Dataset fallbacks in datasetControllerOwnerReference
// are unreachable for a scheme-registered type, guarding the "unreachable by design"
// comments in the production code.
//
// B3 (canary, unit): a group-less apiVersion ("v1alpha1", missing the data.fluid.io
// group) still passes through unrecovered on both base and head. This asserts CURRENT
// behavior; it flips red if GenerateOwnerReferenceFromObject ever learns to recover the
// group — invert it (promote to contract) then.

import (
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/utils/transformer"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// legacyDatasetControllerOwnerReference is a verbatim copy of the pre-PR implementation of
// datasetControllerOwnerReference from the base branch (1bad02a9^), used as the
// differential oracle for B1.
func legacyDatasetControllerOwnerReference(dataset *datav1alpha1.Dataset) v1.OwnerReference {
	kind := dataset.GetObjectKind().GroupVersionKind().Kind
	if len(kind) == 0 {
		kind = datav1alpha1.Datasetkind
	}
	apiVersion := dataset.APIVersion
	if len(apiVersion) == 0 {
		apiVersion = datav1alpha1.GroupVersion.String()
	}

	return v1.OwnerReference{
		Kind:       kind,
		APIVersion: apiVersion,
		Name:       dataset.GetName(),
		UID:        dataset.GetUID(),
		Controller: ptr.To(true),
	}
}

var verifyTypeMetaMatrix = []struct {
	name      string
	tm        v1.TypeMeta
	malformed bool
}{
	{"empty TypeMeta", v1.TypeMeta{}, false},
	{"complete TypeMeta", v1.TypeMeta{Kind: "Dataset", APIVersion: "data.fluid.io/v1alpha1"}, false},
	{"kind only", v1.TypeMeta{Kind: "Dataset"}, false},
	{"apiVersion only", v1.TypeMeta{APIVersion: "data.fluid.io/v1alpha1"}, false},
	{"custom version", v1.TypeMeta{Kind: "Dataset", APIVersion: "data.fluid.io/v1beta1"}, false},
	{"group-less apiVersion", v1.TypeMeta{Kind: "Dataset", APIVersion: "v1alpha1"}, false},
	{"group-less apiVersion, no kind", v1.TypeMeta{APIVersion: "v1alpha1"}, false},
	{"foreign kind", v1.TypeMeta{Kind: "NotDataset", APIVersion: "data.fluid.io/v1alpha1"}, false},
	{"foreign kind, no apiVersion", v1.TypeMeta{Kind: "NotDataset"}, false},
	// Malformed apiVersion shapes: the legacy implementation passed them through verbatim,
	// producing an ownerReference whose apiVersion the API server rejects.
	{"malformed apiVersion: group only", v1.TypeMeta{Kind: "Dataset", APIVersion: "data.fluid.io/"}, true},
	{"malformed apiVersion: three segments", v1.TypeMeta{Kind: "Dataset", APIVersion: "a/b/c"}, true},
	{"malformed apiVersion: bare slash", v1.TypeMeta{Kind: "Dataset", APIVersion: "/"}, true},
}

// ownerRefsEqual compares metav1.OwnerReference field-wise: the Controller and
// BlockOwnerDeletion pointers come from separate ptr.To allocations, so == on the struct
// would compare pointer identity and always differ.
func ownerRefsEqual(a, b v1.OwnerReference) bool {
	if a.Kind != b.Kind || a.APIVersion != b.APIVersion || a.Name != b.Name || a.UID != b.UID {
		return false
	}
	if (a.Controller == nil) != (b.Controller == nil) {
		return false
	}
	if a.Controller != nil && *a.Controller != *b.Controller {
		return false
	}
	if (a.BlockOwnerDeletion == nil) != (b.BlockOwnerDeletion == nil) {
		return false
	}
	if a.BlockOwnerDeletion != nil && *a.BlockOwnerDeletion != *b.BlockOwnerDeletion {
		return false
	}
	return true
}

func TestVerifyDelegationEquivalence(t *testing.T) {
	wellFormedAPIVersion := "data.fluid.io/v1alpha1"
	for _, tc := range verifyTypeMetaMatrix {
		dataset := &datav1alpha1.Dataset{
			TypeMeta:   tc.tm,
			ObjectMeta: v1.ObjectMeta{Name: "verify-dataset", Namespace: "default", UID: "verify-uid"},
		}
		got := datasetControllerOwnerReference(dataset)
		want := legacyDatasetControllerOwnerReference(dataset)

		if tc.malformed {
			// The only intended behavior change of the delegation: the malformed apiVersion
			// shapes are recovered from the scheme instead of passed through.
			if got.APIVersion != wellFormedAPIVersion {
				t.Errorf("%s: expected recovered APIVersion %q, got %q", tc.name, wellFormedAPIVersion, got.APIVersion)
			}
			if want.APIVersion != tc.tm.APIVersion {
				t.Errorf("%s: legacy oracle unexpectedly recovered the malformed apiVersion: %q", tc.name, want.APIVersion)
			}
			continue
		}

		if !ownerRefsEqual(got, want) {
			t.Errorf("%s: delegation changed behavior: got %+v, legacy %+v", tc.name, got, want)
		}
	}
}

func TestVerifyWellKnownFallbackUnreachable(t *testing.T) {
	for _, tc := range verifyTypeMetaMatrix {
		dataset := &datav1alpha1.Dataset{
			TypeMeta:   tc.tm,
			ObjectMeta: v1.ObjectMeta{Name: "verify-dataset", Namespace: "default", UID: "verify-uid"},
		}
		ref := transformer.GenerateOwnerReferenceFromObject(dataset)
		if len(ref.Kind) == 0 || len(ref.APIVersion) == 0 {
			t.Errorf("%s: helper returned an incomplete GVK (Kind=%q, APIVersion=%q); the well-known fallback would fire",
				tc.name, ref.Kind, ref.APIVersion)
		}
	}
}

func TestVerifyGrouplessAPIVersionPassesThrough(t *testing.T) {
	dataset := &datav1alpha1.Dataset{
		TypeMeta:   v1.TypeMeta{Kind: "Dataset", APIVersion: "v1alpha1"},
		ObjectMeta: v1.ObjectMeta{Name: "verify-dataset", Namespace: "default", UID: "verify-uid"},
	}
	got := datasetControllerOwnerReference(dataset)
	legacy := legacyDatasetControllerOwnerReference(dataset)

	// CANARY: current behavior passes the group-less apiVersion through. This documents the
	// residual gap (an ownerReference with apiVersion "v1alpha1" cannot be resolved to the
	// data.fluid.io group by an owner-based watch), it does not endorse it.
	if got.APIVersion != "v1alpha1" {
		t.Errorf("canary broken: expected group-less pass-through %q, got %q — if the helper now recovers the group, invert this canary into a contract test", "v1alpha1", got.APIVersion)
	}
	// The delegation must not regress this shape relative to the legacy implementation.
	if got.APIVersion != legacy.APIVersion || got.Kind != legacy.Kind {
		t.Errorf("delegation changed the group-less shape: got %+v, legacy %+v", got, legacy)
	}
}
