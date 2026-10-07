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

// Verification harness for PR #6177 / issue #6173, written by the reviewer.
// Additive only: no production code is touched by the verification branch.
//
// Polarity:
//   - TestVerifyPR6177_*Overlay* / *KeepsUnnamedRequirements are CONTRACT tests:
//     they assert the intended merged behaviour, so they FAIL on the base
//     commit (reproducing #6173) and PASS once the fix lands.
//   - TestVerifyPR6177_*Guard* tests pin behaviour that must hold both before
//     and after the fix (the #6165 "nil means leave the workload alone"
//     contract and the template-as-baseline fallback). They PASS on both.

import (
	"context"
	"testing"

	workloadv1alpha1 "github.com/fluid-cloudnative/advanced-statefulset/api/workload/v1alpha1"
	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	cruntime "github.com/fluid-cloudnative/fluid/pkg/runtime"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// verifyPR6177FullTemplateResources is the CacheRuntimeClass template from the
// #6173 reproduction: requests {cpu 1, memory 2Gi}, limits {cpu 2, memory 4Gi}.
func verifyPR6177FullTemplateResources() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("1"),
			corev1.ResourceMemory: resource.MustParse("2Gi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("2"),
			corev1.ResourceMemory: resource.MustParse("4Gi"),
		},
	}
}

// verifyPR6177PartialRuntimeResources is the CacheRuntime edit from the #6173
// reproduction: only limits.memory is raised to 8Gi.
func verifyPR6177PartialRuntimeResources() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse("8Gi"),
		},
	}
}

func verifyPR6177CheckMerged(t *testing.T, got corev1.ResourceRequirements, where string) {
	t.Helper()
	if q := got.Requests.Cpu().String(); q != "1" {
		t.Errorf("%s: requests.cpu = %q, want %q (template value dropped)", where, q, "1")
	}
	if q := got.Requests.Memory().String(); q != "2Gi" {
		t.Errorf("%s: requests.memory = %q, want %q (template value dropped)", where, q, "2Gi")
	}
	if q := got.Limits.Cpu().String(); q != "2" {
		t.Errorf("%s: limits.cpu = %q, want %q (template value dropped)", where, q, "2")
	}
	if q := got.Limits.Memory().String(); q != "8Gi" {
		t.Errorf("%s: limits.memory = %q, want %q (CacheRuntime overlay not applied)", where, q, "8Gi")
	}
}

// P0/F1 contract, unit layer, sync resolver: a CacheRuntime naming one key must
// keep the template's other requirements. Fails on base (#6173 reproduced).
func TestVerifyPR6177_DesiredComponentResources_OverlaysOntoTemplate(t *testing.T) {
	def := &datav1alpha1.RuntimeComponentDefinition{
		Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name:      "worker",
					Resources: verifyPR6177FullTemplateResources(),
				}},
			},
		},
	}

	desired := desiredComponentResources(verifyPR6177PartialRuntimeResources(), def)
	if desired == nil {
		t.Fatalf("desiredComponentResources returned nil; want merged template+overlay")
	}
	verifyPR6177CheckMerged(t, *desired, "desiredComponentResources")

	// The template in the CacheRuntimeClass must not be mutated by the merge.
	if q := def.Template.Spec.Containers[0].Resources.Limits.Memory().String(); q != "4Gi" {
		t.Errorf("template mutated: limits.memory = %q, want %q", q, "4Gi")
	}
}

// F1 contract, unit layer, creation path: transformComponentPodTemplate must
// overlay the component resources onto the template baseline key by key.
// Fails on base (creation path replaces the whole struct pre-#6177).
func TestVerifyPR6177_TransformComponentPodTemplate_OverlaysOntoTemplate(t *testing.T) {
	e := &CacheEngine{name: "test", namespace: "default"}
	value := &common.CacheRuntimeComponentValue{
		Name:          "test-worker",
		Namespace:     "default",
		ComponentType: common.ComponentTypeWorker,
		PodTemplateSpec: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name:      "worker",
					Image:     "fluid/cache:v1",
					Resources: verifyPR6177FullTemplateResources(),
				}},
			},
		},
	}
	dataset := &datav1alpha1.Dataset{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"}}

	e.transformComponentPodTemplate(datav1alpha1.CacheRuntimeSpec{},
		datav1alpha1.RuntimeComponentCommonSpec{Resources: verifyPR6177PartialRuntimeResources()},
		dataset, value)

	verifyPR6177CheckMerged(t, value.PodTemplateSpec.Spec.Containers[0].Resources, "transformComponentPodTemplate")
}

