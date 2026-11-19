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

// Reviewer verification harness for PR #6272 (additive; production code untouched).
// Polarity: CONTRACT tests. On the pre-fix code (base dcb76167) they panic with a
// nil pointer dereference; on the fixed code they pass.

package utils

import (
	"context"
	"fmt"
	"reflect"
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

// TestVerifyRemoveNotFoundDatasetRefErrorMidLoop covers the case the PR's own test
// does not: the Get failure happens on a LATER entry of Status.DatasetRef, after an
// earlier reference already resolved successfully. The contract being pinned is
// "on a non-NotFound lookup error, return the FULL original datasetRef list
// untouched together with the error" (the caller requeues and ignores the slice,
// so refs must not be half-pruned by the returned value).
//
// Pre-fix behavior: dataset, err := GetDataset(...) shadows the dataset argument;
// the error return dereferences the nil *Dataset and panics.
func TestVerifyRemoveNotFoundDatasetRefErrorMidLoop(t *testing.T) {
	testScheme := runtime.NewScheme()
	_ = v1.AddToScheme(testScheme)
	_ = datav1alpha1.AddToScheme(testScheme)

	healthyRef := &datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "healthy-ref",
			Namespace: "fluid",
		},
	}

	refList := []string{"fluid/healthy-ref", "fluid/flaky-ref"}
	dataset := datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "referenced",
			Namespace: "fluid",
		},
		Status: datav1alpha1.DatasetStatus{
			DatasetRef: refList,
		},
	}

	fakeBase := ctrlfake.NewClientBuilder().WithScheme(testScheme).WithObjects(healthyRef).Build()
	// Fail only the lookup of the second reference dataset; delegate everything
	// else so the first reference resolves normally.
	client := interceptor.NewClient(fakeBase, interceptor.Funcs{
		Get: func(ctx context.Context, c ctrlclient.WithWatch, key ctrlclient.ObjectKey, obj ctrlclient.Object, opts ...ctrlclient.GetOption) error {
			if _, isDataset := obj.(*datav1alpha1.Dataset); isDataset && key.Name == "flaky-ref" {
				return apierrors.NewInternalError(fmt.Errorf("simulated transient API server error"))
			}
			return c.Get(ctx, key, obj, opts...)
		},
	})

	datasetRef, err := RemoveNotFoundDatasetRef(client, dataset, fake.NullLogger())
	if err == nil {
		t.Fatal("expect the non-NotFound error to be propagated, but got nil")
	}
	if !reflect.DeepEqual(datasetRef, refList) {
		t.Errorf("expect the original datasetRef %v to be returned unchanged, but got %v", refList, datasetRef)
	}
}
