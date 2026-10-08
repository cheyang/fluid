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

// Verification harness for PR #6200 (reviewer artifact; additive, test-only).
//
// P0 premise claim (issue #5879, https://github.com/fluid-cloudnative/fluid/issues/5879):
// "GetNodeAffinity calls kubeclient.GetStatefulSet for every status update cycle"
// i.e. on the base branch the worker workload is read TWICE per
// CheckAndUpdateRuntimeStatus cycle (once in ConstructComponentStatus, once in
// GetNodeAffinity).
//
// Polarity: CONTRACT — asserts the intended behavior (exactly 1 worker workload
// Get per status cycle). On the BASE branch this test FAILS with observed=2 per
// cycle (that failure IS the P0 confirmation); on the PR head it PASSES.
// This file intentionally uses only symbols that exist on the base branch so it
// compiles and runs there unchanged.

import (
	"context"
	"testing"

	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// verifyWorkerGetCountingClient counts Gets of the worker component object.
type verifyWorkerGetCountingClient struct {
	ctrlclient.Client
	workerGets int
}

func (c *verifyWorkerGetCountingClient) Get(ctx context.Context, key types.NamespacedName, obj ctrlclient.Object, opts ...ctrlclient.GetOption) error {
	if key.Name == testStatusWorker && key.Namespace == testStatusNamespace {
		c.workerGets++
	}
	return c.Client.Get(ctx, key, obj, opts...)
}

func TestVerifyWorkerGetCountPerStatusCycle(t *testing.T) {
	base := fake.NewFakeClientWithScheme(
		CacheEngineTestScheme,
		newStatusTestRuntime(),
		newAdvancedStatefulSetComponent(testStatusMaster, testStatusNamespace, 1, 1),
		newAdvancedStatefulSetComponent(testStatusWorker, testStatusNamespace, 1, 1),
	)
	counting := &verifyWorkerGetCountingClient{Client: base}
	engine, _ := newStatusTestEngineWithClient(counting)

	for cycle := 1; cycle <= 2; cycle++ {
		ready, err := engine.CheckAndUpdateRuntimeStatus(newStatusTestRuntimeValue(false))
		if err != nil {
			t.Fatalf("cycle %d: CheckAndUpdateRuntimeStatus returned error: %v", cycle, err)
		}
		if !ready {
			t.Fatalf("cycle %d: expected runtime ready", cycle)
		}
		if counting.workerGets != cycle {
			t.Fatalf("cycle %d: expected exactly %d worker workload Get(s) (1 per status cycle), observed %d",
				cycle, cycle, counting.workerGets)
		}
	}
	t.Logf("observed exactly 1 worker workload Get per status cycle across 2 cycles")
}
