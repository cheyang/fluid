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

// Reviewer verification harness for PR #6177, round 2 (delta c1c303e8..e259d738).
//
// The delta makes desiredComponentResources always return the merged result, so a
// key neither the CacheRuntime nor its CacheRuntimeClass template declares is
// cleared from the workload. These tests pin the new boundary semantics and the
// one behavior the PR's own tests did not cover at the time: the merge can
// synthesize a requests-above-limits combination from two individually valid
// inputs.
//
// Round 3 (e259d738 -> 497bbeaf) inverted the D2 canary into a contract test:
// commit f02054bd added the validation the round-2 review asked for, so the
// conflicting merge must now be refused with a RuntimeResourcesConflict Warning
// event instead of applied silently.

import (
	"context"
	"strings"
	"testing"

	workloadv1alpha1 "github.com/fluid-cloudnative/advanced-statefulset/api/workload/v1alpha1"
	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	cruntime "github.com/fluid-cloudnative/fluid/pkg/runtime"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// v6177r2WorkerTemplate builds a CacheRuntimeClass worker definition whose
// template container carries the given resources.
func v6177r2WorkerTemplate(resources corev1.ResourceRequirements) corev1.PodTemplateSpec {
	return corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:      "worker",
				Image:     "fluid/cache:v1",
				Resources: *resources.DeepCopy(),
			}},
		},
	}
}

// v6177r2Engine builds a CacheEngine over a fake client holding the ASTS,
// CacheRuntime and CacheRuntimeClass the sync path needs.
func v6177r2Engine(t *testing.T, asts *workloadv1alpha1.AdvancedStatefulSet,
	runtime *datav1alpha1.CacheRuntime, runtimeClass *datav1alpha1.CacheRuntimeClass) *CacheEngine {
	client := fake.NewClientBuilder().
		WithScheme(CacheEngineTestScheme).
		WithObjects(asts, runtime, runtimeClass).
		Build()
	return &CacheEngine{
		name:      "test",
		namespace: "default",
		Client:    client,
		Log:       ctrl.Log.WithName("verify-delta-6177"),
	}
}

// D1 (contract): the nil that meant "leave the workload alone" is gone —
// desiredComponentResources must resolve to a non-nil empty result when neither
// side declares anything, which is what clears a key neither side declares.
func TestD1NeitherDeclaresResolvesEmpty(t *testing.T) {
	for _, def := range []*datav1alpha1.RuntimeComponentDefinition{
		v6177WorkerDefinition(corev1.ResourceRequirements{}),
		nil,
	} {
		desired := desiredComponentResources(corev1.ResourceRequirements{}, def)
		if desired == nil {
			t.Fatalf("desiredComponentResources returned nil for definition %v: the delta must resolve to empty, not nil", def)
		}
		if desired.Requests != nil || desired.Limits != nil || desired.Claims != nil {
			t.Errorf("neither side declares anything, got non-empty desired: %+v", *desired)
		}
	}
}

// D2 (CONTRACT — inverted in round 3 after commit f02054bd landed the validation):
// the overlay can synthesize an invalid pod spec from two individually valid
// inputs. The template declares requests {cpu 1} / limits {cpu 2}; the CacheRuntime
// lowers limits.cpu to 500m. Both are valid on their own; the merged result has
// requests.cpu=1 above limits.cpu=500m, which the Kubernetes API server rejects
// ("must be less than or equal to cpu limit"). Round 2 pinned the then-broken
// behavior as a canary (sync applies the combination, no error, no event); the
// fix flips it, so the assertion is now the contract the round-2 review asked
// for: the sync refuses the resource update, leaves the workload's current
// values alone and emits a RuntimeResourcesConflict Warning event naming the
// conflicting keys.
func TestD2MergeSynthesizesRequestsAboveLimits(t *testing.T) {
	template := v6177TemplateResources // requests {cpu 1, mem 2Gi}, limits {cpu 2, mem 4Gi}
	runtimeRes := corev1.ResourceRequirements{
		Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m")},
	}

	// Resolution layer: the merged desired state carries the invalid combination.
	desired := desiredComponentResources(runtimeRes, v6177WorkerDefinition(template))
	if desired == nil {
		t.Fatal("desiredComponentResources returned nil")
	}
	if got := desired.Requests.Cpu().String(); got != "1" {
		t.Errorf("requests.cpu: got %q, want 1 (template value kept)", got)
	}
	if got := desired.Limits.Cpu().String(); got != "500m" {
		t.Errorf("limits.cpu: got %q, want 500m (the override)", got)
	}
	if desired.Requests.Cpu().Cmp(*desired.Limits.Cpu()) <= 0 {
		t.Fatalf("fixture sanity: merged requests.cpu (%s) must exceed merged limits.cpu (%s) for this test to mean anything",
			desired.Requests.Cpu().String(), desired.Limits.Cpu().String())
	}

	// Workload layer: the sync must refuse the resource update. The fake client
	// performs no validation, mirroring a schemaless ASTS pod template, so the
	// only thing standing between the invalid merge and the workload is the
	// controller's own validation (validDesiredResources).
	replicas := int32(1)
	asts := &workloadv1alpha1.AdvancedStatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "test-worker", Namespace: "default"},
		Spec: workloadv1alpha1.AdvancedStatefulSetSpec{
			Replicas: &replicas,
			Template: v6177r2WorkerTemplate(template),
		},
	}
	runtimeObj := &datav1alpha1.CacheRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: datav1alpha1.CacheRuntimeSpec{
			RuntimeClassName: "test-class",
			Worker: datav1alpha1.CacheRuntimeWorkerSpec{
				Replicas:                   1,
				RuntimeComponentCommonSpec: datav1alpha1.RuntimeComponentCommonSpec{Resources: runtimeRes},
			},
		},
	}
	runtimeClass := &datav1alpha1.CacheRuntimeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "test-class"},
		Topology: &datav1alpha1.RuntimeTopology{
			Worker: &datav1alpha1.RuntimeComponentDefinition{Template: v6177r2WorkerTemplate(template)},
		},
	}
	engine := v6177r2Engine(t, asts, runtimeObj, runtimeClass)
	recorder := record.NewFakeRecorder(10)
	engine.Recorder = recorder

	if err := engine.syncRuntimeSpec(cruntime.ReconcileRequestContext{Context: context.TODO()}, runtimeObj, runtimeClass); err != nil {
		t.Fatalf("syncRuntimeSpec failed: %v — a conflicting merge must be skipped, not fail the sync", err)
	}

	key := types.NamespacedName{Name: "test-worker", Namespace: "default"}
	synced := &workloadv1alpha1.AdvancedStatefulSet{}
	if err := engine.Client.Get(context.TODO(), key, synced); err != nil {
		t.Fatalf("get ASTS: %v", err)
	}
	got := synced.Spec.Template.Spec.Containers[0].Resources
	// The workload keeps what it already carried (the template combination), not
	// the invalid merged one.
	if got.Requests.Cpu().String() != "1" || got.Limits.Cpu().String() != "2" {
		t.Fatalf("workload resources: got requests.cpu=%s limits.cpu=%s, want the untouched 1 / 2 (the merge must be refused)",
			got.Requests.Cpu().String(), got.Limits.Cpu().String())
	}
	if got.Requests.Cpu().Cmp(*got.Limits.Cpu()) > 0 {
		t.Fatalf("an invalid combination reached the workload: requests=%s limits=%s",
			got.Requests.Cpu().String(), got.Limits.Cpu().String())
	}

	// The only signal an operator gets is the event: it must name the conflict.
	select {
	case ev := <-recorder.Events:
		if !strings.Contains(ev, v6177r3ConflictReason) {
			t.Errorf("event reason: got %q, want it to contain %q", ev, v6177r3ConflictReason)
		}
		if !strings.Contains(ev, "cpu request 1 exceeds limit 500m") {
			t.Errorf("event body: got %q, want it to name the conflicting pair", ev)
		}
	default:
		t.Fatal("no RuntimeResourcesConflict event was emitted for the conflicting merge")
	}
}

