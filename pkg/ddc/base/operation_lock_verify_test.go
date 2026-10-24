/*
Verification harness for PR #5754 (fix: enforce concurrency limits for data operations).
Reviewer B (Codex). Additive test file only — production code untouched.

Polarity per test is documented inline. The same file compiles on the merge-base
(965f661b) and on the PR head (20b7e5ed), so it can A/B the premise.

Layer: unit + fake-client integration (exercises the real shipped functions
SetDataOperationInTargetDataset / ReleaseTargetDataset end to end, including the
retry-closure, the status update and the new CanStartDataOperation gate).
*/
package base_test

import (
	"strings"
	"testing"

	"github.com/golang/mock/gomock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apimachineryRuntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/dataoperation"
	mockop "github.com/fluid-cloudnative/fluid/pkg/dataoperation/mock"
	enginemock "github.com/fluid-cloudnative/fluid/pkg/ddc/base/mock"
	"github.com/fluid-cloudnative/fluid/pkg/ddc/base"
	cruntime "github.com/fluid-cloudnative/fluid/pkg/runtime"
	"github.com/fluid-cloudnative/fluid/pkg/utils"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
)

func verifyScheme() *apimachineryRuntime.Scheme {
	s := apimachineryRuntime.NewScheme()
	s.AddKnownTypes(datav1alpha1.GroupVersion, &datav1alpha1.Dataset{}, &datav1alpha1.DatasetList{})
	return s
}

func verifyCtx(dataset *datav1alpha1.Dataset) cruntime.ReconcileRequestContext {
	return cruntime.ReconcileRequestContext{
		Dataset:  dataset,
		Client:   fake.NewFakeClientWithScheme(verifyScheme(), dataset),
		Log:      fake.NullLogger(),
		Recorder: record.NewFakeRecorder(16),
	}
}

func verifyEngine(ctrl *gomock.Controller) *base.TemplateEngine {
	impl := enginemock.NewMockImplement(ctrl)
	impl.EXPECT().CheckRuntimeReady().Return(true).AnyTimes()
	return &base.TemplateEngine{Implement: impl}
}

func verifyOp(ctrl *gomock.Controller, opType dataoperation.OperationType, name, namespace string, parallel int32) *mockop.MockOperationInterface {
	op := mockop.NewMockOperationInterface(ctrl)
	op.EXPECT().GetOperationObject().Return(&datav1alpha1.DataLoad{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
	}).AnyTimes()
	op.EXPECT().GetOperationType().Return(opType).AnyTimes()
	op.EXPECT().GetParallelTaskNumber().Return(parallel).AnyTimes()
	op.EXPECT().SetTargetDatasetStatusInProgress(gomock.Any()).AnyTimes()
	return op
}

func datasetOpRef(t *testing.T, ctx cruntime.ReconcileRequestContext, datasetName string) string {
	t.Helper()
	ds, err := utils.GetDataset(ctx.Client, datasetName, "default")
	if err != nil {
		t.Fatalf("get dataset: %v", err)
	}
	return ds.Status.OperationRef["DataLoad"]
}

// TestVerifyContractSecondDataLoadBlocked is the master CONTRACT test for the PR.
// Intended behavior (per PR body + issue #1138): while one DataLoad is registered
// on a dataset, a second DataLoad for the same dataset must be rejected so it
// stays Pending.
//
// Polarity: contract.
//   - on merge-base 965f661b: EXPECTED TO FAIL -> reproduces premise P0
//     ("no lock: multiple same DataLoad tasks run concurrently").
//   - on PR head 20b7e5ed:    EXPECTED TO PASS -> the fix blocks the second op.
func TestVerifyContractSecondDataLoadBlocked(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	dataset := &datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: "ds", Namespace: "default"},
		Status: datav1alpha1.DatasetStatus{
			OperationRef: map[string]string{"DataLoad": "load-1"},
		},
	}
	ctx := verifyCtx(dataset)

	second := verifyOp(ctrl, dataoperation.DataLoadType, "load-2", "default", 1)
	err := base.SetDataOperationInTargetDataset(ctx, second, verifyEngine(ctrl))
	got := datasetOpRef(t, ctx, "ds")
	if err == nil {
		t.Fatalf("P0 reproduced: second DataLoad admitted while load-1 in progress; OperationRef[DataLoad]=%q", got)
	}
	if got != "load-1" {
		t.Fatalf("blocked op must not modify OperationRef, got %q", got)
	}
	t.Logf("second DataLoad rejected as intended: %v", err)
}

// TestVerifyContractDataMigrateParallelismNotWorkerCap documents finding F1.
// dataMigrateOperation.GetParallelTaskNumber() returns Spec.Parallelism, which is
// the number of WORKERS of a single DataMigrate job (kubebuilder default 1,
// minimum 1) — not a cap on concurrent DataMigrate CRs. The new gate reuses it as
// the inter-operation limit, so with spec.parallelism=4 up to 4 distinct
// DataMigrate CRs are admitted on the same dataset, contradicting the PR's claim
// "parallel limit (which is 1 for DataLoad/DataMigrate)" and the mutual exclusion
// issue #1138 asks about.
//
// Polarity: contract (asserts the PR-claimed semantics: second DataMigrate blocked).
//   - on PR head: EXPECTED TO FAIL -> confirms F1.
func TestVerifyContractDataMigrateParallelismNotWorkerCap(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	dataset := &datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: "ds", Namespace: "default"},
		Status: datav1alpha1.DatasetStatus{
			OperationRef: map[string]string{"DataMigrate": "mig-1"},
		},
	}
	ctx := verifyCtx(dataset)

	// mechanism: SetDataOperationInTargetDataset passes operation.GetParallelTaskNumber()
	// (for DataMigrate = spec.parallelism, the worker count) as maxParallel to
	// dataset.CanStartDataOperation, and 1 in-progress < 4 admits the 2nd op.

	second := verifyOp(ctrl, dataoperation.DataMigrateType, "mig-2", "default", 4)
	err := base.SetDataOperationInTargetDataset(ctx, second, verifyEngine(ctrl))
	gotRef := func() string {
		ds, gerr := utils.GetDataset(ctx.Client, "ds", "default")
		if gerr != nil {
			t.Fatalf("get dataset: %v", gerr)
		}
		return ds.Status.OperationRef["DataMigrate"]
	}()
	if err == nil {
		t.Fatalf("F1 confirmed: 2nd DataMigrate admitted because GetParallelTaskNumber()=spec.parallelism=4 is reused as the inter-op cap; OperationRef[DataMigrate]=%q", gotRef)
	}
	t.Logf("second DataMigrate rejected: %v", err)
}

