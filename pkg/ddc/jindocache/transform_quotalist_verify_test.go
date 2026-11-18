/*
Verification harness for PR #6261 (fluid-cloudnative/fluid).
Reviewer: Codex (reviewer B). Additive only - does not modify production code.

  P0 (premise, BASE): jindocache transformWorkerResources panics on "10Gi,5Gi".
  F1 (contract): jindocache worker memory request for quotaList "10Gi,5Gi" is the sum "15Gi".
  F2 (contract): Mi-denominated quota list must sum in Mi, not milli-bytes.
*/

package jindocache

import (
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newVerifyJindoCacheEngine(t *testing.T, rt *datav1alpha1.JindoRuntime) *JindoCacheEngine {
	t.Helper()
	client := fake.NewFakeClientWithScheme(datav1alpha1.UnitTestScheme, rt)
	return &JindoCacheEngine{
		name:      rt.Name,
		namespace: rt.Namespace,
		Client:    client,
		Log:       fake.NullLogger(),
	}
}

func newVerifyJindoCacheRuntime() *datav1alpha1.JindoRuntime {
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

func TestVerifyJindoCacheQuotaListSumNoPanic(t *testing.T) {
	rt := newVerifyJindoCacheRuntime()
	engine := newVerifyJindoCacheEngine(t, rt)
	value := &Jindo{}

	// On the base branch this call panics:
	//   panic: cannot parse '10Gi,5Gi': quantities must match the regular expression ...
	err := engine.transformWorkerResources(rt, value, "10g,5g")
	if err != nil {
		t.Fatalf("expected no error for valid quota list, got: %v", err)
	}
	if value.Worker.Resources.Requests[corev1.ResourceMemory] != "15Gi" {
		t.Fatalf("expected worker memory request 15Gi, got %q",
			value.Worker.Resources.Requests[corev1.ResourceMemory])
	}
}

func TestVerifyJindoCacheQuotaListMiUnitsSum(t *testing.T) {
	rt := newVerifyJindoCacheRuntime()
	engine := newVerifyJindoCacheEngine(t, rt)
	value := &Jindo{}

	err := engine.transformWorkerResources(rt, value, "2048m,2048m")
	if err != nil {
		t.Fatalf("expected no error for Mi quota list, got: %v", err)
	}
	if value.Worker.Resources.Requests[corev1.ResourceMemory] != "4Gi" {
		t.Fatalf("expected worker memory request 4Gi (4096Mi), got %q",
			value.Worker.Resources.Requests[corev1.ResourceMemory])
	}
}
