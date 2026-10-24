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

package base

// Reviewer verification harness for https://github.com/fluid-cloudnative/fluid/pull/5754.
// Additive only: no production code is changed on this branch.
//
// These tests verify the PR's user-visible contract at the dataset-lock layer:
//   V1 (contract): a second data operation is refused with an error once the
//      dataset-level parallel limit is reached (reconcilePending turns this
//      error into a 20s requeue, so the operation CR stays Pending).
//   V2 (contract): re-entrant registration of the SAME operation is allowed
//      (cron / OnEvent re-execution must not self-block).
//   V3 (contract): after the operation is released, the slot is free and the
//      next operation can register ("second task proceeds once the first
//      finishes").
//   V4 (contract): with limit > 1 exactly that many operations may register;
//      the (limit+1)-th is refused.
//
// Layer: fake-client (controller-runtime fake) exercising utils.GetDataset +
// status update + the retry.RetryOnConflict loop of updateDatasetDataOperation.

import (
	"strings"
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/dataoperation"
	cruntime "github.com/fluid-cloudnative/fluid/pkg/runtime"
	"github.com/fluid-cloudnative/fluid/pkg/utils"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apimachineryRuntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// verifyLockOperation is a minimal dataoperation.OperationInterface whose
// target dataset is resolved from the fake client, so the full
// updateDatasetDataOperation / removeDatasetDataOperation round trip works.
type verifyLockOperation struct {
	client       client.Client
	parallelTask int32
	name         string
}

func (o *verifyLockOperation) GetOperationType() dataoperation.OperationType {
	return dataoperation.DataLoadType
}
func (o *verifyLockOperation) GetParallelTaskNumber() int32 { return o.parallelTask }
func (o *verifyLockOperation) SetTargetDatasetStatusInProgress(_ *datav1alpha1.Dataset) {}
func (o *verifyLockOperation) RemoveTargetDatasetStatusInProgress(_ *datav1alpha1.Dataset) {
}
func (o *verifyLockOperation) GetOperationObject() client.Object {
	return &datav1alpha1.DataLoad{ObjectMeta: metav1.ObjectMeta{Name: o.name, Namespace: "default"}}
}
func (o *verifyLockOperation) GetTargetDataset() (*datav1alpha1.Dataset, error) {
	return utils.GetDataset(o.client, "verify-dataset", "default")
}
func (o *verifyLockOperation) GetPossibleTargetDatasetNamespacedNames() []types.NamespacedName {
	return nil
}
func (o *verifyLockOperation) HasPrecedingOperation() bool { return false }
func (o *verifyLockOperation) GetReleaseNameSpacedName() types.NamespacedName {
	return types.NamespacedName{}
}
func (o *verifyLockOperation) GetChartsDirectory() string                    { return "" }
func (o *verifyLockOperation) GetStatusHandler() dataoperation.StatusHandler { return nil }
func (o *verifyLockOperation) GetTTL() (*int32, error)                       { return nil, nil }
func (o *verifyLockOperation) UpdateOperationApiStatus(_ *datav1alpha1.OperationStatus) error {
	return nil
}
func (o *verifyLockOperation) UpdateStatusInfoForCompleted(_ map[string]string) error { return nil }
func (o *verifyLockOperation) Validate(_ cruntime.ReconcileRequestContext) ([]datav1alpha1.Condition, error) {
	return nil, nil
}

func newVerifyLockContext(initial *datav1alpha1.Dataset) (cruntime.ReconcileRequestContext, client.Client) {
	s := apimachineryRuntime.NewScheme()
	s.AddKnownTypes(datav1alpha1.GroupVersion, &datav1alpha1.Dataset{}, &datav1alpha1.DatasetList{})
	c := fake.NewFakeClientWithScheme(s, initial)
	return cruntime.ReconcileRequestContext{
		Client: c,
		Log:    fake.NullLogger(),
	}, c
}

func getVerifyDataset(t *testing.T, c client.Client) *datav1alpha1.Dataset {
	t.Helper()
	ds, err := utils.GetDataset(c, "verify-dataset", "default")
	if err != nil {
		t.Fatalf("get dataset: %v", err)
	}
	return ds
}

// V1 (contract): second operation refused at limit 1.
func TestVerifyClaude_SecondOperationBlockedAtLimit(t *testing.T) {
	ctx, c := newVerifyLockContext(&datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: "verify-dataset", Namespace: "default"},
		Status: datav1alpha1.DatasetStatus{
			OperationRef: map[string]string{"DataLoad": "load-1"},
		},
	})
	op := &verifyLockOperation{client: c, parallelTask: 1, name: "load-2"}

	err := updateDatasetDataOperation(ctx, "verify-dataset", "default", "DataLoad", "load-2", op)
	if err == nil {
		t.Fatal("expected the second operation to be refused at parallel limit 1, got nil error")
	}
	if !strings.Contains(err.Error(), "reached the maximum number of parallel") {
		t.Fatalf("expected a 'maximum number of parallel' refusal error, got: %v", err)
	}
	// the blocked operation must not have registered itself
	if got := getVerifyDataset(t, c).Status.OperationRef["DataLoad"]; got != "load-1" {
		t.Fatalf("blocked operation must not register: OperationRef[DataLoad] = %q", got)
	}
}

