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

package engine

// Reviewer harness for https://github.com/fluid-cloudnative/fluid/pull/6264 (Reviewer A).
//
// These tests are ADDITIVE reviewer verification, not part of the PR under review.
// They close the coverage gap recorded as finding T1 in
// docs/verification/cache-engine-unit-tests/: the PR's routing tests only assert
// the early type-mismatch error of each value-file generator, so a regression in
// the *happy path* of generateDataLoadValueFile (DataOperationSpec selection ->
// image/command injection into the generated value file) would not turn any of
// the PR's specs red. Mutating generateDataLoadValueFile to return a bogus file
// leaves the PR's 23 specs green; the test below fails under that mutation
// (see docs/verification/cache-engine-unit-tests/results/).
//
// Polarity: CONTRACT tests. They pass on correct code and fail when the
// DataLoad value-file generation regresses.

import (
	"os"
	"strings"
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/dataoperation"
	fluiderrors "github.com/fluid-cloudnative/fluid/pkg/errors"
	cruntime "github.com/fluid-cloudnative/fluid/pkg/runtime"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// harnessOperation is a self-contained dataoperation.OperationInterface (independent
// of the fakeOperation defined in the PR's data_operation_test.go, so this file
// stays graftable on its own).
type harnessOperation struct {
	operationType dataoperation.OperationType
	object        client.Object
}

var _ dataoperation.OperationInterface = (*harnessOperation)(nil)

func (h *harnessOperation) HasPrecedingOperation() bool       { return false }
func (h *harnessOperation) GetOperationObject() client.Object { return h.object }
func (h *harnessOperation) GetPossibleTargetDatasetNamespacedNames() []types.NamespacedName {
	return nil
}
func (h *harnessOperation) GetTargetDataset() (*datav1alpha1.Dataset, error) { return nil, nil }
func (h *harnessOperation) GetReleaseNameSpacedName() types.NamespacedName {
	return types.NamespacedName{}
}
func (h *harnessOperation) GetChartsDirectory() string { return "" }
func (h *harnessOperation) GetOperationType() dataoperation.OperationType {
	return h.operationType
}
func (h *harnessOperation) UpdateOperationApiStatus(*datav1alpha1.OperationStatus) error {
	return nil
}
func (h *harnessOperation) Validate(cruntime.ReconcileRequestContext) ([]datav1alpha1.Condition, error) {
	return nil, nil
}
func (h *harnessOperation) UpdateStatusInfoForCompleted(map[string]string) error      { return nil }
func (h *harnessOperation) SetTargetDatasetStatusInProgress(*datav1alpha1.Dataset)    {}
func (h *harnessOperation) RemoveTargetDatasetStatusInProgress(*datav1alpha1.Dataset) {}
func (h *harnessOperation) GetStatusHandler() dataoperation.StatusHandler             { return nil }
func (h *harnessOperation) GetTTL() (*int32, error)                                   { return nil, nil }
func (h *harnessOperation) GetParallelTaskNumber() int32                              { return 1 }

// TestDataOperationValueFileHappyPath is the T1 contract test: a DataLoad routed
// through GetDataOperationValueFile must produce a real value file that carries
// the image and command selected from the CacheRuntimeClass DataOperationSpecs.
func TestDataOperationValueFileHappyPath(t *testing.T) {
	runtime := &datav1alpha1.CacheRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "default", UID: types.UID("demo-uid")},
		Spec: datav1alpha1.CacheRuntimeSpec{
			RuntimeClassName: "test-class",
			Master:           datav1alpha1.CacheRuntimeMasterSpec{RuntimeComponentCommonSpec: datav1alpha1.RuntimeComponentCommonSpec{Disabled: true}},
			Worker:           datav1alpha1.CacheRuntimeWorkerSpec{RuntimeComponentCommonSpec: datav1alpha1.RuntimeComponentCommonSpec{Disabled: false}},
			Client:           datav1alpha1.CacheRuntimeClientSpec{RuntimeComponentCommonSpec: datav1alpha1.RuntimeComponentCommonSpec{Disabled: true}},
		},
	}
	runtimeClass := &datav1alpha1.CacheRuntimeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "test-class"},
		Topology: &datav1alpha1.RuntimeTopology{
			Worker: &datav1alpha1.RuntimeComponentDefinition{},
		},
		DataOperationSpecs: []datav1alpha1.DataOperationSpec{
			{
				Name:    string(dataoperation.DataLoadType),
				Image:   "harness-dataload:v1",
				Command: []string{"/harness/dataload.sh"},
			},
		},
	}
	dataset := &datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "default", UID: types.UID("demo-dataset-uid")},
	}
	dataload := &datav1alpha1.DataLoad{
		ObjectMeta: metav1.ObjectMeta{Name: "test-dataload", Namespace: "default", UID: types.UID("dataload-uid")},
		Spec: datav1alpha1.DataLoadSpec{
			Dataset: datav1alpha1.TargetDataset{Name: "demo", Namespace: "default"},
			Target:  []datav1alpha1.TargetPath{{Path: "/data", Replicas: 1}},
		},
	}

	c := fake.NewFakeClientWithScheme(CacheEngineTestScheme, runtime, runtimeClass, dataset, dataload)
	engine := &CacheEngine{
		Client:    c,
		name:      "demo",
		namespace: "default",
		Log:       ctrl.Log.WithName("harness-test"),
	}
	ctx := cruntime.ReconcileRequestContext{Client: c}

	valueFile, err := engine.GetDataOperationValueFile(ctx, &harnessOperation{
		operationType: dataoperation.DataLoadType,
		object:        dataload,
	})
	if err != nil {
		t.Fatalf("expected happy-path value file, got error: %v", err)
	}
	defer os.Remove(valueFile)

	data, err := os.ReadFile(valueFile)
	if err != nil {
		t.Fatalf("value file %q not readable: %v", valueFile, err)
	}
	content := string(data)
	for _, want := range []string{
		"harness-dataload:v1",       // DataOperationSpecs image must reach the value file
		"/harness/dataload.sh",      // DataOperationSpecs command must reach the value file
		"/data",                     // target path must reach the value file
		"fluid-runtime-demo-config", // runtime config volume must be wired to the job
	} {
		if !strings.Contains(content, want) {
			t.Errorf("generated value file missing %q; content:\n%s", want, content)
		}
	}
	if !strings.Contains(valueFile, "default-test-dataload-loader-values.yaml") {
		t.Errorf("value file name %q does not follow the <ns>-<name>-loader-values.yaml convention", valueFile)
	}
}

