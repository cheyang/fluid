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

// Head-only part of the PR #6233 verification harness: asserts the
// ErrPVCNotBound sentinel survives the full wrap chain
//   extractVolumeInfo (%w)
//     -> transformFuseConfig (github.com/pkg/errors.Wrapf, Unwrap supported)
//       -> updateFuseConfigOnChange (returned unwrapped)
//         -> ShouldUpdateUFS (errors.Is switch)
// POLARITY: contract tests. RED if any layer starts using fmt.Errorf with %v
// or a string conversion and silently breaks errors.Is propagation -- which
// would re-introduce the Error-level noise this PR removes.
//
// This file intentionally does NOT compile on the PR base (ErrPVCNotBound is
// introduced by the PR); the base verdict comes from
// verify_lognoise_harness_test.go.
package thin

import (
	"errors"
	"strings"
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
)

// Contract: the sentinel is detectable at the updateFuseConfigOnChange layer
// (the layer ShouldUpdateUFS actually consults).
func TestHarness_ErrPVCNotBoundSurvivesWrapChain(t *testing.T) {
	t.Setenv(EnvFuseConfigStorage, "configmap")

	runtime := &datav1alpha1.ThinRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: "verify-sentinel", Namespace: "fluid"},
	}
	dataset := &datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: "verify-sentinel", Namespace: "fluid"},
		Spec: datav1alpha1.DatasetSpec{
			Mounts: []datav1alpha1.Mount{
				{Name: "my-pvc", MountPoint: "pvc://my-pvc"},
			},
		},
	}
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "verify-sentinel-fuse-conf", Namespace: "fluid"},
		Data:       map[string]string{"config.json": "{}"},
	}
	pendingPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "my-pvc", Namespace: "fluid"},
		Status: corev1.PersistentVolumeClaimStatus{
			Phase: corev1.ClaimPending,
		},
	}

	engine := &ThinEngine{
		name:      "verify-sentinel",
		namespace: "fluid",
		runtime:   runtime,
		Client:    fake.NewFakeClientWithScheme(testScheme, []k8sruntime.Object{runtime, dataset, configMap, pendingPVC}...),
	}

	_, err := engine.updateFuseConfigOnChange(runtime, dataset)
	if err == nil {
		t.Fatal("expected an error for a not-yet-bound PVC, got nil")
	}
	if !errors.Is(err, ErrPVCNotBound) {
		t.Errorf("expected errors.Is(err, ErrPVCNotBound) through the transformFuseConfig Wrapf chain, got: %v", err)
	}
}

// Contract: extractVolumeInfo itself returns the sentinel (wrapped), and the
// message keeps the PVC name for diagnosis.
func TestHarness_ExtractVolumeInfoReturnsSentinel(t *testing.T) {
	engine := &ThinEngine{
		name:      "verify-sentinel",
		namespace: "fluid",
		Client: fake.NewFakeClientWithScheme(testScheme, []k8sruntime.Object{
			&corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{Name: "my-pvc", Namespace: "fluid"},
				Status:     corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimPending},
			},
		}...),
	}

	_, _, err := engine.extractVolumeInfo("my-pvc")
	if err == nil {
		t.Fatal("expected an error for a not-yet-bound PVC, got nil")
	}
	if !errors.Is(err, ErrPVCNotBound) {
		t.Errorf("expected errors.Is(err, ErrPVCNotBound), got: %v", err)
	}
	if !strings.Contains(err.Error(), "my-pvc") {
		t.Errorf("expected error message to keep the PVC name, got: %v", err)
	}
}
