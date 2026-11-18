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
