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

// Verification harness for fluid-cloudnative/fluid PR #6254 (reviewer: codex).
// Additive only; no production code is changed.
//
// Claims under test:
//
//	P0 (contract): a data operation that is waiting
//	  (status.waitingFor.operationComplete=true) but whose spec.runAfter is nil
//	  must be released (flag cleared) WITHOUT panicking. On the base branch
//	  (f432bdc8) reconcileOperationDataFlow dereferences runAfter.Namespace and
//	  panics, so this test is RED there and GREEN on the PR head.
//
//	P0-xkind (contract): the same crash is reachable without any spec edit —
//	  Reconcile runs all four per-kind reconcile funcs for one request name, so
//	  a waiting DataLoad named "x" makes the reconciler fetch a same-named
//	  DataBackup that has no runAfter at all. On base that panics too.
//
//	F1 (contract since head 50933d0b; was a canary on cc3947c0): the
//	  nil-runAfter early return must fire only for objects that are actually
//	  waiting. A non-waiting, never-waiting object that merely shares its
//	  namespace/name with a waiting operation of another kind must be left
//	  untouched: no status write (flag stays nil), no event. On head cc3947c0
//	  the guard was missing and this test was RED (spurious waitingFor=false
//	  write); on 50933d0b the guard landed and it is GREEN.
//
//	F2 (contract): releasing a waiting operation records a
//	  DataOperationWaitingEnded event with the reason, on both release paths
//	  (runAfter removed; preceding operation completed). An operation that was
//	  not waiting records no event. Absent on base and on cc3947c0 (RED),
//	  present on 50933d0b (GREEN).

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("verify PR6254: waiting operation without spec.runAfter", func() {

	const namespace = "default"

	var s *runtime.Scheme

	BeforeEach(func() {
		s = runtime.NewScheme()
		_ = datav1alpha1.AddToScheme(s)
	})

	waitingStatus := func() datav1alpha1.OperationStatus {
		return datav1alpha1.OperationStatus{
			Phase:      common.PhasePending,
			WaitingFor: datav1alpha1.WaitingStatus{OperationComplete: ptr.To(true)},
			Conditions: []datav1alpha1.Condition{},
			Duration:   "Unfinished",
		}
	}

	// P0, contract polarity: RED (panic) on base, GREEN on head.
	It("P0-contract: Reconcile releases a waiting DataLoad whose runAfter is gone, without panicking", func() {
		waitingLoad := &datav1alpha1.DataLoad{
			ObjectMeta: metav1.ObjectMeta{Name: "verify-p0", Namespace: namespace},
			Status:     waitingStatus(),
		}
		r := newTestDataFlowReconciler(s, waitingLoad)
		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "verify-p0", Namespace: namespace}}

		var (
			result ctrl.Result
			err    error
		)
		Expect(func() { result, err = r.Reconcile(context.TODO(), req) }).NotTo(Panic())
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(ctrl.Result{}))

		updated := &datav1alpha1.DataLoad{}
		Expect(r.Get(context.TODO(), req.NamespacedName, updated)).To(Succeed())
		Expect(updated.Status.WaitingFor.OperationComplete).To(HaveValue(BeFalse()))
		Expect(updated.Status.Phase).To(Equal(common.PhasePending))
	})

	// P0-xkind, contract polarity: RED (panic) on base, GREEN on head.
	// Proves the bug class is broader than "user removes runAfter": a plain
	// cross-kind name collision reaches the same nil dereference.
	It("P0-xkind-contract: a waiting DataLoad must not crash on a same-named non-waiting DataBackup without runAfter", func() {
		preceding := &datav1alpha1.DataLoad{
			ObjectMeta: metav1.ObjectMeta{Name: "preceding", Namespace: namespace},
			Status:     datav1alpha1.OperationStatus{Phase: common.PhaseExecuting},
		}
		waitingLoad := &datav1alpha1.DataLoad{
			ObjectMeta: metav1.ObjectMeta{Name: "verify-xkind", Namespace: namespace},
			Spec: datav1alpha1.DataLoadSpec{
				RunAfter: &datav1alpha1.OperationRef{
					ObjectRef: datav1alpha1.ObjectRef{Kind: "DataLoad", Name: "preceding"},
				},
			},
			Status: waitingStatus(),
		}
		idleBackup := &datav1alpha1.DataBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "verify-xkind", Namespace: namespace},
			Status:     datav1alpha1.OperationStatus{Phase: common.PhaseComplete},
		}
		r := newTestDataFlowReconciler(s, preceding, waitingLoad, idleBackup)
		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "verify-xkind", Namespace: namespace}}

		Expect(func() { _, _ = r.Reconcile(context.TODO(), req) }).NotTo(Panic())
	})

	// F1, CONTRACT polarity (inverted at 50933d0b, where the release gained a
	// waiting guard): RED on cc3947c0 (spurious write happened), GREEN on head.
	It("F1-contract: a non-waiting same-named object of another kind is left untouched", func() {
		preceding := &datav1alpha1.DataLoad{
			ObjectMeta: metav1.ObjectMeta{Name: "preceding", Namespace: namespace},
			Status:     datav1alpha1.OperationStatus{Phase: common.PhaseExecuting},
		}
		waitingLoad := &datav1alpha1.DataLoad{
			ObjectMeta: metav1.ObjectMeta{Name: "verify-f1", Namespace: namespace},
			Spec: datav1alpha1.DataLoadSpec{
				RunAfter: &datav1alpha1.OperationRef{
					ObjectRef: datav1alpha1.ObjectRef{Kind: "DataLoad", Name: "preceding"},
				},
			},
			Status: waitingStatus(),
		}
		idleBackup := &datav1alpha1.DataBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "verify-f1", Namespace: namespace},
			Status:     datav1alpha1.OperationStatus{Phase: common.PhaseComplete},
		}
		r := newTestDataFlowReconciler(s, preceding, waitingLoad, idleBackup)
		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "verify-f1", Namespace: namespace}}

		backupBefore := &datav1alpha1.DataBackup{}
		Expect(r.Get(context.TODO(), req.NamespacedName, backupBefore)).To(Succeed())

		Expect(func() { _, _ = r.Reconcile(context.TODO(), req) }).NotTo(Panic())

		updatedBackup := &datav1alpha1.DataBackup{}
		Expect(r.Get(context.TODO(), req.NamespacedName, updatedBackup)).To(Succeed())
		// contract: the DataBackup never waited (WaitingFor.OperationComplete was
		// nil) and must stay untouched: flag still nil, no status write, no event.
		Expect(updatedBackup.Status.WaitingFor.OperationComplete).To(BeNil(),
			"non-waiting same-name object must not get a waitingFor write")
		Expect(updatedBackup.ResourceVersion).To(Equal(backupBefore.ResourceVersion),
			"non-waiting same-name object must not be written at all")

		// control: the waiting DataLoad itself must still be waiting (preceding op not complete)
		updatedLoad := &datav1alpha1.DataLoad{}
		Expect(r.Get(context.TODO(), req.NamespacedName, updatedLoad)).To(Succeed())
		Expect(updatedLoad.Status.WaitingFor.OperationComplete).To(HaveValue(BeTrue()))
	})
})

