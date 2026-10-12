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

// Round-3 contract tests for finding F6 (PR #6177): the per-key merge can
// synthesize a pod spec neither side declared -- a request above its limit --
// from two individually valid documents (template requests.cpu=1 plus a
// CacheRuntime lowering limits.cpu to 500m). Round 2 proved (canary, both
// reviewers) that head e259d738 applied that invalid spec to the
// AdvancedStatefulSet with nil error and no event, sticking the rollout
// silently. Head 497bbeaf (commit f02054bd) remediates it: the sync path
// refuses to apply a conflicting merge (Warning event RuntimeResourcesConflict,
// workload left untouched, image/replicas still sync), and the creation path
// fails the transform.
//
// Polarity: CONTRACT tests. They compile against base e0fc4c18, previous head
// e259d738 and head 497bbeaf (only pre-existing symbols are referenced), FAIL
// on base and on the previous head, and PASS on the head that carries the fix.

import (
	"context"
	"strings"
	"testing"

	workloadv1alpha1 "github.com/fluid-cloudnative/advanced-statefulset/api/workload/v1alpha1"
	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// F6 contract, integration layer (fake client): when the merged worker
// resources put a request above its limit, syncRuntimeSpec must NOT patch the
// workload's resources, must emit a Warning event naming the conflict, must
// still return nil (image and replicas keep syncing), and must converge.
// RED on base e0fc4c18 (wholesale replace patches the workload) and on
// previous head e259d738 (the invalid pair is patched on silently).
func TestVerifyPR6177_F6_ConflictMerge_SyncSkipsAndWarns(t *testing.T) {
	template := verifyPR6177FullTemplateResources() // requests.cpu=1, limits.cpu=2, memory 2Gi/4Gi
	overlay := corev1.ResourceRequirements{
		Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m")},
	}
	// The workload currently carries the (valid) template values.
	engine, ctx, runtimeObj, runtimeClass := verifyPR6177BuildSyncFixture(template, &template, &overlay)
	recorder := record.NewFakeRecorder(10)
	engine.Recorder = recorder

	before := verifyPR6177WorkerResources(t, engine)

	if err := engine.syncRuntimeSpec(ctx, runtimeObj, runtimeClass); err != nil {
		t.Fatalf("syncRuntimeSpec returned %v; the conflict must not fail the sync (image/replicas still sync)", err)
	}

	got := verifyPR6177WorkerResources(t, engine)
	if got.Requests.Cpu().String() != before.Requests.Cpu().String() ||
		got.Limits.Cpu().String() != before.Limits.Cpu().String() ||
		got.Requests.Memory().String() != before.Requests.Memory().String() ||
		got.Limits.Memory().String() != before.Limits.Memory().String() {
		t.Errorf("conflicting merge was applied to the workload: before %+v, after %+v; "+
			"the workload's resources must be left untouched", before, got)
	}
	if got.Limits.Cpu().String() == "500m" && got.Requests.Cpu().String() == "1" {
		t.Errorf("workload now carries requests.cpu=1 above limits.cpu=500m, the invalid pair F6 reported")
	}

	select {
	case event := <-recorder.Events:
		if !strings.Contains(event, "Warning") || !strings.Contains(event, "RuntimeResourcesConflict") {
			t.Errorf("event = %q, want a Warning event with reason RuntimeResourcesConflict", event)
		}
		if !strings.Contains(event, "cpu") {
			t.Errorf("event = %q, want it to name the conflicting key (cpu)", event)
		}
	default:
		t.Errorf("no event emitted: a silently stuck rollout is the failure shape #6173 was filed about")
	}

	// Convergence: a second sync with the conflict still present must not patch.
	getRV := func() string {
		s := &workloadv1alpha1.AdvancedStatefulSet{}
		if err := engine.Client.Get(context.Background(),
			types.NamespacedName{Name: "test-runtime-worker", Namespace: "default"}, s); err != nil {
			t.Fatalf("get worker sts: %v", err)
		}
		return s.ResourceVersion
	}
	rvBefore := getRV()
	if err := engine.syncRuntimeSpec(ctx, runtimeObj, runtimeClass); err != nil {
		t.Fatalf("second syncRuntimeSpec: %v", err)
	}
	if rvAfter := getRV(); rvAfter != rvBefore {
		t.Errorf("sync did not converge under a persistent conflict: resourceVersion %q -> %q", rvBefore, rvAfter)
	}
}

// F6 contract, unit layer, creation path: transformWorker must refuse a
// CacheRuntime/CacheRuntimeClass pair that merges into a request above its
// limit, instead of rendering a pod spec the API server would reject at Pod
// creation time. RED on base and previous head (no error returned), GREEN on
// the head carrying the fix.
func TestVerifyPR6177_F6_ConflictMerge_CreationRefuses(t *testing.T) {
	dataset := &datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: "test-dataset", Namespace: "default", UID: "test-dataset-uid"},
	}
	runtimeObj := &datav1alpha1.CacheRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: "test-runtime", Namespace: "default"},
		Spec: datav1alpha1.CacheRuntimeSpec{
			Worker: datav1alpha1.CacheRuntimeWorkerSpec{
				RuntimeComponentCommonSpec: datav1alpha1.RuntimeComponentCommonSpec{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m")},
					},
				},
			},
		},
	}
	runtimeClass := &datav1alpha1.CacheRuntimeClass{
		ObjectMeta:     metav1.ObjectMeta{Name: "test-class"},
		FileSystemType: "test-fs",
		Topology: &datav1alpha1.RuntimeTopology{
			Worker: &datav1alpha1.RuntimeComponentDefinition{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{Containers: []corev1.Container{{
						Name:      "worker",
						Image:     "test-worker:latest",
						Resources: verifyPR6177FullTemplateResources(),
					}}},
				},
			},
		},
	}

	scheme := CacheEngineTestScheme
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(dataset, runtimeObj).Build()
	engine := &CacheEngine{
		name:      "test-runtime",
		namespace: "default",
		Client:    fakeClient,
		Log:       ctrl.Log.WithName("test"),
	}
	config := &CacheRuntimeComponentCommonConfig{
		Owner: &common.OwnerReference{
			APIVersion: "data.fluid.io/v1alpha1",
			Kind:       "CacheRuntime",
			Name:       "test-runtime",
			UID:        "test-uid",
		},
	}
	value := &common.CacheRuntimeValue{}

	err := engine.transformWorker(dataset, runtimeObj, runtimeClass, config, value)
	if err == nil {
		t.Fatalf("transformWorker rendered a pod spec with requests.cpu=1 above limits.cpu=500m without an error; " +
			"the API server would reject the first Pod and the CacheRuntime would never become ready")
	}
	if !strings.Contains(err.Error(), "exceeds limit") {
		t.Fatalf("transformWorker error = %q, want it to name the conflicting key (\"exceeds limit\")", err)
	}
}
