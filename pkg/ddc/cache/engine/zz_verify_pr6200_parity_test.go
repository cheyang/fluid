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

// Parity harness for PR #6200 (head-only: uses ConstructComponentStatusAndAffinity,
// which does not exist on the merge-base).
//
// Claim: the combined single-read method returns
//   (a) exactly the status that ConstructComponentStatus returns, and
//   (b) exactly the affinity that the removed GetNodeAffinity computed
//       (= kubeclient.MergeNodeSelectorAndNodeAffinity over the same object).
// Polarity: contract.

package engine

import (
	"context"
	"reflect"
	"sort"
	"testing"

	workloadv1alpha1 "github.com/fluid-cloudnative/advanced-statefulset/api/workload/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/ddc/cache/component"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	"github.com/fluid-cloudnative/fluid/pkg/utils/kubeclient"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

// zzSortedExprs copies a NodeAffinity and sorts every MatchExpressions slice so
// comparisons are insensitive to the (pre-existing, F3) map-iteration order
// instability inside MergeNodeSelectorAndNodeAffinity.
func zzSortedExprs(a *corev1.NodeAffinity) *corev1.NodeAffinity {
	if a == nil {
		return nil
	}
	out := a.DeepCopy()
	if req := out.RequiredDuringSchedulingIgnoredDuringExecution; req != nil {
		for i := range req.NodeSelectorTerms {
			exprs := req.NodeSelectorTerms[i].MatchExpressions
			sort.Slice(exprs, func(p, q int) bool {
				if exprs[p].Key != exprs[q].Key {
					return exprs[p].Key < exprs[q].Key
				}
				if exprs[p].Operator != exprs[q].Operator {
					return exprs[p].Operator < exprs[q].Operator
				}
				if len(exprs[p].Values) != len(exprs[q].Values) {
					return len(exprs[p].Values) < len(exprs[q].Values)
				}
				for k := range exprs[p].Values {
					if exprs[p].Values[k] != exprs[q].Values[k] {
						return exprs[p].Values[k] < exprs[q].Values[k]
					}
				}
				return false
			})
		}
	}
	return out
}

func TestVerifyPR6200ParityCombinedMatchesLegacyOutputs(t *testing.T) {
	scheme := zzVerifyScheme(t)

	workerASTS := zzVerifyASTS(zzVerifyWorker, map[string]string{
		"disktype": "ssd",
		"zone":     "hz",
	})
	workerASTS.Spec.Template.Spec.Affinity = &corev1.Affinity{
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
					Weight: 10,
					Preference: corev1.NodeSelectorTerm{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{Key: "rack", Operator: corev1.NodeSelectorOpIn, Values: []string{"rack-1"}},
						},
					},
				},
			},
		},
	}
	workerASTS.Spec.Replicas = ptr.To(int32(3))
	workerASTS.Status.ReadyReplicas = 2
	workerASTS.Status.CurrentReplicas = 3
	workerASTS.Status.AvailableReplicas = 2

	c := fake.NewFakeClientWithScheme(scheme, workerASTS.DeepCopy())
	manager := component.NewComponentHelper(common.ComponentTypeWorker, c)
	identity := &common.ComponentIdentity{Name: zzVerifyWorker, Namespace: zzVerifyNS}
	ctx := context.TODO()

	// New combined single-read path (under review).
	gotStatus, gotAffinity, err := manager.ConstructComponentStatusAndAffinity(ctx, identity)
	if err != nil {
		t.Fatalf("ConstructComponentStatusAndAffinity: %v", err)
	}

	// Legacy reference path: status from ConstructComponentStatus, affinity the
	// way the removed GetNodeAffinity computed it (fresh Get + merge).
	wantStatus, err := manager.ConstructComponentStatus(ctx, identity)
	if err != nil {
		t.Fatalf("ConstructComponentStatus: %v", err)
	}
	fresh := &workloadv1alpha1.AdvancedStatefulSet{}
	if err := c.Get(ctx, types.NamespacedName{Name: zzVerifyWorker, Namespace: zzVerifyNS}, fresh); err != nil {
		t.Fatalf("get asts: %v", err)
	}
	wantAffinity := kubeclient.MergeNodeSelectorAndNodeAffinity(
		fresh.Spec.Template.Spec.NodeSelector, fresh.Spec.Template.Spec.Affinity)

	if !reflect.DeepEqual(gotStatus, wantStatus) {
		t.Fatalf("status mismatch:\n got: %+v\nwant: %+v", gotStatus, wantStatus)
	}
	// Compare affinity order-insensitively (map iteration order is unstable, see F3).
	if !reflect.DeepEqual(zzSortedExprs(gotAffinity), zzSortedExprs(wantAffinity)) {
		t.Fatalf("affinity mismatch:\n got: %+v\nwant: %+v", gotAffinity, wantAffinity)
	}

	// Sanity: the derived affinity must carry both the pod-affinity term and the
	// nodeSelector labels.
	req := gotAffinity.RequiredDuringSchedulingIgnoredDuringExecution
	if req == nil || len(req.NodeSelectorTerms) != 1 {
		t.Fatalf("expected exactly 1 required node selector term, got %+v", req)
	}
	keys := map[string]bool{}
	for _, e := range req.NodeSelectorTerms[0].MatchExpressions {
		keys[e.Key] = true
	}
	for _, k := range []string{"kubernetes.io/os", "disktype", "zone"} {
		if !keys[k] {
			t.Fatalf("expected merged affinity to contain key %q, got keys %v", k, keys)
		}
	}
	if len(gotAffinity.PreferredDuringSchedulingIgnoredDuringExecution) != 1 {
		t.Fatalf("expected preferred terms to be preserved, got %+v", gotAffinity.PreferredDuringSchedulingIgnoredDuringExecution)
	}
}