// F2, CONTRACT polarity: the release records a DataOperationWaitingEnded event
// with the reason on both release paths; non-waiting operations record none.
var _ = Describe("verify PR6254 F2: DataOperationWaitingEnded event", func() {

	const namespace = "default"

	var s *runtime.Scheme

	BeforeEach(func() {
		s = runtime.NewScheme()
		_ = datav1alpha1.AddToScheme(s)
	})

	waitingStatus := func() datav1alpha1.OperationStatus {
		return datav1alpha1.OperationStatus{
			Phase:      common.PhasePending,
			WaitingFor: datav1alpha1.WaitingStatus{OperationComplete: ptr.To(true)},
			Conditions: []datav1alpha1.Condition{},
			Duration:   "Unfinished",
		}
	}

	It("F2-contract: removal-path release records the event", func() {
		waitingLoad := &datav1alpha1.DataLoad{
			ObjectMeta: metav1.ObjectMeta{Name: "verify-f2-removal", Namespace: namespace},
			Status:     waitingStatus(),
		}
		ctx := makeTestCtx(s, "verify-f2-removal", namespace, waitingLoad)

		needRequeue, err := reconcileDataLoad(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(needRequeue).To(BeFalse())
		Expect(ctx.Recorder.(*record.FakeRecorder).Events).To(Receive(And(
			ContainSubstring(common.DataOperationWaitingEnded),
			ContainSubstring("spec.runAfter was removed"))))
	})

	It("F2-contract: completion-path release records the event only for a waiting operation", func() {
		preceding := &datav1alpha1.DataLoad{
			ObjectMeta: metav1.ObjectMeta{Name: "verify-f2-preceding", Namespace: namespace},
			Status:     datav1alpha1.OperationStatus{Phase: common.PhaseComplete},
		}
		waitingLoad := &datav1alpha1.DataLoad{
			ObjectMeta: metav1.ObjectMeta{Name: "verify-f2-completion", Namespace: namespace},
			Spec: datav1alpha1.DataLoadSpec{
				RunAfter: &datav1alpha1.OperationRef{
					ObjectRef: datav1alpha1.ObjectRef{Kind: "DataLoad", Name: "verify-f2-preceding"},
				},
			},
			Status: waitingStatus(),
		}
		ctx := makeTestCtx(s, "verify-f2-completion", namespace, preceding, waitingLoad)

		needRequeue, err := reconcileDataLoad(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(needRequeue).To(BeFalse())
		Expect(ctx.Recorder.(*record.FakeRecorder).Events).To(Receive(And(
			ContainSubstring(common.DataOperationWaitingEnded),
			ContainSubstring("completed"))))
	})

	It("F2-contract: a non-waiting operation records no event on either path", func() {
		preceding := &datav1alpha1.DataLoad{
			ObjectMeta: metav1.ObjectMeta{Name: "verify-f2-preceding2", Namespace: namespace},
			Status:     datav1alpha1.OperationStatus{Phase: common.PhaseComplete},
		}
		// never waited: flag nil, runAfter removed -> nil-runAfter branch
		idleNoRunAfter := &datav1alpha1.DataLoad{
			ObjectMeta: metav1.ObjectMeta{Name: "verify-f2-idle1", Namespace: namespace},
			Status:     datav1alpha1.OperationStatus{Phase: common.PhaseComplete},
		}
		// already stopped waiting: flag false, preceding complete -> completion path
		idleCompleted := &datav1alpha1.DataLoad{
			ObjectMeta: metav1.ObjectMeta{Name: "verify-f2-idle2", Namespace: namespace},
			Spec: datav1alpha1.DataLoadSpec{
				RunAfter: &datav1alpha1.OperationRef{
					ObjectRef: datav1alpha1.ObjectRef{Kind: "DataLoad", Name: "verify-f2-preceding2"},
				},
			},
			Status: datav1alpha1.OperationStatus{
				Phase:      common.PhaseComplete,
				WaitingFor: datav1alpha1.WaitingStatus{OperationComplete: ptr.To(false)},
			},
		}

		ctx1 := makeTestCtx(s, "verify-f2-idle1", namespace, idleNoRunAfter)
		needRequeue, err := reconcileDataLoad(ctx1)
		Expect(err).NotTo(HaveOccurred())
		Expect(needRequeue).To(BeFalse())
		Expect(ctx1.Recorder.(*record.FakeRecorder).Events).To(BeEmpty())

		ctx2 := makeTestCtx(s, "verify-f2-idle2", namespace, preceding, idleCompleted)
		needRequeue, err = reconcileDataLoad(ctx2)
		Expect(err).NotTo(HaveOccurred())
		Expect(needRequeue).To(BeFalse())
		Expect(ctx2.Recorder.(*record.FakeRecorder).Events).To(BeEmpty())
	})
})
