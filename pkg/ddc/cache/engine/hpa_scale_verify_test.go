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

// ADDITIVE verification harness for PR #6223 (Reviewer B / Codex).
// Production code is untouched. These are plain go tests (not Ginkgo specs) so
// they can be filtered with -run 'TestVerifyHPA' and grafted onto the BASE
// branch for the premise check (P0): on base, CacheEngine never sets
// Status.Selector, so the contract tests below must FAIL there and PASS on the
// PR head.
//
// Polarity: all tests in this file are CONTRACT tests (assert intended correct
// behavior): red on the buggy/unpatched code, green once the PR behavior is
// present.

package engine

import (
	"context"
	"strings"
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/ddc/cache/component"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"

	workloadv1alpha1 "github.com/fluid-cloudnative/advanced-statefulset/api/workload/v1alpha1"
)

func verifyHPAGetRuntime(t *testing.T, engine *CacheEngine) *datav1alpha1.CacheRuntime {
	t.Helper()
	runtime := &datav1alpha1.CacheRuntime{}
	if err := engine.Client.Get(context.TODO(), types.NamespacedName{Name: testStatusRuntime, Namespace: testStatusNamespace}, runtime); err != nil {
		t.Fatalf("failed to read back CacheRuntime: %v", err)
	}
	return runtime
}

// F-A2 (contract): the selector written into status must match the labels that
// the REAL worker creation path (component manager Reconciler) puts on the
// worker pods. This cross-checks pkg/ddc/cache/engine against
// pkg/ddc/cache/component through the actual AdvancedStatefulSet creation path,
// not a duplicated label map.
func TestVerifyHPASelectorMatchesCreatedWorkerPods(t *testing.T) {
	client := fake.NewFakeClientWithScheme(CacheEngineTestScheme)
	engine, _ := newStatusTestEngineWithClient(client)

	workerValue := &common.CacheRuntimeComponentValue{
		Name:          common.GetCacheComponentName(testStatusRuntime, common.ComponentTypeWorker),
		Namespace:     testStatusNamespace,
		Enabled:       true,
		Replicas:      2,
		ComponentType: common.ComponentTypeWorker,
		Owner: &common.OwnerReference{
			APIVersion: "data.fluid.io/v1alpha1",
			Kind:       "CacheRuntime",
			Name:       testStatusRuntime,
			UID:        "verify-uid",
		},
		PodTemplateSpec: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "worker", Image: "example/worker:v1"}},
			},
		},
	}

	manager := component.NewComponentHelper(common.ComponentTypeWorker, engine.Client)
	if err := manager.Reconciler(context.TODO(), workerValue); err != nil {
		t.Fatalf("worker Reconciler (creation path) failed: %v", err)
	}

	asts := &workloadv1alpha1.AdvancedStatefulSet{}
	if err := engine.Client.Get(context.TODO(), types.NamespacedName{Name: workerValue.Name, Namespace: testStatusNamespace}, asts); err != nil {
		t.Fatalf("failed to read created worker AdvancedStatefulSet: %v", err)
	}
	podLabels := asts.Spec.Template.Labels
	if len(podLabels) == 0 {
		t.Fatalf("created worker pod template carries no labels at all")
	}

	selectorStr, err := engine.getWorkerSelectors()
	if err != nil {
		t.Fatalf("getWorkerSelectors returned error: %v", err)
	}
	selector, err := labels.Parse(selectorStr)
	if err != nil {
		t.Fatalf("status selector %q is not a parseable label selector: %v", selectorStr, err)
	}
	if !selector.Matches(labels.Set(podLabels)) {
		t.Fatalf("status selector %q does NOT match the worker pod labels %v produced by the creation path", selectorStr, podLabels)
	}

	// guard against a too-broad selector: it must not match pods of another
	// runtime with the same component type
	otherRuntimeLabels := component.GetComponentMatchLabels("some-other-runtime", "some-other-runtime-worker")
	if selector.Matches(labels.Set(otherRuntimeLabels)) {
		t.Fatalf("status selector %q unexpectedly matches another runtime's worker pods %v", selectorStr, otherRuntimeLabels)
	}
}

// F-A3 (contract on head behavior / documents the disabled-worker edge):
// when the worker component is disabled (e.g. P2P topologies),
// CheckAndUpdateRuntimeStatus currently still stamps a worker selector that
// matches no pods. This pins the current behavior so a future change (e.g.
// leaving selector empty when no worker exists) is a deliberate decision.
func TestVerifyHPASelectorBehaviorWhenWorkerDisabled(t *testing.T) {
	client := fake.NewFakeClientWithScheme(
		CacheEngineTestScheme,
		newStatusTestRuntime(),
		newAdvancedStatefulSetComponent(testStatusMaster, testStatusNamespace, 1, 1),
		// no worker workload exists: worker is disabled
	)
	engine, _ := newStatusTestEngineWithClient(client)

	value := newStatusTestRuntimeValue(false)
	value.Worker.Enabled = false

	_, err := engine.CheckAndUpdateRuntimeStatus(value)
	if err != nil {
		t.Fatalf("CheckAndUpdateRuntimeStatus returned error: %v", err)
	}

	runtime := verifyHPAGetRuntime(t, engine)
	t.Logf("observed status.selector with disabled worker: %q", runtime.Status.Selector)
	if runtime.Status.Selector == "" {
		t.Fatalf("on the PR head the selector is expected to be populated even with the worker disabled (pinned current behavior); got empty")
	}
	if !strings.Contains(runtime.Status.Selector, testStatusWorker) {
		t.Fatalf("expected selector to reference the (nonexistent) worker component %q, got %q", testStatusWorker, runtime.Status.Selector)
	}
}

