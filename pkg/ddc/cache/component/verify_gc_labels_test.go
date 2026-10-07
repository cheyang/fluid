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

package component

// Verification harness for the review of
// https://github.com/fluid-cloudnative/fluid/pull/6175 (additive test file; no
// production code is touched).
//
// The e2e cleanup assertion in test/gha-e2e/{mooncake,curvine}/test.sh runs
//
//	kubectl get advancedstatefulset,daemonset,svc -l "cacheruntime.fluid.io/name=<dataset>" -n default
//
// to prove runtime resources are garbage collected. A previous revision of the
// PR selected on "fluid.io/managed-by=fluid", a label the controller never
// sets on these objects, so the assertion passed vacuously (review finding by
// cheyang, 2026-09-08). This CONTRACT test pins, against the real construction
// code, that the objects the controller creates carry the literal label key
// the shell script selects on, and do not carry the old one.

import (
	"testing"

	"github.com/fluid-cloudnative/fluid/pkg/common"
	corev1 "k8s.io/api/core/v1"
)

func verifyGCComponent() *common.CacheRuntimeComponentValue {
	return &common.CacheRuntimeComponentValue{
		Name:          "mooncake-demo-worker",
		Namespace:     "default",
		Enabled:       true,
		Replicas:      1,
		ComponentType: common.ComponentTypeWorker,
		Owner: &common.OwnerReference{
			APIVersion: "data.fluid.io/v1alpha1",
			Kind:       "CacheRuntime",
			Name:       "mooncake-demo",
			UID:        "verify-uid",
		},
		PodTemplateSpec: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "worker", Image: "fluidcloudnative/mooncake:e2e"}},
			},
		},
		Service: &common.CacheRuntimeComponentServiceConfig{Name: "svc-mooncake-demo-worker"},
	}
}

func assertGCSelectorLabels(t *testing.T, kind string, labels map[string]string) {
	t.Helper()
	// Literal key, on purpose: this must match the string in the shell scripts,
	// not the Go constant, so a drift between them fails here.
	const shellSelectorKey = "cacheruntime.fluid.io/name"
	const oldBrokenSelectorKey = "fluid.io/managed-by"

	if labels == nil {
		t.Fatalf("%s carries no labels at all; the e2e GC selector would match nothing", kind)
	}
	if got := labels[shellSelectorKey]; got != "mooncake-demo" {
		t.Fatalf("%s labels must include %s=mooncake-demo for the e2e GC assertion to match, got %q (labels: %v)",
			kind, shellSelectorKey, got, labels)
	}
	if _, ok := labels[oldBrokenSelectorKey]; ok {
		t.Fatalf("%s unexpectedly carries %s; the previous e2e selector was not as vacuous as believed", kind, oldBrokenSelectorKey)
	}
}

func TestAdvancedStatefulSetCarriesGCSelectorLabel_Verify(t *testing.T) {
	mgr := newAdvancedStatefulSetManager(nil)
	asts := mgr.constructAdvancedStatefulSet(verifyGCComponent())
	assertGCSelectorLabels(t, "AdvancedStatefulSet", asts.Labels)
}

func TestDaemonSetCarriesGCSelectorLabel_Verify(t *testing.T) {
	mgr := newDaemonSetManager(nil)
	ds := mgr.constructDaemonSet(verifyGCComponent())
	assertGCSelectorLabels(t, "DaemonSet", ds.Labels)
}

func TestHeadlessServiceCarriesGCSelectorLabel_Verify(t *testing.T) {
	svc := constructService(verifyGCComponent())
	assertGCSelectorLabels(t, "Service", svc.Labels)
}
