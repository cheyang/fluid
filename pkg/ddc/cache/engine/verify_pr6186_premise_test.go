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
// P0 - premise of the PR (issue #6178): on the BASE branch, a CacheRuntime that names
// only imageTag in a component's runtimeVersion is silently ignored on BOTH paths:
// the create path keeps the CacheRuntimeClass template image, and the update path
// (syncRuntimeSpec -> updateImage) never patches the AdvancedStatefulSet.
//
// Polarity: CONTRACT tests asserting the intended (fixed) behaviour, and they are run
// against the BASE branch (merge-base d8b37f28) to prove the symptom exists there:
// on base both specs FAIL (observed: the workload keeps "fluid/cache:v1"), which is
// exactly the reported symptom. On the PR head both PASS.
//
// This file deliberately uses only symbols that already exist on the base branch
// (syncRuntimeSpec, transformComponentPodTemplate, initComponentValue), so the same
// file compiles and runs against both base and PR head.

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

var _ = Describe("P0 premise (PR #6186 / issue #6178): partial runtimeVersion",
	Label("verify-pr6186", "premise"), func() {

		const templateImage = "fluid/cache:v1"

		// buildSyncFixture seeds the post-creation state: a worker AdvancedStatefulSet
		// carrying the template image, a CacheRuntime whose worker names only imageTag,
		// and the CacheRuntimeClass whose worker template pins the image.
		buildSyncFixture := func() (*CacheEngine, *datav1alpha1.CacheRuntime, *datav1alpha1.CacheRuntimeClass) {
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
							RuntimeVersion: datav1alpha1.VersionSpec{ImageTag: "v2"},
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
				Log:       ctrl.Log.WithName("verify-pr6186-premise"),
			}
			return e, runtimeObj, runtimeClass
		}

		It("P0a [contract vs base]: update path applies an imageTag-only version, "+
			"completed from the class template", func() {
			e, runtimeObj, runtimeClass := buildSyncFixture()

			Expect(e.syncRuntimeSpec(cruntime.ReconcileRequestContext{}, runtimeObj, runtimeClass)).To(Succeed())

			got := &workloadv1alpha1.AdvancedStatefulSet{}
			Expect(e.Client.Get(context.TODO(), types.NamespacedName{
				Name:      common.GetCacheComponentName("test", common.ComponentTypeWorker),
				Namespace: "default",
			}, got)).To(Succeed())

			// INTENDED: the missing image half is completed from the class template.
			// BASE observed: "fluid/cache:v1" - the edit is silently dropped (the bug).
			Expect(got.Spec.Template.Spec.Containers[0].Image).To(Equal("fluid/cache:v2"))
		})

		It("P0b [contract vs base]: create path applies an imageTag-only version, "+
			"completed from the class template", func() {
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
						RuntimeVersion: datav1alpha1.VersionSpec{ImageTag: "v2"},
					},
				},
			}

			e := &CacheEngine{name: "test", namespace: "default"}
			value, err := e.initComponentValue(common.ComponentTypeWorker,
				&datav1alpha1.RuntimeComponentDefinition{Template: tmpl}, nil, runtimeSpec.Worker.Replicas)
			Expect(err).NotTo(HaveOccurred())

			e.transformComponentPodTemplate(runtimeSpec, runtimeSpec.Worker.RuntimeComponentCommonSpec,
				&datav1alpha1.Dataset{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"}}, value)

			// INTENDED: "fluid/cache:v2". BASE observed: "fluid/cache:v1" (the bug).
			Expect(value.PodTemplateSpec.Spec.Containers[0].Image).To(Equal("fluid/cache:v2"))
		})
	})
