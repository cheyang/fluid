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

// Reviewer verification harness for PR #6177, round 4 (delta 497bbeaf..3adc547d).
//
// The delta extends validateResourceRequirements to the non-overcommittable
// family (ledger F9): request must equal limit for hugepages-* and extended
// resources, and a request needs a limit. D7/D8 pin that fix as contracts and
// are expected to FAIL on the previous head 497bbeaf and PASS on 3adc547d.
//
// E1 pins a NEW claim raised against this head (Copilot discussion_r4276559859,
// filed as ledger F11): a CacheRuntimeClass template edit that adds a pod-level
// resourceClaims entry plus the matching container claim propagates the
// container claim into an existing workload through the merged desired
// resources, while SyncComponentSpec never writes the workload's pod-level
// spec.resourceClaims. Replacement Pods then reference an undeclared claim and
// the API server rejects them ("must be one of the names in spec.resourceClaims",
// k8s.io/kubernetes pkg/apis/core/validation validateResourceClaimNames),
// stalling the rollout with no event on the CacheRuntime. The contract asserts
// the safe behavior and is expected to FAIL at head — that failure is the repro.
//
// E2 refutes Copilot's carried thread discussion_r4212708077 ("empty resource
// maps fail to clear workload limits"): an explicit limits:{} on the CacheRuntime
// still clears the workload's stale limit when the template declares none.

import (
	"context"
	"strings"
	"testing"

	workloadv1alpha1 "github.com/fluid-cloudnative/advanced-statefulset/api/workload/v1alpha1"
	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	cruntime "github.com/fluid-cloudnative/fluid/pkg/runtime"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
)

const v6177r4ConflictReason = "RuntimeResourcesConflict"

const (
	v6177r4GPU       corev1.ResourceName = "nvidia.com/gpu"
	v6177r4HugePages corev1.ResourceName = "hugepages-2Mi"
)

// v6177r4WorkerTemplate returns a class-template worker pod spec with the given
// container resources (and optional pod-level claims for E1).
func v6177r4WorkerPodTemplate(resources corev1.ResourceRequirements, podClaims []corev1.PodResourceClaim) corev1.PodTemplateSpec {
	return corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			ResourceClaims: podClaims,
			Containers: []corev1.Container{{
				Name:      "worker",
				Image:     "fluid/cache:v1",
				Resources: *resources.DeepCopy(),
			}},
		},
	}
}

