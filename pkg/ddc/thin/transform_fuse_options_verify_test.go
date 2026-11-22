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

package thin

// Reviewer verification harness for PR #6274 / issue #6273.
//
// These are CONTRACT tests for the fuse mount option assembly in
// ThinEngine.parseFuseOptions / transformFuse:
//   - they FAIL on the merge-base (896d21e) where `options` was replaced by
//     profile.Spec.Fuse.Options, dropping "ro" for read-only datasets and
//     aliasing the profile's option map;
//   - they PASS on the PR head where profile options are merged in.
//
// Polarity: contract (red = bug reproduced, green = fixed).

import (
	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ThinEngine fuse option verification harness (PR #6274, issue #6273)", Label("pkg.ddc.thin.transform_fuse_options_verify_test.go"), func() {
	var (
		dataset     *datav1alpha1.Dataset
		thinruntime *datav1alpha1.ThinRuntime
		profile     *datav1alpha1.ThinRuntimeProfile
		engine      *ThinEngine
		client      client.Client
		resources   []runtime.Object

		value *ThinValue
	)

	BeforeEach(func() {
		dataset, thinruntime, profile = mockFluidObjectsForTests(types.NamespacedName{Name: "test-dataset", Namespace: "default"})
		engine = mockThinEngineForTests(dataset, thinruntime, profile)
		// The mock dataset has no AccessModes, which GetAccessModesOfDataset
		// defaults to ReadOnlyMany -> "ro" must end up in the fuse options.
		profile.Spec.Fuse.Options = map[string]string{
			"option1": "value1",
			"option2": "value2",
		}
		thinruntime.Spec.Fuse.Options = map[string]string{
			"option3": "value3",
		}
		resources = []runtime.Object{dataset, thinruntime, profile}
		value = &ThinValue{}
	})

	JustBeforeEach(func() {
		client = fake.NewFakeClientWithScheme(datav1alpha1.UnitTestScheme, resources...)
		engine.Client = client
	})

	It("contract: the rendered fuse container MOUNT_OPTIONS keeps ro when the profile defines fuse options", func() {
		err := engine.transformFuse(thinruntime, profile, dataset, value)
		Expect(err).NotTo(HaveOccurred())

		mountOptions := ""
		found := false
		for _, env := range value.Fuse.Envs {
			if env.Name == common.ThinFuseOptionEnvKey {
				found = true
				mountOptions = env.Value
			}
		}
		Expect(found).To(BeTrue(), "MOUNT_OPTIONS env should be rendered for the fuse container")
		Expect(mountOptions).To(MatchRegexp(`(^|,)ro(,|$)`))
		Expect(mountOptions).To(ContainSubstring("option1=value1"))
		Expect(mountOptions).To(ContainSubstring("option2=value2"))
		Expect(mountOptions).To(ContainSubstring("option3=value3"))
	})

	It("contract: runtime fuse options must not be written into the profile's option map", func() {
		_, err := engine.parseFuseOptions(thinruntime, profile, dataset)
		Expect(err).NotTo(HaveOccurred())
		Expect(profile.Spec.Fuse.Options).To(Equal(map[string]string{
			"option1": "value1",
			"option2": "value2",
		}))
	})

	It("contract: profile options are merged, they cannot silently drop ro", func() {
		options, err := engine.parseFuseOptions(thinruntime, profile, dataset)
		Expect(err).NotTo(HaveOccurred())
		Expect(options).To(MatchRegexp(`(^|,)ro(,|$)`))
		Expect(options).To(ContainSubstring("option1=value1"))
		Expect(options).To(ContainSubstring("option3=value3"))
	})

	When("the dataset is writable", func() {
		BeforeEach(func() {
			// Mutated before the fake client is built in JustBeforeEach, so the
			// client-served dataset is ReadWriteMany as well.
			dataset.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany}
		})

		It("contract: no ro is added for a writable dataset with profile options", func() {
			options, err := engine.parseFuseOptions(thinruntime, profile, dataset)
			Expect(err).NotTo(HaveOccurred())
			Expect(options).NotTo(MatchRegexp(`(^|,)ro(,|$)`))
			Expect(options).To(ContainSubstring("option1=value1"))
		})
	})
})
