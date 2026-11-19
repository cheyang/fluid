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

package dataset

// Reviewer verification harness for PR #6272 (Reviewer A / Claude).
// POLARITY: contract test (integration layer, controller-runtime fake client
// with interceptors). RED on the PR base (dcb7616): reconcileDatasetDeletion
// panics when resolving a referenced dataset fails with a transient API
// error, crashing the controller. GREEN on the PR head (071ef58d): the
// reconcile requeues and the dataset's datasetRef/finalizer are left
// untouched so the deletion is retried later.

import (
	"context"
	"errors"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

var _ = Describe("reconcileDatasetDeletion with a transient reference-dataset lookup error", func() {
	It("requeues without panicking and keeps datasetRef and finalizer", func() {
		now := metav1.Now()

		// The referenced dataset exists in the store; the interceptor below
		// still forces its Get to fail, simulating an API server blip.
		ref := datav1alpha1.Dataset{
			ObjectMeta: metav1.ObjectMeta{Name: "ref-dataset", Namespace: "default"},
		}
		ds := datav1alpha1.Dataset{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "del-transient-err",
				Namespace:         "default",
				Finalizers:        []string{finalizer},
				DeletionTimestamp: &now,
			},
			Status: datav1alpha1.DatasetStatus{
				DatasetRef: []string{"default/ref-dataset"},
			},
		}

		// Fail only the referenced dataset's Get with a non-NotFound error;
		// every other Get delegates to the wrapped fake client so
		// ShouldDeleteDataset and the post-reconcile assertions observe real
		// state (an interceptor Get that returns nil without delegating
		// leaves the object unpopulated).
		r := newTestReconcilerWithInterceptor(interceptor.Funcs{
			Get: func(ctx context.Context, c ctrlclient.WithWatch, key ctrlclient.ObjectKey, obj ctrlclient.Object, opts ...ctrlclient.GetOption) error {
				if _, ok := obj.(*datav1alpha1.Dataset); ok && key.Name == ref.Name && key.Namespace == ref.Namespace {
					return apierrors.NewInternalError(errors.New("simulated API server error"))
				}
				return c.Get(ctx, key, obj, opts...)
			},
		}, &ref, &ds)
		ctx := makeReconcileCtx(r, ds)

		// Seed the status subresource the way a real cluster would hold it
		// (the fake client ignores status at seed time, like the API server
		// ignores status on create).
		seed := ds.DeepCopy()
		seed.Status.DatasetRef = []string{"default/ref-dataset"}
		Expect(r.Status().Update(ctx.Context, seed)).To(Succeed())

		// On the base commit this call panics (nil pointer dereference in
		// utils.RemoveNotFoundDatasetRef), failing the suite.
		result, err := r.reconcileDatasetDeletion(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter > 0 || result.Requeue).To(BeTrue(),
			"deletion must be retried after a transient lookup error")

		// The dataset must not be mutated: refs pruned only on confirmed
		// NotFound, finalizer removed only when refs are empty.
		var after datav1alpha1.Dataset
		Expect(r.Get(ctx.Context, ctrlclient.ObjectKeyFromObject(&ds), &after)).To(Succeed())
		Expect(after.Status.DatasetRef).To(Equal([]string{"default/ref-dataset"}))
		Expect(after.Finalizers).To(ContainElement(finalizer))
	})
})
