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

// Reviewer verification harness for PR #6254 — ROUND 2 (Reviewer A / Claude).
// Additive only: production code is untouched by this branch.
//
// Contracts added for the second revision of the PR (head 50933d0b), which
// addressed the round-1 follow-ups:
//
//   - F1/MT2: the nil-runAfter release is gated on the fetched waiting status,
//     so non-waiting operations (same-name cross-kind collisions included) are
//     left untouched: no status write, no event, no log.
//   - F2: a DataOperationWaitingEnded event is recorded when an operation that
//     was waiting stops waiting, on both release paths (runAfter removed and
//     preceding operation completed); non-waiting operations get no event.
//
// Polarity: contract. On head 50933d0b these tests PASS. On the previous head
// cc3947c0 (gate and event absent) V4/V5/V6/V7 FAIL — that differential is the
// revision-regression proof. On the merge-base f432bdc8 the same fixtures panic
// in reconcileOperationDataFlow (nil runAfter dereference), so they also fail
// there for the original reason.

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// pr6254DrainEvents collects all currently buffered events from a FakeRecorder.
func pr6254DrainEvents(rec *record.FakeRecorder) []string {
	var events []string
	for {
		select {
		case e := <-rec.Events:
			events = append(events, e)
		default:
			return events
		}
	}
}

// TestPR6254NonWaitingUntouched is the round-2 contract for the F1/MT2 fix:
// an operation that is NOT waiting (waitingFor.operationComplete nil or false)
// with no spec.runAfter must be left completely untouched by the dataflow
// reconcile: no error, no requeue, no status write (resourceVersion and status
// unchanged), and no event.
func TestPR6254NonWaitingUntouched(t *testing.T) {
	s := pr6254NewScheme()
	name := "pr6254-nonwaiting"

	cases := []struct {
		desc        string
		obj         client.Object
		reconcileFn func(reconcileRequestContext) (bool, error)
	}{
		{
			desc: "DataLoad never waited (flag nil)",
			obj: &datav1alpha1.DataLoad{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: pr6254Namespace},
				Status:     datav1alpha1.OperationStatus{Phase: common.PhaseComplete},
			},
			reconcileFn: reconcileDataLoad,
		},
		{
			desc: "DataLoad already stopped waiting (flag false)",
			obj: &datav1alpha1.DataLoad{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: pr6254Namespace},
				Status: datav1alpha1.OperationStatus{
					Phase:      common.PhaseExecuting,
					WaitingFor: datav1alpha1.WaitingStatus{OperationComplete: ptr.To(false)},
				},
			},
			reconcileFn: reconcileDataLoad,
		},
		{
			desc: "DataBackup never waited (flag nil)",
			obj: &datav1alpha1.DataBackup{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: pr6254Namespace},
				Status:     datav1alpha1.OperationStatus{Phase: common.PhaseComplete},
			},
			reconcileFn: reconcileDataBackup,
		},
		{
			desc: "DataMigrate already stopped waiting (flag false)",
			obj: &datav1alpha1.DataMigrate{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: pr6254Namespace},
				Status: datav1alpha1.OperationStatus{
					Phase:      common.PhaseExecuting,
					WaitingFor: datav1alpha1.WaitingStatus{OperationComplete: ptr.To(false)},
				},
			},
			reconcileFn: reconcileDataMigrate,
		},
		{
			desc: "DataProcess never waited (flag nil)",
			obj: &datav1alpha1.DataProcess{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: pr6254Namespace},
				Status:     datav1alpha1.OperationStatus{Phase: common.PhaseComplete},
			},
			reconcileFn: reconcileDataProcess,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			rec := record.NewFakeRecorder(32)
			ctx := reconcileRequestContext{
				Context:        context.TODO(),
				NamespacedName: types.NamespacedName{Name: name, Namespace: pr6254Namespace},
				Client:         fake.NewFakeClientWithScheme(s, tc.obj.DeepCopyObject().(client.Object)),
				Log:            logf.Log.WithName("pr6254-verify"),
				Recorder:       rec,
			}

			before := tc.obj.DeepCopyObject().(client.Object)
			if err := ctx.Client.Get(context.TODO(), client.ObjectKeyFromObject(tc.obj), before); err != nil {
				t.Fatalf("failed to read object before reconcile: %v", err)
			}

			var needRequeue bool
			var err error
			func() {
				defer pr6254RecoverToFailure(t, "reconcile")
				needRequeue, err = tc.reconcileFn(ctx)
			}()
			if err != nil {
				t.Fatalf("reconcile returned error for non-waiting operation: %v", err)
			}
			if needRequeue {
				t.Fatal("reconcile requested a requeue for a non-waiting operation; expected none")
			}

			after := tc.obj.DeepCopyObject().(client.Object)
			if err := ctx.Client.Get(context.TODO(), client.ObjectKeyFromObject(tc.obj), after); err != nil {
				t.Fatalf("failed to read object after reconcile: %v", err)
			}
			if after.GetResourceVersion() != before.GetResourceVersion() {
				t.Fatalf("non-waiting operation was written: resourceVersion %s -> %s",
					before.GetResourceVersion(), after.GetResourceVersion())
			}
			beforeStatus, _ := pr6254OperationStatusOf(before)
			afterStatus, _ := pr6254OperationStatusOf(after)
			if !reflect.DeepEqual(afterStatus, beforeStatus) {
				t.Fatalf("non-waiting operation status changed: %+v -> %+v", beforeStatus, afterStatus)
			}
			if events := pr6254DrainEvents(rec); len(events) > 0 {
				t.Fatalf("non-waiting operation got events: %v", events)
			}
		})
	}
}

