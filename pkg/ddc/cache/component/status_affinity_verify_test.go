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

// Reviewer verification harness for PR #6200 (branch verify/cache-worker-affinity-single-read-claude).
// Additive only: production code untouched. HEAD-ONLY: exercises ConstructComponentStatusAndAffinity,
// which does not exist on the base merge-base; re-verify.sh grafts this onto fixed PR heads.
//
// Claims (all contract polarity):
//   C1: successive ConstructComponentStatusAndAffinity calls return deep-independent affinity
//       objects (mutating one result cannot corrupt a later cycle).
//   C2: the returned affinity does not alias the workload object stored in the API server
//       (mutating the result does not rewrite the workload's pod template).
//   C3: for both managers the derived affinity equals kubeclient.MergeNodeSelectorAndNodeAffinity
//       computed on the workload's pod template — parity with the removed GetNodeAffinity.

import (
	"context"

	workloadv1alpha1 "github.com/fluid-cloudnative/advanced-statefulset/api/workload/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	"github.com/fluid-cloudnative/fluid/pkg/utils/kubeclient"
)

func verifyAffinityTestClient() ctrlclient.Client {
	scheme := runtime.NewScheme()
	_ = workloadv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	return fake.NewFakeClientWithScheme(scheme)
}

func verifyAffinityPodTemplate() corev1.PodTemplateSpec {
	return corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			NodeSelector: map[string]string{"disktype": "ssd"},
			Affinity: &corev1.Affinity{
				NodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{{
							MatchExpressions: []corev1.NodeSelectorRequirement{{
								Key: "zone", Operator: corev1.NodeSelectorOpIn, Values: []string{"zone-a"},
							}},
						}},
					},
				},
			},
		},
	}
}

var _ = Describe("Verification: combined status+affinity derivation (PR #6200)", func() {
	ctx := context.Background()

	It("C1+C2+C3: ASTS manager returns independent, non-aliasing, semantically-equal affinity", func() {
		c := verifyAffinityTestClient()
		manager := newAdvancedStatefulSetManager(c)

		replicas := int32(1)
		asts := &workloadv1alpha1.AdvancedStatefulSet{
			ObjectMeta: metav1.ObjectMeta{Name: "verify-worker", Namespace: "fluid"},
			Spec: workloadv1alpha1.AdvancedStatefulSetSpec{
				Replicas: &replicas,
				Template: verifyAffinityPodTemplate(),
			},
		}
		Expect(c.Create(ctx, asts)).To(Succeed())

		identity := &common.ComponentIdentity{Name: "verify-worker", Namespace: "fluid"}

		_, affinity1, err := manager.ConstructComponentStatusAndAffinity(ctx, identity)
		Expect(err).NotTo(HaveOccurred())
		_, affinity2, err := manager.ConstructComponentStatusAndAffinity(ctx, identity)
		Expect(err).NotTo(HaveOccurred())

		// C1: fresh objects per call — mutating the first must not leak into the second.
		affinity1.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions[0].Key = "corrupted"
		Expect(affinity2.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions[0].Key).To(Equal("zone"))
		Expect(affinity2).NotTo(Equal(affinity1))

		// C2: no aliasing with the stored workload.
		affinity2.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions[0].Key = "corrupted"
		fetched := &workloadv1alpha1.AdvancedStatefulSet{}
		Expect(c.Get(ctx, types.NamespacedName{Name: "verify-worker", Namespace: "fluid"}, fetched)).To(Succeed())
		Expect(fetched.Spec.Template.Spec.NodeSelector).To(Equal(map[string]string{"disktype": "ssd"}))

		// C3: parity with the removed GetNodeAffinity semantics.
		expected := kubeclient.MergeNodeSelectorAndNodeAffinity(
			fetched.Spec.Template.Spec.NodeSelector, fetched.Spec.Template.Spec.Affinity)
		_, affinity3, err := manager.ConstructComponentStatusAndAffinity(ctx, identity)
		Expect(err).NotTo(HaveOccurred())
		Expect(affinity3).To(Equal(expected))
	})

	It("C3: DaemonSet manager derives the same affinity the removed GetNodeAffinity produced", func() {
		c := verifyAffinityTestClient()
		manager := newDaemonSetManager(c)

		ds := &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Name: "verify-client", Namespace: "fluid"},
			Spec:       appsv1.DaemonSetSpec{Template: verifyAffinityPodTemplate()},
		}
		Expect(c.Create(ctx, ds)).To(Succeed())

		identity := &common.ComponentIdentity{Name: "verify-client", Namespace: "fluid"}
		_, affinity, err := manager.ConstructComponentStatusAndAffinity(ctx, identity)
		Expect(err).NotTo(HaveOccurred())

		fetched := &appsv1.DaemonSet{}
		Expect(c.Get(ctx, types.NamespacedName{Name: "verify-client", Namespace: "fluid"}, fetched)).To(Succeed())
		expected := kubeclient.MergeNodeSelectorAndNodeAffinity(
			fetched.Spec.Template.Spec.NodeSelector, fetched.Spec.Template.Spec.Affinity)
		Expect(affinity).To(Equal(expected))
	})
})
