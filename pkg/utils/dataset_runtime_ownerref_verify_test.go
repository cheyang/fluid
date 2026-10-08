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

// Review-verification harness for PR #6199 (additive; production code untouched).
//
// The PR replaces the local GVK fallback logic of datasetControllerOwnerReference with a
// delegation to transformer.GenerateOwnerReferenceFromObject. The core invariant of that
// convergence is: the delegated implementation is observably identical to the pre-PR one for
// every TypeMeta shape a real client can produce, and any divergence on malformed shapes is
// deliberate (normalization) and pinned.
//
// Polarity: contract. legacyDatasetControllerOwnerReference below is the pre-PR
// implementation copied verbatim from master @ e0fc4c18 (the merge-base), acting as oracle.
// On the PR head every equivalence entry must hold; the pinned-divergence entries document
// the only intentional behavior change.

import (
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

// comparableOwnerRef dereferences the pointer fields of an ownerReference so two
// references can be compared by value (each constructor call allocates a fresh
// *bool for Controller, so a plain struct comparison would always differ).
type comparableOwnerRef struct {
	APIVersion         string
	Kind               string
	Name               string
	UID                string
	Controller         bool
	BlockOwnerDeletion bool
}

func toComparable(ref metav1.OwnerReference) comparableOwnerRef {
	out := comparableOwnerRef{
		APIVersion: ref.APIVersion,
		Kind:       ref.Kind,
		Name:       ref.Name,
		UID:        string(ref.UID),
	}
	if ref.Controller != nil {
		out.Controller = *ref.Controller
	}
	if ref.BlockOwnerDeletion != nil {
		out.BlockOwnerDeletion = *ref.BlockOwnerDeletion
	}
	return out
}

// legacyDatasetControllerOwnerReference is the pre-PR implementation of
// datasetControllerOwnerReference, copied verbatim from the merge-base (e0fc4c18).
func legacyDatasetControllerOwnerReference(dataset *datav1alpha1.Dataset) metav1.OwnerReference {
	kind := dataset.GetObjectKind().GroupVersionKind().Kind
	if len(kind) == 0 {
		kind = datav1alpha1.Datasetkind
	}
	apiVersion := dataset.APIVersion
	if len(apiVersion) == 0 {
		apiVersion = datav1alpha1.GroupVersion.String()
	}

	return metav1.OwnerReference{
		Kind:       kind,
		APIVersion: apiVersion,
		Name:       dataset.GetName(),
		UID:        dataset.GetUID(),
		Controller: ptr.To(true),
	}
}

func ownerRefShape(name, kind, apiVersion string) *datav1alpha1.Dataset {
	return &datav1alpha1.Dataset{
		TypeMeta:   metav1.TypeMeta{Kind: kind, APIVersion: apiVersion},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default", UID: types.UID("uid-" + name)},
	}
}

func TestVerifyOwnerRefConvergenceEquivalence(t *testing.T) {
	// Shapes a typed client / decoder can realistically produce. For all of these the
	// delegated implementation must be observably identical to the legacy oracle.
	reachable := map[string]*datav1alpha1.Dataset{
		"empty TypeMeta":                       ownerRefShape("empty", "", ""),
		"complete standard TypeMeta":           ownerRefShape("complete", "Dataset", "data.fluid.io/v1alpha1"),
		"kind only":                            ownerRefShape("kind-only", "Dataset", ""),
		"apiVersion only":                      ownerRefShape("version-only", "", "data.fluid.io/v1alpha1"),
		"complete custom version":              ownerRefShape("custom", "Dataset", "data.fluid.io/v1beta1"),
		"apiVersion only custom version":       ownerRefShape("custom-version-only", "", "data.fluid.io/v1beta1"),
		"wrong kind only":                      ownerRefShape("wrong-kind", "DataLoad", ""),
		"wrong kind with custom version":       ownerRefShape("wrong-kind-custom", "DataLoad", "data.fluid.io/v1beta1"),
		"core-style apiVersion (no group)":     ownerRefShape("core-style", "", "v1alpha1"),
		"kind only non-standard":               ownerRefShape("kind-nonstrd", "ThinRuntime", ""),
		"apiVersion only other registered grp": ownerRefShape("grp-only", "", "data.fluid.io/v1alpha1"),
	}
	for name, ds := range reachable {
		t.Run("equivalent/"+name, func(t *testing.T) {
			legacy := toComparable(legacyDatasetControllerOwnerReference(ds))
			current := toComparable(datasetControllerOwnerReference(ds))
			if legacy != current {
				t.Errorf("divergence on reachable shape %q: legacy=%+v current=%+v", name, legacy, current)
			}
		})
	}

	// Malformed TypeMeta shapes. The legacy implementation copies a non-empty APIVersion
	// through verbatim; the delegated one normalizes group-set/version-empty (and
	// unparseable) apiVersions to the registered data.fluid.io/v1alpha1. This is the ONLY
	// intentional behavior change of the PR, and it is an improvement: the legacy output
	// fails API-server ownerReference validation.
	malformed := map[string]struct {
		dataset            *datav1alpha1.Dataset
		legacyAPIVersion   string
		expectedAPIVersion string
		expectedKind       string
	}{
		"group set, version empty": {
			dataset:            ownerRefShape("malformed-gv", "Dataset", "data.fluid.io/"),
			legacyAPIVersion:   "data.fluid.io/",
			expectedAPIVersion: "data.fluid.io/v1alpha1",
			expectedKind:       "Dataset",
		},
		"lone slash": {
			dataset:            ownerRefShape("malformed-slash", "", "/"),
			legacyAPIVersion:   "/",
			expectedAPIVersion: "data.fluid.io/v1alpha1",
			expectedKind:       "Dataset",
		},
		"unparseable apiVersion": {
			dataset:            ownerRefShape("malformed-multi", "Dataset", "a/b/c"),
			legacyAPIVersion:   "a/b/c",
			expectedAPIVersion: "data.fluid.io/v1alpha1",
			expectedKind:       "Dataset",
		},
	}
	for name, tc := range malformed {
		t.Run("normalized/"+name, func(t *testing.T) {
			legacy := legacyDatasetControllerOwnerReference(tc.dataset)
			if legacy.APIVersion != tc.legacyAPIVersion {
				t.Errorf("oracle sanity check failed: legacy apiVersion=%q, want %q (oracle drifted from merge-base?)",
					legacy.APIVersion, tc.legacyAPIVersion)
			}
			current := datasetControllerOwnerReference(tc.dataset)
			if current.APIVersion != tc.expectedAPIVersion || current.Kind != tc.expectedKind {
				t.Errorf("expected normalization to {%s %s}, got {%s %s}",
					tc.expectedKind, tc.expectedAPIVersion, current.Kind, current.APIVersion)
			}
		})
	}
}

// TestVerifyBaseHelperPartialRecovery probes the shared transformer helper's per-field
// recovery. Issue #6140's "Why the difference matters" table described a gvk.Empty()-gated
// helper; this probe records what the helper on the branch under test actually does with
// partially populated TypeMeta. Polarity: informational contract (per-field recovery holds
// on base AND on the PR head, because #6139 landed it before this PR).
func TestVerifyHelperPartialTypeMetaRecovery(t *testing.T) {
	cases := map[string]struct {
		obj                *datav1alpha1.Dataset
		expectedKind       string
		expectedAPIVersion string
	}{
		"only kind set":        {obj: ownerRefShape("p-kind", "DataLoad", ""), expectedKind: "DataLoad", expectedAPIVersion: "data.fluid.io/v1alpha1"},
		"only apiVersion set":  {obj: ownerRefShape("p-version", "", "data.fluid.io/v1alpha1"), expectedKind: "Dataset", expectedAPIVersion: "data.fluid.io/v1alpha1"},
		"fully empty TypeMeta": {obj: ownerRefShape("p-empty", "", ""), expectedKind: "Dataset", expectedAPIVersion: "data.fluid.io/v1alpha1"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// Go through the same path datasetControllerOwnerReference now uses.
			ref := datasetControllerOwnerReference(tc.obj)
			if ref.Kind != tc.expectedKind || ref.APIVersion != tc.expectedAPIVersion {
				t.Errorf("expected {%s %s}, got {%s %s}", tc.expectedKind, tc.expectedAPIVersion, ref.Kind, ref.APIVersion)
			}
		})
	}
}
