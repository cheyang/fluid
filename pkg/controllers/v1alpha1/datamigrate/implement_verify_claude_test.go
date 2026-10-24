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

package datamigrate

// Reviewer verification harness for https://github.com/fluid-cloudnative/fluid/pull/5754.
// Additive only: no production code is changed on this branch.
//
// BUG-CANARY (polarity: canary). It documents that the dataset-level
// concurrency limit for DataMigrate operations introduced by PR #5754 is
// wired to DataMigrate.Spec.Parallelism — the job-internal worker count of a
// single migration job ("If the value is greater than 1, the job acts as a
// launcher, and users should define the WorkerSpec"), NOT a fixed 1 like
// DataLoad/DataBackup/DataProcess, and NOT a dedicated concurrency knob.
//
// On the current PR head this test PASSES (the behavior is present). If the
// semantics are changed (e.g. the dataset-level limit is decoupled from the
// job-internal Parallelism), this test FLIPS to fail — invert it or promote
// the new behavior to a contract test at that point.

import (
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
)

func TestVerifyClaude_DataMigrateDatasetLimitUsesJobParallelism(t *testing.T) {
	dm := &datav1alpha1.DataMigrate{
		Spec: datav1alpha1.DataMigrateSpec{Parallelism: 4},
	}
	op := &dataMigrateOperation{dataMigrate: dm}

	if got := op.GetParallelTaskNumber(); got != 4 {
		t.Fatalf("GetParallelTaskNumber() = %d, want 4 (Spec.Parallelism)", got)
	}

	// The dataset-level lock then admits 4 concurrent DataMigrate operations...
	ds := &datav1alpha1.Dataset{
		Status: datav1alpha1.DatasetStatus{
			OperationRef: map[string]string{"DataMigrate": "m-1,m-2,m-3"},
		},
	}
	if !ds.CanStartDataOperation("DataMigrate", op.GetParallelTaskNumber(), "m-4") {
		t.Fatal("a 4th concurrent DataMigrate is admitted when Spec.Parallelism=4 (current behavior)")
	}
	// ...and refuses the 5th.
	ds5 := &datav1alpha1.Dataset{
		Status: datav1alpha1.DatasetStatus{
			OperationRef: map[string]string{"DataMigrate": "m-1,m-2,m-3,m-4"},
		},
	}
	if ds5.CanStartDataOperation("DataMigrate", op.GetParallelTaskNumber(), "m-5") {
		t.Fatal("a 5th concurrent DataMigrate is refused when Spec.Parallelism=4 (current behavior)")
	}
}
