package utils

// Verification harness for PR #6199 (issue #6140): datasetControllerOwnerReference now
// delegates to transformer.GenerateOwnerReferenceFromObject. Additive only — production code
// untouched. Polarity: CONTRACT (these tests pass on the converged implementation and fail on
// the pre-#6199 local implementation; see docs/verification/ownerref-gvk-delegation/README.md).

import (
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/utils/transformer"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// oldDatasetControllerOwnerReference is an exact replica of the pre-#6199 implementation
// (per-field string fallback on the raw TypeMeta). It exists only to document, as executable
// evidence, which input shapes change behavior with the delegation.
func oldDatasetControllerOwnerReference(dataset *datav1alpha1.Dataset) metav1.OwnerReference {
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

type ownerRefShape struct {
	name           string
	typeMeta       metav1.TypeMeta
	expectBehavior string // "same" = identical to old impl; "repaired" = old kept a malformed apiVersion, new must return the canonical one
}

var ownerRefShapes = []ownerRefShape{
	{"empty TypeMeta", metav1.TypeMeta{}, "same"},
	{"complete TypeMeta", metav1.TypeMeta{Kind: "Dataset", APIVersion: "data.fluid.io/v1alpha1"}, "same"},
	{"kind only", metav1.TypeMeta{Kind: "Dataset"}, "same"},
	{"apiVersion only", metav1.TypeMeta{APIVersion: "data.fluid.io/v1alpha1"}, "same"},
	{"custom version", metav1.TypeMeta{Kind: "Dataset", APIVersion: "data.fluid.io/v1beta1"}, "same"},
	{"group-less apiVersion", metav1.TypeMeta{Kind: "Dataset", APIVersion: "v1alpha1"}, "same"},
	{"wrong kind preserved", metav1.TypeMeta{Kind: "NotADataset"}, "same"},
	{"malformed apiVersion: group without version", metav1.TypeMeta{Kind: "Dataset", APIVersion: "data.fluid.io/"}, "repaired"},
	{"malformed apiVersion: bare slash", metav1.TypeMeta{Kind: "Dataset", APIVersion: "/"}, "repaired"},
}

// TestVerifyOwnerRefConvergenceContract is the primary contract: for every TypeMeta shape the
// delegated implementation must agree with transformer.GenerateOwnerReferenceFromObject (plus
// the documented last-resort fallback), and must never return an empty Kind or APIVersion.
func TestVerifyOwnerRefConvergenceContract(t *testing.T) {
	for _, shape := range ownerRefShapes {
		t.Run(shape.name, func(t *testing.T) {
			dataset := &datav1alpha1.Dataset{
				TypeMeta:   shape.typeMeta,
				ObjectMeta: metav1.ObjectMeta{Name: "verify", Namespace: "default", UID: "uid-verify"},
			}
			got := datasetControllerOwnerReference(dataset)

			shared := transformer.GenerateOwnerReferenceFromObject(dataset)
			wantKind := shared.Kind
			if len(wantKind) == 0 {
				wantKind = datav1alpha1.Datasetkind
			}
			wantAPIVersion := shared.APIVersion
			if len(wantAPIVersion) == 0 {
				wantAPIVersion = datav1alpha1.GroupVersion.String()
			}

			if got.Kind != wantKind || got.APIVersion != wantAPIVersion {
				t.Errorf("divergence from shared helper: got {Kind:%q APIVersion:%q}, want {Kind:%q APIVersion:%q}",
					got.Kind, got.APIVersion, wantKind, wantAPIVersion)
			}
			if len(got.Kind) == 0 || len(got.APIVersion) == 0 {
				t.Errorf("ownerReference must never have empty Kind/APIVersion, got %+v", got)
			}
			if got.Name != dataset.GetName() || got.UID != dataset.GetUID() || got.Controller == nil || !*got.Controller {
				t.Errorf("identity fields broken: got %+v", got)
			}
		})
	}
}

// TestVerifyOwnerRefBehaviorDeltaVsOldImpl pins down the exact behavioral delta of the PR:
// identical to the old implementation for every well-formed shape, and strictly repairing the
// malformed empty-version apiVersion shapes the old code passed through untouched.
func TestVerifyOwnerRefBehaviorDeltaVsOldImpl(t *testing.T) {
	for _, shape := range ownerRefShapes {
		t.Run(shape.name, func(t *testing.T) {
			dataset := &datav1alpha1.Dataset{
				TypeMeta:   shape.typeMeta,
				ObjectMeta: metav1.ObjectMeta{Name: "verify", Namespace: "default", UID: "uid-verify"},
			}
			got := datasetControllerOwnerReference(dataset)
			old := oldDatasetControllerOwnerReference(dataset)

			switch shape.expectBehavior {
			case "same":
				if got.Kind != old.Kind || got.APIVersion != old.APIVersion {
					t.Errorf("unexpected behavior change for well-formed shape: old={Kind:%q APIVersion:%q} new={Kind:%q APIVersion:%q}",
						old.Kind, old.APIVersion, got.Kind, got.APIVersion)
				}
			case "repaired":
				if got.APIVersion != datav1alpha1.GroupVersion.String() || got.Kind != datav1alpha1.Datasetkind {
					t.Errorf("malformed shape must be repaired to the canonical Dataset GVK, got {Kind:%q APIVersion:%q}", got.Kind, got.APIVersion)
				}
				if old.APIVersion == got.APIVersion {
					t.Errorf("replica of the old implementation unexpectedly already repaired the shape; check the replica")
				}
			}
		})
	}
}
