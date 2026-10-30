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

// ADDITIVE verification harness for PR #6223 (Reviewer B / Codex), P0 premise check.
// This file deliberately uses ONLY symbols that already exist on the BASE branch,
// so it can be grafted onto base unmodified:
//   - on BASE:  CheckAndUpdateRuntimeStatus never sets Status.Selector -> FAIL (premise confirmed)
//   - on HEAD:  the PR populates Status.Selector -> PASS
// Polarity: CONTRACT test.

package engine

import (
	"context"
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	"k8s.io/apimachinery/pkg/types"
)

const verifyHPAPremiseExpectedSelector = "cacheruntime.fluid.io/component-name=curvine-demo-worker,cacheruntime.fluid.io/name=curvine-demo"

// P0 premise canary (contract): after a status sync, status.selector must be
// populated with the worker label selector so HPA / pod-discovery tooling can
// find the worker pods (issue #6063).
func TestVerifyHPAPremiseSelectorPopulated(t *testing.T) {
	client := fake.NewFakeClientWithScheme(
		CacheEngineTestScheme,
		newStatusTestRuntime(),
		newAdvancedStatefulSetComponent(testStatusMaster, testStatusNamespace, 1, 1),
		newAdvancedStatefulSetComponent(testStatusWorker, testStatusNamespace, 1, 1),
	)
	engine, _ := newStatusTestEngineWithClient(client)

	ready, err := engine.CheckAndUpdateRuntimeStatus(newStatusTestRuntimeValue(false))
	if err != nil {
		t.Fatalf("CheckAndUpdateRuntimeStatus returned error: %v", err)
	}
	if !ready {
		t.Fatalf("expected runtime to report ready")
	}

	runtime := &datav1alpha1.CacheRuntime{}
	if err := engine.Client.Get(context.TODO(), types.NamespacedName{Name: testStatusRuntime, Namespace: testStatusNamespace}, runtime); err != nil {
		t.Fatalf("failed to read back CacheRuntime: %v", err)
	}
	if runtime.Status.Selector == "" {
		t.Fatalf("PREMISE REPRODUCED: status.selector is empty after status sync; HPA cannot discover worker pods")
	}
	if runtime.Status.Selector != verifyHPAPremiseExpectedSelector {
		t.Fatalf("status.selector mismatch: got %q, want %q", runtime.Status.Selector, verifyHPAPremiseExpectedSelector)
	}
}
