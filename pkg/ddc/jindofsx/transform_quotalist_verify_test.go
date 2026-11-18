/*
Verification harness for PR #6261 (fluid-cloudnative/fluid).
Reviewer: Codex (reviewer B). Additive only - does not modify production code.

Claims under test (see docs/verification/jindo-quotalist-sum-codex/):
  P0 (premise, runs against BASE): JindoRuntime with MEM tieredstore level and a
     comma-separated quotaList ("10Gi,5Gi") panics the controller in
     transformWorkerResources via resource.MustParse("10Gi,5Gi").
     -> On the BASE branch this test PANICS (= premise confirmed).
     -> On the PR head it must PASS (no panic, request == summed quota).
  F1 (contract): worker memory request for quotaList "10Gi,5Gi" equals the sum "15Gi",
     and the summed value is persisted back to the runtime CR spec.
  F2 (contract): quotaList with Mi units ("2048Mi,2048Mi" -> jindo units "2048m,2048m")
     must yield a 4096Mi ("4Gi") worker memory request. k8s quantity semantics parse
     "2048m" as 2048 MILLI-bytes, so a correct fix must restore "m"->"Mi" before summing.
  F3 (contract): an invalid quota element returns an error instead of panicking.
*/

package jindofsx

import (
	"strings"
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newVerifyEngine(t *testing.T, rt *datav1alpha1.JindoRuntime) *JindoFSxEngine {
	t.Helper()
	client := fake.NewFakeClientWithScheme(datav1alpha1.UnitTestScheme, rt)
	return &JindoFSxEngine{
		name:      rt.Name,
		namespace: rt.Namespace,
		Client:    client,
		Log:       fake.NullLogger(),
	}
}

func newVerifyRuntime() *datav1alpha1.JindoRuntime {
	return &datav1alpha1.JindoRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: "jindo-demo", Namespace: "fluid"},
		Spec: datav1alpha1.JindoRuntimeSpec{
			TieredStore: datav1alpha1.TieredStore{
				Levels: []datav1alpha1.Level{{
					MediumType: common.Memory,
					Path:       "/dev/shm,/mnt/ram",
					QuotaList:  "10Gi,5Gi",
				}},
			},
		},
	}
}

// P0+F1: comma-separated Gi quotaList must not panic and must be summed.
func TestVerifyQuotaListSumNoPanic(t *testing.T) {
	rt := newVerifyRuntime()
	engine := newVerifyEngine(t, rt)
	value := &Jindo{}

	// On the base branch this call panics with:
	//   panic: cannot parse '10Gi,5Gi': quantities must match the regular expression ...
	err := engine.transformWorkerResources(rt, value, "10g,5g")
	if err != nil {
		t.Fatalf("expected no error for valid quota list, got: %v", err)
	}
	if value.Worker.Resources.Requests.Memory != "15Gi" {
		t.Fatalf("expected worker memory request 15Gi (sum of 10Gi+5Gi), got %q",
			value.Worker.Resources.Requests.Memory)
	}

	// the summed quantity must also be persisted back into the runtime CR spec
	updated, err := engine.getRuntime()
	if err != nil {
		t.Fatalf("getRuntime: %v", err)
	}
	got := updated.Spec.Worker.Resources.Requests.Memory()
	if got == nil || got.String() != "15Gi" {
		t.Fatalf("expected persisted spec.worker.resources.requests.memory=15Gi, got %v", got)
	}
}

// F2: Mi-denominated quota lists must be summed in Mi, not in milli-bytes.
// transform() normalizes quotaList "2048Mi,2048Mi" to jindo units "2048m,2048m";
// transformWorkerResources restores "g"->"Gi" but not "m"->"Mi".
func TestVerifyQuotaListMiUnitsSum(t *testing.T) {
	rt := newVerifyRuntime()
	engine := newVerifyEngine(t, rt)
	value := &Jindo{}

	err := engine.transformWorkerResources(rt, value, "2048m,2048m")
	if err != nil {
		t.Fatalf("expected no error for Mi quota list, got: %v", err)
	}
	// 2048Mi + 2048Mi = 4096Mi = 4Gi. Anything else (e.g. "4096m" = 4.096 bytes) is wrong.
	if value.Worker.Resources.Requests.Memory != "4Gi" {
		t.Fatalf("expected worker memory request 4Gi (4096Mi), got %q",
			value.Worker.Resources.Requests.Memory)
	}
}

// F3: invalid quota element must return an error, not panic.
func TestVerifyInvalidQuotaListReturnsError(t *testing.T) {
	rt := newVerifyRuntime()
	engine := newVerifyEngine(t, rt)
	value := &Jindo{}

	err := engine.transformWorkerResources(rt, value, "10g,abc")
	if err == nil {
		t.Fatalf("expected an error for invalid quota element")
	}
	if !strings.Contains(err.Error(), "abc") {
		t.Fatalf("error should name the offending element, got: %v", err)
	}
}
