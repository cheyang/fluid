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

package jindocache

import (
	"strings"
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
)

// Reviewer harness for PR #6261 (fluid-cloudnative/fluid), jindocache layer.
// CONTRACT tests (green = correct): on the merge base 1321a217 the multi-value
// cases panic inside resource.MustParse.

// C1: multi-value quotaList sums into the worker memory request (10g+5g=15Gi).
func TestVerifyJindoCacheQuotaListSummedRequest(t *testing.T) {
	dataset, jindoruntime := mockFluidObjectsForTests(types.NamespacedName{Namespace: "fluid", Name: "jindo-demo"})
	jindoruntime.Spec.TieredStore = datav1alpha1.TieredStore{
		Levels: []datav1alpha1.Level{{
			MediumType: common.Memory,
			Path:       "/dev/shm,/mnt/ram",
			QuotaList:  "10Gi,5Gi",
		}},
	}
	jindoruntime.Spec.Worker.Resources = corev1.ResourceRequirements{
		Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("20Gi")},
	}
	engine := mockJindoCacheEngineForTests(dataset, jindoruntime)
	engine.Client = fake.NewFakeClientWithScheme(datav1alpha1.UnitTestScheme, dataset.DeepCopy(), jindoruntime.DeepCopy())

	value := Jindo{}
	err := engine.transformWorkerResources(jindoruntime, &value, "10g,5g")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got := value.Worker.Resources.Requests[corev1.ResourceMemory]; got != "15Gi" {
		t.Errorf("worker memory request = %q, want %q", got, "15Gi")
	}
}

// C2: invalid quota in the list returns an error instead of panicking.
func TestVerifyJindoCacheInvalidQuotaReturnsError(t *testing.T) {
	dataset, jindoruntime := mockFluidObjectsForTests(types.NamespacedName{Namespace: "fluid", Name: "jindo-demo"})
	jindoruntime.Spec.TieredStore = datav1alpha1.TieredStore{
		Levels: []datav1alpha1.Level{{
			MediumType: common.Memory,
			Path:       "/dev/shm,/mnt/ram",
			QuotaList:  "10Gi,abc",
		}},
	}
	jindoruntime.Spec.Worker.Resources = corev1.ResourceRequirements{
		Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("20Gi")},
	}
	engine := mockJindoCacheEngineForTests(dataset, jindoruntime)
	engine.Client = fake.NewFakeClientWithScheme(datav1alpha1.UnitTestScheme, dataset.DeepCopy(), jindoruntime.DeepCopy())

	value := Jindo{}
	err := engine.transformWorkerResources(jindoruntime, &value, "10Gi,abc")
	if err == nil || !strings.Contains(err.Error(), "invalid quota") {
		t.Errorf("expected error containing %q, got %v", "invalid quota", err)
	}
}

// C3: summed quota above the worker memory limit returns an error instead of panicking.
func TestVerifyJindoCacheSumExceedsLimitReturnsError(t *testing.T) {
	dataset, jindoruntime := mockFluidObjectsForTests(types.NamespacedName{Namespace: "fluid", Name: "jindo-demo"})
	jindoruntime.Spec.TieredStore = datav1alpha1.TieredStore{
		Levels: []datav1alpha1.Level{{
			MediumType: common.Memory,
			Path:       "/dev/shm,/mnt/ram",
			QuotaList:  "10Gi,5Gi",
		}},
	}
	jindoruntime.Spec.Worker.Resources = corev1.ResourceRequirements{
		Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("12Gi")},
	}
	engine := mockJindoCacheEngineForTests(dataset, jindoruntime)
	engine.Client = fake.NewFakeClientWithScheme(datav1alpha1.UnitTestScheme, dataset.DeepCopy(), jindoruntime.DeepCopy())

	value := Jindo{}
	err := engine.transformWorkerResources(jindoruntime, &value, "10g,5g")
	if err == nil || !strings.Contains(err.Error(), "greater than") {
		t.Errorf("expected error containing %q, got %v", "greater than", err)
	}
}