// verifyPR6177BuildSyncFixture builds the engine, runtime, class and workloads
// for the integration layer, mirroring the post-creation state of the #6173
// reproduction: the worker AdvancedStatefulSet carries the full template
// resources, and the CacheRuntime raises only limits.memory.
func verifyPR6177BuildSyncFixture(workerWorkloadResources corev1.ResourceRequirements, templateResources *corev1.ResourceRequirements, runtimeResources *corev1.ResourceRequirements) (*CacheEngine, cruntime.ReconcileRequestContext, *datav1alpha1.CacheRuntime, *datav1alpha1.CacheRuntimeClass) {
	scheme := CacheEngineTestScheme

	runtimeObj := &datav1alpha1.CacheRuntime{
		TypeMeta:   metav1.TypeMeta{APIVersion: "data.fluid.io/v1alpha1", Kind: "CacheRuntime"},
		ObjectMeta: metav1.ObjectMeta{Name: "test-runtime", Namespace: "default", UID: "test-runtime-uid"},
		Spec: datav1alpha1.CacheRuntimeSpec{
			RuntimeClassName: "test-class",
			Master:           datav1alpha1.CacheRuntimeMasterSpec{Replicas: 1},
			Worker:           datav1alpha1.CacheRuntimeWorkerSpec{Replicas: 2},
		},
	}
	if runtimeResources != nil {
		runtimeObj.Spec.Worker.Resources = *runtimeResources
	}

	workerTemplate := corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "worker", Image: "test-worker:latest"}}},
	}
	if templateResources != nil {
		workerTemplate.Spec.Containers[0].Resources = *templateResources.DeepCopy()
	}
	runtimeClass := &datav1alpha1.CacheRuntimeClass{
		ObjectMeta:     metav1.ObjectMeta{Name: "test-class"},
		FileSystemType: "test-fs",
		Topology: &datav1alpha1.RuntimeTopology{
			Master: &datav1alpha1.RuntimeComponentDefinition{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "master", Image: "test-master:latest"}}},
				},
			},
			Worker: &datav1alpha1.RuntimeComponentDefinition{Template: workerTemplate},
		},
	}

	masterReplicas, workerReplicas := int32(1), int32(2)
	masterSts := &workloadv1alpha1.AdvancedStatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "test-runtime-master", Namespace: "default"},
		Spec: workloadv1alpha1.AdvancedStatefulSetSpec{
			Replicas: &masterReplicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "master", Image: "test-master:latest"}}},
			},
		},
	}
	workerSts := &workloadv1alpha1.AdvancedStatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "test-runtime-worker", Namespace: "default"},
		Spec: workloadv1alpha1.AdvancedStatefulSetSpec{
			Replicas: &workerReplicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{
					Name:      "worker",
					Image:     "test-worker:latest",
					Resources: *workerWorkloadResources.DeepCopy(),
				}}},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(runtimeObj, runtimeClass, masterSts, workerSts).
		Build()

	engine := &CacheEngine{
		name:      "test-runtime",
		namespace: "default",
		Client:    fakeClient,
		Log:       ctrl.Log.WithName("test"),
	}
	ctx := cruntime.ReconcileRequestContext{
		Client:         fakeClient,
		Context:        context.Background(),
		Log:            ctrl.Log.WithName("test"),
		RuntimeType:    "cache",
		NamespacedName: types.NamespacedName{Name: "test-runtime", Namespace: "default"},
	}
	return engine, ctx, runtimeObj, runtimeClass
}

func verifyPR6177WorkerResources(t *testing.T, engine *CacheEngine) corev1.ResourceRequirements {
	t.Helper()
	sts := &workloadv1alpha1.AdvancedStatefulSet{}
	if err := engine.Client.Get(context.Background(),
		types.NamespacedName{Name: "test-runtime-worker", Namespace: "default"}, sts); err != nil {
		t.Fatalf("get worker sts: %v", err)
	}
	return sts.Spec.Template.Spec.Containers[0].Resources
}