// D7 (contract, ledger F9): the validator must mirror the API server's rules
// for resources that cannot be overcommitted, exactly as
// k8s.io/kubernetes@v1.29.15 pkg/apis/core/validation.ValidateResourceRequirements
// states them (validation.go:6568-6578): request == limit, and a limit whenever
// a request is set. A merge of two individually valid inputs must be caught.
func TestD7NonOvercommittableMergeRules(t *testing.T) {
	cases := []struct {
		name    string
		base    corev1.ResourceRequirements
		overlay corev1.ResourceRequirements
		wantErr string // empty means the merge must be accepted
	}{
		{
			name:    "gpu overlay raising only the limit",
			base:    corev1.ResourceRequirements{Requests: corev1.ResourceList{v6177r4GPU: resource.MustParse("1")}, Limits: corev1.ResourceList{v6177r4GPU: resource.MustParse("1")}},
			overlay: corev1.ResourceRequirements{Limits: corev1.ResourceList{v6177r4GPU: resource.MustParse("2")}},
			wantErr: "nvidia.com/gpu request 1 must equal limit 2",
		},
		{
			name:    "hugepages overlay raising only the limit",
			base:    corev1.ResourceRequirements{Requests: corev1.ResourceList{v6177r4HugePages: resource.MustParse("1Gi")}, Limits: corev1.ResourceList{v6177r4HugePages: resource.MustParse("1Gi")}},
			overlay: corev1.ResourceRequirements{Limits: corev1.ResourceList{v6177r4HugePages: resource.MustParse("2Gi")}},
			wantErr: "hugepages-2Mi request 1Gi must equal limit 2Gi",
		},
		{
			name:    "gpu request with no limit at all",
			base:    corev1.ResourceRequirements{Requests: corev1.ResourceList{v6177r4GPU: resource.MustParse("1")}},
			overlay: corev1.ResourceRequirements{},
			wantErr: "nvidia.com/gpu request 1 has no limit",
		},
		{
			name:    "gpu request above its limit is reported as unequal",
			base:    corev1.ResourceRequirements{Requests: corev1.ResourceList{v6177r4GPU: resource.MustParse("2")}, Limits: corev1.ResourceList{v6177r4GPU: resource.MustParse("1")}},
			overlay: corev1.ResourceRequirements{},
			wantErr: "nvidia.com/gpu request 2 must equal limit 1",
		},
		{
			name: "equal pair accepted, quantities spelled two ways",
			base: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{v6177r4GPU: resource.MustParse("1"), v6177r4HugePages: resource.MustParse("1024Mi")},
				Limits:   corev1.ResourceList{v6177r4GPU: resource.MustParse("1"), v6177r4HugePages: resource.MustParse("1Gi")},
			},
		},
		{
			name: "limit alone accepted, the API server copies it into the request",
			base: corev1.ResourceRequirements{Limits: corev1.ResourceList{v6177r4GPU: resource.MustParse("1")}},
		},
		{
			name: "native request below its limit still accepted",
			base: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")}},
		},
		{
			// k8s.io/kubernetes IsNativeResource: a name containing the
			// kubernetes.io/ default namespace prefix counts as native, so a
			// request below its limit is allowed there and must be here.
			name: "kubernetes.io-prefixed name keeps native semantics",
			base: corev1.ResourceRequirements{Requests: corev1.ResourceList{"example.kubernetes.io/foo": resource.MustParse("1")}, Limits: corev1.ResourceList{"example.kubernetes.io/foo": resource.MustParse("2")}},
		},
		{
			name:    "cpu conflict and gpu conflict are named together",
			base:    corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1"), v6177r4GPU: resource.MustParse("1")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m")}},
			overlay: corev1.ResourceRequirements{},
			wantErr: "cpu request 1 exceeds limit 500m; nvidia.com/gpu request 1 has no limit",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			merged := mergeResourceRequirements(tc.base, tc.overlay)
			err := validateResourceRequirements(merged)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("merged %v rejected: %v — the API server accepts this pair", merged, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("merged %v accepted; the API server rejects it: want error containing %q", merged, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not name the conflict; want it to contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

// D8 (contract, ledger F9): the sync path must refuse a merged non-overcommittable
// conflict the same way it refuses a request above its limit — the workload's
// resources stay untouched, replicas still move, a RuntimeResourcesConflict
// Warning event names the key, and a second sync does not patch (convergent).
func TestD8SyncRefusesNonOvercommittableConflict(t *testing.T) {
	replicas := int32(1)
	template := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{v6177r4GPU: resource.MustParse("1")},
		Limits:   corev1.ResourceList{v6177r4GPU: resource.MustParse("1")},
	}
	asts := &workloadv1alpha1.AdvancedStatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "test-worker", Namespace: "default"},
		Spec: workloadv1alpha1.AdvancedStatefulSetSpec{
			Replicas: &replicas,
			Template: v6177r4WorkerPodTemplate(template, nil),
		},
	}
	runtimeObj := &datav1alpha1.CacheRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: datav1alpha1.CacheRuntimeSpec{
			RuntimeClassName: "test-class",
			Worker: datav1alpha1.CacheRuntimeWorkerSpec{
				Replicas: 3,
				RuntimeComponentCommonSpec: datav1alpha1.RuntimeComponentCommonSpec{
					Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{v6177r4GPU: resource.MustParse("2")}},
				},
			},
		},
	}
	runtimeClass := &datav1alpha1.CacheRuntimeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "test-class"},
		Topology: &datav1alpha1.RuntimeTopology{
			Worker: &datav1alpha1.RuntimeComponentDefinition{Template: v6177r4WorkerPodTemplate(template, nil)},
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
	gotRequest, gotLimit := got.Requests[v6177r4GPU], got.Limits[v6177r4GPU]
	if gotLimit.String() != "1" || gotRequest.String() != "1" {
		t.Fatalf("workload resources: got gpu request=%s limit=%s, want the untouched 1/1 — the invalid merge must not be patched",
			gotRequest.String(), gotLimit.String())
	}

	if synced.Spec.Replicas == nil || *synced.Spec.Replicas != 3 {
		t.Fatalf("replicas: got %v, want 3 — the resource conflict must not block the replica sync", synced.Spec.Replicas)
	}

	select {
	case ev := <-recorder.Events:
		if !strings.Contains(ev, v6177r4ConflictReason) {
			t.Errorf("event reason: got %q, want it to contain %q", ev, v6177r4ConflictReason)
		}
		if !strings.Contains(ev, "nvidia.com/gpu request 1 must equal limit 2") {
			t.Errorf("event body: got %q, want it to name the conflicting pair", ev)
		}
	default:
		t.Fatal("no RuntimeResourcesConflict event was emitted for the non-overcommittable conflict")
	}

	// Convergence under a persistent conflict: a second sync patches nothing.
	rv := synced.ResourceVersion
	if err := engine.syncRuntimeSpec(cruntime.ReconcileRequestContext{Context: context.TODO()}, runtimeObj, runtimeClass); err != nil {
		t.Fatalf("second syncRuntimeSpec failed: %v", err)
	}
	synced2 := &workloadv1alpha1.AdvancedStatefulSet{}
	if err := engine.Client.Get(context.TODO(), key, synced2); err != nil {
		t.Fatalf("get ASTS after second sync: %v", err)
	}
	if synced2.ResourceVersion != rv {
		t.Fatalf("second sync patched the workload (resourceVersion %s -> %s): the conflict loop is not convergent", rv, synced2.ResourceVersion)
	}
}

