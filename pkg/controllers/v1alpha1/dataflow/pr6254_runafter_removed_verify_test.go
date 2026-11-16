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

package dataflow

// Reviewer verification harness for PR #6254 (Reviewer A / Claude).
// Additive only: production code is untouched by this branch.
//
// These are CONTRACT tests for the PR's fixed behavior: a data operation that is
// waiting (status.waitingFor.operationComplete=true) whose spec.runAfter has been
// removed must be released (flag set to false, no requeue) instead of panicking on
// the nil runAfter dereference.
//
// Polarity: contract. On the PR head (cc3947c0) these tests PASS; on the merge-base
// (f432bdc8, fix reverted) they FAIL with a nil pointer dereference in
// reconcileOperationDataFlow — that differential is the reproduction of the premise.
// Self-contained on purpose: no dependency on the PR's own test files, so this file
// can be grafted onto any ref (see docs/verification/dataflow-runafter-removed/).

import (
	"context"
	"testing"
	"time"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const pr6254Namespace = "default"

func pr6254WaitingStatus() datav1alpha1.OperationStatus {
	return datav1alpha1.OperationStatus{
		Phase: common.PhasePending,
		WaitingFor: datav1alpha1.WaitingStatus{
			OperationComplete: ptr.To(true),
		},
	}
}

// pr6254RecoverToFailure turns a panic into a test failure with a clear message,
// so a regression shows up as "panicked: ..." instead of crashing the binary.
func pr6254RecoverToFailure(t *testing.T, name string) {
	t.Helper()
	if r := recover(); r != nil {
		t.Fatalf("%s panicked (expected release without panic): %v", name, r)
	}
}

func pr6254NewScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = datav1alpha1.AddToScheme(s)
	return s
}

// TestPR6254RunAfterRemovedReleasesAllKinds is the per-kind contract:
// for DataLoad/DataBackup/DataMigrate/DataProcess waiting with spec.runAfter removed,
// reconcileOperationDataFlow (via each reconcile* entry) must release the operation:
// no panic, no error, no requeue, and waitingFor.operationComplete cleared in the store.
func TestPR6254RunAfterRemovedReleasesAllKinds(t *testing.T) {
	s := pr6254NewScheme()
	name := "pr6254-op"

	cases := []struct {
		kind        string
		obj         client.Object
		reconcileFn func(reconcileRequestContext) (bool, error)
	}{
		{
			kind: "DataLoad",
			obj: &datav1alpha1.DataLoad{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: pr6254Namespace},
				Status:     pr6254WaitingStatus(),
			},
			reconcileFn: reconcileDataLoad,
		},
		{
			kind: "DataBackup",
			obj: &datav1alpha1.DataBackup{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: pr6254Namespace},
				Status:     pr6254WaitingStatus(),
			},
			reconcileFn: reconcileDataBackup,
		},
		{
			kind: "DataMigrate",
			obj: &datav1alpha1.DataMigrate{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: pr6254Namespace},
				Status:     pr6254WaitingStatus(),
			},
			reconcileFn: reconcileDataMigrate,
		},
		{
			kind: "DataProcess",
			obj: &datav1alpha1.DataProcess{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: pr6254Namespace},
				Status:     pr6254WaitingStatus(),
			},
			reconcileFn: reconcileDataProcess,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.kind, func(t *testing.T) {
			ctx := reconcileRequestContext{
				Context:        context.TODO(),
				NamespacedName: types.NamespacedName{Name: name, Namespace: pr6254Namespace},
				Client:         fake.NewFakeClientWithScheme(s, tc.obj),
				Log:            logf.Log.WithName("pr6254-verify"),
				Recorder:       record.NewFakeRecorder(32),
			}

			var needRequeue bool
			var err error
			func() {
				defer pr6254RecoverToFailure(t, "reconcile"+tc.kind)
				needRequeue, err = tc.reconcileFn(ctx)
			}()
			if err != nil {
				t.Fatalf("reconcile%s returned error: %v", tc.kind, err)
			}
			if needRequeue {
				t.Fatalf("reconcile%s requested a requeue after release; expected none", tc.kind)
			}

			// the waiting flag must be persisted as false
			key := client.ObjectKeyFromObject(tc.obj)
			updated := tc.obj.DeepCopyObject().(client.Object)
			if getErr := ctx.Client.Get(context.TODO(), key, updated); getErr != nil {
				t.Fatalf("failed to get updated %s: %v", tc.kind, getErr)
			}
			opStatus, opErr := pr6254OperationStatusOf(updated)
			if opErr != nil {
				t.Fatalf("unexpected object for %s: %v", tc.kind, opErr)
			}
			if opStatus.WaitingFor.OperationComplete == nil || *opStatus.WaitingFor.OperationComplete {
				t.Fatalf("%s still waiting after runAfter removal: waitingFor.operationComplete=%v", tc.kind, opStatus.WaitingFor.OperationComplete)
			}
			if opStatus.Phase != common.PhasePending {
				t.Fatalf("%s phase changed during release: %s (expected Pending)", tc.kind, opStatus.Phase)
			}
		})
	}
}

