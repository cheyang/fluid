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

// Reviewer verification harness (Reviewer A) for PR #6223
// "feat(cache): support HPA on CacheRuntime via scale subresource and status selector".
//
// F1 / P0 (contract, layer: unit): CheckAndUpdateRuntimeStatus must populate
// Status.Selector with the serialized label selector of the worker component.
// On the PR base (fe074af) this test FAILS because Status.Selector is never
// set — that failure is the reproduction of issue #6063 ("CacheEngine does not
// populate Status.Selector, breaking HPA and worker pod discovery").
//
// This file deliberately only uses symbols that also exist on the base branch
// so it can be grafted onto the base for the differential (premise) run.

import (
	"context"
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	"k8s.io/apimachinery/pkg/types"
)

func TestHarnessStatusSelectorPopulated(t *testing.T) {
	client := fake.NewFakeClientWithScheme(
		CacheEngineTestScheme,
		newStatusTestRuntime(),
		newAdvancedStatefulSetComponent(testStatusMaster, testStatusNamespace, 1, 1),
		newAdvancedStatefulSetComponent(testStatusWorker, testStatusNamespace, 1, 1),
	)
	engine := &CacheEngine{
		Client:    client,
		name:      testStatusRuntime,
		namespace: testStatusNamespace,
		Log:       fake.NullLogger(),
	}

	_, err := engine.CheckAndUpdateRuntimeStatus(newStatusTestRuntimeValue(false))
	if err != nil {
		t.Fatalf("F1/P0: CheckAndUpdateRuntimeStatus returned error: %v", err)
	}

	updated := &datav1alpha1.CacheRuntime{}
	if err := client.Get(context.TODO(), types.NamespacedName{Name: testStatusRuntime, Namespace: testStatusNamespace}, updated); err != nil {
		t.Fatalf("F1/P0: failed to get updated runtime: %v", err)
	}

	want := common.LabelCacheRuntimeComponentName + "=" + testStatusWorker + "," +
		common.LabelCacheRuntimeName + "=" + testStatusRuntime
	if updated.Status.Selector == "" {
		t.Errorf("F1/P0: Status.Selector is empty after CheckAndUpdateRuntimeStatus; want %q", want)
	}
	if updated.Status.Selector != want {
		t.Errorf("F1/P0: Status.Selector = %q, want %q", updated.Status.Selector, want)
	}
}