// E1 (contract, ledger F11 — new this round): a CacheRuntimeClass edit that adds
// a pod-level resourceClaims entry and the matching container claim must not
// leave an existing workload's pod template referencing a claim name the
// workload does not declare at pod level — that pod spec is rejected by the API
// server on every replacement Pod and the rollout stalls with no signal on the
// CacheRuntime. Either propagate the pod-level declaration too, or skip the
// claims sync (and say why). EXPECTED TO FAIL at head 3adc547d; the failure is
// the reproduction.
func TestE1ClassClaimAdditionMustNotWedgeExistingWorkload(t *testing.T) {
	replicas := int32(1)
	// The workload as created before the class edit: no claims anywhere.
	asts := &workloadv1alpha1.AdvancedStatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "test-worker", Namespace: "default"},
		Spec: workloadv1alpha1.AdvancedStatefulSetSpec{
			Replicas: &replicas,
			Template: v6177r4WorkerPodTemplate(corev1.ResourceRequirements{}, nil),
		},
	}
	// The CacheRuntime declares nothing; only the class template carries the
	// newly added claim pair (valid for freshly rendered workloads).
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
			Worker: &datav1alpha1.RuntimeComponentDefinition{
				Template: v6177r4WorkerPodTemplate(
					corev1.ResourceRequirements{
						Claims: []corev1.ResourceClaim{{Name: "shm"}},
					},
					[]corev1.PodResourceClaim{{Name: "shm"}},
				),
			},
		},
	}
	engine := v6177r2Engine(t, asts, runtimeObj, runtimeClass)
	recorder := record.NewFakeRecorder(10)
	engine.Recorder = recorder

	if err := engine.syncRuntimeSpec(cruntime.ReconcileRequestContext{Context: context.TODO()}, runtimeObj, runtimeClass); err != nil {
		t.Fatalf("syncRuntimeSpec failed: %v", err)
	}

	key := types.NamespacedName{Name: "test-worker", Namespace: "default"}
	synced := &workloadv1alpha1.AdvancedStatefulSet{}
	if err := engine.Client.Get(context.TODO(), key, synced); err != nil {
		t.Fatalf("get ASTS: %v", err)
	}

	container := synced.Spec.Template.Spec.Containers[0]
	declared := map[string]bool{}
	for _, pc := range synced.Spec.Template.Spec.ResourceClaims {
		declared[pc.Name] = true
	}
	for _, claim := range container.Resources.Claims {
		if !declared[claim.Name] {
			t.Fatalf("F11 repro: after the class edit, the workload's container references claim %q which the workload's pod-level spec.resourceClaims does not declare (%v) — every replacement Pod is rejected by the API server (validateResourceClaimNames) and the rollout stalls with no event on the CacheRuntime",
				claim.Name, synced.Spec.Template.Spec.ResourceClaims)
		}
	}

	// The safe outcome must also not be silent: skipping or propagating both
	// deserve a signal (event) OR the pod-level declaration must be present.
	if len(container.Resources.Claims) > 0 && len(synced.Spec.Template.Spec.ResourceClaims) == 0 {
		t.Fatal("F11 repro: container claims were synced without the pod-level declarations")
	}
}