// TestVerifyContractCrossNamespaceSameNameBlocked documents finding F3.
// getDataOperationKey() keys operations by NAME ONLY. Two DataLoads with the same
// name in different namespaces can target the same dataset
// (dataLoadOperation.GetTargetDataset uses spec.dataset.namespace). The second
// one matches the "already in progress" (re-entrant) branch of
// CanStartDataOperation and is admitted, bypassing the limit; a later
// ReleaseTargetDataset by either one removes the shared key.
// The name-only key predates this PR, but the PR makes it load-bearing for exclusion.
//
// Polarity: contract (asserts intended: a different op object must be blocked).
//   - on PR head: EXPECTED TO FAIL -> confirms F3.
func TestVerifyContractCrossNamespaceSameNameBlocked(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	dataset := &datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: "ds", Namespace: "default"},
		Status: datav1alpha1.DatasetStatus{
			// registered by DataLoad "load" in namespace "ns-a"
			OperationRef: map[string]string{"DataLoad": "load"},
		},
	}
	ctx := verifyCtx(dataset)

	// a DIFFERENT DataLoad, also named "load", in namespace "ns-b"
	otherNsSameName := verifyOp(ctrl, dataoperation.DataLoadType, "load", "ns-b", 1)
	err := base.SetDataOperationInTargetDataset(ctx, otherNsSameName, verifyEngine(ctrl))
	if err == nil {
		t.Fatalf("F3 confirmed: same-named DataLoad from another namespace bypassed the limit via the name-only operation key")
	}
	t.Logf("same-named op from other namespace rejected: %v", err)
}

// TestVerifyContractReentrantSelfAdmission guards the re-entrant sync cycle:
// the SAME operation re-entering reconcilePending (e.g. status update failed
// after the lock was set) must keep being admitted, exactly once.
//
// Polarity: contract. Expected PASS on base and head.
func TestVerifyContractReentrantSelfAdmission(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	dataset := &datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: "ds", Namespace: "default"},
		Status: datav1alpha1.DatasetStatus{
			OperationRef: map[string]string{"DataLoad": "load-1"},
		},
	}
	ctx := verifyCtx(dataset)

	self := verifyOp(ctrl, dataoperation.DataLoadType, "load-1", "default", 1)
	if err := base.SetDataOperationInTargetDataset(ctx, self, verifyEngine(ctrl)); err != nil {
		t.Fatalf("re-entrant admission of the same op must succeed, got: %v", err)
	}
	if got := datasetOpRef(t, ctx, "ds"); got != "load-1" {
		t.Fatalf("re-entrant admission must not duplicate the entry, got %q", got)
	}
}

// TestVerifyContractReleaseUnblocksNextOperation covers the lifecycle:
// op2 blocked while op1 runs -> op1 released -> op2 admitted.
//
// Polarity: contract. Expected PASS on head. On base the "op2 blocked" step
// cannot hold (no gate), so this test is head-focused (see manifest).
func TestVerifyContractReleaseUnblocksNextOperation(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	dataset := &datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: "ds", Namespace: "default"},
	}
	ctx := verifyCtx(dataset)

	op1 := verifyOp(ctrl, dataoperation.DataLoadType, "load-1", "default", 1)
	op1.EXPECT().GetTargetDataset().DoAndReturn(func() (*datav1alpha1.Dataset, error) {
		return utils.GetDataset(ctx.Client, "ds", "default")
	}).AnyTimes()
	op1.EXPECT().RemoveTargetDatasetStatusInProgress(gomock.Any()).AnyTimes()

	if err := base.SetDataOperationInTargetDataset(ctx, op1, verifyEngine(ctrl)); err != nil {
		t.Fatalf("first op must be admitted: %v", err)
	}

	op2 := verifyOp(ctrl, dataoperation.DataLoadType, "load-2", "default", 1)
	if err := base.SetDataOperationInTargetDataset(ctx, op2, verifyEngine(ctrl)); err == nil {
		t.Fatalf("second op must be blocked while first runs (premise P0 reproduced on base)")
	}

	if err := base.ReleaseTargetDataset(ctx, op1); err != nil {
		t.Fatalf("release of first op failed: %v", err)
	}
	if got := datasetOpRef(t, ctx, "ds"); got != "" {
		t.Fatalf("release must clear OperationRef, got %q", got)
	}

	if err := base.SetDataOperationInTargetDataset(ctx, op2, verifyEngine(ctrl)); err != nil {
		t.Fatalf("second op must be admitted after release, got: %v", err)
	}
	if got := datasetOpRef(t, ctx, "ds"); !strings.Contains(got, "load-2") {
		t.Fatalf("second op must be registered after release, got %q", got)
	}
}
