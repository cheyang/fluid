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

// Reviewer harness (round 3, PR #6223). CONTRACT TEST for finding
// C4420756561-currentreplicas-undercount: during a rolling update,
// asts.Status.CurrentReplicas counts only pods on the current revision, so it
// undercounts the worker fleet; the value exposed through
// RuntimeComponentStatus.CurrentReplicas (which feeds
// .status.worker.currentReplicas, i.e. the scale subresource's
// statusReplicasPath and the HPA's observed metric) must count ALL observed
// pods (asts.Status.Replicas), matching pkg/ctrl/worker.go:128
// (CurrentWorkerNumberScheduled = workers.Status.Replicas) for the existing
// runtimes.
//
// Polarity: contract. FAILS on code that exposes the revision-scoped
// CurrentReplicas (previous head 9a265eb3); PASSES on the fixed head.
package component

import (
	"context"
	"testing"

	workloadv1alpha1 "github.com/fluid-cloudnative/advanced-statefulset/api/workload/v1alpha1"
	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestHarnessConstructStatusCountsAllObservedPods(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = workloadv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	client := fake.NewFakeClientWithScheme(scheme)
	manager := newAdvancedStatefulSetManager(client)
	ctx := context.Background()

	replicas := int32(3)
	// mid-rolling-update statefulset: 3 observed pods total, only 2 still on
	// the current revision, 1 already on the update revision, all available.
	asts := &workloadv1alpha1.AdvancedStatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "harness-runtime-worker",
			Namespace: "fluid",
		},
		Spec: workloadv1alpha1.AdvancedStatefulSetSpec{
			Replicas: &replicas,
		},
		Status: workloadv1alpha1.AdvancedStatefulSetStatus{
			Replicas:          3,
			CurrentReplicas:   2,
			UpdatedReplicas:   1,
			ReadyReplicas:     3,
			AvailableReplicas: 3,
			CurrentRevision:   "rev-1",
			UpdateRevision:    "rev-2",
		},
	}
	if err := client.Create(ctx, asts); err != nil {
		t.Fatalf("failed to create ASTS: %v", err)
	}

	status, err := manager.ConstructComponentStatus(ctx, &common.ComponentIdentity{
		Name:      "harness-runtime-worker",
		Namespace: "fluid",
	})
	if err != nil {
		t.Fatalf("ConstructComponentStatus returned error: %v", err)
	}

	if status.CurrentReplicas != 3 {
		t.Errorf("CurrentReplicas = %d, want 3 (all observed pods, not the revision-scoped count 2)", status.CurrentReplicas)
	}
	if status.UnavailableReplicas != 0 {
		t.Errorf("UnavailableReplicas = %d, want 0 (observed - available = 3 - 3)", status.UnavailableReplicas)
	}
	if status.DesiredReplicas != 3 {
		t.Errorf("DesiredReplicas = %d, want 3", status.DesiredReplicas)
	}
	if status.Phase != datav1alpha1.RuntimePhaseReady {
		t.Errorf("Phase = %q, want %q", status.Phase, datav1alpha1.RuntimePhaseReady)
	}
}

func TestHarnessConstructStatusUnavailableFromObserved(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = workloadv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	client := fake.NewFakeClientWithScheme(scheme)
	manager := newAdvancedStatefulSetManager(client)
	ctx := context.Background()

	replicas := int32(3)
	// 4 observed pods (scale-up in flight), 2 available, 2 on current revision:
	// unavailable must be derived from the OBSERVED count (4-2=2), not from the
	// revision-scoped count.
	asts := &workloadv1alpha1.AdvancedStatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "harness-runtime-worker",
			Namespace: "fluid",
		},
		Spec: workloadv1alpha1.AdvancedStatefulSetSpec{
			Replicas: &replicas,
		},
		Status: workloadv1alpha1.AdvancedStatefulSetStatus{
			Replicas:          4,
			CurrentReplicas:   2,
			UpdatedReplicas:   0,
			ReadyReplicas:     2,
			AvailableReplicas: 2,
		},
	}
	if err := client.Create(ctx, asts); err != nil {
		t.Fatalf("failed to create ASTS: %v", err)
	}

	status, err := manager.ConstructComponentStatus(ctx, &common.ComponentIdentity{
		Name:      "harness-runtime-worker",
		Namespace: "fluid",
	})
	if err != nil {
		t.Fatalf("ConstructComponentStatus returned error: %v", err)
	}

	if status.CurrentReplicas != 4 {
		t.Errorf("CurrentReplicas = %d, want 4", status.CurrentReplicas)
	}
	if status.UnavailableReplicas != 2 {
		t.Errorf("UnavailableReplicas = %d, want 2 (observed 4 - available 2)", status.UnavailableReplicas)
	}
}
