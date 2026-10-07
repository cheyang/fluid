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

/*
Verification harness for PR #6183, finding F2: the doc claim
"if the replica count exceeds the number of schedulable nodes, the surplus
Worker Pods stay Pending" holds only under the default (Exclusive) dataset
placement, where worker pods carry a REQUIRED hostname anti-affinity against
same-dataset pods. Under Shared placement there is no such required term for
same-dataset workers, so surplus workers can co-locate on one node.

Polarity: contract (documents the mechanism the doc statement depends on).
*/

package engine

import (
	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/ddc/base"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("CacheEngine worker scheduling anti-affinity per placement mode (verification harness for PR #6183)", Label("pkg.ddc.cache.engine.worker_affinity_placement_verify_test.go"), func() {
	// buildAffinityFor runs the same wiring the worker transform path uses
	// (SetupWithDataset reads dataset.Spec.PlacementMode, defaulting "" to Exclusive).
	buildAffinityFor := func(placement datav1alpha1.PlacementMode) corev1.Affinity {
		dataset := &datav1alpha1.Dataset{
			ObjectMeta: metav1.ObjectMeta{Name: "test-runtime", Namespace: "default"},
			Spec:       datav1alpha1.DatasetSpec{PlacementMode: placement},
		}
		runtimeInfo, err := base.BuildRuntimeInfo("test-runtime", "default", "cache")
		Expect(err).NotTo(HaveOccurred())
		runtimeInfo.SetupWithDataset(dataset)

		engine := &CacheEngine{Log: ctrl.Log.WithName("verify-test")}
		affinity := &corev1.Affinity{}
		engine.buildWorkerAffinity(affinity, dataset, runtimeInfo)
		return *affinity
	}

	// requiredTermsAgainstDatasetPods returns the required anti-affinity terms that
	// match pods of THIS dataset (i.e. that forbid same-dataset workers from
	// co-locating on one node).
	requiredTermsAgainstDatasetPods := func(affinity corev1.Affinity) []corev1.PodAffinityTerm {
		var terms []corev1.PodAffinityTerm
		if affinity.PodAntiAffinity == nil {
			return terms
		}
		for _, term := range affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution {
			if term.TopologyKey != common.K8sNodeNameLabelKey {
				continue
			}
			for _, req := range term.LabelSelector.MatchExpressions {
				if req.Key == common.LabelAnnotationDataset {
					terms = append(terms, term)
				}
			}
		}
		return terms
	}

	It("should forbid same-dataset workers from co-locating under the default (Exclusive) placement", func() {
		affinity := buildAffinityFor(datav1alpha1.DefaultMode) // "" -> treated as Exclusive
		Expect(requiredTermsAgainstDatasetPods(affinity)).NotTo(BeEmpty())
	})

	It("should allow same-dataset workers to co-locate under Shared placement", func() {
		affinity := buildAffinityFor(datav1alpha1.ShareMode)
		Expect(requiredTermsAgainstDatasetPods(affinity)).To(BeEmpty())
	})
})
