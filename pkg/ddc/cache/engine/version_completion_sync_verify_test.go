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

// Reviewer verification harness for PR #6186 (issue #6178): completing a partially
// specified runtime version from the CacheRuntimeClass template.
//
// This file intentionally references only symbols that exist on the merge base, so it
// can be run against BOTH the base branch and the PR head:
//   - against the base: the P0 contract specs are RED, which is the reproduction of
//     the issue (#6178) the PR claims to fix.
//   - against the PR head: all specs here are GREEN.
//
// Polarity (see docs/verification/cache-partial-version/):
//   - "P0 contract" specs assert intended behavior -> red on base, green on PR.
//   - "F1a canary" asserts the current (arguably wrong) behavior of
//     getDataOperationImage, which the PR leaves untouched -> green on base AND on
//     the PR head; it must flip to red if image.go is ever aligned with the
//     completion semantics.

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	workloadv1alpha1 "github.com/fluid-cloudnative/advanced-statefulset/api/workload/v1alpha1"
	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	cruntime "github.com/fluid-cloudnative/fluid/pkg/runtime"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	cclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	verifyRuntimeName = "verify-runtime"
	verifyClassName   = "verify-class"
	verifyWorkerSts   = "verify-runtime-worker"
	verifyMasterSts   = "verify-runtime-master"
)

// newPartialVersionFixture seeds the state a CacheRuntime past setup is in: a class
// whose worker template pins templateImage, a worker AdvancedStatefulSet already
// carrying stsImage, and a runtime declaring no runtimeVersion. The caller edits the
// runtime and calls syncRuntimeSpec.
func newPartialVersionFixture(templateImage, stsImage string) (*CacheEngine, *datav1alpha1.CacheRuntime, *datav1alpha1.CacheRuntimeClass, cruntime.ReconcileRequestContext, cclient.Client) {
	replicas := int32(1)
	runtimeObj := &datav1alpha1.CacheRuntime{
		TypeMeta:   metav1.TypeMeta{APIVersion: "data.fluid.io/v1alpha1", Kind: "CacheRuntime"},
		ObjectMeta: metav1.ObjectMeta{Name: verifyRuntimeName, Namespace: "default", UID: "verify-runtime-uid"},
		Spec: datav1alpha1.CacheRuntimeSpec{
			RuntimeClassName: verifyClassName,
			Master:           datav1alpha1.CacheRuntimeMasterSpec{Replicas: 1},
			Worker:           datav1alpha1.CacheRuntimeWorkerSpec{Replicas: 1},
		},
	}

	masterSts := &workloadv1alpha1.AdvancedStatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: verifyMasterSts, Namespace: "default"},
		Spec: workloadv1alpha1.AdvancedStatefulSetSpec{
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "master", Image: "verify/master:v1"}}},
			},
		},
	}
	workerSts := &workloadv1alpha1.AdvancedStatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: verifyWorkerSts, Namespace: "default"},
		Spec: workloadv1alpha1.AdvancedStatefulSetSpec{
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "worker", Image: stsImage}}},
			},
		},
	}

	runtimeClass := &datav1alpha1.CacheRuntimeClass{
		ObjectMeta:     metav1.ObjectMeta{Name: verifyClassName},
		FileSystemType: "verify-fs",
		Topology: &datav1alpha1.RuntimeTopology{
			Master: &datav1alpha1.RuntimeComponentDefinition{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "master", Image: "verify/master:v1"}}},
				},
			},
			Worker: &datav1alpha1.RuntimeComponentDefinition{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "worker", Image: templateImage}}},
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(CacheEngineTestScheme).
		WithObjects(runtimeObj, runtimeClass, masterSts, workerSts).
		Build()

	engine := &CacheEngine{
		name:      verifyRuntimeName,
		namespace: "default",
		Client:    fakeClient,
		Log:       ctrl.Log.WithName("verify-partial-version"),
	}
	ctx := cruntime.ReconcileRequestContext{Context: context.Background()}

	return engine, runtimeObj, runtimeClass, ctx, fakeClient
}

// syncWorkerVersion writes the version onto the CacheRuntime and runs one sync.
func syncWorkerVersion(engine *CacheEngine, client cclient.Client, ctx cruntime.ReconcileRequestContext,
	runtimeClass *datav1alpha1.CacheRuntimeClass, version datav1alpha1.VersionSpec) {
	edited := &datav1alpha1.CacheRuntime{}
	Expect(client.Get(ctx.Context, types.NamespacedName{Name: verifyRuntimeName, Namespace: "default"}, edited)).To(Succeed())
	edited.Spec.Worker.RuntimeVersion = version
	Expect(client.Update(ctx.Context, edited)).To(Succeed())

	syncRuntime := &datav1alpha1.CacheRuntime{}
	Expect(client.Get(ctx.Context, types.NamespacedName{Name: verifyRuntimeName, Namespace: "default"}, syncRuntime)).To(Succeed())
	Expect(engine.syncRuntimeSpec(ctx, syncRuntime, runtimeClass)).To(Succeed())
}

