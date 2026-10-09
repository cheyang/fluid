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

package component

// Verification harness — Reviewer A (Claude) — PR #6200
// https://github.com/fluid-cloudnative/fluid/pull/6200
//
// Component-level CONTRACT tests for ConstructComponentStatusAndAffinity.
// This file targets the PR head (the method does not exist on the
// merge-base, so it is only grafted onto PR head / later refs).
//
//   - TestVerifyAffinityIsDeepCopied: because the affinity is now derived
//     from the same cached workload object the status read returns, the
//     returned *corev1.NodeAffinity must be a deep copy — mutating it must
//     not corrupt the next call's result or the stored workload. (Red
//     condition: aliasing between the informer-cache object and
//     status.CacheAffinity.)
//   - TestVerifyDaemonSetAffinityIsDeepCopied: same for DaemonSetManager.

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	workloadv1alpha1 "github.com/fluid-cloudnative/advanced-statefulset/api/workload/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func verifySetupClient() ctrlclient.Client {
	scheme := runtime.NewScheme()
	_ = workloadv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	return fake.NewFakeClientWithScheme(scheme)
}

func verifyZoneAffinity() *corev1.Affinity {
	return &corev1.Affinity{
		NodeAffinity: &corev1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{
					{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{Key: "topology.kubernetes.io/zone", Operator: corev1.NodeSelectorOpIn, Values: []string{"zone-a"}},
						},
					},
				},
			},
		},
	}
}

// TestVerifyAffinityIsDeepCopied: two calls must return independent affinity
// objects, and mutating the first result must not affect the stored workload.
func TestVerifyAffinityIsDeepCopied(t *testing.T) {
	client := verifySetupClient()

	asts := &workloadv1alpha1.AdvancedStatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "verify-runtime-worker", Namespace: "fluid"},
		Spec: workloadv1alpha1.AdvancedStatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					NodeSelector: map[string]string{"disktype": "ssd"},
					Affinity:     verifyZoneAffinity(),
				},
			},
		},
	}
	if err := client.Create(context.TODO(), asts); err != nil {
		t.Fatalf("failed to create worker AdvancedStatefulSet: %v", err)
	}

	manager := newAdvancedStatefulSetManager(client)
	identity := &common.ComponentIdentity{Name: "verify-runtime-worker", Namespace: "fluid"}

	_, first, err := manager.ConstructComponentStatusAndAffinity(context.TODO(), identity)
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	_, second, err := manager.ConstructComponentStatusAndAffinity(context.TODO(), identity)
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}

	// Mutate the first result: append an expression to its first term.
	if len(first.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms) == 0 {
		t.Fatal("expected at least one NodeSelectorTerm in derived affinity")
	}
	first.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions = append(
		first.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions,
		corev1.NodeSelectorRequirement{Key: "harness", Operator: corev1.NodeSelectorOpIn, Values: []string{"mutated"}},
	)

	// The second, independently derived affinity must not see the mutation.
	for _, term := range second.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
		for _, expr := range term.MatchExpressions {
			if expr.Key == "harness" {
				t.Error("contract: mutating the first returned affinity leaked into a later derivation (not a deep copy)")
			}
		}
	}

	// The stored workload must not be corrupted either.
	stored := &workloadv1alpha1.AdvancedStatefulSet{}
	if err := client.Get(context.TODO(), types.NamespacedName{Name: "verify-runtime-worker", Namespace: "fluid"}, stored); err != nil {
		t.Fatalf("failed to re-read worker AdvancedStatefulSet: %v", err)
	}
	for _, term := range stored.Spec.Template.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
		for _, expr := range term.MatchExpressions {
			if expr.Key == "harness" {
				t.Error("contract: mutating the returned affinity corrupted the stored workload (aliasing)")
			}
		}
	}
}

// TestVerifyDaemonSetAffinityIsDeepCopied: same no-aliasing property for the
// DaemonSetManager implementation of ConstructComponentStatusAndAffinity.
func TestVerifyDaemonSetAffinityIsDeepCopied(t *testing.T) {
	client := verifySetupClient()

	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "verify-runtime-client", Namespace: "fluid"},
		Spec: appsv1.DaemonSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					NodeSelector: map[string]string{"disktype": "ssd"},
					Affinity:     verifyZoneAffinity(),
				},
			},
		},
	}
	if err := client.Create(context.TODO(), ds); err != nil {
		t.Fatalf("failed to create client DaemonSet: %v", err)
	}

	manager := newDaemonSetManager(client)
	identity := &common.ComponentIdentity{Name: "verify-runtime-client", Namespace: "fluid"}

	_, first, err := manager.ConstructComponentStatusAndAffinity(context.TODO(), identity)
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	_, second, err := manager.ConstructComponentStatusAndAffinity(context.TODO(), identity)
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}

	if len(first.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms) == 0 {
		t.Fatal("expected at least one NodeSelectorTerm in derived affinity")
	}
	first.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions = append(
		first.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions,
		corev1.NodeSelectorRequirement{Key: "harness", Operator: corev1.NodeSelectorOpIn, Values: []string{"mutated"}},
	)

	for _, term := range second.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
		for _, expr := range term.MatchExpressions {
			if expr.Key == "harness" {
				t.Error("contract: mutating the first returned affinity leaked into a later derivation (not a deep copy)")
			}
		}
	}

	stored := &appsv1.DaemonSet{}
	if err := client.Get(context.TODO(), types.NamespacedName{Name: "verify-runtime-client", Namespace: "fluid"}, stored); err != nil {
		t.Fatalf("failed to re-read client DaemonSet: %v", err)
	}
	for _, term := range stored.Spec.Template.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
		for _, expr := range term.MatchExpressions {
			if expr.Key == "harness" {
				t.Error("contract: mutating the returned affinity corrupted the stored workload (aliasing)")
			}
		}
	}
}
