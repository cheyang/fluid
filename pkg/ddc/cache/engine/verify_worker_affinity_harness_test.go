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

// Verification harness — Reviewer A (Claude) — PR #6200
// https://github.com/fluid-cloudnative/fluid/pull/6200
//
// This file is part of the review verification harness
// (docs/verification/cache-worker-affinity-single-read/). It is additive and
// intentionally compiles against BOTH the merge-base d8b37f28 and the PR head
// so the same commands can be run on each:
//
//   - TestVerifyWorkerGetCountPerStatusCycle is a CONTRACT test for the PR's
//     claim ("exactly 1 worker Get per status cycle"). On the merge-base it
//     must FAIL with observed workerGetCount == 2 — that failure is the P0
//     premise reproduction (the duplicate read the PR removes) and doubles as
//     the harness-bites check: the test is red on unfixed code for the right
//     reason.
//   - TestVerifyWorkerAffinityMergedFromSelectorAndAffinity is a CONTRACT
//     test for behavior preservation: CacheAffinity must still merge the
//     worker pod template's nodeSelector AND affinity.NodeAffinity. The PR's
//     own tests only exercise the nodeSelector path.
//   - TestVerifyWorkerMissingErrorPropagation is a CONTRACT test that a
//     missing worker workload still fails the status cycle.

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
)

// verifyCountingClient wraps a client and counts Get calls that target the
// worker component's name. It is a harness-local duplicate of the counting
// wrapper the PR added to status_test.go, kept under its own name so this file
// grafts cleanly onto both base and PR-head trees.
type verifyCountingClient struct {
	ctrlclient.Client
	workerGetCount int
}

func (c *verifyCountingClient) Get(ctx context.Context, key types.NamespacedName, obj ctrlclient.Object, opts ...ctrlclient.GetOption) error {
	if key.Name == testStatusWorker {
		c.workerGetCount++
	}
	return c.Client.Get(ctx, key, obj, opts...)
}

func verifyGetRuntime(t *testing.T, client ctrlclient.Client) *datav1alpha1.CacheRuntime {
	t.Helper()
	runtime := &datav1alpha1.CacheRuntime{}
	if err := client.Get(context.TODO(), types.NamespacedName{Name: testStatusRuntime, Namespace: testStatusNamespace}, runtime); err != nil {
		t.Fatalf("failed to get updated runtime: %v", err)
	}
	return runtime
}

// TestVerifyWorkerGetCountPerStatusCycle asserts the PR's core claim: one
// worker AdvancedStatefulSet Get per CheckAndUpdateRuntimeStatus cycle.
// Expected: 1 per cycle. On the merge-base this observes 2 (premise P0).
func TestVerifyWorkerGetCountPerStatusCycle(t *testing.T) {
	baseClient := fake.NewFakeClientWithScheme(
		CacheEngineTestScheme,
		newStatusTestRuntime(),
		newAdvancedStatefulSetComponent(testStatusMaster, testStatusNamespace, 1, 1),
		newAdvancedStatefulSetComponent(testStatusWorker, testStatusNamespace, 1, 1),
	)
	counting := &verifyCountingClient{Client: baseClient}
	engine, client := newStatusTestEngineWithClient(counting)

	ready, err := engine.CheckAndUpdateRuntimeStatus(newStatusTestRuntimeValue(false))
	if err != nil {
		t.Fatalf("first status cycle failed: %v", err)
	}
	if !ready {
		t.Fatal("expected runtime to be ready after first status cycle")
	}
	if counting.workerGetCount != 1 {
		t.Errorf("contract: expected exactly 1 worker Get in first status cycle, observed %d", counting.workerGetCount)
	}

	updated := verifyGetRuntime(t, client)
	if updated.Status.CacheAffinity == nil {
		t.Error("contract: expected status.CacheAffinity to be set after status cycle")
	}

	// Second cycle: still exactly one Get for the worker.
	ready, err = engine.CheckAndUpdateRuntimeStatus(newStatusTestRuntimeValue(false))
	if err != nil {
		t.Fatalf("second status cycle failed: %v", err)
	}
	if !ready {
		t.Fatal("expected runtime to be ready after second status cycle")
	}
	if counting.workerGetCount != 2 {
		t.Errorf("contract: expected exactly 1 worker Get per status cycle (2 total), observed %d", counting.workerGetCount)
	}
}

