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

// Verification harness for PR #6200 review (Reviewer B / Codex). Additive only:
// this file does not modify production code. It deliberately uses only helpers
// that already exist on the base branch (pre-PR) so the same file can be
// grafted onto the base commit for premise verification.

package engine

import (
	"context"
	"testing"

	workloadv1alpha1 "github.com/fluid-cloudnative/advanced-statefulset/api/workload/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
)

// verifyGetCountingClient counts client.Get calls against the worker
// AdvancedStatefulSet. Distinct name from the PR's getCallCountingClient so
// this file also compiles on the base branch.
type verifyGetCountingClient struct {
	ctrlclient.Client
	workerGetCount int
}

func (c *verifyGetCountingClient) Get(ctx context.Context, key types.NamespacedName, obj ctrlclient.Object, opts ...ctrlclient.GetOption) error {
	if key.Name == testStatusWorker && key.Namespace == testStatusNamespace {
		c.workerGetCount++
	}
	return c.Client.Get(ctx, key, obj, opts...)
}

func verifyWorkerWithNodeSelector(nodeSelector map[string]string) *workloadv1alpha1.AdvancedStatefulSet {
	sts := newAdvancedStatefulSetComponent(testStatusWorker, testStatusNamespace, 1, 1)
	sts.Spec.Template.Spec.NodeSelector = nodeSelector
	return sts
}

func verifyZoneAffinity(zone string) *corev1.NodeAffinity {
	return &corev1.NodeAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
			NodeSelectorTerms: []corev1.NodeSelectorTerm{
				{
					MatchExpressions: []corev1.NodeSelectorRequirement{
						{
							Key:      "topology.kubernetes.io/zone",
							Operator: corev1.NodeSelectorOpIn,
							Values:   []string{zone},
						},
					},
				},
			},
		},
	}
}

func verifyNodeSelectorTermsContain(affinity *corev1.NodeAffinity, key, value string) bool {
	if affinity == nil || affinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		return false
	}
	for _, term := range affinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
		for _, expr := range term.MatchExpressions {
			if expr.Key != key {
				continue
			}
			for _, v := range expr.Values {
				if v == value {
					return true
				}
			}
		}
	}
	return false
}

// TestVerifyWorkerAffinityFetchCount is the P0 premise probe AND the fix-effect
// check. Two full CheckAndUpdateRuntimeStatus cycles, counting worker
// AdvancedStatefulSet Gets:
//   - base branch: 2 Gets per cycle (ConstructComponentStatus + GetNodeAffinity)
//     -> 4 total. This test FAILS there, and that failure is the premise
//     evidence: the redundant per-cycle fetch exists on base.
//   - PR head: 2 on the first cycle, 1 on the second -> 3 total. PASS = the
//     optimization works as claimed.
func TestVerifyWorkerAffinityFetchCount(t *testing.T) {
	baseClient := fake.NewFakeClientWithScheme(
		CacheEngineTestScheme,
		newStatusTestRuntime(),
		newAdvancedStatefulSetComponent(testStatusMaster, testStatusNamespace, 1, 1),
		verifyWorkerWithNodeSelector(map[string]string{"disktype": "ssd"}),
	)
	counting := &verifyGetCountingClient{Client: baseClient}
	engine, _ := newStatusTestEngineWithClient(counting)

	if _, err := engine.CheckAndUpdateRuntimeStatus(newStatusTestRuntimeValue(false)); err != nil {
		t.Fatalf("cycle 1: %v", err)
	}
	afterCycle1 := counting.workerGetCount

	if _, err := engine.CheckAndUpdateRuntimeStatus(newStatusTestRuntimeValue(false)); err != nil {
		t.Fatalf("cycle 2: %v", err)
	}
	afterCycle2 := counting.workerGetCount
	t.Logf("worker AdvancedStatefulSet client.Get calls: after cycle1=%d after cycle2=%d", afterCycle1, afterCycle2)

	if afterCycle1 != 2 {
		t.Errorf("expected 2 worker Gets in first cycle (construct status + node affinity), got %d", afterCycle1)
	}
	if afterCycle2 != 3 {
		t.Errorf("expected 3 worker Gets after two cycles (affinity fetched once then cached), got %d", afterCycle2)
	}
}

// TestVerifyStaleCacheAffinityAfterWorkloadChange is the F1 contract test:
// CacheRuntime.status.cacheAffinity should reflect the worker workload's
// CURRENT node affinity. Scenario: status.cacheAffinity already holds affinity
// A (e.g. from an earlier generation of the workload); the worker
// AdvancedStatefulSet has since been re-created / edited with nodeSelector
// disktype=ssd (affinity B). One status cycle should propagate B.
//   - base branch: PASS (affinity re-fetched every cycle).
//   - PR head: FAIL (status.cacheAffinity stays A forever; also observed by
//     the nodeaffinitywithcache webhook when injecting pod affinity).
func TestVerifyStaleCacheAffinityAfterWorkloadChange(t *testing.T) {
	rt := newStatusTestRuntime()
	rt.Status.CacheAffinity = verifyZoneAffinity("zone-a") // stale affinity A

	baseClient := fake.NewFakeClientWithScheme(
		CacheEngineTestScheme,
		rt,
		newAdvancedStatefulSetComponent(testStatusMaster, testStatusNamespace, 1, 1),
		verifyWorkerWithNodeSelector(map[string]string{"disktype": "ssd"}), // workload now carries affinity B
	)
	engine, client := newStatusTestEngineWithClient(baseClient)

	if _, err := engine.CheckAndUpdateRuntimeStatus(newStatusTestRuntimeValue(false)); err != nil {
		t.Fatalf("cycle: %v", err)
	}

	updated := &datav1alpha1.CacheRuntime{}
	if err := client.Get(context.TODO(), types.NamespacedName{Name: testStatusRuntime, Namespace: testStatusNamespace}, updated); err != nil {
		t.Fatalf("get runtime: %v", err)
	}

	if !verifyNodeSelectorTermsContain(updated.Status.CacheAffinity, "disktype", "ssd") {
		t.Errorf("F1 reproduced: status.cacheAffinity did not track the worker workload affinity change; got %+v, expected it to contain disktype=ssd",
			updated.Status.CacheAffinity)
	}
}
