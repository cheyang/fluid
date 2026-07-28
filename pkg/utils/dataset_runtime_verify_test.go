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

// Verification harness for the review of
// https://github.com/fluid-cloudnative/fluid/pull/6138
//
// Additive only: no production code is modified by this file. Each test states its
// polarity (contract = asserts the intended behaviour, so it FAILS on the code under
// review and PASSES once fixed).

package utils

import (
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func newVerifyFakeClient(objs ...runtime.Object) client.Client {
	s := runtime.NewScheme()
	_ = datav1alpha1.AddToScheme(s)
	return fake.NewFakeClientWithScheme(s, objs...)
}

// B1 (contract) — the adopt branch of CreateRuntimeForReferenceDatasetIfNotExist must not
// wipe ownerReferences which do not belong to the dataset controller.
//
// pkg/utils/dataset_runtime.go:85-86 replaces the whole slice via SetOwnerReferences, so a
// pre-existing foreign owner (or a stale Dataset owner carrying a superseded UID) is
// silently dropped. Expected: the dataset owner is added/refreshed, the foreign owner
// survives. Suspected: the foreign owner is gone.
func TestVerifyB1AdoptPreservesForeignOwnerReferences(t *testing.T) {
	foreignOwner := metav1.OwnerReference{
		Kind:       "SomeOtherKind",
		APIVersion: "example.com/v1",
		Name:       "some-other-owner",
		UID:        "11111111-1111-1111-1111-111111111111",
	}

	existing := &datav1alpha1.ThinRuntime{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "b1-foreign-owner",
			Namespace:       "default",
			OwnerReferences: []metav1.OwnerReference{foreignOwner},
		},
	}

	c := newVerifyFakeClient(existing.DeepCopy())

	dataset := &datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "b1-foreign-owner",
			Namespace: "default",
			UID:       "22222222-2222-2222-2222-222222222222",
		},
	}

	if err := CreateRuntimeForReferenceDatasetIfNotExist(c, dataset); err != nil {
		t.Fatalf("CreateRuntimeForReferenceDatasetIfNotExist() unexpected error = %v", err)
	}

	got, err := GetThinRuntime(c, "b1-foreign-owner", "default")
	if err != nil {
		t.Fatalf("failed to get the thinRuntime after adoption: %v", err)
	}

	var sawForeign, sawDataset bool
	for _, ref := range got.GetOwnerReferences() {
		if ref.Kind == foreignOwner.Kind && ref.UID == foreignOwner.UID {
			sawForeign = true
		}
		if ref.Kind == datav1alpha1.Datasetkind && ref.UID == dataset.GetUID() {
			sawDataset = true
		}
	}

	if !sawDataset {
		t.Errorf("expected the dataset controller ownerReference (kind=%s uid=%s) to be present, got %v",
			datav1alpha1.Datasetkind, dataset.GetUID(), got.GetOwnerReferences())
	}
	if !sawForeign {
		t.Errorf("B1 reproduced: the pre-existing non-Dataset ownerReference %s/%s (uid=%s) was wiped by the adopt path; ownerReferences are now %v",
			foreignOwner.APIVersion, foreignOwner.Kind, foreignOwner.UID, got.GetOwnerReferences())
	}
}

