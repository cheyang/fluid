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
// production code. Uses only symbols present on the base branch so it compiles and runs
// against both base and PR head.
//
// F2 - behaviour change beyond the issue's scope: because syncRuntimeSpec now completes
// an EMPTY runtimeVersion from the class template on every reconcile, editing the
// CacheRuntimeClass template image rolls every existing CacheRuntime of that class onto
// the new image at the next sync (the controller requeues periodically and
// syncRuntimeSpec runs on every Sync once the runtime is past setup). On the base
// branch a class template image edit never touches existing workloads - the image was a
// creation-time-only input.
//
// Polarity: CANARY documenting the new behaviour. PASSES on the PR head (propagation
// happens); on the base branch it FAILS (no propagation). If a later change removes the
// propagation this flips to red and must be inverted.

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

var _ = Describe("F2 (PR #6186): a class template image edit propagates to existing runtimes",
	Label("verify-pr6186", "f2-class-template"), func() {

		It("F2 [canary]: an empty-version worker follows the edited class template image",
			func() {
				workerName := common.GetCacheComponentName("test", common.ComponentTypeWorker)
				templateV1 := corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "worker", Image: "fluid/cache:v1"}},
					},
				}
				replicas := int32(1)
				asts := &workloadv1alpha1.AdvancedStatefulSet{
					ObjectMeta: metav1.ObjectMeta{Name: workerName, Namespace: "default"},
					Spec:       workloadv1alpha1.AdvancedStatefulSetSpec{Replicas: &replicas, Template: templateV1},
				}
				// The runtime never names a runtimeVersion: it was created from the class.
				runtimeObj := &datav1alpha1.CacheRuntime{
					ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
					Spec: datav1alpha1.CacheRuntimeSpec{
						RuntimeClassName: "test-class",
						Worker:           datav1alpha1.CacheRuntimeWorkerSpec{Replicas: 1},
					},
				}
				runtimeClass := &datav1alpha1.CacheRuntimeClass{
					ObjectMeta: metav1.ObjectMeta{Name: "test-class"},
					Topology: &datav1alpha1.RuntimeTopology{
						Worker: &datav1alpha1.RuntimeComponentDefinition{Template: templateV1},
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
					Log:       ctrl.Log.WithName("verify-pr6186-f2"),
				}

				imageOfWorker := func() string {
					got := &workloadv1alpha1.AdvancedStatefulSet{}
					Expect(client.Get(context.TODO(),
						types.NamespacedName{Name: workerName, Namespace: "default"}, got)).To(Succeed())
					return got.Spec.Template.Spec.Containers[0].Image
				}

				// Steady state: sync with the class the runtime was created from.
				Expect(e.syncRuntimeSpec(cruntime.ReconcileRequestContext{}, runtimeObj, runtimeClass)).To(Succeed())
				Expect(imageOfWorker()).To(Equal("fluid/cache:v1"))

				// An admin edits the CacheRuntimeClass: the worker template image moves to v2.
				editedClass := runtimeClass.DeepCopy()
				editedClass.Topology.Worker.Template.Spec.Containers[0].Image = "fluid/cache:v2"

				Expect(e.syncRuntimeSpec(cruntime.ReconcileRequestContext{}, runtimeObj, editedClass)).To(Succeed())

				// CANARY, new behaviour introduced by this PR: the existing runtime's worker
				// is rolled to the edited class image. Base-branch behaviour: stays v1.
				Expect(imageOfWorker()).To(Equal("fluid/cache:v2"))
			})
	})
