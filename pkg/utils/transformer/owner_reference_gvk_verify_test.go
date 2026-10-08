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

// Reviewer verification harness for PR #6199 (owner-reference GVK convergence), finding F1.
//
// Polarity: TestVerifyGroupRecoveredWhenVersionIsSet is a CONTRACT test — it asserts the
// behavior issue #6140's proposal 1 asks for (fill the apiVersion from the scheme
// independently, i.e. recover the missing GROUP even when the VERSION is already set) and is
// therefore expected to FAIL (red) on the PR head: that red IS the reproduction. It goes green
// once GenerateOwnerReferenceFromObject recovers Group whenever Group is empty.

import (
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestVerifyGroupRecoveredWhenVersionIsSet asserts that a partial TypeMeta carrying a version
// but no group ("v1alpha1") gets its group recovered from the fluid scheme, so the generated
// ownerReference carries the fully qualified "data.fluid.io/v1alpha1" rather than the
// group-less "v1alpha1" the API server would reject.
//
// Current state (PR head 1f8666fc): RED — the recovery in GenerateOwnerReferenceFromObject
// only overwrites (Group, Version) as a pair and only when Version is empty, so a group-less
// apiVersion passes through unrecovered. Issue #6140 (proposal 1: "fill Kind and APIVersion
// independently ... keeping the scheme lookup as the source of truth for both fields") is only
// half-implemented for this shape, and the delegation added by PR #6199 routes the Dataset
// owner path through it.
func TestVerifyGroupRecoveredWhenVersionIsSet(t *testing.T) {
	dataset := &datav1alpha1.Dataset{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1alpha1"},
		ObjectMeta: metav1.ObjectMeta{Name: "h-groupless-dataset", Namespace: "default", UID: "h-t-uid-1"},
	}
	ref := GenerateOwnerReferenceFromObject(dataset)
	if ref.APIVersion != "data.fluid.io/v1alpha1" {
		t.Errorf("F1 reproduction: group not recovered for group-less apiVersion — got APIVersion=%q, want %q",
			ref.APIVersion, "data.fluid.io/v1alpha1")
	}
	if ref.Kind != "Dataset" {
		t.Errorf("F1 reproduction: kind not recovered — got Kind=%q, want %q", ref.Kind, "Dataset")
	}

	alluxio := &datav1alpha1.AlluxioRuntime{
		TypeMeta:   metav1.TypeMeta{Kind: "AlluxioRuntime", APIVersion: "v1alpha1"},
		ObjectMeta: metav1.ObjectMeta{Name: "h-groupless-runtime", Namespace: "default", UID: "h-t-uid-2"},
	}
	ref2 := GenerateOwnerReferenceFromObject(alluxio)
	if ref2.APIVersion != "data.fluid.io/v1alpha1" {
		t.Errorf("F1 reproduction: group not recovered for kind+group-less apiVersion — got APIVersion=%q, want %q",
			ref2.APIVersion, "data.fluid.io/v1alpha1")
	}
}