// B1b (contract) — a stale Dataset ownerReference carrying an old UID (delete + recreate of
// the same dataset name) must not leave the runtime with two controller owners, and the
// fresh UID must win. This documents the concrete flavour of B1 that matters in practice.
func TestVerifyB1StaleDatasetOwnerReplacedByFreshUID(t *testing.T) {
	staleOwner := metav1.OwnerReference{
		Kind:       datav1alpha1.Datasetkind,
		APIVersion: datav1alpha1.GroupVersion.String(),
		Name:       "b1-stale-owner",
		UID:        "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		Controller: ptr.To(true),
	}

	existing := &datav1alpha1.ThinRuntime{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "b1-stale-owner",
			Namespace:       "default",
			OwnerReferences: []metav1.OwnerReference{staleOwner},
		},
	}

	c := newVerifyFakeClient(existing.DeepCopy())

	dataset := &datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "b1-stale-owner",
			Namespace: "default",
			UID:       "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
		},
	}

	if err := CreateRuntimeForReferenceDatasetIfNotExist(c, dataset); err != nil {
		t.Fatalf("CreateRuntimeForReferenceDatasetIfNotExist() unexpected error = %v", err)
	}

	got, err := GetThinRuntime(c, "b1-stale-owner", "default")
	if err != nil {
		t.Fatalf("failed to get the thinRuntime after adoption: %v", err)
	}

	refs := got.GetOwnerReferences()
	var controllers int
	var freshFound bool
	for _, ref := range refs {
		if ref.Controller != nil && *ref.Controller {
			controllers++
		}
		if ref.UID == dataset.GetUID() {
			freshFound = true
		}
	}
	if !freshFound {
		t.Errorf("expected the fresh dataset UID %s in ownerReferences, got %v", dataset.GetUID(), refs)
	}
	if controllers > 1 {
		t.Errorf("expected at most one controller ownerReference, got %d in %v", controllers, refs)
	}
}

// B2 (contract) — the adopt branch must backfill the common.LabelAnnotationDatasetId label.
//
// pkg/utils/dataset_runtime.go:103-105 sets the label only in the create branch. A runtime
// which predates the label, or which is reached through the existing-runtime/adopt branch
// (84-91), never gets it, yet the label is read by pkg/utils/label.go,
// pkg/ddc/thin/referencedataset/volume.go and pkg/utils/kubeclient/configmap.go.
// Expected: the label equals GetDatasetId(ns, name, uid). Suspected: it is absent.
func TestVerifyB2AdoptBackfillsDatasetIdLabel(t *testing.T) {
	existing := &datav1alpha1.ThinRuntime{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "b2-no-label",
			Namespace: "default",
			// No labels at all: a runtime created before the datasetId label existed.
		},
	}

	c := newVerifyFakeClient(existing.DeepCopy())

	dataset := &datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "b2-no-label",
			Namespace: "default",
			UID:       "33333333-3333-3333-3333-333333333333",
		},
	}

	if err := CreateRuntimeForReferenceDatasetIfNotExist(c, dataset); err != nil {
		t.Fatalf("CreateRuntimeForReferenceDatasetIfNotExist() unexpected error = %v", err)
	}

	got, err := GetThinRuntime(c, "b2-no-label", "default")
	if err != nil {
		t.Fatalf("failed to get the thinRuntime after adoption: %v", err)
	}

	want := GetDatasetId(dataset.GetNamespace(), dataset.GetName(), string(dataset.GetUID()))
	gotLabel, ok := got.GetLabels()[common.LabelAnnotationDatasetId]
	if !ok {
		t.Errorf("B2 reproduced: the adopt path did not backfill the %s label; labels are %v (want %q)",
			common.LabelAnnotationDatasetId, got.GetLabels(), want)
		return
	}
	if gotLabel != want {
		t.Errorf("B2 reproduced (wrong value): label %s = %q, want %q",
			common.LabelAnnotationDatasetId, gotLabel, want)
	}
}

// B2b (reference, expected green on the code under review) — the create branch does set the
// label. Kept so a regression in the create path is distinguishable from the B2 gap.
func TestVerifyB2CreateSetsDatasetIdLabel(t *testing.T) {
	c := newVerifyFakeClient()

	dataset := &datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "b2-created",
			Namespace: "default",
			UID:       "44444444-4444-4444-4444-444444444444",
		},
	}

	if err := CreateRuntimeForReferenceDatasetIfNotExist(c, dataset); err != nil {
		t.Fatalf("CreateRuntimeForReferenceDatasetIfNotExist() unexpected error = %v", err)
	}

	got, err := GetThinRuntime(c, "b2-created", "default")
	if err != nil {
		t.Fatalf("failed to get the created thinRuntime: %v", err)
	}

	want := GetDatasetId(dataset.GetNamespace(), dataset.GetName(), string(dataset.GetUID()))
	if gotLabel := got.GetLabels()[common.LabelAnnotationDatasetId]; gotLabel != want {
		t.Errorf("create path: label %s = %q, want %q", common.LabelAnnotationDatasetId, gotLabel, want)
	}
}
