/*
Verification harness for PR #6262 review (Reviewer B / Codex).
Additive test file only — no production code changes.

Polarity: INVARIANCE probe for finding F1. The PR sorts map iteration inside
getRequiredAndPreferredSchedulingRuntimes, but the function only returns maps
(which carry no order) and its consumers already iterate via
utils.OrderedKeys. This test therefore must PASS on BOTH base and head —
it documents that the hunk has no observable behavioral effect.
(encoding/json serializes maps with sorted keys, so the comparison is
order-insensitive by construction.)
*/
package nodeaffinitywithcache

import (
	"encoding/json"
	"testing"

	"github.com/fluid-cloudnative/fluid/pkg/ddc/base"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestVerifyRequiredPreferredPartitionOrderIndependent(t *testing.T) {
	mkInfos := func() map[string]base.RuntimeInfoInterface {
		riZ, err := base.BuildRuntimeInfo("z-ds", "fluid", "jindo")
		if err != nil {
			t.Fatal(err)
		}
		riA, err := base.BuildRuntimeInfo("a-ds", "fluid", "alluxio")
		if err != nil {
			t.Fatal(err)
		}
		riM, err := base.BuildRuntimeInfo("m-ds", "fluid", "juicefs")
		if err != nil {
			t.Fatal(err)
		}
		return map[string]base.RuntimeInfoInterface{
			"z-ds": riZ,
			"a-ds": riA,
			"m-ds": riM,
		}
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"fluid.io/dataset.z-ds.sched": "required",
				"fluid.io/dataset.m-ds.sched": "required",
				"unrelated-label":             "value",
			},
		},
	}

	var firstReq, firstPref string
	for i := 0; i < 200; i++ {
		required, preferred := getRequiredAndPreferredSchedulingRuntimes(pod, mkInfos())
		reqJSON, err := json.Marshal(required)
		if err != nil {
			t.Fatal(err)
		}
		prefJSON, err := json.Marshal(preferred)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			firstReq, firstPref = string(reqJSON), string(prefJSON)
			// sanity: partition must put the two "required"-labeled datasets in
			// requiredRuntimes and the rest in preferredRuntimes
			if len(required) != 2 || len(preferred) != 1 {
				t.Fatalf("partition wrong: required=%d preferred=%d, want 2/1", len(required), len(preferred))
			}
			if _, ok := required["z-ds"]; !ok {
				t.Fatalf("z-ds missing from requiredRuntimes")
			}
			if _, ok := required["m-ds"]; !ok {
				t.Fatalf("m-ds missing from requiredRuntimes")
			}
			if _, ok := preferred["a-ds"]; !ok {
				t.Fatalf("a-ds missing from preferredRuntimes")
			}
			continue
		}
		if string(reqJSON) != firstReq || string(prefJSON) != firstPref {
			t.Fatalf("iteration %d: partition output varied across runs", i)
		}
	}
}
