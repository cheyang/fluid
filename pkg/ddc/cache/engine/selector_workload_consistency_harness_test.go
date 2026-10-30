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

// Reviewer verification harness (Reviewer A) for PR #6223 — part 2.
//
// F1b (contract, layer: unit): the selector string written to Status.Selector
// (and exposed to the HPA through the CRD labelSelectorPath) must actually
// select the pods of the worker workload that the creation path renders. It is
// built by CacheEngine.getWorkerSelectors() from the same labels the component
// manager stamps on the AdvancedStatefulSet, so this guards against future
// drift between the two derivations.
//
// F2 (canary, layer: unit): documents the CURRENT behavior —
// CheckAndUpdateRuntimeStatus populates Status.Selector even when the worker
// component is disabled (value.Worker.Enabled == false). The selector then
// matches no pods. This canary PASSES while that behavior is present; if the
// behavior is intentionally changed (e.g. leave the selector empty when there
// is no worker), this test FLIPS TO FAIL and must be inverted / promoted to a
// contract test.

import (
	"context"
	"testing"

	workloadv1alpha1 "github.com/fluid-cloudnative/advanced-statefulset/api/workload/v1alpha1"
	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/ddc/cache/component"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const harnessRuntime = "harness-runtime"

func newHarnessEngine(client ctrlclient.Client) *CacheEngine {
	return &CacheEngine{
		Client:    client,
		name:      harnessRuntime,
		namespace: "default",
		Log:       fake.NullLogger(),
	}
}

// F1b: status selector must match the worker workload's selector and pod labels.
func TestHarnessSelectorMatchesWorkerWorkload(t *testing.T) {
	client := fake.NewFakeClientWithScheme(CacheEngineTestScheme)
	engine := newHarnessEngine(client)

	selectorStr, err := engine.getWorkerSelectors()
	if err != nil {
		t.Fatalf("F1b: getWorkerSelectors returned error: %v", err)
	}
	selector, err := labels.Parse(selectorStr)
	if err != nil {
		t.Fatalf("F1b: status selector %q is not a parseable label selector: %v", selectorStr, err)
	}

	// create the worker workload through the real creation path
	workerValue := &common.CacheRuntimeComponentValue{
		Name:            common.GetCacheComponentName(harnessRuntime, common.ComponentTypeWorker),
		Namespace:       "default",
		Enabled:         true,
		Replicas:        1,
		ComponentType:   common.ComponentTypeWorker,
		PodTemplateSpec: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "c", Image: "img"}}}},
		Owner: &common.OwnerReference{
			APIVersion: "data.fluid.io/v1alpha1",
			Kind:       "CacheRuntime",
			Name:       harnessRuntime,
			UID:        "test-uid",
		},
	}
	manager := component.NewComponentHelper(common.ComponentTypeWorker, client)
	if err := manager.Reconciler(context.TODO(), workerValue); err != nil {
		t.Fatalf("F1b: failed to create worker workload: %v", err)
	}

	asts := &workloadv1alpha1.AdvancedStatefulSet{}
	if err := client.Get(context.TODO(), types.NamespacedName{Name: workerValue.Name, Namespace: "default"}, asts); err != nil {
		t.Fatalf("F1b: failed to get worker workload: %v", err)
	}

	// the workload's own selector must match the selector exposed for HPA
	if !selector.Matches(labels.Set(asts.Spec.Selector.MatchLabels)) {
		t.Errorf("F1b: status selector %q does not match workload spec.selector %v", selectorStr, asts.Spec.Selector.MatchLabels)
	}
	// the pods the workload creates must carry labels matching the HPA selector
	if !selector.Matches(labels.Set(asts.Spec.Template.Labels)) {
		t.Errorf("F1b: status selector %q does not match worker pod labels %v", selectorStr, asts.Spec.Template.Labels)
	}
}

// F2 (canary): selector is currently set even when the worker component is disabled.
func TestHarnessSelectorSetEvenWhenWorkerDisabled(t *testing.T) {
	client := fake.NewFakeClientWithScheme(
		CacheEngineTestScheme,
		newStatusTestRuntime(),
		newAdvancedStatefulSetComponent(testStatusMaster, testStatusNamespace, 1, 1),
		// worker workload intentionally absent: worker component disabled
	)
	engine := newHarnessEngine(client)
	engine.name = testStatusRuntime
	engine.namespace = testStatusNamespace

	value := newStatusTestRuntimeValue(false)
	value.Worker.Enabled = false

	_, err := engine.CheckAndUpdateRuntimeStatus(value)
	if err != nil {
		t.Fatalf("F2: CheckAndUpdateRuntimeStatus returned error: %v", err)
	}

	updated := &datav1alpha1.CacheRuntime{}
	if err := client.Get(context.TODO(), types.NamespacedName{Name: testStatusRuntime, Namespace: testStatusNamespace}, updated); err != nil {
		t.Fatalf("F2: failed to get updated runtime: %v", err)
	}

	if updated.Status.Selector == "" {
		t.Errorf("F2 canary flipped: Status.Selector is empty for a worker-disabled runtime; " +
			"if leaving it empty is now the intended behavior, invert this test into a contract test")
	}
}
