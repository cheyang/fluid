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

package utils

// Reviewer harness for PR #6197 (Claude), round 3. Contract test for finding
// B-F5: the over-long/invalid dataset-name denial on the annotation path must
// carry the reason, not only the dataset name, so the docs' claim ("rejected,
// with the reason in the error") holds on the primary path too. Round 2
// observed the generic aggregate message on 6d3a6d7; fixed by commit c01cbcce
// at head 268012a.

import (
	"strings"

	"github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/ddc/base"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var _ = Describe("Reviewer findings (PR 6197): annotation name denials carry the reason", func() {
	const namespace = "default"

	collect := func(annotation string, objs ...client.Object) (map[string]base.RuntimeInfoInterface, error) {
		fakeClient := fake.NewClientBuilder().WithScheme(setupScheme()).WithObjects(objs...).Build()
		return CollectRuntimeInfosFromAnnotations(fakeClient,
			map[string]string{common.LabelAnnotationDatasets: annotation},
			namespace, zap.New(zap.UseDevMode(true)), false)
	}

	It("states that an over-long dataset name is too long", func() {
		longName := strings.Repeat("a", common.MaxClientlessDatasetNameLength+1)
		dataset := &v1alpha1.Dataset{
			ObjectMeta: metav1.ObjectMeta{Name: longName, Namespace: namespace},
			Status: v1alpha1.DatasetStatus{
				Phase:    v1alpha1.BoundDatasetPhase,
				Runtimes: []v1alpha1.Runtime{{Name: longName, Namespace: namespace, Type: common.CacheRuntime}},
			},
		}
		runtime := &v1alpha1.CacheRuntime{
			ObjectMeta: metav1.ObjectMeta{Name: longName, Namespace: namespace},
		}

		_, err := collect(longName, dataset, runtime)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(longName))
		// the reason must reach the user, not only the webhook log
		Expect(err.Error()).To(ContainSubstring("dataset name is too long"))
	})

	It("states why a non-DNS-1035 dataset name is invalid", func() {
		_, err := collect("demo.v1")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("demo.v1"))
		Expect(err.Error()).To(ContainSubstring("a DNS-1035 label must consist of"))
	})

	It("still states the bound status for an unbound dataset", func() {
		dataset := &v1alpha1.Dataset{
			ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: namespace},
			Status: v1alpha1.DatasetStatus{
				Phase:    v1alpha1.NotBoundDatasetPhase,
				Runtimes: []v1alpha1.Runtime{{Name: "demo", Namespace: namespace, Type: common.CacheRuntime}},
			},
		}
		_, err := collect("demo", dataset)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("not bound"))
	})
})
