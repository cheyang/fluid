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

// Reviewer verification harness for PR #6177 / issue #6173.
//
// P0 (premise): a CacheRuntime that names only `limits.memory` must not drop the
// template's other requirements. The canary asserts the CORRECT behavior; on the
// base branch it fails (premise reproduced), on the PR head it passes (fix proven).

import (
	"reflect"
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var v6177TemplateResources = corev1.ResourceRequirements{
	Requests: corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("1"),
		corev1.ResourceMemory: resource.MustParse("2Gi"),
	},
	Limits: corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("2"),
		corev1.ResourceMemory: resource.MustParse("4Gi"),
	},
}

var v6177PartialRuntimeResources = corev1.ResourceRequirements{
	Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("8Gi")},
}

func v6177WorkerDefinition(res corev1.ResourceRequirements) *datav1alpha1.RuntimeComponentDefinition {
	return &datav1alpha1.RuntimeComponentDefinition{
		Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "worker", Image: "fluid/cache:v1", Resources: res}},
			},
		},
	}
}

// P0, sync layer: desiredComponentResources must keep the template keys the
// CacheRuntime does not restate (issue #6173 names sync.go's whole-struct return).
func TestP0SyncDesiredKeepsUnstatedTemplateKeys(t *testing.T) {
	desired := desiredComponentResources(v6177PartialRuntimeResources, v6177WorkerDefinition(v6177TemplateResources))
	if desired == nil {
		t.Fatal("desiredComponentResources returned nil for a partially specified CacheRuntime")
	}
	for name, want := range map[string]string{
		"limits.cpu":    desired.Limits.Cpu().String(),
		"limits.memory": desired.Limits.Memory().String(),
	} {
		if want == "" || want == "0" {
			t.Errorf("%s dropped (got %q)", name, want)
		}
	}
	if desired.Limits.Cpu().String() != "2" {
		t.Errorf("limits.cpu: got %q, want 2 (template value must survive)", desired.Limits.Cpu().String())
	}
	if desired.Limits.Memory().String() != "8Gi" {
		t.Errorf("limits.memory: got %q, want 8Gi (the key the CacheRuntime raised)", desired.Limits.Memory().String())
	}
	if desired.Requests == nil || len(desired.Requests) != 2 {
		t.Errorf("requests dropped: got %v, want cpu=1 memory=2Gi", desired.Requests)
	}
}

