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
// Caller-chain layer: exercises the enclosing controller transaction
// (DatasetReconciler.reconcileDatasetDeletion -> utils.RemoveNotFoundDatasetRef)
// with a client whose Get fails with a non-NotFound error for the referenced
// dataset. Polarity: CONTRACT. On pre-fix code (base dcb76167) the spec dies with
// a nil pointer dereference panic (the same panic that crashes the dataset
// controller process, which does not enable RecoverPanic); on fixed code it passes.

package dataset

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
		. "github.com/onsi/gomega"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

var _ = Describe("Verify PR6272 reconcileDatasetDeletion with failing reference Get", func() {
	It("should requeue without panic and leave datasetRef and finalizer untouched on a transient Get error", func() {
		scheme := runtime.NewScheme()
		Expect(datav1alpha1.AddToScheme(scheme)).NotTo(HaveOccurred())
		Expect(corev1.AddToScheme(scheme)).To(Succeed())

		deletionTime := metav1.NewTime(time.Now())
		storedDataset := &datav1alpha1.Dataset{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "verify-referenced-dataset",
				Namespace:         "default",
				Finalizers:        []string{finalizer},
				DeletionTimestamp: &deletionTime,
			},
			Status: datav1alpha1.DatasetStatus{
				DatasetRef: []string{"default/flaky-consumer"},
			},
		}

		// Build the fake client directly (fluid's fake.NewFakeClientWithScheme hides
		// the client.WithWatch that interceptor.NewClient requires); register the
		// status subresource the same way fluid's wrapper does.
		baseClient := fake.NewClientBuilder().WithScheme(scheme).
			WithRuntimeObjects(storedDataset).WithStatusSubresource(storedDataset).Build()
		// Simulate a transient API server failure only for the reference dataset
		// lookup; every other Get (PVC check, etc.) delegates to the fake client.
		wrapped := interceptor.NewClient(baseClient, interceptor.Funcs{
			Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, isDataset := obj.(*datav1alpha1.Dataset); isDataset && key.Name == "flaky-consumer" {
					return apierrors.NewInternalError(fmt.Errorf("simulated transient API server error"))
				}
				return c.Get(ctx, key, obj, opts...)
			},
		})

		r := &DatasetReconciler{
			Client:   wrapped,
			Scheme:   scheme,
			Recorder: record.NewFakeRecorder(16),
		}

		currentDataset := datav1alpha1.Dataset{}
		Expect(baseClient.Get(context.Background(),
			types.NamespacedName{Name: storedDataset.Name, Namespace: storedDataset.Namespace},
			&currentDataset)).To(Succeed())

		result, err := r.reconcileDatasetDeletion(reconcileRequestContext{
			Context: context.Background(),
			Dataset: currentDataset,
			NamespacedName: types.NamespacedName{
				Name:      currentDataset.Name,
				Namespace: currentDataset.Namespace,
			},
		})

		// The caller contract: the error is swallowed into a 10s requeue; the
		// dataset keeps its refs and its finalizer until the next reconcile.
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(10 * time.Second))

		updatedDataset := &datav1alpha1.Dataset{}
		Expect(baseClient.Get(context.Background(),
			types.NamespacedName{Name: storedDataset.Name, Namespace: storedDataset.Namespace},
			updatedDataset)).To(Succeed())
		Expect(updatedDataset.Status.DatasetRef).To(Equal([]string{"default/flaky-consumer"}))
		Expect(updatedDataset.GetFinalizers()).To(ContainElement(finalizer))
	})
})