// TestPR6254WaitingEndedEventRemoval is the round-2 contract for the F2 fix on
// the runAfter-removal path: a WAITING operation whose runAfter was removed is
// released and exactly one DataOperationWaitingEnded event is recorded, whose
// message explains the release.
func TestPR6254WaitingEndedEventRemoval(t *testing.T) {
	s := pr6254NewScheme()
	name := "pr6254-released"

	objs := []struct {
		kind string
		obj  client.Object
		fn   func(reconcileRequestContext) (bool, error)
	}{
		{"DataLoad", &datav1alpha1.DataLoad{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: pr6254Namespace},
			Status:     pr6254WaitingStatus(),
		}, reconcileDataLoad},
		{"DataBackup", &datav1alpha1.DataBackup{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: pr6254Namespace},
			Status:     pr6254WaitingStatus(),
		}, reconcileDataBackup},
		{"DataMigrate", &datav1alpha1.DataMigrate{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: pr6254Namespace},
			Status:     pr6254WaitingStatus(),
		}, reconcileDataMigrate},
		{"DataProcess", &datav1alpha1.DataProcess{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: pr6254Namespace},
			Status:     pr6254WaitingStatus(),
		}, reconcileDataProcess},
	}

	for _, tc := range objs {
		tc := tc
		t.Run(tc.kind, func(t *testing.T) {
			rec := record.NewFakeRecorder(32)
			ctx := reconcileRequestContext{
				Context:        context.TODO(),
				NamespacedName: types.NamespacedName{Name: name, Namespace: pr6254Namespace},
				Client:         fake.NewFakeClientWithScheme(s, tc.obj.DeepCopyObject().(client.Object)),
				Log:            logf.Log.WithName("pr6254-verify"),
				Recorder:       rec,
			}

			var needRequeue bool
			var err error
			func() {
				defer pr6254RecoverToFailure(t, "reconcile")
				needRequeue, err = tc.fn(ctx)
			}()
			if err != nil || needRequeue {
				t.Fatalf("reconcile returned err=%v needRequeue=%v; expected clean release", err, needRequeue)
			}

			events := pr6254DrainEvents(rec)
			if len(events) != 1 {
				t.Fatalf("expected exactly one event for a released waiting %s, got %d: %v", tc.kind, len(events), events)
			}
			if !strings.Contains(events[0], "DataOperationWaitingEnded") {
				t.Fatalf("event is not %s: %s", "DataOperationWaitingEnded", events[0])
			}
			if !strings.Contains(events[0], "spec.runAfter was removed") {
				t.Fatalf("event message does not explain the release: %s", events[0])
			}
		})
	}
}