// TestVerifyWorkerAffinityMergedFromSelectorAndAffinity pins affinity
// semantics: nodeSelector entries and an existing
// affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution term
// must BOTH appear in status.CacheAffinity, merged into the same term
// (MergeNodeSelectorAndNodeAffinity appends selector expressions to each
// existing term). This path (affinity from pod Affinity) is not covered by
// the PR's own tests, which only set nodeSelector.
func TestVerifyWorkerAffinityMergedFromSelectorAndAffinity(t *testing.T) {
	worker := newAdvancedStatefulSetComponent(testStatusWorker, testStatusNamespace, 1, 1)
	worker.Spec.Template.Spec.NodeSelector = map[string]string{"disktype": "ssd"}
	worker.Spec.Template.Spec.Affinity = &corev1.Affinity{
		NodeAffinity: &corev1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{
					{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{Key: "topology.kubernetes.io/zone", Operator: corev1.NodeSelectorOpIn, Values: []string{"zone-a"}},
						},
					},
				},
			},
		},
	}

	baseClient := fake.NewFakeClientWithScheme(
		CacheEngineTestScheme,
		newStatusTestRuntime(),
		newAdvancedStatefulSetComponent(testStatusMaster, testStatusNamespace, 1, 1),
		worker,
	)
	engine, client := newStatusTestEngineWithClient(baseClient)

	ready, err := engine.CheckAndUpdateRuntimeStatus(newStatusTestRuntimeValue(false))
	if err != nil {
		t.Fatalf("status cycle failed: %v", err)
	}
	if !ready {
		t.Fatal("expected runtime to be ready after status cycle")
	}

	updated := verifyGetRuntime(t, client)
	affinity := updated.Status.CacheAffinity
	if affinity == nil {
		t.Fatal("contract: expected status.CacheAffinity to be set")
	}
	terms := affinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	if len(terms) != 1 {
		t.Fatalf("contract: expected 1 merged NodeSelectorTerm, observed %d", len(terms))
	}

	exprs := map[string]string{}
	for _, expr := range terms[0].MatchExpressions {
		exprs[expr.Key] = expr.Values[0]
	}
	if v, ok := exprs["topology.kubernetes.io/zone"]; !ok || v != "zone-a" {
		t.Errorf("contract: expected merged term to keep zone-a from worker pod affinity, got %v", exprs)
	}
	if v, ok := exprs["disktype"]; !ok || v != "ssd" {
		t.Errorf("contract: expected merged term to include nodeSelector disktype=ssd, got %v", exprs)
	}
}

// TestVerifyWorkerMissingErrorPropagation: when the worker workload does not
// exist, the status cycle must return an error and report not-ready (the
// merged single read must not swallow the Get error).
func TestVerifyWorkerMissingErrorPropagation(t *testing.T) {
	baseClient := fake.NewFakeClientWithScheme(
		CacheEngineTestScheme,
		newStatusTestRuntime(),
		newAdvancedStatefulSetComponent(testStatusMaster, testStatusNamespace, 1, 1),
		// worker intentionally not created
	)
	engine, _ := newStatusTestEngineWithClient(baseClient)

	ready, err := engine.CheckAndUpdateRuntimeStatus(newStatusTestRuntimeValue(false))
	if err == nil {
		t.Error("contract: expected an error when the worker component is missing")
	}
	if ready {
		t.Error("contract: expected ready=false when the worker component is missing")
	}
}
