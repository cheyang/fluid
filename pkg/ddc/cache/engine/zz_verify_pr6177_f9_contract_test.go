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
// a template declaring hugepages-2Mi request=1Gi limit=1Gi, plus a CacheRuntime
// raising only the limit to 2Gi, merges into request 1Gi != limit 2Gi. The
// head's validateResourceRequirements only rejects request > limit
// (transform_common.go), so it accepts the pair and syncRuntimeSpec applies it
// with no Warning event -- the same silent stuck-rollout shape as F6, one
// resource family over. On the base the same input wholesale-dropped the
// requests, leaving a limit-only list the API server defaults to request=limit
// (valid), so this exposure was introduced by this PR's merge and is not closed
// by the head's validation.
//
// Polarity: CONTRACT. Compile-safe on base e0fc4c18, previous head e259d738
// and head 497bbeaf (pre-existing symbols only). Expected RED on all three
// (the exposure exists on every ref; only the head's remediation attempt is
// incomplete). Goes GREEN when the gate mirrors the apiserver rule.

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
// Warning event -- the apiserver would reject the first Pod ("must be equal to
// hugepages-2Mi limit of 2Gi"), sticking the rollout silently.
func TestVerifyPR6177_F9_NonOvercommittableMerge_SyncMustRefuse(t *testing.T) {
	hugepages := corev1.ResourceName("hugepages-2Mi")
	template := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{hugepages: resource.MustParse("1Gi")},
		Limits:   corev1.ResourceList{hugepages: resource.MustParse("1Gi")},
	}
	overlay := corev1.ResourceRequirements{
		Limits: corev1.ResourceList{hugepages: resource.MustParse("2Gi")},
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