// TestPR6254WaitingEndedEventCompletion is the round-2 contract for the F2 fix
// on the preceding-operation-completed path: a waiting operation whose
// preceding operation completed is released with one DataOperationWaitingEnded
// event naming the completed preceding operation; an operation that was NOT
// waiting (flag false) on the same path is released without an event.
func TestPR6254WaitingEndedEventCompletion(t *testing.T) {
	s := pr6254NewScheme()
	name := "pr6254-after-complete"

	newFixture := func(operationComplete bool) (*datav1alpha1.DataLoad, *datav1alpha1.DataLoad) {
		preceding := &datav1alpha1.DataLoad{
			ObjectMeta: metav1.ObjectMeta{Name: "pr6254-preceding", Namespace: pr6254Namespace},
			Status:     datav1alpha1.OperationStatus{Phase: common.PhaseComplete},
		}
		load := &datav1alpha1.DataLoad{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: pr6254Namespace},
			Spec: datav1alpha1.DataLoadSpec{
				RunAfter: &datav1alpha1.OperationRef{
					ObjectRef: datav1alpha1.ObjectRef{Kind: "DataLoad", Name: "pr6254-preceding"},
				},
			},
			Status: datav1alpha1.OperationStatus{
				Phase:      common.PhasePending,
				WaitingFor: datav1alpha1.WaitingStatus{OperationComplete: ptr.To(operationComplete)},
			},
		}
		return preceding, load
	}

	t.Run("waiting operation gets the event", func(t *testing.T) {
		preceding, load := newFixture(true)
		rec := record.NewFakeRecorder(32)
		ctx := reconcileRequestContext{
			Context:        context.TODO(),
			NamespacedName: types.NamespacedName{Name: name, Namespace: pr6254Namespace},
			Client:         fake.NewFakeClientWithScheme(s, preceding, load),
			Log:            logf.Log.WithName("pr6254-verify"),
			Recorder:       rec,
		}

		needRequeue, err := reconcileDataLoad(ctx)
		if err != nil || needRequeue {
			t.Fatalf("reconcile returned err=%v needRequeue=%v; expected clean release", err, needRequeue)
		}

		events := pr6254DrainEvents(rec)
		if len(events) != 1 {
			t.Fatalf("expected exactly one event, got %d: %v", len(events), events)
		}
		if !strings.Contains(events[0], "DataOperationWaitingEnded") {
			t.Fatalf("event is not %s: %s", "DataOperationWaitingEnded", events[0])
		}
		if !strings.Contains(events[0], "pr6254-preceding") || !strings.Contains(events[0], "completed") {
			t.Fatalf("event message does not name the completed preceding operation: %s", events[0])
		}
	})

	t.Run("non-waiting operation gets no event", func(t *testing.T) {
		preceding, load := newFixture(false)
		rec := record.NewFakeRecorder(32)
		ctx := reconcileRequestContext{
			Context:        context.TODO(),
			NamespacedName: types.NamespacedName{Name: name, Namespace: pr6254Namespace},
			Client:         fake.NewFakeClientWithScheme(s, preceding, load),
			Log:            logf.Log.WithName("pr6254-verify"),
			Recorder:       rec,
		}

		needRequeue, err := reconcileDataLoad(ctx)
		if err != nil || needRequeue {
			t.Fatalf("reconcile returned err=%v needRequeue=%v; expected clean release", err, needRequeue)
		}
		if events := pr6254DrainEvents(rec); len(events) > 0 {
			t.Fatalf("non-waiting operation got events: %v", events)
		}
	})
}

