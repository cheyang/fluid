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
//   - F1b is a canary over the divergence the PR introduces between the worker
//     workload image and the DataLoad image for a partially specified version. It is
//     green while the divergence exists and flips to red when image.go is aligned.
//   - the F3 specs are contract tests over the documented corner cases of the
//     completion rules (tagless template, digest-pinned template, registry ports).

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("PR-6186 verification: partial runtime version on the unit layer", Label("pkg.ddc.cache.engine.version_completion_unit_verify_test.go"), func() {

	// F1b pins the worker/DataLoad image divergence the PR introduces: the sync path
	// completes a partial version while getDataOperationImage does not, so the same
	// CacheRuntime yields two different images for the same declared intent.
	Context("F1b worker vs DataLoad image divergence", func() {
		It("canary: an imageTag-only upgrade moves the worker but not the DataLoad image [flips when image.go is aligned]", func() {
			engine, _, runtimeClass, ctx, client := newPartialVersionFixture("verify/worker:v1", "verify/worker:v1")

			syncWorkerVersion(engine, client, ctx, runtimeClass, datav1alpha1.VersionSpec{ImageTag: "v2"})
			Expect(workerImageOf(client, ctx)).To(Equal("verify/worker:v2"))

			edited := &datav1alpha1.CacheRuntime{}
			Expect(client.Get(ctx.Context, types.NamespacedName{Name: verifyRuntimeName, Namespace: "default"}, edited)).To(Succeed())
			dataLoadImage, err := engine.getDataOperationImage(edited, runtimeClass)
			Expect(err).NotTo(HaveOccurred())
			// CURRENT behavior: the DataLoad image lags one override behind the worker.
			Expect(dataLoadImage).To(Equal("verify/worker:v1"))
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
