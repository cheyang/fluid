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

// Reviewer harness for PR #6197 (Claude), round 3. These specs started as
// bug-canaries at 6d3a6d7 and FLIPPED at head 268012a, so they are inverted
// into contract tests of the fixed behavior (see
// docs/verification/clientless-runtime-inject/README.md for the table):
//
//   - A-F1: a dataset of a non-cache runtime is rejected naming the runtime
//     type instead of "may not be ready yet";
//   - A-F2: a PVC-resolved dataset name that cannot form a valid volume name
//     is rejected before the pod is touched;
//   - A-N1: only fluid's own fuse sidecars (fluid-fuse / fluid-fuse-<index>)
//     are skipped on injection, not any container whose name starts with the
//     prefix.

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/ddc/base"
	webhookutils "github.com/fluid-cloudnative/fluid/pkg/webhook/utils"
)

var _ = Describe("Reviewer findings (PR 6197)", func() {
	Context("A-F1: a dataset of a fuse-served runtime is listed in fluid.io/datasets", func() {
		It("is rejected naming the runtime type, not as a config that may not be ready yet", func() {
			// Contract (inverted from the round-2 canary): the plugin checks the
			// runtime type, so an Alluxio/JuiceFS/... dataset resolves fine and is
			// then rejected with a message that names the type and tells the user
			// to mount the PVC instead of waiting for a ConfigMap that a
			// fuse-served runtime never creates.
			injector := newInjector() // no configmap for any dataset
			runtimeInfo, err := base.BuildRuntimeInfo("alluxio-demo", testNamespace, common.AlluxioRuntime)
			Expect(err).NotTo(HaveOccurred())

			pod := podWithContainers("app")
			shouldStop, err := injector.Mutate(pod, map[string]base.RuntimeInfoInterface{"alluxio-demo": runtimeInfo})

			Expect(shouldStop).To(BeTrue())
			Expect(err).To(HaveOccurred())
			Expect(webhookutils.IsRejectPodError(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring(`served by a "alluxio" runtime`))
			Expect(err.Error()).To(ContainSubstring("mount the dataset through its PVC"))
			// the misleading wait-for-read message is gone
			Expect(err.Error()).NotTo(ContainSubstring("may not be ready yet"))
			// the pod itself stays untouched
			Expect(pod.Spec.Volumes).To(BeEmpty())
		})
	})

	Context("A-F2: a cache dataset whose name is not a DNS-1123 label arrives via a mounted PVC", func() {
		It("is rejected before the pod is touched, instead of injecting an API-server-invalid volume", func() {
			// Contract (inverted from the round-2 canary): the annotation path
			// rejects non-DNS-1035 dataset names up front; a dataset resolved
			// from a mounted PVC (the merge path in MutatePod) reaches the
			// plugin unchecked. "a.b" is a legal object name, so a CacheRuntime
			// dataset with that name must be rejected at admission, not turned
			// into volume "fluid-runtime-config-a.b" that the API server
			// rejects with an error that never points at fluid.
			injector := newInjector(configMapFor("a.b"))
			runtimeInfo, err := base.BuildRuntimeInfo("a.b", testNamespace, common.CacheRuntime)
			Expect(err).NotTo(HaveOccurred())

			pod := podWithContainers("app")
			shouldStop, err := injector.Mutate(pod, map[string]base.RuntimeInfoInterface{"a.b": runtimeInfo})

			Expect(shouldStop).To(BeTrue())
			Expect(err).To(HaveOccurred())
			Expect(webhookutils.IsRejectPodError(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring(`cannot form the volume name "fluid-runtime-config-a.b"`))
			// the pod itself stays untouched
			Expect(pod.Spec.Volumes).To(BeEmpty())
		})
	})

	Context("A-N1: an app container's name merely starts like a fuse sidecar's", func() {
		It("is injected into, while fluid's own sidecars are still skipped", func() {
			// Contract: the skip matches only the names fluid generates
			// (fluid-fuse from the injection template, fluid-fuse-<index> from
			// the per-PVC index), so an app container named fluid-fuse-proxy
			// must receive the volume mount and env var.
			injector := newInjector(configMapFor("demo"))
			pod := podWithContainers(common.FuseContainerName+"-proxy", common.FuseContainerName+"-0")

			_, err := injector.Mutate(pod, runtimeInfosFor("demo"))
			Expect(err).NotTo(HaveOccurred())

			Expect(pod.Spec.Containers[0].VolumeMounts).To(HaveLen(1))
			Expect(pod.Spec.Containers[1].VolumeMounts).To(BeEmpty())
		})
	})
})
