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

// Verification harness for PR #6200 review (Reviewer B / Codex). PR-head only:
// references the unexported cacheAffinity field introduced by the PR, so it
// does not compile on the base branch.

package engine

import (
	"testing"

	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
)

// TestVerifyCacheAffinityPointerAliasing documents F2: in the fetch branch,
// engine.cacheAffinity and status.CacheAffinity alias the same
// *corev1.NodeAffinity, while the other two branches DeepCopy. Today nothing
// mutates either reference after assignment, so this is a latent fragility,
// not a live bug.
func TestVerifyCacheAffinityPointerAliasing(t *testing.T) {
	baseClient := fake.NewFakeClientWithScheme(
		CacheEngineTestScheme,
		newStatusTestRuntime(),
		newAdvancedStatefulSetComponent(testStatusMaster, testStatusNamespace, 1, 1),
		verifyWorkerWithNodeSelector(map[string]string{"disktype": "ssd"}),
	)
	engine, _ := newStatusTestEngineWithClient(baseClient)

	status := newStatusTestRuntime().Status.DeepCopy()
	info := newStatusTestComponentStatusInfo(common.ComponentTypeWorker, testStatusWorker)
	if _, err := engine.setWorkerComponentStatus(info, status); err != nil {
		t.Fatalf("setWorkerComponentStatus: %v", err)
	}
	if engine.cacheAffinity == nil || status.CacheAffinity == nil {
		t.Fatalf("expected both engine cache and status affinity to be set")
	}
	if engine.cacheAffinity != status.CacheAffinity {
		// Bug-canary semantics: this FAILS once the aliasing is removed
		// (e.g. the fetch branch starts DeepCopy-ing like the other two).
		t.Errorf("aliasing gone: engine.cacheAffinity and status.CacheAffinity are distinct pointers now; invert this canary")
		return
	}

	// Same pointer: a mutation through one reference is visible through the other.
	engine.cacheAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions[0].Key = "mutated-by-engine"
	got := status.CacheAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions[0].Key
	if got != "mutated-by-engine" {
		t.Errorf("expected aliasing to make the mutation visible via status.CacheAffinity, got key %q", got)
	}
	t.Logf("F2 confirmed: engine.cacheAffinity and status.CacheAffinity alias the same object in the fetch branch")
}
