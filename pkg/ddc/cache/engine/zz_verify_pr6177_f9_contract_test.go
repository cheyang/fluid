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

// Round-3 contract test for finding F9 (PR #6177, new this round): the merged
// resources can still be a pod spec the API server rejects even when no
// request exceeds its limit. Kubernetes requires request == limit for
// non-overcommittable resources -- hugepages-* and every extended resource
// (a name with a domain slash, e.g. nvidia.com/gpu):
//
//	k8s.io/kubernetes@v1.29.15 pkg/apis/core/validation/validation.go
//	ValidateResourceRequirements: "For non overcommitable resources, not
//	only requests can't exceed limits, they also can't be lower, i.e. must
//	be equal."  (helper.IsOvercommitAllowed = native non-hugepages only)
//
// The merge can synthesize exactly that from two individually valid documents:
// a template declaring cpu plus hugepages-2Mi request=1Gi limit=1Gi, and a
// CacheRuntime raising the hugepages limit to 2Gi (restating cpu so it stays
// valid on its own). Both are specs the API server accepts. The merge yields
// hugepages-2Mi request 1Gi != limit 2Gi; the head's
// validateResourceRequirements only rejects request > limit
// (transform_common.go), so it accepts the pair and syncRuntimeSpec applies it
// with no Warning event -- the same silent stuck-rollout shape as F6, one
// resource family over.
//
// Differential, same input: on base e0fc4c18 the sync wholesale-replaces with
// the CacheRuntime's list, whose hugepages request the API server defaults to
// the limit -- a valid pod spec. On head the merged (invalid) pair is applied
// instead, and the F6 gate added this round does not catch it. So the
// apiserver-invalid applied state is introduced by this PR's merge and survives
// its remediation. (Proven against a real API server: see
// docs/verification/cacheruntime-resources-merge/results/round3/envtest-apiserver-rule.txt.)
//
// Polarity: CONTRACT. Compile-safe on base e0fc4c18, previous head e259d738
// and head 497bbeaf (pre-existing symbols only). RED on all three today: on
// base because it patches the workload at all (this contract demands the
// controller refuse), on previous head and head because the apiserver-invalid
// pair is applied silently. Goes GREEN only when the gate mirrors the
// apiserver rule (reject request != limit for non-overcommittable resources).

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/tools/record"
)

// F9 contract, integration layer (fake client): when the merged worker
// resources set request != limit on a non-overcommittable resource,
// syncRuntimeSpec must NOT patch the workload's resources and must emit a
// Warning event -- the apiserver rejects the first Pod ("must be equal to
// hugepages-2Mi limit of 2Gi"), sticking the rollout silently.
func TestVerifyPR6177_F9_NonOvercommittableMerge_SyncMustRefuse(t *testing.T) {
	hugepages := corev1.ResourceName("hugepages-2Mi")
	// Individually valid template: cpu present, hugepages request == limit.
	template := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("1"),
			hugepages:          resource.MustParse("1Gi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("2"),
			hugepages:          resource.MustParse("1Gi"),
		},
	}
	// Individually valid overlay: raising only the hugepages limit; its
	// hugepages request is absent, which the API server would default to the
	// limit, so on its own (and on the base's wholesale apply) this is valid.
	overlay := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("2"),
			hugepages:          resource.MustParse("2Gi"),
		},
	}
	// The workload currently carries the (valid, equal) template values.
	engine, ctx, runtimeObj, runtimeClass := verifyPR6177BuildSyncFixture(template, &template, &overlay)
	recorder := record.NewFakeRecorder(10)
	engine.Recorder = recorder

	before := verifyPR6177WorkerResources(t, engine)

	if err := engine.syncRuntimeSpec(ctx, runtimeObj, runtimeClass); err != nil {
		t.Fatalf("syncRuntimeSpec returned %v; the conflict must not fail the sync (image/replicas still sync)", err)
	}

	got := verifyPR6177WorkerResources(t, engine)
	req, reqOK := got.Requests[hugepages]
	lim, limOK := got.Limits[hugepages]
	if reqOK && limOK && req.Cmp(lim) != 0 {
		t.Errorf("workload now carries hugepages-2Mi request %s != limit %s: the API server rejects that "+
			"pod spec (non-overcommittable resources must be equal), so the rollout sticks with no Warning event",
			req.String(), lim.String())
	}
	if before.Limits[hugepages] != got.Limits[hugepages] {
		t.Errorf("workload resources changed under an apiserver-invalid merge: before %+v, after %+v", before, got)
	}

	drainedWarn := false
	for {
		select {
		case event := <-recorder.Events:
			if strings.Contains(event, "Warning") && strings.Contains(event, "hugepages") {
				drainedWarn = true
			}
		default:
			if !drainedWarn {
				t.Errorf("no Warning event naming the conflicting hugepages key was emitted; " +
					"the stuck rollout is invisible on the CacheRuntime")
			}
			return
		}
	}
}
