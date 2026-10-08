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

// Reviewer verification harness for PR #6199 (owner-reference GVK convergence).
// Additive only: no production file is modified by the harness.
//
// Polarity (see docs/verification/ownerref-delegate/README.md):
//   - TestVerifyOwnerReferenceDelegationEquivalence     CONTRACT (green = delegation preserves
//     the pre-PR behavior of datasetControllerOwnerReference on well-formed shapes)
//   - TestVerifyDelegationMatchesLegacyForMalformedShapes CANARY (green = delegation also
//     reproduces legacy's malformed output for group-less shapes; flips red on the F1 fix)
//   - TestVerifyDatasetOwnerRefGrouplessVersionShape   CANARY   (green today = documents the
//     group-not-recovered gap; flips red once the transformer recovers Group whenever it is
//     empty, and must then be inverted)
//   - TestVerifyThinRuntimeCreationCarriesOwnerReference INTEGRATION (green = the end-to-end
//     path fed by the delegation mints a well-formed ownerReference)

import (
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
)

// legacyDatasetControllerOwnerReference is a verbatim copy of the PRE-PR implementation of
// datasetControllerOwnerReference (as of base e0fc4c189a6e45ee17a5e812abf3fcbecf566f09), kept
// as the golden master the converged implementation must reproduce.
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

// TestVerifyOwnerReferenceDelegationEquivalence is a CONTRACT test: for every TypeMeta shape
// where the pre-PR per-field fallback produced a well-formed reference (typed-client empty,
// complete, kind-only, full-apiVersion-only, stale version), delegating to
// transformer.GenerateOwnerReferenceFromObject must produce exactly the same ownerReference.
// Any divergence is a behavior change smuggled in under a "convergence" refactor. The
// group-less-apiVersion shapes are deliberately NOT here: legacy mangled those too, and they
// are covered by the canary below.
func TestVerifyOwnerReferenceDelegationEquivalence(t *testing.T) {
	cases := map[string]*datav1alpha1.Dataset{
		"empty TypeMeta (typed client)": {
			ObjectMeta: v1.ObjectMeta{Name: "h-eq-empty", Namespace: "default", UID: "h-uid-1"},
		},
		"complete TypeMeta": {
			TypeMeta:   v1.TypeMeta{Kind: "Dataset", APIVersion: "data.fluid.io/v1alpha1"},
			ObjectMeta: v1.ObjectMeta{Name: "h-eq-complete", Namespace: "default", UID: "h-uid-2"},
		},
		"kind only": {
			TypeMeta:   v1.TypeMeta{Kind: "Dataset"},
			ObjectMeta: v1.ObjectMeta{Name: "h-eq-kind", Namespace: "default", UID: "h-uid-3"},
		},
		"full apiVersion only": {
			TypeMeta:   v1.TypeMeta{APIVersion: "data.fluid.io/v1alpha1"},
			ObjectMeta: v1.ObjectMeta{Name: "h-eq-version", Namespace: "default", UID: "h-uid-4"},
		},
		"stale custom version": {
			TypeMeta:   v1.TypeMeta{Kind: "Dataset", APIVersion: "data.fluid.io/v1beta1"},
			ObjectMeta: v1.ObjectMeta{Name: "h-eq-stale", Namespace: "default", UID: "h-uid-7"},
		},
	}

	for name, ds := range cases {
		t.Run(name, func(t *testing.T) {
			got := datasetControllerOwnerReference(ds)
			want := legacyDatasetControllerOwnerReference(ds)
			if got.Kind != want.Kind || got.APIVersion != want.APIVersion ||
				got.Name != want.Name || got.UID != want.UID ||
				(got.Controller == nil) != (want.Controller == nil) ||
				(got.Controller != nil && want.Controller != nil && *got.Controller != *want.Controller) {
				t.Errorf("delegation diverged from legacy for %q:\n got %+v\nwant %+v", name, got, want)
			}
		})
	}
}

// TestVerifyDelegationMatchesLegacyForMalformedShapes is a CANARY companion to the contract
// test above: for the group-less-apiVersion shapes the pre-PR fallback ALSO produced a
// malformed reference, and the delegation reproduces that legacy output byte for byte. It
// PASSES today and flips red once the transformer's group gating is fixed upstream (finding
// F1) — at that point retire it or invert it to assert the improvement.
func TestVerifyDelegationMatchesLegacyForMalformedShapes(t *testing.T) {
	cases := map[string]*datav1alpha1.Dataset{
		"group-less apiVersion only": {
			TypeMeta:   v1.TypeMeta{APIVersion: "v1alpha1"},
			ObjectMeta: v1.ObjectMeta{Name: "h-eq-groupless", Namespace: "default", UID: "h-uid-5"},
		},
		"kind + group-less apiVersion": {
			TypeMeta:   v1.TypeMeta{Kind: "Dataset", APIVersion: "v1alpha1"},
			ObjectMeta: v1.ObjectMeta{Name: "h-eq-kind-groupless", Namespace: "default", UID: "h-uid-6"},
		},
	}

	for name, ds := range cases {
		t.Run(name, func(t *testing.T) {
			got := datasetControllerOwnerReference(ds)
			want := legacyDatasetControllerOwnerReference(ds)
			if got.Kind != want.Kind || got.APIVersion != want.APIVersion {
				t.Errorf("delegation no longer matches legacy on the malformed shape %q:\n got %+v\nwant %+v\n"+
					"(if the transformer group-gating was fixed, this canary has flipped — invert it)",
					name, got, want)
			}
		})
	}
}

