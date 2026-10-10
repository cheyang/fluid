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

// CANARY tests for finding F3 (PRE-EXISTING on the merge-base; not introduced
// by PR #6200): kubeclient.MergeNodeSelectorAndNodeAffinity builds
// MatchExpressions by iterating the nodeSelector map, so the expression order
// is nondeterministic whenever the worker pod template carries >= 2
// nodeSelector labels. CheckAndUpdateRuntimeStatus gates its status write on
// reflect.DeepEqual(old, new), so an order flip alone produces a spurious
// status write (a real API-server UPDATE), which directly counteracts the
// load-reduction goal of the PR.
//
// Polarity: CANARY. These tests PASS while the instability exists and must be
// INVERTED (or promoted to contract tests) once the expressions are sorted.
// Compiles on both base and head (uses only pre-existing symbols).

package engine

import (
	"context"
	"reflect"
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	"github.com/fluid-cloudnative/fluid/pkg/utils/kubeclient"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// zzStatusWriteCounter counts status writes against the CacheRuntime object.
type zzStatusWriteCounter struct {
	ctrlclient.Client
	runtimeStatusWrites int
}

func (c *zzStatusWriteCounter) Status() ctrlclient.StatusWriter {
	return &zzStatusWriterCounter{StatusWriter: c.Client.Status(), parent: c}
}

type zzStatusWriterCounter struct {
	ctrlclient.StatusWriter
	parent *zzStatusWriteCounter
}

func (w *zzStatusWriterCounter) Update(ctx context.Context, obj ctrlclient.Object, opts ...ctrlclient.SubResourceUpdateOption) error {
	if _, ok := obj.(*datav1alpha1.CacheRuntime); ok {
		w.parent.runtimeStatusWrites++
	}
	return w.StatusWriter.Update(ctx, obj, opts...)
}

func zzFiveLabelSelector() map[string]string {
	return map[string]string{
		"label-a": "a",
		"label-b": "b",
		"label-c": "c",
		"label-d": "d",
		"label-e": "e",
	}
}

// TestVerifyPR6200CanaryAffinityExpressionOrderUnstable documents that the
// merged MatchExpressions order is nondeterministic for multi-label selectors.
// PASS = instability still present (expected today). FAIL = someone sorted the
// expressions -> invert this canary.
func TestVerifyPR6200CanaryAffinityExpressionOrderUnstable(t *testing.T) {
	selector := zzFiveLabelSelector()
	reference := kubeclient.MergeNodeSelectorAndNodeAffinity(selector, nil).
		RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions
	if len(reference) != len(selector) {
		t.Fatalf("harness broken: expected %d merged expressions, got %d", len(selector), len(reference))
	}

	variants := 0
	for i := 0; i < 200; i++ {
		got := kubeclient.MergeNodeSelectorAndNodeAffinity(selector, nil).
			RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions
		if !reflect.DeepEqual(reference, got) {
			variants++
		}
	}
	t.Logf("order variants observed: %d/200 computations differed from the reference order", variants)
	if variants == 0 {
		t.Fatalf("CANARY FLIPPED: MatchExpressions order appears stable now; " +
			"if expressions were sorted, invert this canary into a contract test")
	}
}

// TestVerifyPR6200CanaryStatusWriteChurn documents the observable consequence:
// after the first cycle converges the status, later cycles keep issuing real
// status UPDATEs purely because the recomputed affinity expression order
// differs from the stored one. With 5 labels a no-churn run of 5 extra cycles
// has probability (1/5!)^5 ~= 4e-11, i.e. effectively deterministic.
// PASS = churn still present (expected today). FAIL = fixed -> invert.
func TestVerifyPR6200CanaryStatusWriteChurn(t *testing.T) {
	scheme := zzVerifyScheme(t)
	baseClient := fake.NewFakeClientWithScheme(scheme,
		zzVerifyRuntimeObj(),
		zzVerifyASTS(zzVerifyMaster, nil),
		zzVerifyASTS(zzVerifyWorker, zzFiveLabelSelector()),
	)
	counting := &zzStatusWriteCounter{Client: baseClient}
	engine := zzVerifyEngine(counting)

	// Cycle 1 always writes (initial status population); reset the counter after it.
	if _, err := engine.CheckAndUpdateRuntimeStatus(zzVerifyStatusValue()); err != nil {
		t.Fatalf("cycle 1: %v", err)
	}
	counting.runtimeStatusWrites = 0

	churnWrites := 0
	for cycle := 2; cycle <= 6; cycle++ {
		before := counting.runtimeStatusWrites
		if _, err := engine.CheckAndUpdateRuntimeStatus(zzVerifyStatusValue()); err != nil {
			t.Fatalf("cycle %d: %v", cycle, err)
		}
		if counting.runtimeStatusWrites > before {
			churnWrites++
		}
	}

	t.Logf("spurious status writes in cycles 2..6: %d/5 cycles rewrote status with no spec change", churnWrites)
	if churnWrites == 0 {
		t.Fatalf("CANARY FLIPPED: no spurious status writes observed; " +
			"if affinity expressions are now deterministically ordered, invert this canary")
	}
}

// TestVerifyPR6200ConsequenceDeepEqual shows the mechanism directly: two
// consecutive computations of CacheAffinity from the SAME spec can differ
// under reflect.DeepEqual, which is exactly the gate used by
// CheckAndUpdateRuntimeStatus before writing status.
func TestVerifyPR6200ConsequenceDeepEqual(t *testing.T) {
	selector := zzFiveLabelSelector()
	first := kubeclient.MergeNodeSelectorAndNodeAffinity(selector, nil)
	differ := 0
	for i := 0; i < 200; i++ {
		next := kubeclient.MergeNodeSelectorAndNodeAffinity(selector, nil)
		if !reflect.DeepEqual(first, next) {
			differ++
		}
	}
	t.Logf("DeepEqual mismatches across 200 identical computations: %d", differ)
	if differ == 0 {
		t.Log("order stable in this run; nothing to report (canary companion may still hold)")
	}
}
