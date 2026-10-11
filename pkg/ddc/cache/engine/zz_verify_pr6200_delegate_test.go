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

// Delegation harness for PR #6200 round 4 (delta commit 0182b4f1).
//
// Claim: after the delta, ConstructComponentStatus is a pure delegate of
// ConstructComponentStatusAndAffinity for BOTH managers. It must return
// exactly the same status and the same error as the combined method on:
//   (a) the happy path (workload present, with nodeSelector + affinity), and
//   (b) the error path (workload not found),
// for the AdvancedStatefulSet manager (master/worker) and the DaemonSet
// manager (client).
// Polarity: contract — FAILS if the delegate drifts from the combined method.

package engine

import (
	"context"
	"reflect"
	"testing"

	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/ddc/cache/component"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestVerifyPR6200DelegateConstructComponentStatus(t *testing.T) {
	scheme := zzVerifyScheme(t)
	ctx := context.TODO()

	t.Run("asts manager happy path", func(t *testing.T) {
		asts := zzVerifyASTS(zzVerifyWorker, map[string]string{"disktype": "ssd", "zone": "hz"})
		asts.Spec.Template.Spec.Affinity = &corev1.Affinity{
			NodeAffinity: &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{Key: "kubernetes.io/os", Operator: corev1.NodeSelectorOpIn, Values: []string{"linux"}},
						},
					}},
				},
			},
		}
		asts.Spec.Replicas = ptr.To(int32(3))
		asts.Status.ReadyReplicas = 2
		asts.Status.CurrentReplicas = 3
		asts.Status.AvailableReplicas = 2

		c := fake.NewFakeClientWithScheme(scheme, asts.DeepCopy())
		manager := component.NewComponentHelper(common.ComponentTypeWorker, c)
		id := &common.ComponentIdentity{Name: zzVerifyWorker, Namespace: zzVerifyNS}

		gotStatus, gotErr := manager.ConstructComponentStatus(ctx, id)
		wantStatus, _, wantErr := manager.ConstructComponentStatusAndAffinity(ctx, id)
		if (gotErr == nil) != (wantErr == nil) {
			t.Fatalf("error mismatch: delegate err=%v, combined err=%v", gotErr, wantErr)
		}
		if !reflect.DeepEqual(gotStatus, wantStatus) {
			t.Fatalf("status mismatch:\n got: %+v\nwant: %+v", gotStatus, wantStatus)
		}
	})

	t.Run("asts manager not-found path", func(t *testing.T) {
		c := fake.NewFakeClientWithScheme(scheme)
		manager := component.NewComponentHelper(common.ComponentTypeWorker, c)
		id := &common.ComponentIdentity{Name: "absent", Namespace: zzVerifyNS}

		gotStatus, gotErr := manager.ConstructComponentStatus(ctx, id)
		wantStatus, gotAffinity, wantErr := manager.ConstructComponentStatusAndAffinity(ctx, id)
		if gotErr == nil || wantErr == nil {
			t.Fatalf("expected both to fail on missing workload, got delegate err=%v combined err=%v", gotErr, wantErr)
		}
		if gotAffinity != nil {
			t.Fatalf("combined method must return nil affinity on error, got %+v", gotAffinity)
		}
		if !reflect.DeepEqual(gotStatus, wantStatus) {
			t.Fatalf("error-path status mismatch:\n got: %+v\nwant: %+v", gotStatus, wantStatus)
		}
	})

	t.Run("daemonset manager happy path", func(t *testing.T) {
		ds := &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Name: zzVerifyClient, Namespace: zzVerifyNS},
			Spec: appsv1.DaemonSetSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						NodeSelector: map[string]string{"disktype": "ssd"},
						Containers:   []corev1.Container{{Name: "c", Image: "i"}},
					},
				},
			},
			Status: appsv1.DaemonSetStatus{
				DesiredNumberScheduled: 3,
				CurrentNumberScheduled: 3,
				NumberReady:            2,
				NumberAvailable:        2,
				NumberUnavailable:      1,
			},
		}
		c := fake.NewFakeClientWithScheme(scheme, ds.DeepCopy())
		manager := component.NewComponentHelper(common.ComponentTypeClient, c)
		id := &common.ComponentIdentity{Name: zzVerifyClient, Namespace: zzVerifyNS}

		gotStatus, gotErr := manager.ConstructComponentStatus(ctx, id)
		wantStatus, _, wantErr := manager.ConstructComponentStatusAndAffinity(ctx, id)
		if (gotErr == nil) != (wantErr == nil) {
			t.Fatalf("error mismatch: delegate err=%v, combined err=%v", gotErr, wantErr)
		}
		if !reflect.DeepEqual(gotStatus, wantStatus) {
			t.Fatalf("status mismatch:\n got: %+v\nwant: %+v", gotStatus, wantStatus)
		}
		if gotStatus.CurrentReplicas != 3 || gotStatus.ReadyReplicas != 2 || gotStatus.UnavailableReplicas != 1 {
			t.Fatalf("daemonset status fields not propagated through delegate: %+v", gotStatus)
		}
	})

	t.Run("daemonset manager not-found path", func(t *testing.T) {
		c := fake.NewFakeClientWithScheme(scheme)
		manager := component.NewComponentHelper(common.ComponentTypeClient, c)
		id := &common.ComponentIdentity{Name: "absent", Namespace: zzVerifyNS}

		_, gotErr := manager.ConstructComponentStatus(ctx, id)
		_, _, wantErr := manager.ConstructComponentStatusAndAffinity(ctx, id)
		if gotErr == nil || wantErr == nil {
			t.Fatalf("expected both to fail on missing workload, got delegate err=%v combined err=%v", gotErr, wantErr)
		}
	})
}