// P0, creation layer: transformComponentPodTemplate must keep the template keys
// the CacheRuntime does not restate when it renders the workload.
func TestP0CreationKeepsUnstatedTemplateKeys(t *testing.T) {
	engine := &CacheEngine{name: "test", namespace: "default"}
	dataset := &datav1alpha1.Dataset{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"}}
	value := &common.CacheRuntimeComponentValue{
		Name:            "test-worker",
		Namespace:       "default",
		ComponentType:   common.ComponentTypeWorker,
		PodTemplateSpec: v6177WorkerDefinition(v6177TemplateResources).Template,
	}

	engine.transformComponentPodTemplate(datav1alpha1.CacheRuntimeSpec{}, datav1alpha1.RuntimeComponentCommonSpec{
		Resources: v6177PartialRuntimeResources,
	}, dataset, value)

	got := value.PodTemplateSpec.Spec.Containers[0].Resources
	if got.Limits.Cpu().String() != "2" {
		t.Errorf("creation: limits.cpu: got %q, want 2", got.Limits.Cpu().String())
	}
	if got.Limits.Memory().String() != "8Gi" {
		t.Errorf("creation: limits.memory: got %q, want 8Gi", got.Limits.Memory().String())
	}
	if got.Requests == nil || len(got.Requests) != 2 || got.Requests.Cpu().String() != "1" || got.Requests.Memory().String() != "2Gi" {
		t.Errorf("creation: requests: got %v, want cpu=1 memory=2Gi", got.Requests)
	}
}

// P1: creation and sync must resolve the same desired resources, otherwise the
// workload rolls on every reconcile. Matrix over the interesting layer shapes.
func TestP1CreationAndSyncAgree(t *testing.T) {
	engine := &CacheEngine{name: "test", namespace: "default"}
	dataset := &datav1alpha1.Dataset{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"}}

	cases := []struct {
		name     string
		template corev1.ResourceRequirements
		runtime  corev1.ResourceRequirements
	}{
		{"full template, partial runtime", v6177TemplateResources, v6177PartialRuntimeResources},
		{"full template, empty runtime", v6177TemplateResources, corev1.ResourceRequirements{}},
		{"empty template, full runtime", corev1.ResourceRequirements{}, v6177TemplateResources},
		{"requests-only runtime on limits-only template", corev1.ResourceRequirements{Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("4Gi")}},
			corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")}}},
		{"claims on both layers", corev1.ResourceRequirements{Claims: []corev1.ResourceClaim{{Name: "gpu"}}},
			corev1.ResourceRequirements{Claims: []corev1.ResourceClaim{{Name: "nic"}}}},
		{"neither declares", corev1.ResourceRequirements{}, corev1.ResourceRequirements{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			def := v6177WorkerDefinition(tc.template)
			value := &common.CacheRuntimeComponentValue{
				Name:            "test-worker",
				Namespace:       "default",
				ComponentType:   common.ComponentTypeWorker,
				PodTemplateSpec: def.Template,
			}
			engine.transformComponentPodTemplate(datav1alpha1.CacheRuntimeSpec{}, datav1alpha1.RuntimeComponentCommonSpec{
				Resources: tc.runtime,
			}, dataset, value)
			rendered := value.PodTemplateSpec.Spec.Containers[0].Resources

			desired := desiredComponentResources(tc.runtime, def)

			if desired == nil {
				// nil means "leave the workload alone"; acceptable only when creation
				// also rendered no requirements.
				if !reflect.DeepEqual(rendered, corev1.ResourceRequirements{}) {
					t.Errorf("sync resolves to nil (leave alone) but creation rendered %v", rendered)
				}
				return
			}
			if !reflect.DeepEqual(rendered, *desired) {
				t.Errorf("creation and sync disagree:\ncreation: %+v\nsync:     %+v", rendered, *desired)
			}
		})
	}
}

// P2: resource claims must survive both the creation render and the sync resolution.
// The old code dropped Claims entirely (creation required Limits/Requests to be set,
// sync returned the runtime struct only when Requests/Limits were set).
func TestP2ClaimsSurvive(t *testing.T) {
	claims := []corev1.ResourceClaim{{Name: "gpu"}}
	// sync layer
	desired := desiredComponentResources(corev1.ResourceRequirements{}, v6177WorkerDefinition(corev1.ResourceRequirements{Claims: claims}))
	if desired == nil || len(desired.Claims) != 1 || desired.Claims[0].Name != "gpu" {
		t.Errorf("sync: claims from template dropped: %+v", desired)
	}
	desired = desiredComponentResources(corev1.ResourceRequirements{Claims: claims}, v6177WorkerDefinition(corev1.ResourceRequirements{}))
	if desired == nil || len(desired.Claims) != 1 || desired.Claims[0].Name != "gpu" {
		t.Errorf("sync: claims from CacheRuntime dropped: %+v", desired)
	}

	// creation layer
	engine := &CacheEngine{name: "test", namespace: "default"}
	dataset := &datav1alpha1.Dataset{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"}}
	value := &common.CacheRuntimeComponentValue{
		Name:            "test-worker",
		Namespace:       "default",
		ComponentType:   common.ComponentTypeWorker,
		PodTemplateSpec: v6177WorkerDefinition(corev1.ResourceRequirements{}).Template,
	}
	engine.transformComponentPodTemplate(datav1alpha1.CacheRuntimeSpec{}, datav1alpha1.RuntimeComponentCommonSpec{
		Resources: corev1.ResourceRequirements{Claims: claims},
	}, dataset, value)
	if got := value.PodTemplateSpec.Spec.Containers[0].Resources.Claims; len(got) != 1 || got[0].Name != "gpu" {
		t.Errorf("creation: claims from CacheRuntime dropped: %+v", got)
	}
}