// D3 (contract): a workload whose template and CacheRuntime declare no resources
// but whose tiered store left a memory-medium tmpfs volume: the sync must not
// invent a memory request from the read-back quota. Creation leaves the container
// unconstrained in this shape (withTieredStoreMemoryQuota only charges a
// constraint that exists), and the sync must converge to the same state rather
// than materializing requests.memory = quota.
func TestD3QuotaWithoutBaselineStaysUnconstrained(t *testing.T) {
	replicas := int32(1)
	quota := resource.MustParse("1Gi")
	podTemplate := v6177r2WorkerTemplate(corev1.ResourceRequirements{})
	podTemplate.Spec.Volumes = []corev1.Volume{{
		Name: "tiered-store-level-0-memory",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{
				Medium:    corev1.StorageMediumMemory,
				SizeLimit: &quota,
			},
		},
	}}
	asts := &workloadv1alpha1.AdvancedStatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "test-worker", Namespace: "default"},
		Spec:       workloadv1alpha1.AdvancedStatefulSetSpec{Replicas: &replicas, Template: podTemplate},
	}
	runtimeObj := &datav1alpha1.CacheRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: datav1alpha1.CacheRuntimeSpec{
			RuntimeClassName: "test-class",
			Worker: datav1alpha1.CacheRuntimeWorkerSpec{
				Replicas:                   1,
				RuntimeComponentCommonSpec: datav1alpha1.RuntimeComponentCommonSpec{},
			},
		},
	}
	runtimeClass := &datav1alpha1.CacheRuntimeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "test-class"},
		Topology: &datav1alpha1.RuntimeTopology{
			Worker: &datav1alpha1.RuntimeComponentDefinition{
				Template: v6177r2WorkerTemplate(corev1.ResourceRequirements{}),
			},
		},
	}
	engine := v6177r2Engine(t, asts, runtimeObj, runtimeClass)

	key := types.NamespacedName{Name: "test-worker", Namespace: "default"}
	before := &workloadv1alpha1.AdvancedStatefulSet{}
	if err := engine.Client.Get(context.TODO(), key, before); err != nil {
		t.Fatalf("get ASTS: %v", err)
	}

	if err := engine.syncRuntimeSpec(cruntime.ReconcileRequestContext{Context: context.TODO()}, runtimeObj, runtimeClass); err != nil {
		t.Fatalf("syncRuntimeSpec failed: %v", err)
	}

	after := &workloadv1alpha1.AdvancedStatefulSet{}
	if err := engine.Client.Get(context.TODO(), key, after); err != nil {
		t.Fatalf("get ASTS: %v", err)
	}
	got := after.Spec.Template.Spec.Containers[0].Resources
	if got.Requests != nil || got.Limits != nil {
		t.Errorf("sync materialized resources on an unconstrained container: %+v (want all nil)", got)
	}
	if after.ResourceVersion != before.ResourceVersion {
		t.Errorf("sync patched the workload (resourceVersion %s -> %s) though nothing should change",
			before.ResourceVersion, after.ResourceVersion)
	}
}

// Silence the unused import checker for the common package when fixtures evolve.
var _ = common.GetCacheComponentName