// TestVerifyDatasetOwnerRefGrouplessVersionShape is a BUG-CANARY for the transformer's group
// recovery gap inherited by the delegated Dataset path (finding F1 of this review): a partial
// TypeMeta whose apiVersion carries a version but no group (e.g. "v1alpha1") leaves the group
// unrecovered, so the ownerReference is minted with Kind=Dataset but APIVersion="v1alpha1" —
// exactly the malformed shape issue #6140 was filed over. The assertion documents TODAY's
// behavior: it PASSES on the PR head and must be inverted (to expect
// "data.fluid.io/v1alpha1") once the group gating is fixed upstream.
func TestVerifyDatasetOwnerRefGrouplessVersionShape(t *testing.T) {
	ds := &datav1alpha1.Dataset{
		TypeMeta:   v1.TypeMeta{APIVersion: "v1alpha1"},
		ObjectMeta: v1.ObjectMeta{Name: "h-groupless", Namespace: "default", UID: "h-uid-8"},
	}
	got := datasetControllerOwnerReference(ds)
	if got.APIVersion != "v1alpha1" {
		t.Errorf("canary drifted: expected the (malformed) pass-through APIVersion %q, got %q — "+
			"the transformer group-gating looks fixed, invert this canary into a contract test",
			"v1alpha1", got.APIVersion)
	}
	if got.Kind != "Dataset" {
		t.Errorf("canary drifted: expected Kind %q, got %q", "Dataset", got.Kind)
	}
}

// TestVerifyThinRuntimeCreationCarriesOwnerReference is the INTEGRATION-layer guard: it drives
// the real CreateRuntimeForReferenceDatasetIfNotExist path against a fake client and asserts
// the ThinRuntime created for a reference dataset carries a well-formed controller
// ownerReference (the exact contract the delegation must keep working end to end).
func TestVerifyThinRuntimeCreationCarriesOwnerReference(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := datav1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to build scheme: %v", err)
	}
	client := fake.NewFakeClientWithScheme(scheme)

	// Typed clients hand back Datasets with an empty TypeMeta, which is precisely the shape
	// the GVK recovery exists for.
	dataset := &datav1alpha1.Dataset{
		ObjectMeta: v1.ObjectMeta{Name: "h-integration", Namespace: "default", UID: "h-uid-9"},
	}
	if err := CreateRuntimeForReferenceDatasetIfNotExist(client, dataset); err != nil {
		t.Fatalf("CreateRuntimeForReferenceDatasetIfNotExist failed: %v", err)
	}

	createdRuntime, err := GetThinRuntime(client, "h-integration", "default")
	if err != nil {
		t.Fatalf("failed to fetch created ThinRuntime: %v", err)
	}
	refs := createdRuntime.GetOwnerReferences()
	if len(refs) != 1 {
		t.Fatalf("expected exactly 1 ownerReference, got %d", len(refs))
	}
	ref := refs[0]
	if ref.Kind != datav1alpha1.Datasetkind || ref.APIVersion != datav1alpha1.GroupVersion.String() {
		t.Errorf("ownerReference not well-formed: got Kind=%q APIVersion=%q, want Kind=%q APIVersion=%q",
			ref.Kind, ref.APIVersion, datav1alpha1.Datasetkind, datav1alpha1.GroupVersion.String())
	}
	if ref.UID != "h-uid-9" || ref.Name != "h-integration" {
		t.Errorf("ownerReference identity wrong: got Name=%q UID=%q", ref.Name, ref.UID)
	}
	if ref.Controller == nil || !*ref.Controller {
		t.Errorf("expected Controller=true, got %v", ref.Controller)
	}

	// Idempotence: re-running the adoption branch must not corrupt the reference.
	if err := CreateRuntimeForReferenceDatasetIfNotExist(client, dataset); err != nil {
		t.Fatalf("second CreateRuntimeForReferenceDatasetIfNotExist failed: %v", err)
	}
	rereadRuntime, err := GetThinRuntime(client, "h-integration", "default")
	if err != nil {
		t.Fatalf("failed to re-fetch ThinRuntime: %v", err)
	}
	if len(rereadRuntime.GetOwnerReferences()) != 1 || rereadRuntime.GetOwnerReferences()[0].APIVersion != datav1alpha1.GroupVersion.String() {
		t.Errorf("ownerReference corrupted after second pass: %+v", rereadRuntime.GetOwnerReferences())
	}
}
