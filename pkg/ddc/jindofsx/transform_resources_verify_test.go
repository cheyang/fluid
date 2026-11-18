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

package jindofsx

import (
	"context"
	"strings"
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Reviewer harness for PR #6261 (fluid-cloudnative/fluid), jindofsx layer.
//
// These are CONTRACT tests (green = correct): on the merge base 1321a217 the
// same calls panic inside resource.MustParse (see results/ for the captured
// base panic), so these tests bite on the pre-fix code.

func newQuotaListRuntime(name, namespace, limit, quotaList string) *datav1alpha1.JindoRuntime {
	rt := &datav1alpha1.JindoRuntime{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: datav1alpha1.JindoRuntimeSpec{
			TieredStore: datav1alpha1.TieredStore{
				Levels: []datav1alpha1.Level{{
					MediumType: common.Memory,
					Path:       "/dev/shm,/mnt/ram",
					QuotaList:  quotaList,
				}},
			},
		},
	}
	if limit != "" {
		rt.Spec.Worker.Resources.Limits = corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse(limit),
		}
	}
	return rt
}

// C1 / premise P0: a multi-value quotaList must not panic, and the worker
// memory request must become the SUM of the per-path quotas (10Gi+5Gi=15Gi).
// The runtime persisted in the fake client must also be updated to 15Gi.
func TestVerifyQuotaListSummedRequest(t *testing.T) {
	rt := newQuotaListRuntime("hbase", "fluid", "20Gi", "10Gi,5Gi")
	client := fake.NewFakeClientWithScheme(testScheme, rt.DeepCopy())
	engine := &JindoFSxEngine{
		name:      "hbase",
		namespace: "fluid",
		Client:    client,
		Log:       fake.NullLogger(),
	}
	value := &Jindo{}

	err := engine.transformWorkerResources(rt, value, "10g,5g")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got := value.Worker.Resources.Requests.Memory; got != "15Gi" {
		t.Errorf("worker memory request = %q, want %q", got, "15Gi")
	}

	var updated datav1alpha1.JindoRuntime
	if err := client.Get(context.TODO(), types.NamespacedName{Name: "hbase", Namespace: "fluid"}, &updated); err != nil {
		t.Fatalf("failed to get updated runtime: %v", err)
	}
	persisted, ok := updated.Spec.Worker.Resources.Requests[corev1.ResourceMemory]
	if !ok || persisted.String() != "15Gi" {
		t.Errorf("persisted runtime worker memory request = %v (ok=%t), want 15Gi", persisted, ok)
	}
}

// C2: single-value quota behavior is unchanged (5g -> 5Gi request).
func TestVerifySingleQuotaStillWorks(t *testing.T) {
	rt := newQuotaListRuntime("spark", "fluid", "20Gi", "5Gi")
	client := fake.NewFakeClientWithScheme(testScheme, rt.DeepCopy())
	engine := &JindoFSxEngine{
		name:      "spark",
		namespace: "fluid",
		Client:    client,
		Log:       fake.NullLogger(),
	}
	value := &Jindo{}

	err := engine.transformWorkerResources(rt, value, "5g")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got := value.Worker.Resources.Requests.Memory; got != "5Gi" {
		t.Errorf("worker memory request = %q, want %q", got, "5Gi")
	}
}

// C3: whitespace inside the CRD-documented quotaList format ("100Gi, 50Gi")
// must survive the round trip (SumQuotas must TrimSpace).
func TestVerifyQuotaListWithSpaces(t *testing.T) {
	rt := newQuotaListRuntime("hadoop", "fluid", "200Gi", "100Gi, 50Gi")
	client := fake.NewFakeClientWithScheme(testScheme, rt.DeepCopy())
	engine := &JindoFSxEngine{
		name:      "hadoop",
		namespace: "fluid",
		Client:    client,
		Log:       fake.NullLogger(),
	}
	value := &Jindo{}

	// "100g, 50g" is what transform() hands down for QuotaList "100Gi, 50Gi"
	err := engine.transformWorkerResources(rt, value, "100g, 50g")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got := value.Worker.Resources.Requests.Memory; got != "150Gi" {
		t.Errorf("worker memory request = %q, want %q", got, "150Gi")
	}
}

