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

package engine

// Reviewer verification harness for PR #6186, part 2. These specs exercise symbols
// that only exist on the PR head (desiredComponentVersion, splitImageReference,
// componentTemplateImage), so this file is not compiled on the merge base.
//
// Polarity:
//   - F1b started (round 1) as a canary over the worker/DataLoad image divergence
//     the PR introduced. Commit 99659bf4 aligned image.go, the canary flipped, and
//     it is now inverted into a contract: the worker and the DataLoad image must
//     move together. Red on base and on the round-1 head, green since 99659bf4.
//   - the F3 specs are contract tests over the documented corner cases of the
//     completion rules (tagless template, digest-pinned template, registry ports).
//   - F4 pins the digest completion rule added by 7c7abfbf: a runtimeVersion whose
//     image is pinned by digest must not gain a tag. Red on the round-1 head
//     (0361cc03, where the template tag was appended, rendering an invalid
//     "repo@sha256:...:v1" reference), green since 7c7abfbf.

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("PR-6186 verification: partial runtime version on the unit layer", Label("pkg.ddc.cache.engine.version_completion_unit_verify_test.go"), func() {

	// F1b (inverted in round 2): the worker workload and the DataLoad image must not
	// diverge for a partially specified version — the same CacheRuntime expresses one
	// intent and must yield one image on both paths.
	Context("F1b worker and DataLoad image agreement", func() {
		It("contract: an imageTag-only upgrade moves the worker and the DataLoad image together [red on base and on round-1 head]", func() {
			engine, _, runtimeClass, ctx, client := newPartialVersionFixture("verify/worker:v1", "verify/worker:v1")

			syncWorkerVersion(engine, client, ctx, runtimeClass, datav1alpha1.VersionSpec{ImageTag: "v2"})
			Expect(workerImageOf(client, ctx)).To(Equal("verify/worker:v2"))

			edited := &datav1alpha1.CacheRuntime{}
			Expect(client.Get(ctx.Context, types.NamespacedName{Name: verifyRuntimeName, Namespace: "default"}, edited)).To(Succeed())
			dataLoadImage, err := engine.getDataOperationImage(edited, runtimeClass)
			Expect(err).NotTo(HaveOccurred())
			Expect(dataLoadImage).To(Equal("verify/worker:v2"))
		})
	})

	// F4 pins the digest rule from 7c7abfbf: a runtimeVersion naming only a
	// digest-pinned image must stay incomplete rather than take the template's tag,
	// which would render "repo@sha256:...:v1" — a reference no runtime can pull.
	Context("F4 digest-pinned runtime image", func() {
		It("contract: a digest-pinned runtime image does not gain a tag [red on round-1 head 0361cc03]", func() {
			const digestImage = "verify/worker@sha256:067614b70d25b496e3edc3480747d558ee8a364ef47a67f669f5d96ca5098552"

			desired := desiredComponentVersion(datav1alpha1.VersionSpec{Image: digestImage}, "verify/worker:v1")

			Expect(desired.Image).To(Equal(digestImage))
			Expect(desired.ImageTag).To(BeEmpty())
		})

		It("contract: a digest-pinned template does not lend its (absent) tag to an image-only version", func() {
			const digestTemplate = "verify/worker@sha256:067614b70d25b496e3edc3480747d558ee8a364ef47a67f669f5d96ca5098552"

			desired := desiredComponentVersion(datav1alpha1.VersionSpec{Image: "other/app"}, digestTemplate)

			Expect(desired.Image).To(Equal("other/app"))
			Expect(desired.ImageTag).To(BeEmpty())
		})
	})

	Context("F3 completion corner cases", func() {
		It("contract: splitImageReference does not read a trailing colon as a tag", func() {
			repository, tag := splitImageReference("verify/worker:")
			Expect(repository).To(Equal("verify/worker"))
			Expect(tag).To(BeEmpty())
		})

		It("contract: splitImageReference handles a registry port with a nested path", func() {
			repository, tag := splitImageReference("reg.example.com:5000/team/app")
			Expect(repository).To(Equal("reg.example.com:5000/team/app"))
			Expect(tag).To(BeEmpty())

			repository, tag = splitImageReference("reg.example.com:5000/team/app:v2")
			Expect(repository).To(Equal("reg.example.com:5000/team/app"))
			Expect(tag).To(Equal("v2"))
		})

		It("contract: an image-only version over a tagless template stays incomplete and the edit is dropped (documented residual gap)", func() {
			desired := desiredComponentVersion(datav1alpha1.VersionSpec{Image: "other/app"}, "verify/worker")
			Expect(desired.Image).To(Equal("other/app"))
			Expect(desired.ImageTag).To(BeEmpty())
		})

		It("contract: creation keeps the template digest image when the version is partial", func() {
			const digestImage = "verify/worker@sha256:067614b70d25b496e3edc3480747d558ee8a364ef47a67f669f5d96ca5098552"
			value, err := (&CacheEngine{name: "verify-runtime", namespace: "default"}).initComponentValue(
				common.ComponentTypeWorker,
				&datav1alpha1.RuntimeComponentDefinition{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "worker", Image: digestImage}}},
					},
				},
				nil, 1)
			Expect(err).NotTo(HaveOccurred())

			(&CacheEngine{name: "verify-runtime", namespace: "default"}).transformComponentPodTemplate(
				datav1alpha1.CacheRuntimeSpec{},
				datav1alpha1.RuntimeComponentCommonSpec{RuntimeVersion: datav1alpha1.VersionSpec{ImageTag: "v9"}},
				&datav1alpha1.Dataset{}, value)

			Expect(value.PodTemplateSpec.Spec.Containers[0].Image).To(Equal(digestImage))
		})

		It("contract: creation over a tagless template drops an image-only edit (documented residual gap)", func() {
			value, err := (&CacheEngine{name: "verify-runtime", namespace: "default"}).initComponentValue(
				common.ComponentTypeWorker,
				&datav1alpha1.RuntimeComponentDefinition{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "worker", Image: "verify/worker"}}},
					},
				},
				nil, 1)
			Expect(err).NotTo(HaveOccurred())

			(&CacheEngine{name: "verify-runtime", namespace: "default"}).transformComponentPodTemplate(
				datav1alpha1.CacheRuntimeSpec{},
				datav1alpha1.RuntimeComponentCommonSpec{RuntimeVersion: datav1alpha1.VersionSpec{Image: "other/app"}},
				&datav1alpha1.Dataset{}, value)

			// The user asked to change the repository; nothing they declared survives.
			Expect(value.PodTemplateSpec.Spec.Containers[0].Image).To(Equal("verify/worker"))
		})
	})
})
