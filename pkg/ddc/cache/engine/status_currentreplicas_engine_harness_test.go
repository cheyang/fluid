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

// Reviewer verification harness (Reviewer A) for PR #6223, round 5.
//
// F-engine-fixture-stale-replicas (contract, layer: unit): the engine-level ASTS
// fixture newAdvancedStatefulSetComponent must model a state the real ASTS
// controller can produce, so the engine-level status path actually exercises the
// field that feeds the scale subresource's statusReplicasPath.
//
//   - On the previous head 99e35af0 the fixture set only Status.CurrentReplicas
//     (never Status.Replicas). After the round-4 constructStatus change (which
//     reads asts.Status.Replicas), that fixture made the engine path report
//     Status.Worker.CurrentReplicas = 0 — an impossible controller state — and
//     desensitized every engine-level test to the scale-fed field.
//   - 73ff9ba3 adds Status.Replicas: desiredReplicas to the fixture; this test
//     FAILS on 99e35af0 ("got 0, want 3") and PASSES on 73ff9ba3, so the
//     fixture cannot silently regress to an unreachable state again.
//
// Non-circular: the expectation (3 / 1) is a literal, taken from the fixture
// arguments (desired=3, ready=2), through the real CheckAndUpdateRuntimeStatus
// → setWorkerComponentStatus → ConstructComponentStatusAndAffinity →
// constructStatus path with the fake client.

import (
	"context"
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	"k8s.io/apimachinery/pkg/types"
)

func TestHarnessEngineStatusCurrentReplicasFromFixture(t *testing.T) {
	const desired, ready = int32(3), int32(2)

	client := fake.NewFakeClientWithScheme(
		CacheEngineTestScheme,
		newStatusTestRuntime(),
		newAdvancedStatefulSetComponent(testStatusMaster, testStatusNamespace, 1, 1),
		newAdvancedStatefulSetComponent(testStatusWorker, testStatusNamespace, desired, ready),
	)
	engine := &CacheEngine{
		Client:    client,
		name:      testStatusRuntime,
		namespace: testStatusNamespace,
		Log:       fake.NullLogger(),
	}

	if _, err := engine.CheckAndUpdateRuntimeStatus(newStatusTestRuntimeValue(false)); err != nil {
		t.Fatalf("CheckAndUpdateRuntimeStatus returned error: %v", err)
	}

	updated := &datav1alpha1.CacheRuntime{}
	if err := client.Get(context.TODO(), types.NamespacedName{Name: testStatusRuntime, Namespace: testStatusNamespace}, updated); err != nil {
		t.Fatalf("failed to get updated runtime: %v", err)
	}

	worker := updated.Status.Worker
	if worker.CurrentReplicas != desired {
		t.Errorf("F-engine-fixture-stale-replicas: status.worker.currentReplicas = %d, want %d (the observed-pod count from a realistic ASTS fixture; 0 means the fixture models an impossible ASTS state)", worker.CurrentReplicas, desired)
	}
	if worker.UnavailableReplicas != desired-ready {
		t.Errorf("F-engine-fixture-stale-replicas: status.worker.unavailableReplicas = %d, want %d (observed %d - available %d)", worker.UnavailableReplicas, desired-ready, desired, ready)
	}
	if worker.DesiredReplicas != desired {
		t.Errorf("status.worker.desiredReplicas = %d, want %d", worker.DesiredReplicas, desired)
	}
}
