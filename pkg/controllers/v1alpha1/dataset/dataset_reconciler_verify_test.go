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
// Plain go test (not part of the Ginkgo suite) so it can be scoped with -run and stays
// independent of the suite's gomonkey patching. Additive only: no production code changes.

package dataset

import (
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// B3 (bug-canary) — while a leftover ThinRuntime is stuck Terminating, step 3 of
// reconcileDataset (pkg/controllers/v1alpha1/dataset/dataset_controller.go:153-159) returns
// RequeueIfError before step 4 (dataset_controller.go:161-174), so a reference dataset in
// NoneDatasetPhase never advances to NotBound.
//
// Polarity is canary on purpose: the correct behaviour is a design decision (fail loudly and
// back off vs. still surface NotBound). This test records the CURRENT behaviour, so it PASSES
// on the code under review and FLIPS TO RED if the ordering is changed. If it flips, invert
// the assertion (or promote NotBound to the contract).
func TestVerifyB3TerminatingRuntimeBlocksNoneToNotBoundPhaseUpdate(t *testing.T) {
	now := metav1.Now()

	ds := datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "b3-none-phase",
			Namespace:  "default",
			UID:        types.UID("b3-none-phase-uid"),
			Finalizers: []string{finalizer},
		},
		Spec: datav1alpha1.DatasetSpec{
			Mounts: []datav1alpha1.Mount{
				{Name: "m1", MountPoint: "dataset://default/physical-ds"},
			},
		},
		// NoneDatasetPhase ("") is the state a freshly created dataset is in, i.e. exactly the
		// state step 4 is supposed to move to NotBound.
		Status: datav1alpha1.DatasetStatus{Phase: datav1alpha1.NoneDatasetPhase},
	}

	// The finalizer keeps the terminating runtime in the fake client's tracker.
	terminatingRuntime := datav1alpha1.ThinRuntime{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "b3-none-phase",
			Namespace:         "default",
			DeletionTimestamp: &now,
			Finalizers:        []string{"thin-runtime-controller-finalizer"},
		},
	}

	r := newTestReconciler(&ds, &terminatingRuntime)
	ctx := makeReconcileCtx(r, ds)

	_, err := r.reconcileDataset(ctx, false)
	if err == nil {
		t.Fatalf("expected reconcileDataset to fail on the terminating runtime, got nil error")
	}

	stored := &datav1alpha1.Dataset{}
	if getErr := r.Get(ctx, types.NamespacedName{Namespace: "default", Name: "b3-none-phase"}, stored); getErr != nil {
		t.Fatalf("failed to get the dataset back: %v", getErr)
	}

	// Canary: the phase is expected to be still NoneDatasetPhase ("") because step 3 returned
	// before step 4 ran.
	if stored.Status.Phase != datav1alpha1.NoneDatasetPhase {
		t.Errorf("B3 canary flipped: expected phase to still be NoneDatasetPhase (%q) because the terminating-runtime error short-circuits the phase update, but observed %q. Invert this canary.",
			datav1alpha1.NoneDatasetPhase, stored.Status.Phase)
	}
	t.Logf("B3 observed: reconcileDataset err=%q, dataset phase=%q (NotBound would be %q)",
		err.Error(), stored.Status.Phase, datav1alpha1.NotBoundDatasetPhase)
}

// B3b (reference, expected green) — without the terminating runtime the same dataset does
// advance from NoneDatasetPhase to NotBound, which shows the canary above isolates the
// terminating-runtime short-circuit and not some unrelated reason the phase never updates.
func TestVerifyB3NoneToNotBoundWithoutTerminatingRuntime(t *testing.T) {
	ds := datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "b3-none-phase-ok",
			Namespace:  "default",
			UID:        types.UID("b3-none-phase-ok-uid"),
			Finalizers: []string{finalizer},
		},
		Spec: datav1alpha1.DatasetSpec{
			Mounts: []datav1alpha1.Mount{
				{Name: "m1", MountPoint: "dataset://default/physical-ds"},
			},
		},
		Status: datav1alpha1.DatasetStatus{Phase: datav1alpha1.NoneDatasetPhase},
	}

	r := newTestReconciler(&ds)
	ctx := makeReconcileCtx(r, ds)

	if _, err := r.reconcileDataset(ctx, false); err != nil {
		t.Fatalf("reconcileDataset() unexpected error = %v", err)
	}

	stored := &datav1alpha1.Dataset{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: "default", Name: "b3-none-phase-ok"}, stored); err != nil {
		t.Fatalf("failed to get the dataset back: %v", err)
	}
	if stored.Status.Phase != datav1alpha1.NotBoundDatasetPhase {
		t.Errorf("expected phase %q, got %q", datav1alpha1.NotBoundDatasetPhase, stored.Status.Phase)
	}
}
