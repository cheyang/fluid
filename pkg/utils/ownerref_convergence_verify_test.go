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

// Reviewer verification harness for PR #6199 ("converge datasetControllerOwnerReference
// with transformer helper", fixes #6140). Additive only - no production code is changed.
//
// Polarity (see docs/verification/ownerref-gvk-convergence-claude/verify-manifest.json):
//   - TestP0OwnerRefImplementationsAgree     contract  (red on base = premise confirmed, green on PR head)
//   - TestP0TransformerPartialRecovery       contract  (documents issue #6140 items 1+2, already on base)
//   - TestF3VersionOnlyGroupNotRecovered     canary    (documents a pre-existing gap; must be
//                                                       inverted if the transformer is ever fixed)

import (
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/utils/transformer"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// p0Inputs are the TypeMeta shapes where the two GVK-recovery implementations could
// disagree. They are exercised through BOTH implementations on the same object.
var p0Inputs = map[string]*datav1alpha1.Dataset{
	"empty typemeta": {
		ObjectMeta: v1.ObjectMeta{Name: "p0-empty", Namespace: "default", UID: "p0-1"},
	},
	"kind only": {
		TypeMeta:   v1.TypeMeta{Kind: "Dataset"},
		ObjectMeta: v1.ObjectMeta{Name: "p0-kind-only", Namespace: "default", UID: "p0-2"},
	},
	"apiversion only": {
		TypeMeta:   v1.TypeMeta{APIVersion: "data.fluid.io/v1alpha1"},
		ObjectMeta: v1.ObjectMeta{Name: "p0-version-only", Namespace: "default", UID: "p0-3"},
	},
	"complete typemeta": {
		TypeMeta:   v1.TypeMeta{Kind: "Dataset", APIVersion: "data.fluid.io/v1alpha1"},
		ObjectMeta: v1.ObjectMeta{Name: "p0-complete", Namespace: "default", UID: "p0-4"},
	},
	// The shapes the issue #6140 table called out as producing malformed references.
	"malformed apiversion, group only": {
		TypeMeta:   v1.TypeMeta{Kind: "Dataset", APIVersion: "data.fluid.io/"},
		ObjectMeta: v1.ObjectMeta{Name: "p0-group-only", Namespace: "default", UID: "p0-5"},
	},
	"malformed apiversion, slash only": {
		TypeMeta:   v1.TypeMeta{Kind: "Dataset", APIVersion: "/"},
		ObjectMeta: v1.ObjectMeta{Name: "p0-slash-only", Namespace: "default", UID: "p0-6"},
	},
	"malformed apiversion, two slashes": {
		TypeMeta:   v1.TypeMeta{Kind: "Dataset", APIVersion: "a/b/c"},
		ObjectMeta: v1.ObjectMeta{Name: "p0-two-slashes", Namespace: "default", UID: "p0-7"},
	},
}

// TestP0OwnerRefImplementationsAgree is the premise check for PR #6199 (claim P0):
// "#6138 adds a local helper ... #6139 adds a scheme-based recovery to the shared helper ...
// the two code paths do not overlap". The observable symptom of two divergent
// implementations is that the same owner object yields different ownerReferences
// depending on which helper is used. After the PR, datasetControllerOwnerReference
// delegates to the shared helper, so the outputs must agree for every input shape -
// including the malformed ones, which only the scheme-backed path repairs.
//
// CONTRACT: passes on the PR head; on the merge base the local helper passes
// malformed apiVersions through untouched, so this test goes red there.
func TestP0OwnerRefImplementationsAgree(t *testing.T) {
	for name, ds := range p0Inputs {
		local := datasetControllerOwnerReference(ds)
		shared := transformer.GenerateOwnerReferenceFromObject(ds)

		if local.Kind != shared.Kind || local.APIVersion != shared.APIVersion {
			t.Errorf("[%s] the two implementations disagree: datasetControllerOwnerReference=%q/%q, transformer=%q/%q",
				name, local.APIVersion, local.Kind, shared.APIVersion, shared.Kind)
		}

		// A controller ownerReference the API server accepts needs both fields complete;
		// an owner-based watch resolves the dependent through them.
		if local.Kind == "" || local.APIVersion == "" || local.APIVersion == "/" {
			t.Errorf("[%s] ownerReference is malformed: Kind=%q APIVersion=%q", name, local.Kind, local.APIVersion)
		}
	}
}

// TestP0TransformerPartialRecovery documents that issue #6140's proposals 1 and 2
// (per-field recovery + a diagnostic on lookup failure) are already satisfied by the
// merged #6139 on the base branch, so the only remaining work for #6140 was the
// convergence (proposal 3) and the regression coverage (proposal 4) that PR #6199 adds.
//
// CONTRACT: expected to pass on BOTH the base branch and the PR head.
func TestP0TransformerPartialRecovery(t *testing.T) {
	shapes := map[string]v1.TypeMeta{
		"kind only":        {Kind: "Dataset"},
		"apiversion only":  {APIVersion: "data.fluid.io/v1alpha1"},
		"empty typemeta":   {},
		"group-only slash": {Kind: "Dataset", APIVersion: "data.fluid.io/"},
	}
	for name, tm := range shapes {
		ds := &datav1alpha1.Dataset{
			TypeMeta:   tm,
			ObjectMeta: v1.ObjectMeta{Name: "p0-tr-" + name, Namespace: "default", UID: "p0-tr"},
		}
		ref := transformer.GenerateOwnerReferenceFromObject(ds)
		if ref.Kind != "Dataset" {
			t.Errorf("[%s] transformer did not recover Kind: got %q", name, ref.Kind)
		}
		if ref.APIVersion != datav1alpha1.GroupVersion.String() {
			t.Errorf("[%s] transformer did not recover APIVersion: got %q, want %q",
				name, ref.APIVersion, datav1alpha1.GroupVersion.String())
		}
	}
}

// TestF3VersionOnlyGroupNotRecovered is a CANARY: it pins the *current* behavior of the
// shared helper, which does not recover the group when only the version is populated
// (TypeMeta{Kind: "Dataset", APIVersion: "v1alpha1"} -> ownerReference APIVersion
// "v1alpha1", i.e. the data.fluid.io group is missing and the API server would reject
// the reference). PR #6199 inherits this gap through the delegation; the pre-PR local
// helper behaved the same way, so this is a latent gap of the shared helper, not a
// regression of this PR.
//
// If the transformer is ever changed to recover the group as well, this test flips to
// red and must be inverted into a contract test.
func TestF3VersionOnlyGroupNotRecovered(t *testing.T) {
	ds := &datav1alpha1.Dataset{
		TypeMeta:   v1.TypeMeta{Kind: "Dataset", APIVersion: "v1alpha1"},
		ObjectMeta: v1.ObjectMeta{Name: "f3-version-only", Namespace: "default", UID: "f3-1"},
	}
	ref := transformer.GenerateOwnerReferenceFromObject(ds)
	if ref.APIVersion != "v1alpha1" {
		t.Errorf("canary: expected the group to stay unrecovered (APIVersion %q), got %q - invert this test",
			"v1alpha1", ref.APIVersion)
	}
	local := datasetControllerOwnerReference(ds)
	if local.APIVersion != "v1alpha1" {
		t.Errorf("canary: expected the delegated helper to inherit the same gap, got APIVersion %q", local.APIVersion)
	}
}
