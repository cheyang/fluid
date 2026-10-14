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

package transformer

// Reviewer verification harness for PR #6206 ("recover group for group-less
// apiVersion in owner reference"). Additive only; production code untouched.
//
// Polarity: CONTRACT tests. They assert the intended correct behavior:
//   - PASS on the fixed PR head (0fa32294)
//   - FAIL on the pre-fix base   (e4a10bf)  <- a red result IS the reproduction
//
// Claim P0 (premise, runs against the base): a group-less apiVersion such as
// "v1alpha1" parses with non-empty Kind and Version, so scheme recovery was
// skipped and the generated OwnerReference kept APIVersion "v1alpha1", which
// the Kubernetes garbage collector and owner-based watches cannot resolve.
//
// F1: group-less apiVersion with the kind set -> group must be recovered.
// F2: group-less apiVersion with the kind missing -> kind AND group recovered.
// F3: core k8s type with a complete group-less TypeMeta (apiVersion "v1") ->
//     output must be unchanged (recovery attempt fails, reference not corrupted).

import (
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const expectedFluidAPIVersion = "data.fluid.io/v1alpha1"

func TestOwnerRefGroupRecoveryGrouplessAPIVersionWithKind(t *testing.T) {
	obj := &datav1alpha1.Dataset{
		TypeMeta:   metav1.TypeMeta{Kind: "Dataset", APIVersion: "v1alpha1"},
		ObjectMeta: metav1.ObjectMeta{Name: "harness-groupless", Namespace: "default", UID: "uid-h1"},
	}
	ref := GenerateOwnerReferenceFromObject(obj)
	if ref.APIVersion != expectedFluidAPIVersion {
		t.Errorf("F1: APIVersion = %q, want %q (group must be recovered from the scheme)", ref.APIVersion, expectedFluidAPIVersion)
	}
	if ref.Kind != "Dataset" {
		t.Errorf("F1: Kind = %q, want %q", ref.Kind, "Dataset")
	}
}

func TestOwnerRefGroupRecoveryGrouplessAPIVersionWithoutKind(t *testing.T) {
	obj := &datav1alpha1.Dataset{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1alpha1"},
		ObjectMeta: metav1.ObjectMeta{Name: "harness-groupless-nokind", Namespace: "default", UID: "uid-h2"},
	}
	ref := GenerateOwnerReferenceFromObject(obj)
	if ref.Kind != "Dataset" {
		t.Errorf("F2: Kind = %q, want %q", ref.Kind, "Dataset")
	}
	if ref.APIVersion != expectedFluidAPIVersion {
		t.Errorf("F2: APIVersion = %q, want %q", ref.APIVersion, expectedFluidAPIVersion)
	}
}

func TestOwnerRefGroupRecoveryCoreTypeCompleteTypeMetaUnchanged(t *testing.T) {
	// A core type is not in the fluid scheme, so recovery fails and only logs.
	// The reference must keep the caller-supplied (valid, core group-less) GVK.
	obj := &corev1.ConfigMap{
		TypeMeta:   metav1.TypeMeta{Kind: "ConfigMap", APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{Name: "harness-core", Namespace: "default", UID: "uid-h3"},
	}
	ref := GenerateOwnerReferenceFromObject(obj)
	if ref.APIVersion != "v1" {
		t.Errorf("F3: APIVersion = %q, want %q (unregistered type must not be rewritten)", ref.APIVersion, "v1")
	}
	if ref.Kind != "ConfigMap" {
		t.Errorf("F3: Kind = %q, want %q", ref.Kind, "ConfigMap")
	}
}
