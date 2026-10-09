package utils

// Premise-verification test for PR #6199 / issue #6140. Runs against the BASE branch (without
// the patch). Polarity: bug-canary — it asserts the CURRENT base behavior (two divergent
// implementations). On the patched code the same input shapes go through one shared helper and
// this divergence disappears.

import (
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/utils/transformer"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// On base, datasetControllerOwnerReference keeps a TypeMeta apiVersion whose version part is
// empty ("data.fluid.io/") because it only fall-backs when the whole string is empty, while the
// shared transformer helper (merged via #6139) repairs the same input through the scheme.
// The two implementations therefore disagree on the same object — the divergence #6140 reports.
func TestBasePremise_TwoImplementationsDiverge(t *testing.T) {
	dataset := &datav1alpha1.Dataset{
		TypeMeta:   v1.TypeMeta{Kind: "Dataset", APIVersion: "data.fluid.io/"},
		ObjectMeta: v1.ObjectMeta{Name: "premise-dataset", Namespace: "default", UID: "uid-premise"},
	}

	local := datasetControllerOwnerReference(dataset)
	shared := transformer.GenerateOwnerReferenceFromObject(dataset)

	t.Logf("base local helper  -> Kind=%q APIVersion=%q", local.Kind, local.APIVersion)
	t.Logf("base shared helper -> Kind=%q APIVersion=%q", shared.Kind, shared.APIVersion)

	// Canary: on base this PASSES (the divergence exists). After the PR it must flip.
	if local.APIVersion == shared.APIVersion && local.Kind == shared.Kind {
		t.Errorf("expected the two implementations to diverge on base, but they agree: %+v vs %+v", local, shared)
	}
	if local.APIVersion != "data.fluid.io/" {
		t.Errorf("expected base local helper to keep the malformed apiVersion %q, got %q", "data.fluid.io/", local.APIVersion)
	}
	if shared.APIVersion != datav1alpha1.GroupVersion.String() {
		t.Errorf("expected base shared helper to repair the apiVersion to %q, got %q", datav1alpha1.GroupVersion.String(), shared.APIVersion)
	}
}
