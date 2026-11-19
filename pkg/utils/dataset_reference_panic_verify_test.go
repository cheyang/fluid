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

// Reviewer verification harness for PR #6272 (Reviewer A / Claude).
// POLARITY: contract test. RED on the PR base (dcb7616) where
// RemoveNotFoundDatasetRef dereferences the nil dataset shadowing its
// `dataset` argument on the non-NotFound error path; GREEN on the PR head
// (071ef58d) after the rename to `refDataset`.

import (
	"context"
	"errors"
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	ctrlfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// VerifyRemoveNotFoundDatasetRefGetErrorIsSafe is the contract for claim
// "under a non-NotFound Get error while resolving a datasetRef, the function
// must return the original datasetRef list plus the error, and must not
// panic". It is independent of the PR's own test and also covers
// multi-element ref lists to pin the "refs processed before the error are
// left untouched" semantics.
func TestVerifyRemoveNotFoundDatasetRefGetErrorIsSafe(t *testing.T) {
	testScheme := runtime.NewScheme()
	_ = v1.AddToScheme(testScheme)
	_ = datav1alpha1.AddToScheme(testScheme)

	refList := []string{"fluid/hbase-v2", "fluid/hbase-virtual"}
	dataset := datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: "hbase-ref", Namespace: "fluid"},
		Status:     datav1alpha1.DatasetStatus{DatasetRef: refList},
	}

	// Fail only the dataset Gets with a non-NotFound (internal) error,
	// simulating a transient API server failure.
	simulated := apierrors.NewInternalError(errors.New("simulated API server error"))
	base := ctrlfake.NewClientBuilder().WithScheme(testScheme).Build()
	client := interceptor.NewClient(base, interceptor.Funcs{
		Get: func(ctx context.Context, c ctrlclient.WithWatch, key ctrlclient.ObjectKey, obj ctrlclient.Object, opts ...ctrlclient.GetOption) error {
			if _, ok := obj.(*datav1alpha1.Dataset); ok {
				return simulated
			}
			return nil
		},
	})

	// A panic here (base behavior) fails the test binary, which is the
	// reproduction; a pass means the contract holds.
	gotRef, err := RemoveNotFoundDatasetRef(client, dataset, fake.NullLogger())
	if err == nil {
		t.Fatal("expected the non-NotFound error to be propagated, got nil")
	}
	if gotRef == nil || len(gotRef) != len(refList) {
		t.Fatalf("expected the original datasetRef %v to be returned unchanged, got %v", refList, gotRef)
	}
	for i := range refList {
		if gotRef[i] != refList[i] {
			t.Fatalf("expected datasetRef[%d] == %q, got %q", i, refList[i], gotRef[i])
		}
	}
}

// VerifyRemoveNotFoundDatasetRefNilClientIsSafe covers the other non-NotFound
// error source in GetDataset (nil reader), which hits the same fixed path.
func TestVerifyRemoveNotFoundDatasetRefNilClientIsSafe(t *testing.T) {
	dataset := datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: "hbase-ref", Namespace: "fluid"},
		Status:     datav1alpha1.DatasetStatus{DatasetRef: []string{"fluid/hbase-virtual"}},
	}
	gotRef, err := RemoveNotFoundDatasetRef(nil, dataset, fake.NullLogger())
	if err == nil {
		t.Fatal("expected an error for a nil client, got nil")
	}
	if len(gotRef) != 1 || gotRef[0] != "fluid/hbase-virtual" {
		t.Fatalf("expected the original datasetRef to be returned unchanged, got %v", gotRef)
	}
}