// P0/F1 contract, integration layer (fake client): the full reconcile-side path
// syncRuntimeSpec -> SyncComponentSpec -> updateResources must keep the
// template's CPU request/limit and memory request when the CacheRuntime raises
// only limits.memory, and must converge (second sync patches nothing).
// Fails on base: the workload is left with {limits: {memory: 8Gi}} only.
func TestVerifyPR6177_SyncRuntimeSpec_KeepsUnnamedRequirements(t *testing.T) {
	template := verifyPR6177FullTemplateResources()
	partial := verifyPR6177PartialRuntimeResources()
	engine, ctx, runtimeObj, runtimeClass := verifyPR6177BuildSyncFixture(template, &template, &partial)

	if err := engine.syncRuntimeSpec(ctx, runtimeObj, runtimeClass); err != nil {
		t.Fatalf("syncRuntimeSpec: %v", err)
	}
	verifyPR6177CheckMerged(t, verifyPR6177WorkerResources(t, engine), "syncRuntimeSpec")

	// Convergence: a second sync with no spec change must not patch the workload.
	sts := &workloadv1alpha1.AdvancedStatefulSet{}
	if err := engine.Client.Get(context.Background(),
		types.NamespacedName{Name: "test-runtime-worker", Namespace: "default"}, sts); err != nil {
		t.Fatalf("get worker sts: %v", err)
	}
	convergedRV := sts.ResourceVersion
	if err := engine.syncRuntimeSpec(ctx, runtimeObj, runtimeClass); err != nil {
		t.Fatalf("second syncRuntimeSpec: %v", err)
	}
	sts = &workloadv1alpha1.AdvancedStatefulSet{}
	if err := engine.Client.Get(context.Background(),
		types.NamespacedName{Name: "test-runtime-worker", Namespace: "default"}, sts); err != nil {
		t.Fatalf("get worker sts: %v", err)
	}
	if sts.ResourceVersion != convergedRV {
		t.Errorf("sync did not converge: resourceVersion %q -> %q with no spec change", convergedRV, sts.ResourceVersion)
	}
}

// Guard (green before and after): when neither the CacheRuntime nor the
// template declares resources, the sync must leave the workload's current
// resources untouched — the "nil means hands off" contract from #6165.
func TestVerifyPR6177_NeitherSideDeclares_GuardWorkloadUntouched(t *testing.T) {
	drifted := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("3Gi")},
	}
	engine, ctx, runtimeObj, runtimeClass := verifyPR6177BuildSyncFixture(drifted, nil, nil)

	if err := engine.syncRuntimeSpec(ctx, runtimeObj, runtimeClass); err != nil {
		t.Fatalf("syncRuntimeSpec: %v", err)
	}
	got := verifyPR6177WorkerResources(t, engine)
	if q := got.Requests.Memory().String(); q != "3Gi" {
		t.Errorf("workload resources changed although neither side declares any: requests.memory = %q, want %q", q, "3Gi")
	}
}

// Guard (green before and after): a CacheRuntime that sets nothing resolves to
// the template value, and the template object is not mutated.
func TestVerifyPR6177_TemplateOnly_GuardResolvesToTemplate(t *testing.T) {
	template := verifyPR6177FullTemplateResources()
	def := &datav1alpha1.RuntimeComponentDefinition{
		Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "worker", Resources: template}},
			},
		},
	}

	desired := desiredComponentResources(corev1.ResourceRequirements{}, def)
	if desired == nil {
		t.Fatalf("desiredComponentResources returned nil; want the template values")
	}
	if q := desired.Limits.Memory().String(); q != "4Gi" {
		t.Errorf("limits.memory = %q, want template value %q", q, "4Gi")
	}
	if q := desired.Requests.Cpu().String(); q != "1" {
		t.Errorf("requests.cpu = %q, want template value %q", q, "1")
	}
	if q := def.Template.Spec.Containers[0].Resources.Limits.Memory().String(); q != "4Gi" {
		t.Errorf("template mutated: limits.memory = %q, want %q", q, "4Gi")
	}
}