// TestPR6254SameNameCrossKindUntouched is the controller-level round-2 contract
// (F1/MT2): Reconcile fetches every data operation kind for the request name.
// When a waiting DataLoad shares its name with a non-waiting DataBackup, the
// DataLoad is released with exactly one DataOperationWaitingEnded event and the
// DataBackup is left untouched (resourceVersion and status unchanged).
func TestPR6254SameNameCrossKindUntouched(t *testing.T) {
	s := pr6254NewScheme()
	name := "pr6254-collide"

	preceding := &datav1alpha1.DataLoad{
		ObjectMeta: metav1.ObjectMeta{Name: "pr6254-preceding", Namespace: pr6254Namespace},
		Status:     datav1alpha1.OperationStatus{Phase: common.PhaseComplete},
	}
	waitingLoad := &datav1alpha1.DataLoad{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: pr6254Namespace},
		Spec: datav1alpha1.DataLoadSpec{
			RunAfter: &datav1alpha1.OperationRef{
				ObjectRef: datav1alpha1.ObjectRef{Kind: "DataLoad", Name: "pr6254-preceding"},
			},
		},
		Status: pr6254WaitingStatus(),
	}
	otherKindBackup := &datav1alpha1.DataBackup{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: pr6254Namespace},
		Status:     datav1alpha1.OperationStatus{Phase: common.PhaseComplete},
	}

	r := NewDataFlowReconciler(
		fake.NewFakeClientWithScheme(s, preceding, waitingLoad, otherKindBackup),
		logf.Log.WithName("pr6254-verify"),
		record.NewFakeRecorder(32),
		30*time.Second,
	)
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: pr6254Namespace}}

	backupBefore := &datav1alpha1.DataBackup{}
	if err := r.Get(context.TODO(), req.NamespacedName, backupBefore); err != nil {
		t.Fatalf("failed to read DataBackup before reconcile: %v", err)
	}

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

	// The waiting DataLoad is released.
	updatedLoad := &datav1alpha1.DataLoad{}
	if err := r.Get(context.TODO(), req.NamespacedName, updatedLoad); err != nil {
		t.Fatalf("failed to read DataLoad after reconcile: %v", err)
	}
	if updatedLoad.Status.WaitingFor.OperationComplete == nil || *updatedLoad.Status.WaitingFor.OperationComplete {
		t.Fatalf("DataLoad still waiting after Reconcile: %v", updatedLoad.Status.WaitingFor.OperationComplete)
	}

	// The same-name non-waiting DataBackup is untouched.
	backupAfter := &datav1alpha1.DataBackup{}
	if err := r.Get(context.TODO(), req.NamespacedName, backupAfter); err != nil {
		t.Fatalf("failed to read DataBackup after reconcile: %v", err)
	}
	if backupAfter.ResourceVersion != backupBefore.ResourceVersion {
		t.Fatalf("same-name non-waiting DataBackup was written: resourceVersion %s -> %s",
			backupBefore.ResourceVersion, backupAfter.ResourceVersion)
	}
	if !reflect.DeepEqual(backupAfter.Status, backupBefore.Status) {
		t.Fatalf("same-name non-waiting DataBackup status changed: %+v -> %+v",
			backupBefore.Status, backupAfter.Status)
	}

	// Exactly one event: the waiting DataLoad's release.
	events := pr6254DrainEvents(r.Recorder.(*record.FakeRecorder))
	if len(events) != 1 {
		t.Fatalf("expected exactly one event (the DataLoad release), got %d: %v", len(events), events)
	}
	if !strings.Contains(events[0], "DataOperationWaitingEnded") {
		t.Fatalf("event is not %s: %s", "DataOperationWaitingEnded", events[0])
	}
}
