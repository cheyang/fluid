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

// Reviewer harness for PR #6197 (Claude), round 4. The delta 268012a5..41b47141
// adds a rejection class in checkVolumeNameConflict: a pod volume that already
// uses the config's name and projects the same ConfigMap, but with its own
// items / mode / optional setting, is now rejected instead of silently
// replaced (commit 7f5b625c). These specs pin that contract differentially:
// both were verified RED on the previous head 268012a5 (the volume was
// replaced, the projection dropped — see results/canaries-r4.txt).

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/fluid-cloudnative/fluid/pkg/common"
	webhookutils "github.com/fluid-cloudnative/fluid/pkg/webhook/utils"

	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("Reviewer findings (PR 6197), round 4", func() {
	Context("C-R4: the pod's own volume projects the runtime config ConfigMap non-defaultedly", func() {
		volumeName := common.GetCacheRuntimeConfigConfigMapName("demo")

		It("is rejected instead of being replaced, so the app's projection survives", func() {
			// Contract (bite-verified on 268012a5: the mutation succeeded there
			// and rewrote the volume source, dropping the app's items). The pod
			// must now be rejected naming the volume, and stay byte-identical.
			injector := newInjector(configMapFor("demo"))
			pod := podWithContainers("app")
			volume := buildConfigMount("demo").volume
			volume.ConfigMap.Items = []corev1.KeyToPath{{Key: common.RuntimeConfigShellFileName, Path: "config.sh"}}
			mount := buildConfigMount("demo").volumeMount
			pod.Spec.Volumes = []corev1.Volume{volume}
			pod.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{mount}
			before := pod.DeepCopy()

			shouldStop, err := injector.Mutate(pod, runtimeInfosFor("demo"))

			Expect(shouldStop).To(BeTrue())
			Expect(webhookutils.IsRejectPodError(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring(volumeName))
			Expect(err.Error()).To(ContainSubstring("own items, mode or optional"))
			Expect(pod).To(Equal(before))
		})

		It("still accepts the volume when the non-default-looking fields are explicitly at their defaults", func() {
			// The api server persists DefaultMode=0644; a user (or a re-submitted
			// injected spec) may equally spell out optional: false. Both shapes
			// are the default projection, so idempotent re-mutation keeps
			// working and such a pod is still injected.
			injector := newInjector(configMapFor("demo"))
			pod := podWithContainers("app")
			defaultMode := corev1.ConfigMapVolumeSourceDefaultMode
			notOptional := false
			volume := buildConfigMount("demo").volume
			volume.ConfigMap.DefaultMode = &defaultMode
			volume.ConfigMap.Optional = &notOptional
			mount := buildConfigMount("demo").volumeMount
			pod.Spec.Volumes = []corev1.Volume{volume}
			pod.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{mount}

			_, err := injector.Mutate(pod, runtimeInfosFor("demo"))

			Expect(err).NotTo(HaveOccurred())
			Expect(pod.Spec.Volumes).To(HaveLen(1))
			Expect(pod.Spec.Containers[0].VolumeMounts).To(HaveLen(1))
			Expect(pod.Spec.Containers[0].Env).NotTo(BeEmpty())
		})
	})
})

// A-F6 canary, injector side: a digit-leading dataset name is legal on the
// plugin path (only length and the DNS-1123 volume-name are checked), which is
// exactly the inconsistency with the annotation path's IsDNS1035Label above.
// Asserts the CURRENT behavior; invert if the paths are aligned the other way.
var _ = Describe("Reviewer findings (PR 6197), round 4: digit-leading dataset name on the plugin path", func() {
	It("is injected normally, unlike on the annotation path (A-F6 canary)", func() {
		injector := newInjector(configMapFor("1demo"))
		pod := podWithContainers("app")

		shouldStop, err := injector.Mutate(pod, runtimeInfosFor("1demo"))

		Expect(shouldStop).To(BeFalse())
		Expect(err).NotTo(HaveOccurred())
		Expect(pod.Spec.Volumes[0].Name).To(Equal("fluid-runtime-config-1demo"))
		Expect(pod.Spec.Containers[0].Env[0].Name).To(Equal("FLUID_RUNTIME_CONFIG_PATH_1DEMO"))
	})
})