func workerImageOf(client cclient.Client, ctx cruntime.ReconcileRequestContext) string {
	sts := &workloadv1alpha1.AdvancedStatefulSet{}
	Expect(client.Get(ctx.Context, types.NamespacedName{Name: verifyWorkerSts, Namespace: "default"}, sts)).To(Succeed())
	return sts.Spec.Template.Spec.Containers[0].Image
}

var _ = Describe("PR-6186 verification: partial runtime version on the sync path", Label("pkg.ddc.cache.engine.version_completion_sync_verify_test.go"), func() {

	// P0 is the premise of the PR: the exact scenario of issue #6178. On the merge
	// base these specs are RED (the edit is silently dropped) -- that red IS the
	// reproduction. On the PR head they are GREEN.
	Context("P0 premise (issue #6178)", func() {
		It("contract: an imageTag-only edit is applied to the worker workload [red on base]", func() {
			engine, _, runtimeClass, ctx, client := newPartialVersionFixture("verify/worker:v1", "verify/worker:v1")

			syncWorkerVersion(engine, client, ctx, runtimeClass, datav1alpha1.VersionSpec{ImageTag: "v2"})

			Expect(workerImageOf(client, ctx)).To(Equal("verify/worker:v2"))
		})

		It("contract: an image-only edit is applied to the worker workload [red on base]", func() {
			engine, _, runtimeClass, ctx, client := newPartialVersionFixture("verify/worker:v1", "verify/worker:v1")

			syncWorkerVersion(engine, client, ctx, runtimeClass, datav1alpha1.VersionSpec{Image: "other/app"})

			// the tag half comes from the template
			Expect(workerImageOf(client, ctx)).To(Equal("other/app:v1"))
		})

		It("contract: a complete version is applied unchanged [green everywhere, control]", func() {
			engine, _, runtimeClass, ctx, client := newPartialVersionFixture("verify/worker:v1", "verify/worker:v1")

			syncWorkerVersion(engine, client, ctx, runtimeClass, datav1alpha1.VersionSpec{Image: "other/app", ImageTag: "v9"})

			Expect(workerImageOf(client, ctx)).To(Equal("other/app:v9"))
		})
	})

	// F2 pins the semantic change the PR makes on purpose: a version naming neither
	// half resolves to the template image, so the workload is rolled back to (and
	// thereafter follows) the class template. On base this is RED (the workload kept
	// whatever image it was created with); on the PR head it is GREEN. Recorded as a
	// review question about blast radius, not necessarily as a defect.
	Context("F2 rollback-to-template semantics", func() {
		It("contract: dropping a runtimeVersion rolls the worker back to the template image [red on base]", func() {
			engine, _, runtimeClass, ctx, client := newPartialVersionFixture("verify/worker:v1", "verify/worker:v9")

			syncWorkerVersion(engine, client, ctx, runtimeClass, datav1alpha1.VersionSpec{})

			Expect(workerImageOf(client, ctx)).To(Equal("verify/worker:v1"))
		})

		It("contract: a class template image edit reaches a live worker that set no runtimeVersion [red on base]", func() {
			engine, _, runtimeClass, ctx, client := newPartialVersionFixture("verify/worker:v2", "verify/worker:v1")

			syncWorkerVersion(engine, client, ctx, runtimeClass, datav1alpha1.VersionSpec{})

			Expect(workerImageOf(client, ctx)).To(Equal("verify/worker:v2"))
		})
	})

	// F1a is a canary for the gap the PR does NOT close: getDataOperationImage (the
	// DataLoad image resolution) still requires both halves and otherwise falls back
	// to the template image. It is green on base AND on the PR head; when image.go is
	// aligned with the completion semantics it flips to red and must be inverted.
	Context("F1a DataLoad image resolution", func() {
		It("canary: getDataOperationImage ignores a partially specified worker version [flips when image.go is aligned]", func() {
			engine, _, runtimeClass, ctx, client := newPartialVersionFixture("verify/worker:v1", "verify/worker:v1")

			edited := &datav1alpha1.CacheRuntime{}
			Expect(client.Get(ctx.Context, types.NamespacedName{Name: verifyRuntimeName, Namespace: "default"}, edited)).To(Succeed())
			edited.Spec.Worker.RuntimeVersion = datav1alpha1.VersionSpec{ImageTag: "v2"}
			Expect(client.Update(ctx.Context, edited)).To(Succeed())

			image, err := engine.getDataOperationImage(edited, runtimeClass)
			Expect(err).NotTo(HaveOccurred())
			// CURRENT behavior: the partial version is ignored, the template image wins.
			Expect(image).To(Equal("verify/worker:v1"))
		})
	})
})