// F-A4 (contract): the scale subresource's statusReplicasPath is
// .status.worker.currentReplicas; prove that a status sync populates that path
// from the worker workload, so the API server can serve it through /scale.
//
// Harness-update (round 4, head 99e35af0): constructStatus now sources
// CurrentReplicas from asts.Status.Replicas (all created pods) instead of
// asts.Status.CurrentReplicas (current revision only). The PR's engine-level
// fixture newAdvancedStatefulSetComponent was NOT updated and still only sets
// Status.CurrentReplicas, leaving Status.Replicas=0 - a status combination the
// real ASTS controller can never produce (updateStatus accumulates Replicas
// over every created pod, so Replicas >= CurrentReplicas always). The fixture
// is therefore corrected here locally instead of reusing the stale helper.
func TestVerifyHPAStatusReplicasPathPopulated(t *testing.T) {
	worker := newAdvancedStatefulSetComponent(testStatusWorker, testStatusNamespace, 3, 2)
	worker.Status.Replicas = 3 // total created pods; real ASTS invariant Replicas >= CurrentReplicas
	master := newAdvancedStatefulSetComponent(testStatusMaster, testStatusNamespace, 1, 1)
	master.Status.Replicas = 1
	client := fake.NewFakeClientWithScheme(
		CacheEngineTestScheme,
		newStatusTestRuntime(),
		master,
		worker,
	)
	engine, _ := newStatusTestEngineWithClient(client)

	_, err := engine.CheckAndUpdateRuntimeStatus(newStatusTestRuntimeValue(false))
	if err != nil {
		t.Fatalf("CheckAndUpdateRuntimeStatus returned error: %v", err)
	}

	runtime := verifyHPAGetRuntime(t, engine)
	if runtime.Status.Worker.CurrentReplicas != 3 {
		t.Fatalf("status.worker.currentReplicas mismatch: got %d, want 3 (scale subresource statusReplicasPath would be wrong)", runtime.Status.Worker.CurrentReplicas)
	}
	if runtime.Status.Worker.DesiredReplicas != 3 {
		t.Fatalf("status.worker.desiredReplicas mismatch: got %d, want 3", runtime.Status.Worker.DesiredReplicas)
	}
}

// C4420756561 fix verification (contract, differential): during a mixed-
// revision rolling update, asts.Status.CurrentReplicas counts only the
// current-revision pods and underreports the workers; the scale subresource's
// statusReplicasPath must expose ALL observed pods (asts.Status.Replicas),
// matching native StatefulSet scale semantics and pkg/ctrl/worker.go:128
// (CurrentWorkerNumberScheduled = workers.Status.Replicas) for the existing
// runtimes. FAILS on the previous head 9a265eb3 (which read CurrentReplicas),
// PASSES on 99e35af0.
func TestVerifyHPAMixedRevisionReplicasCount(t *testing.T) {
	newWorker := func(replicas, current, available, ready int32) *workloadv1alpha1.AdvancedStatefulSet {
		w := newAdvancedStatefulSetComponent(testStatusWorker, testStatusNamespace, 3, ready)
		w.Status.Replicas = replicas
		w.Status.CurrentReplicas = current
		w.Status.UpdatedReplicas = replicas - current
		w.Status.AvailableReplicas = available
		return w
	}

	cases := []struct {
		name                string
		replicas, current   int32
		available, ready    int32
		wantCurrentReplicas int32
		wantUnavailable     int32
	}{
		// mid-rollout: 2 pods on currentRevision, 1 on updateRevision
		{name: "mixed revisions count all pods", replicas: 3, current: 2, available: 3, ready: 3, wantCurrentReplicas: 3, wantUnavailable: 0},
		// scale-down in flight: one terminating pod still counted in Replicas,
		// unavailable = replicas - available (matches pkg/ctrl/worker.go:131)
		{name: "terminating pod counted while draining", replicas: 4, current: 3, available: 3, ready: 3, wantCurrentReplicas: 4, wantUnavailable: 1},
		// pod created but not yet available: counted, and shows as unavailable
		{name: "not-yet-available pod counted", replicas: 3, current: 3, available: 2, ready: 2, wantCurrentReplicas: 3, wantUnavailable: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			master := newAdvancedStatefulSetComponent(testStatusMaster, testStatusNamespace, 1, 1)
			master.Status.Replicas = 1
			client := fake.NewFakeClientWithScheme(
				CacheEngineTestScheme,
				newStatusTestRuntime(),
				master,
				newWorker(tc.replicas, tc.current, tc.available, tc.ready),
			)
			engine, _ := newStatusTestEngineWithClient(client)

			if _, err := engine.CheckAndUpdateRuntimeStatus(newStatusTestRuntimeValue(false)); err != nil {
				t.Fatalf("CheckAndUpdateRuntimeStatus returned error: %v", err)
			}
			runtime := verifyHPAGetRuntime(t, engine)
			if runtime.Status.Worker.CurrentReplicas != tc.wantCurrentReplicas {
				t.Fatalf("status.worker.currentReplicas = %d, want %d (all observed pods, revision-agnostic)", runtime.Status.Worker.CurrentReplicas, tc.wantCurrentReplicas)
			}
			if runtime.Status.Worker.UnavailableReplicas != tc.wantUnavailable {
				t.Fatalf("status.worker.unavailableReplicas = %d, want %d", runtime.Status.Worker.UnavailableReplicas, tc.wantUnavailable)
			}
		})
	}
}