// C6 (round 2, F1): a Mi-denominated quotaList must sum as Mi, not as
// milli-bytes. transform() hands "2048Mi,2048Mi" down as "2048m,2048m";
// the worker memory request must be 4096Mi (== 4Gi) and must be persisted
// into the runtime spec. On head 22d2d07c (round 1) this produced "4096m"
// (~4 bytes) silently; on base the input panicked.
func TestVerifyQuotaListMiUnitsSum(t *testing.T) {
	rt := newQuotaListRuntime("hbase-mi", "fluid", "8Gi", "2048Mi,2048Mi")
	client := fake.NewFakeClientWithScheme(testScheme, rt.DeepCopy())
	engine := &JindoFSxEngine{
		name:      "hbase-mi",
		namespace: "fluid",
		Client:    client,
		Log:       fake.NullLogger(),
	}
	value := &Jindo{}

	err := engine.transformWorkerResources(rt, value, "2048m,2048m")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	got := value.Worker.Resources.Requests.Memory
	gotQty, parseErr := resource.ParseQuantity(got)
	if parseErr != nil {
		t.Fatalf("worker memory request %q is not a quantity: %v", got, parseErr)
	}
	if want := resource.MustParse("4Gi"); gotQty.Cmp(want) != 0 {
		t.Errorf("worker memory request = %q, want 4Gi (4096Mi); a milli-byte value like \"4096m\" means the m->Mi restoration is missing", got)
	}

	var updated datav1alpha1.JindoRuntime
	if err := client.Get(context.TODO(), types.NamespacedName{Name: "hbase-mi", Namespace: "fluid"}, &updated); err != nil {
		t.Fatalf("failed to get updated runtime: %v", err)
	}
	persisted, ok := updated.Spec.Worker.Resources.Requests[corev1.ResourceMemory]
	if !ok || persisted.Cmp(resource.MustParse("4Gi")) != 0 {
		t.Errorf("persisted runtime worker memory request = %v (ok=%t), want 4Gi (4096Mi)", persisted, ok)
	}
}

// C7 (round 2, F5): the single-value Mi quota path shares the same
// restoration; "512Mi" handed down as "512m" must yield a 512Mi request
// (round 1: 512 milli-bytes, pre-existing at base too).
func TestVerifySingleQuotaMiUnits(t *testing.T) {
	rt := newQuotaListRuntime("spark-mi", "fluid", "8Gi", "512Mi")
	client := fake.NewFakeClientWithScheme(testScheme, rt.DeepCopy())
	engine := &JindoFSxEngine{
		name:      "spark-mi",
		namespace: "fluid",
		Client:    client,
		Log:       fake.NullLogger(),
	}
	value := &Jindo{}

	err := engine.transformWorkerResources(rt, value, "512m")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	got := value.Worker.Resources.Requests.Memory
	gotQty, parseErr := resource.ParseQuantity(got)
	if parseErr != nil {
		t.Fatalf("worker memory request %q is not a quantity: %v", got, parseErr)
	}
	if want := resource.MustParse("512Mi"); gotQty.Cmp(want) != 0 {
		t.Errorf("worker memory request = %q, want 512Mi", got)
	}
}

// C8 (round 2, F1 guard): with the m->Mi restoration in place, a Mi-denominated
// sum CAN exceed the worker memory limit, so the guard must be able to fire.
func TestVerifyMiUnitsSumExceedsLimitReturnsError(t *testing.T) {
	rt := newQuotaListRuntime("flink-mi", "fluid", "2Gi", "2048Mi,2048Mi")
	engine := &JindoFSxEngine{Log: fake.NullLogger()}
	err := engine.transformWorkerResources(rt, &Jindo{}, "2048m,2048m")
	if err == nil || !strings.Contains(err.Error(), "greater than") {
		t.Errorf("expected error containing %q, got %v", "greater than", err)
	}
}

// C4: summed quota above the worker memory limit must return an error, not panic.
func TestVerifySumExceedsLimitReturnsError(t *testing.T) {
	rt := newQuotaListRuntime("spark", "fluid", "12Gi", "10Gi,5Gi")
	engine := &JindoFSxEngine{Log: fake.NullLogger()}
	err := engine.transformWorkerResources(rt, &Jindo{}, "10g,5g")
	if err == nil || !strings.Contains(err.Error(), "greater than") {
		t.Errorf("expected error containing %q, got %v", "greater than", err)
	}
}

// C5: an invalid quota in the list must return an error, not panic.
func TestVerifyInvalidQuotaReturnsError(t *testing.T) {
	rt := newQuotaListRuntime("spark", "fluid", "20Gi", "10Gi,abc")
	engine := &JindoFSxEngine{Log: fake.NullLogger()}
	err := engine.transformWorkerResources(rt, &Jindo{}, "10Gi,abc")
	if err == nil || !strings.Contains(err.Error(), "invalid quota") {
		t.Errorf("expected error containing %q, got %v", "invalid quota", err)
	}
}
