/*
Copyright 2024 The Fluid Authors.

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

// This file is an ADDITIVE verification harness for the review of
// https://github.com/fluid-cloudnative/fluid/pull/6064 — it modifies no production code.
// See docs/verification/cache-engine-selector/.
//
// FINDING 1 (contract, expect PASS on PR head), worker-pod side:
// This pins the label scheme that CacheRuntime worker pods actually carry, i.e.
// getCommonLabelsFromComponent for the worker component. The engine-package test
// (engine/selector_verify_test.go: TestVerifyWorkerSelectorMatchesWorkerLabels)
// asserts getWorkerSelectors() matches this exact scheme. Together the two prove the
// selector really targets worker pods. If the labeling here changes without the
// selector changing (or vice-versa), one of the two tests goes red.

package component

import (
	"testing"

	"github.com/fluid-cloudnative/fluid/pkg/common"
)

func TestVerifyWorkerCommonLabelScheme(t *testing.T) {
	const runtimeName = "test-runtime"
	workerComponentName := common.GetCacheComponentName(runtimeName, common.ComponentTypeWorker)

	component := &common.CacheRuntimeComponentValue{
		Name:          workerComponentName,
		ComponentType: common.ComponentTypeWorker,
		Owner:         &common.OwnerReference{Name: runtimeName},
	}

	got := getCommonLabelsFromComponent(component)

	want := map[string]string{
		common.LabelCacheRuntimeName:          runtimeName,
		common.LabelCacheRuntimeComponentName: workerComponentName,
	}

	if len(got) != len(want) {
		t.Fatalf("worker common labels have unexpected size: got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("worker label %q = %q, want %q (full got=%v). "+
				"The engine getWorkerSelectors() must be kept in sync with this scheme.", k, got[k], v, got)
		}
	}
}