// E2 (contract, refutes Copilot discussion_r4212708077): an explicit empty
// limits map on the CacheRuntime reads the same as omitting it, so clearing a
// previously set limit with `limits: {}` (template declaring none) must clear
// the workload's stale limit — not retain it forever.
func TestE2ExplicitEmptyMapStillClearsWorkload(t *testing.T) {
	replicas := int32(1)
	stale := corev1.ResourceRequirements{Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("8Gi")}}
	asts := &workloadv1alpha1.AdvancedStatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "test-worker", Namespace: "default"},
		Spec: workloadv1alpha1.AdvancedStatefulSetSpec{
			Replicas: &replicas,
			Template: v6177r4WorkerPodTemplate(stale, nil),
		},
	}
	runtimeObj := &datav1alpha1.CacheRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: datav1alpha1.CacheRuntimeSpec{
			RuntimeClassName: "test-class",
			Worker: datav1alpha1.CacheRuntimeWorkerSpec{
				RuntimeComponentCommonSpec: datav1alpha1.RuntimeComponentCommonSpec{
					Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{}}, // explicit empty map
				},
			},
		},
	}
	runtimeClass := &datav1alpha1.CacheRuntimeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "test-class"},
		Topology: &datav1alpha1.RuntimeTopology{
			Worker: &datav1alpha1.RuntimeComponentDefinition{Template: v6177r4WorkerPodTemplate(corev1.ResourceRequirements{}, nil)},
		},
	}
	engine := v6177r2Engine(t, asts, runtimeObj, runtimeClass)

	if err := engine.syncRuntimeSpec(cruntime.ReconcileRequestContext{Context: context.TODO()}, runtimeObj, runtimeClass); err != nil {
		t.Fatalf("syncRuntimeSpec failed: %v", err)
	}

	key := types.NamespacedName{Name: "test-worker", Namespace: "default"}
	synced := &workloadv1alpha1.AdvancedStatefulSet{}
	if err := engine.Client.Get(context.TODO(), key, synced); err != nil {
		t.Fatalf("get ASTS: %v", err)
	}

	got := synced.Spec.Template.Spec.Containers[0].Resources
	if got.Limits != nil {
		t.Fatalf("workload limits: got %v, want nil — an explicit limits:{} must clear the stale 8Gi (Copilot r4212708077 claims it is retained forever)", got.Limits)
	}
}
