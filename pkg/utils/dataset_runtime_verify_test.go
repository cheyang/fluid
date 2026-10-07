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

// Verification harness for PR #6199 (converge datasetControllerOwnerReference with the
// transformer helper, issue #6140). Additive only — production code is untouched.
//
// Polarity: all tests below are CONTRACT tests. They assert the intended correct
// behavior (behavioral equivalence with the pre-PR implementation, and a well-formed
// ownerReference persisted through the fake client). They FAIL if the convergence
// regresses the wrapper's observable output, and PASS on the PR head.

import (
	"reflect"
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
)

// legacyDatasetControllerOwnerReference is a byte-for-byte copy of the BASE (pre-PR #6199)
// implementation of datasetControllerOwnerReference. It is the oracle for the differential
// equivalence test: the converged implementation must be observably identical for every
// TypeMeta shape a real client can produce.
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

// TestVerifyOwnerReferenceEquivalenceLegacyVsConverged (B1, contract, L1):
// for every well-formed TypeMeta shape, the converged datasetControllerOwnerReference
// must produce exactly the same ownerReference as the legacy implementation, and the
// result must be well-formed (Kind and APIVersion non-empty, Controller=true).
func TestVerifyOwnerReferenceEquivalenceLegacyVsConverged(t *testing.T) {
	shapes := map[string]v1.TypeMeta{
		"complete typemeta":              {Kind: "Dataset", APIVersion: "data.fluid.io/v1alpha1"},
		"empty typemeta":                 {},
		"kind only":                      {Kind: "Dataset"},
		"apiVersion only":                {APIVersion: "data.fluid.io/v1alpha1"},
		"kind only, wrong kind":          {Kind: "ThinRuntime"},
		"apiVersion only, custom beta":   {APIVersion: "data.fluid.io/v1beta1"},
		"custom version, both set":       {Kind: "Dataset", APIVersion: "data.fluid.io/v1beta1"},
		"foreign group":                  {Kind: "Dataset", APIVersion: "other.io/v1"},
		"foreign group, kind empty":      {APIVersion: "other.io/v1"},
		"version only, no group":         {APIVersion: "v1alpha1"},
		"foreign group, custom kind":     {Kind: "NotADataset", APIVersion: "other.io/v9"},
	}

	for name, tm := range shapes {
		t.Run(name, func(t *testing.T) {
			dataset := &datav1alpha1.Dataset{
				TypeMeta:   tm,
				ObjectMeta: v1.ObjectMeta{Name: "ds-" + "equivalence", Namespace: "default", UID: "uid-equivalence"},
			}
			got := datasetControllerOwnerReference(dataset)
			want := legacyDatasetControllerOwnerReference(dataset)

			if !reflect.DeepEqual(got, want) {
				t.Errorf("converged wrapper diverged from legacy implementation:\n got: %+v\nwant: %+v", got, want)
			}
			if got.Kind == "" || got.APIVersion == "" {
				t.Errorf("ownerReference not well-formed (Kind/APIVersion empty): %+v", got)
			}
			if got.Controller == nil || !*got.Controller {
				t.Errorf("expected Controller=true, got %+v", got.Controller)
			}
			if got.Name != dataset.GetName() || got.UID != dataset.GetUID() {
				t.Errorf("Name/UID not carried over: %+v", got)
			}
		})
	}
}

// TestVerifyMalformedAPIVersionIsNormalized (B1 companion, contract, L1):
// the single observable divergence from the legacy implementation is on a malformed
// apiVersion ("data.fluid.io/" — group set, version empty). The legacy code copied the
// garbage through and produced an API-server-invalid ownerReference; the converged code
// recovers the version from the scheme. This pins the new, strictly better behavior.
func TestVerifyMalformedAPIVersionIsNormalized(t *testing.T) {
	dataset := &datav1alpha1.Dataset{
		TypeMeta:   v1.TypeMeta{Kind: "Dataset", APIVersion: "data.fluid.io/"},
		ObjectMeta: v1.ObjectMeta{Name: "malformed", Namespace: "default", UID: "uid-malformed"},
	}
	got := datasetControllerOwnerReference(dataset)
	if got.Kind != "Dataset" || got.APIVersion != "data.fluid.io/v1alpha1" {
		t.Errorf("expected malformed apiVersion to be normalized to data.fluid.io/v1alpha1, got %+v", got)
	}
}

// TestVerifyCreatedThinRuntimeCarriesWellFormedOwnerReference (B2, contract, L2):
// through the fake client (integration layer), CreateRuntimeForReferenceDatasetIfNotExist
// must persist an ownerReference whose Kind and APIVersion are both populated — the exact
// property the owner-based watch of the dataset controller depends on. The dataset is given
// an empty TypeMeta, the shape a typed client is allowed to hand back.
func TestVerifyCreatedThinRuntimeCarriesWellFormedOwnerReference(t *testing.T) {
	dataset := &datav1alpha1.Dataset{
		ObjectMeta: v1.ObjectMeta{
			Name:      "dataset-l2",
			Namespace: "default",
			UID:       "uid-l2",
		},
	}

	scheme := runtime.NewScheme()
	if err := datav1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to build scheme: %v", err)
	}
	fakeClient := fake.NewFakeClientWithScheme(scheme)

	if err := CreateRuntimeForReferenceDatasetIfNotExist(fakeClient, dataset); err != nil {
		t.Fatalf("CreateRuntimeForReferenceDatasetIfNotExist failed: %v", err)
	}

	created, err := GetThinRuntime(fakeClient, dataset.GetName(), dataset.GetNamespace())
	if err != nil {
		t.Fatalf("failed to read back the created ThinRuntime: %v", err)
	}
	refs := created.GetOwnerReferences()
	if len(refs) != 1 {
		t.Fatalf("expected exactly 1 ownerReference on the created ThinRuntime, got %v", refs)
	}
	ref := refs[0]
	if ref.Kind != "Dataset" || ref.APIVersion != "data.fluid.io/v1alpha1" {
		t.Errorf("persisted ownerReference is not well-formed: %+v", ref)
	}
	if ref.Name != dataset.GetName() || ref.UID != dataset.GetUID() {
		t.Errorf("persisted ownerReference does not point at the dataset: %+v", ref)
	}
	if ref.Controller == nil || !*ref.Controller {
		t.Errorf("expected Controller=true on the persisted ownerReference: %+v", ref)
	}
}
