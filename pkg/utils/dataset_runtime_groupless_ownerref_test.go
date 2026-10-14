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

package utils

// Reviewer verification harness for PR #6206 — consumer path.
// Additive only; production code untouched.
//
// Polarity: CONTRACT test. It asserts the intended behavior of
// datasetControllerOwnerReference when the Dataset carries a group-less
// apiVersion ("v1alpha1"): the owner reference written on the Runtime must
// carry the full "data.fluid.io/v1alpha1", because the dataset controller's
// owner-based watch and the garbage collector resolve the owner through
// Kind + Group (see resolveControllerRef in pkg/utils/kubeclient/metadata.go,
// which rejects a controllerRef whose group does not match).
//
// PASS on the fixed PR head (0fa32294), FAIL on the pre-fix base (e4a10bf).

import (
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestDatasetControllerOwnerReferenceGrouplessAPIVersion(t *testing.T) {
	dataset := &datav1alpha1.Dataset{
		TypeMeta:   v1.TypeMeta{Kind: "Dataset", APIVersion: "v1alpha1"},
		ObjectMeta: v1.ObjectMeta{Name: "harness-groupless-dataset", Namespace: "default", UID: "uid-h4"},
	}

	ref := datasetControllerOwnerReference(dataset)

	if ref.Kind != "Dataset" {
		t.Errorf("F4: Kind = %q, want %q", ref.Kind, "Dataset")
	}
	if ref.APIVersion != "data.fluid.io/v1alpha1" {
		t.Errorf("F4: APIVersion = %q, want %q (group must be recovered before the reference is written on the Runtime)", ref.APIVersion, "data.fluid.io/v1alpha1")
	}
	if ref.Name != "harness-groupless-dataset" || ref.UID != "uid-h4" {
		t.Errorf("F4: owner identity not preserved: name=%q uid=%q", ref.Name, ref.UID)
	}
	if !ptr.Deref(ref.Controller, false) {
		t.Errorf("F4: Controller = %v, want true", ref.Controller)
	}
}
