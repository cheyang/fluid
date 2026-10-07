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

// Verification harness for PR #6186 (reviewer: codex). Additive test file; touches no
// production code.
//
// F1 - desiredComponentVersion completes the missing TAG of a version whose image half
// is a digest-pinned reference ("repo@sha256:..."). A tag cannot be appended to a digest
// reference: both the create path (transform_common.go) and the update path
// (advanced_statefulset_manager.go updateImage) render the version as
// image + ":" + imageTag, i.e. "repo@sha256:...:v1", which is not a parseable container
// image reference, so the workload's pods fail to start (kubelet: "couldn't parse image
// reference"). The function's own documented policy says a version "whose missing half
// cannot be recovered ... stays incomplete" - a digest's tag is exactly such a half.
//
// On the base branch this input was dropped by the both-halves guard in both paths, so
// this failure mode is introduced by this PR.
//
// Polarity: CONTRACT tests asserting the policy the PR documents. On the PR head they
// FAIL (observed: the tag is completed and the invalid reference is rendered); after a
// fix (leave a digest image's missing tag incomplete) they PASS.

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	workloadv1alpha1 "github.com/fluid-cloudnative/advanced-statefulset/api/workload/v1alpha1"
	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	cruntime "github.com/fluid-cloudnative/fluid/pkg/runtime"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("F1 (PR #6186): digest-pinned runtime image completed with a template tag",
	Label("verify-pr6186", "f1-digest"), func() {

		const (
			templateImage = "fluid/cache:v1"
			digestImage   = "btxu/mooncake@sha256:067614b70d25b496e3edc3480747d558ee8a364ef47a67f669f5d96ca5098552"
		)

		It("F1a [contract]: a digest image's missing tag stays incomplete", func() {
			desired := desiredComponentVersion(datav1alpha1.VersionSpec{Image: digestImage}, templateImage)

			// Per the function's documented policy, an unrecoverable half stays empty.
			// Observed on PR head: ImageTag is completed to "v1", which renders
			// "btxu/mooncake@sha256:...:v1" - not a valid image reference.
			Expect(desired.ImageTag).To(BeEmpty(),
				"a tag must never be appended to a digest reference")
		})

		It("F1b [contract]: the update path never lands an invalid digest:tag reference "+
			"on the workload", func() {
			tmpl := corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "worker", Image: templateImage}},
				},
			}
			replicas := int32(1)
			asts := &workloadv1alpha1.AdvancedStatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      common.GetCacheComponentName("test", common.ComponentTypeWorker),
					Namespace: "default",
				},
				Spec: workloadv1alpha1.AdvancedStatefulSetSpec{Replicas: &replicas, Template: tmpl},
			}
			runtimeObj := &datav1alpha1.CacheRuntime{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: datav1alpha1.CacheRuntimeSpec{
					RuntimeClassName: "test-class",
					Worker: datav1alpha1.CacheRuntimeWorkerSpec{
						Replicas: 1,
						RuntimeComponentCommonSpec: datav1alpha1.RuntimeComponentCommonSpec{
							RuntimeVersion: datav1alpha1.VersionSpec{Image: digestImage},
						},
					},
				},
			}
			runtimeClass := &datav1alpha1.CacheRuntimeClass{
				ObjectMeta: metav1.ObjectMeta{Name: "test-class"},
				Topology: &datav1alpha1.RuntimeTopology{
					Worker: &datav1alpha1.RuntimeComponentDefinition{Template: tmpl},
				},
			}
			client := fake.NewClientBuilder().
				WithScheme(CacheEngineTestScheme).
				WithObjects(asts, runtimeObj, runtimeClass).
				Build()
			e := &CacheEngine{
				name:      "test",
				namespace: "default",
				Client:    client,
				Log:       ctrl.Log.WithName("verify-pr6186-f1"),
			}

			Expect(e.syncRuntimeSpec(cruntime.ReconcileRequestContext{}, runtimeObj, runtimeClass)).To(Succeed())

			got := &workloadv1alpha1.AdvancedStatefulSet{}
			Expect(client.Get(context.TODO(), types.NamespacedName{
				Name:      common.GetCacheComponentName("test", common.ComponentTypeWorker),
				Namespace: "default",
			}, got)).To(Succeed())

			// INTENDED per the PR's own policy: the edit is dropped, the workload keeps
			// the template image. Observed on PR head: "btxu/mooncake@sha256:...:v1",
			// which the kubelet cannot parse (see docs/verification/.../refcheck).
			Expect(got.Spec.Template.Spec.Containers[0].Image).To(Equal(templateImage))
		})

		It("F1c [contract]: the create path never renders an invalid digest:tag reference",
			func() {
				tmpl := corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "worker", Image: templateImage}},
					},
				}
				runtimeSpec := datav1alpha1.CacheRuntimeSpec{
					RuntimeClassName: "test-class",
					Worker: datav1alpha1.CacheRuntimeWorkerSpec{
						Replicas: 1,
						RuntimeComponentCommonSpec: datav1alpha1.RuntimeComponentCommonSpec{
							RuntimeVersion: datav1alpha1.VersionSpec{Image: digestImage},
						},
					},
				}

				e := &CacheEngine{name: "test", namespace: "default"}
				value, err := e.initComponentValue(common.ComponentTypeWorker,
					&datav1alpha1.RuntimeComponentDefinition{Template: tmpl}, nil, runtimeSpec.Worker.Replicas)
				Expect(err).NotTo(HaveOccurred())

				e.transformComponentPodTemplate(runtimeSpec, runtimeSpec.Worker.RuntimeComponentCommonSpec,
					&datav1alpha1.Dataset{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"}}, value)

				// INTENDED: template image kept. Observed on PR head: invalid digest:tag.
				Expect(value.PodTemplateSpec.Spec.Containers[0].Image).To(Equal(templateImage))
			})
	})