// TestDataLoadValueFileNotSupportedWhenSpecMissing covers the other half of the
// findDataOperationSpec contract in the live flow: a CacheRuntimeClass without a
// DataLoad DataOperationSpec must make the value-file generation fail with a
// NotSupported error instead of generating a job without image/command.
func TestDataLoadValueFileNotSupportedWhenSpecMissing(t *testing.T) {
	runtime := &datav1alpha1.CacheRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "default", UID: types.UID("demo-uid")},
		Spec:       datav1alpha1.CacheRuntimeSpec{RuntimeClassName: "test-class"},
	}
	runtimeClass := &datav1alpha1.CacheRuntimeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "test-class"},
		Topology: &datav1alpha1.RuntimeTopology{
			Worker: &datav1alpha1.RuntimeComponentDefinition{},
		},
		// no DataOperationSpecs at all
	}
	dataset := &datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "default", UID: types.UID("demo-dataset-uid")},
	}
	dataload := &datav1alpha1.DataLoad{
		ObjectMeta: metav1.ObjectMeta{Name: "test-dataload", Namespace: "default", UID: types.UID("dataload-uid")},
		Spec: datav1alpha1.DataLoadSpec{
			Dataset: datav1alpha1.TargetDataset{Name: "demo", Namespace: "default"},
			Target:  []datav1alpha1.TargetPath{{Path: "/data", Replicas: 1}},
		},
	}

	c := fake.NewFakeClientWithScheme(CacheEngineTestScheme, runtime, runtimeClass, dataset, dataload)
	engine := &CacheEngine{
		Client:    c,
		name:      "demo",
		namespace: "default",
		Log:       ctrl.Log.WithName("harness-test"),
	}
	ctx := cruntime.ReconcileRequestContext{Client: c}

	valueFile, err := engine.GetDataOperationValueFile(ctx, &harnessOperation{
		operationType: dataoperation.DataLoadType,
		object:        dataload,
	})
	if err == nil {
		os.Remove(valueFile)
		t.Fatalf("expected NotSupported error for a runtime class without a DataLoad spec, got value file %q", valueFile)
	}
	if !fluiderrors.IsNotSupported(err) {
		t.Fatalf("expected a NotSupported error, got: %v", err)
	}
}