// TestPR6254ControllerReconcileNoPanicAndReleases is the controller-level contract:
// DataFlowReconciler.Reconcile on a waiting DataLoad whose runAfter was removed must
// not panic, return an empty result with no error, and clear the waiting flag.
// This is the entry point the crash-loop reaches on the merge-base.
func TestPR6254ControllerReconcileNoPanicAndReleases(t *testing.T) {
	s := pr6254NewScheme()
	name := "pr6254-dataload"
	waitingLoad := &datav1alpha1.DataLoad{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: pr6254Namespace},
		Status:     pr6254WaitingStatus(),
	}

	r := NewDataFlowReconciler(
		fake.NewFakeClientWithScheme(s, waitingLoad),
		logf.Log.WithName("pr6254-verify"),
		record.NewFakeRecorder(32),
		30*time.Second,
	)
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: pr6254Namespace}}

	var result ctrl.Result
	var err error
	func() {
		defer pr6254RecoverToFailure(t, "DataFlowReconciler.Reconcile")
		result, err = r.Reconcile(context.TODO(), req)
	}()
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if result != (ctrl.Result{}) {
		t.Fatalf("Reconcile returned %+v; expected empty result", result)
	}

	updated := &datav1alpha1.DataLoad{}
	if getErr := r.Get(context.TODO(), req.NamespacedName, updated); getErr != nil {
		t.Fatalf("failed to get updated DataLoad: %v", getErr)
	}
	if updated.Status.WaitingFor.OperationComplete == nil || *updated.Status.WaitingFor.OperationComplete {
		t.Fatalf("DataLoad still waiting after Reconcile: waitingFor.operationComplete=%v", updated.Status.WaitingFor.OperationComplete)
	}
}

// TestPR6254ReleaseErrorPropagatesAndRequeues is the error-path contract:
// if persisting waitingFor.operationComplete=false fails, the reconciler must
// return the (wrapped) error and request a requeue instead of dropping it.
func TestPR6254ReleaseErrorPropagatesAndRequeues(t *testing.T) {
	s := pr6254NewScheme()
	name := "pr6254-dataload"
	waitingLoad := &datav1alpha1.DataLoad{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: pr6254Namespace},
		Status:     pr6254WaitingStatus(),
	}
	ctx := reconcileRequestContext{
		Context:        context.TODO(),
		NamespacedName: types.NamespacedName{Name: name, Namespace: pr6254Namespace},
		Client:         fake.NewFakeClientWithScheme(s, waitingLoad),
		Log:            logf.Log.WithName("pr6254-verify"),
		Recorder:       record.NewFakeRecorder(32),
	}
	sentinelErr := errPR6254StatusUpdate{}

	var needRequeue bool
	var err error
	func() {
		defer pr6254RecoverToFailure(t, "reconcileOperationDataFlow")
		needRequeue, err = reconcileOperationDataFlow(ctx, waitingLoad, nil, waitingLoad.Status, func() error { return sentinelErr })
	}()
	if err == nil {
		t.Fatal("expected the status-update error to be returned")
	}
	if !containsPR6254Err(err, sentinelErr) {
		t.Fatalf("returned error does not wrap the update failure: %v", err)
	}
	if !needRequeue {
		t.Fatal("expected a requeue to be requested when the release fails")
	}
}

type errPR6254StatusUpdate struct{}

func (errPR6254StatusUpdate) Error() string { return "pr6254: status update failed" }

func containsPR6254Err(err, target error) bool {
	for err != nil {
		if err == target {
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// pr6254OperationStatusOf extracts the OperationStatus from any of the four data
// operation kinds without depending on the PR's own test helpers.
func pr6254OperationStatusOf(obj client.Object) (datav1alpha1.OperationStatus, error) {
	switch o := obj.(type) {
	case *datav1alpha1.DataLoad:
		return o.Status, nil
	case *datav1alpha1.DataBackup:
		return o.Status, nil
	case *datav1alpha1.DataMigrate:
		return o.Status, nil
	case *datav1alpha1.DataProcess:
		return o.Status, nil
	default:
		return datav1alpha1.OperationStatus{}, errPR6254StatusUpdate{}
	}
}
