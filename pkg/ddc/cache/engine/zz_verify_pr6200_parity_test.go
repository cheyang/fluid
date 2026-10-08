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
// F-parity claim: the affinity derived by the new single-read path
// (ConstructComponentStatusAndAffinity) must be identical to the one produced by
// the legacy two-read path (ConstructComponentStatus + GetNodeAffinity) for the
// same workload object, including when the pod template carries BOTH a
// nodeSelector and a node affinity (the merge path of
// kubeclient.MergeNodeSelectorAndNodeAffinity).
//
// Polarity: CONTRACT — passes on the PR head when parity holds.
// NOTE: uses the new API, so it only compiles on refs that contain the PR.

import (
	"context"
	"reflect"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	workloadv1alpha1 "github.com/fluid-cloudnative/advanced-statefulset/api/workload/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/ddc/cache/component"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
)

// podSpecWithSelectorAndAffinity carries both a nodeSelector and a node affinity
// (a preferred term and a required term) so the merge logic is fully exercised.
func podSpecWithSelectorAndAffinity() corev1.PodSpec {
	return corev1.PodSpec{
		NodeSelector: map[string]string{
			"disktype":          "ssd",
			"fluid.io/s-worker": "true",
		},
		Affinity: &corev1.Affinity{
			NodeAffinity: &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{
						{
							MatchExpressions: []corev1.NodeSelectorRequirement{
								{Key: "kubernetes.io/os", Operator: corev1.NodeSelectorOpIn, Values: []string{"linux"}},
							},
						},
					},
				},
				PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{
					{
						Weight: 100,
						Preference: corev1.NodeSelectorTerm{
							MatchExpressions: []corev1.NodeSelectorRequirement{
								{Key: "topology.kubernetes.io/zone", Operator: corev1.NodeSelectorOpIn, Values: []string{"zone-a"}},
							},
						},
					},
				},
			},
		},
	}
}

func TestVerifyAffinityParityWithLegacyGetNodeAffinity(t *testing.T) {
	ctx := context.TODO()

	t.Run("AdvancedStatefulSet worker", func(t *testing.T) {
		replicas := int32(2)
		asts := &workloadv1alpha1.AdvancedStatefulSet{
			ObjectMeta: metav1.ObjectMeta{Name: testStatusWorker, Namespace: testStatusNamespace},
			Spec: workloadv1alpha1.AdvancedStatefulSetSpec{
				Replicas: &replicas,
				Template: corev1.PodTemplateSpec{Spec: podSpecWithSelectorAndAffinity()},
			},
			Status: workloadv1alpha1.AdvancedStatefulSetStatus{
				ReadyReplicas:     2,
				CurrentReplicas:   2,
				AvailableReplicas: 2,
			},
		}
		cli := fake.NewFakeClientWithScheme(CacheEngineTestScheme, asts)
		manager := component.NewComponentHelper(common.ComponentTypeWorker, cli)
		identity := &common.ComponentIdentity{Name: testStatusWorker, Namespace: testStatusNamespace}

		newStatus, newAffinity, err := manager.ConstructComponentStatusAndAffinity(ctx, identity)
		if err != nil {
			t.Fatalf("ConstructComponentStatusAndAffinity: %v", err)
		}
		oldStatus, err := manager.ConstructComponentStatus(ctx, identity)
		if err != nil {
			t.Fatalf("ConstructComponentStatus: %v", err)
		}
		oldAffinity, err := manager.GetNodeAffinity(identity)
		if err != nil {
			t.Fatalf("GetNodeAffinity: %v", err)
		}

		if !reflect.DeepEqual(newStatus, oldStatus) {
			t.Fatalf("status mismatch: new=%+v old=%+v", newStatus, oldStatus)
		}
		if !reflect.DeepEqual(newAffinity, oldAffinity) {
			t.Fatalf("affinity mismatch:\nnew=%+v\nold=%+v", newAffinity, oldAffinity)
		}
		if newAffinity == nil || newAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
			t.Fatalf("expected non-nil required node affinity, got %+v", newAffinity)
		}
	})

	t.Run("DaemonSet client", func(t *testing.T) {
		ds := &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Name: testStatusClient, Namespace: testStatusNamespace},
			Spec: appsv1.DaemonSetSpec{
				Template: corev1.PodTemplateSpec{Spec: podSpecWithSelectorAndAffinity()},
			},
			Status: appsv1.DaemonSetStatus{
				DesiredNumberScheduled: 2,
				CurrentNumberScheduled: 2,
				NumberAvailable:        2,
				NumberReady:            2,
			},
		}
		cli := fake.NewFakeClientWithScheme(CacheEngineTestScheme, ds)
		manager := component.NewComponentHelper(common.ComponentTypeClient, cli)
		identity := &common.ComponentIdentity{Name: testStatusClient, Namespace: testStatusNamespace}

		newStatus, newAffinity, err := manager.ConstructComponentStatusAndAffinity(ctx, identity)
		if err != nil {
			t.Fatalf("ConstructComponentStatusAndAffinity: %v", err)
		}
		oldStatus, err := manager.ConstructComponentStatus(ctx, identity)
		if err != nil {
			t.Fatalf("ConstructComponentStatus: %v", err)
		}
		oldAffinity, err := manager.GetNodeAffinity(identity)
		if err != nil {
			t.Fatalf("GetNodeAffinity: %v", err)
		}

		if !reflect.DeepEqual(newStatus, oldStatus) {
			t.Fatalf("status mismatch: new=%+v old=%+v", newStatus, oldStatus)
		}
		if !reflect.DeepEqual(newAffinity, oldAffinity) {
			t.Fatalf("affinity mismatch:\nnew=%+v\nold=%+v", newAffinity, oldAffinity)
		}
	})
}