// V2 (contract): re-entrant registration of the same operation is allowed.
func TestVerifyClaude_ReentrantSameOperationAllowed(t *testing.T) {
	ctx, c := newVerifyLockContext(&datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: "verify-dataset", Namespace: "default"},
		Status: datav1alpha1.DatasetStatus{
			OperationRef: map[string]string{"DataLoad": "load-1"},
		},
	})
	op := &verifyLockOperation{client: c, parallelTask: 1, name: "load-1"}

	err := updateDatasetDataOperation(ctx, "verify-dataset", "default", "DataLoad", "load-1", op)
	if err != nil {
		t.Fatalf("re-entrant registration of the same operation must not be blocked: %v", err)
	}
	if got := getVerifyDataset(t, c).Status.OperationRef["DataLoad"]; got != "load-1" {
		t.Fatalf("re-entrant registration must not duplicate the entry: %q", got)
	}
}

// V3 (contract): releasing the operation frees the slot for the next one.
func TestVerifyClaude_ReleasedOperationFreesSlot(t *testing.T) {
	ctx, c := newVerifyLockContext(&datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: "verify-dataset", Namespace: "default"},
		Status: datav1alpha1.DatasetStatus{
			OperationRef: map[string]string{"DataLoad": "load-1"},
		},
	})

	first := &verifyLockOperation{client: c, parallelTask: 1, name: "load-1"}
	if err := removeDatasetDataOperation(ctx, "DataLoad", "load-1", first); err != nil {
		t.Fatalf("release failed: %v", err)
	}
	if got, ok := getVerifyDataset(t, c).Status.OperationRef["DataLoad"]; ok && got != "" {
		t.Fatalf("release must clear the lock, got %q", got)
	}

	second := &verifyLockOperation{client: c, parallelTask: 1, name: "load-2"}
	err := updateDatasetDataOperation(ctx, "verify-dataset", "default", "DataLoad", "load-2", second)
	if err != nil {
		t.Fatalf("the next operation must be able to start after release: %v", err)
	}
	if got := getVerifyDataset(t, c).Status.OperationRef["DataLoad"]; got != "load-2" {
		t.Fatalf("expected load-2 registered after release, got %q", got)
	}
}

// V4 (contract): limit > 1 admits exactly that many operations, refuses limit+1.
func TestVerifyClaude_LimitGreaterThanOne(t *testing.T) {
	ctx, c := newVerifyLockContext(&datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: "verify-dataset", Namespace: "default"},
	})

	const limit = 3
	for i := 1; i <= limit; i++ {
		name := "load-" + string(rune('0'+i))
		op := &verifyLockOperation{client: c, parallelTask: limit, name: name}
		if err := updateDatasetDataOperation(ctx, "verify-dataset", "default", "DataLoad", name, op); err != nil {
			t.Fatalf("operation %d of %d must be admitted: %v", i, limit, err)
		}
	}

	over := &verifyLockOperation{client: c, parallelTask: limit, name: "load-over"}
	err := updateDatasetDataOperation(ctx, "verify-dataset", "default", "DataLoad", "load-over", over)
	if err == nil {
		t.Fatalf("operation %d (limit+1) must be refused", limit+1)
	}
	if !strings.Contains(err.Error(), "reached the maximum number of parallel") {
		t.Fatalf("expected a 'maximum number of parallel' refusal error, got: %v", err)
	}
}
