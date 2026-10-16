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

package runtimeconfiginjector

// Reviewer harness for PR #6197 (Claude). Bug-canaries for two findings; see
// docs/verification/clientless-runtime-inject/README.md for the full table.
// Both tests encode the CURRENT behavior: a fix must flip them (and they then
// need to be inverted into contract tests).

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/ddc/base"
	webhookutils "github.com/fluid-cloudnative/fluid/pkg/webhook/utils"
	"k8s.io/apimachinery/pkg/util/validation"
)

var _ = Describe("Reviewer findings (PR 6197)", func() {
	Context("F1: a dataset of a fuse-served runtime is listed in fluid.io/datasets", func() {
		It("is reported as a configmap that may not be ready yet, which it can never be", func() {
			// Canary: the annotation path does not check the runtime type, so an
			// Alluxio/JuiceFS/... dataset resolves fine and then fails on a configmap
			// that only cache runtimes ever create. The pod is denied (after the API
			// reader retry) with a message suggesting the runtime is "not ready yet",
			// which tells the user to wait for a state that will never happen.
			injector := newInjector() // no configmap for any dataset
			runtimeInfo, err := base.BuildRuntimeInfo("alluxio-demo", testNamespace, common.AlluxioRuntime)
			Expect(err).NotTo(HaveOccurred())

			pod := podWithContainers("app")
			shouldStop, err := injector.Mutate(pod, map[string]base.RuntimeInfoInterface{"alluxio-demo": runtimeInfo})

			Expect(shouldStop).To(BeTrue())
			Expect(err).To(HaveOccurred())
			Expect(webhookutils.IsNeedRetryWithApiReaderError(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("may not be ready yet"))
			// the pod itself stays untouched
			Expect(pod.Spec.Volumes).To(BeEmpty())
		})
	})

	Context("F2: a cache dataset whose name is not a DNS-1035 label arrives via a mounted PVC", func() {
		It("injects a volume name the API server rejects", func() {
			// Canary: CollectRuntimeInfosFromAnnotations rejects non-DNS-1035 dataset
			// names, but a dataset resolved from a mounted PVC (the merge path in
			// MutatePod) is not validated. "a.b" is a legal object name, so a CacheRuntime
			// dataset with that name reaches the plugin and produces volume
			// "fluid-runtime-config-a.b", which fails volume-name validation at the API
			// server with an error that does not point at the injection.
			injector := newInjector(configMapFor("a.b"))
			runtimeInfo, err := base.BuildRuntimeInfo("a.b", testNamespace, common.CacheRuntime)
			Expect(err).NotTo(HaveOccurred())

			pod := podWithContainers("app")
			shouldStop, err := injector.Mutate(pod, map[string]base.RuntimeInfoInterface{"a.b": runtimeInfo})

			Expect(shouldStop).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(pod.Spec.Volumes).To(HaveLen(1))
			// the gap: the injected volume name is not a valid DNS-1123 label
			Expect(pod.Spec.Volumes[0].Name).To(Equal("fluid-runtime-config-a.b"))
			Expect(validation.IsDNS1123Label(pod.Spec.Volumes[0].Name)).NotTo(BeEmpty())
		})
	})
})
