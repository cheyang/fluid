package utils

// Verification harness for PR #6199 (reviewer: codex). Additive only.
// L1 contract layer: datasetControllerOwnerReference must keep producing exactly the
// base-branch references after delegating to transformer.GenerateOwnerReferenceFromObject.
// Golden values below were captured from the base implementation (e0fc4c18), see
// docs/verification/ownerref-converge/results/premise-base-diff.txt.

import (
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestVerifyDatasetControllerOwnerReferenceGolden(t *testing.T) {
	cases := map[string]struct {
		typeMeta           metav1.TypeMeta
		expectedKind       string
		expectedAPIVersion string
	}{
		"empty TypeMeta":           {metav1.TypeMeta{}, "Dataset", "data.fluid.io/v1alpha1"},
		"complete TypeMeta":        {metav1.TypeMeta{Kind: "Dataset", APIVersion: "data.fluid.io/v1alpha1"}, "Dataset", "data.fluid.io/v1alpha1"},
		"kind only":                {metav1.TypeMeta{Kind: "Dataset"}, "Dataset", "data.fluid.io/v1alpha1"},
		"apiVersion only":          {metav1.TypeMeta{APIVersion: "data.fluid.io/v1alpha1"}, "Dataset", "data.fluid.io/v1alpha1"},
		"custom version preserved": {metav1.TypeMeta{Kind: "Dataset", APIVersion: "data.fluid.io/v1beta1"}, "Dataset", "data.fluid.io/v1beta1"},
		"wrong kind kept as-is":    {metav1.TypeMeta{Kind: "NotADataset"}, "NotADataset", "data.fluid.io/v1alpha1"},
		"group-less version":       {metav1.TypeMeta{Kind: "Dataset", APIVersion: "v1alpha1"}, "Dataset", "v1alpha1"},
		"version only, group-less": {metav1.TypeMeta{APIVersion: "v1alpha1"}, "Dataset", "v1alpha1"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ds := &datav1alpha1.Dataset{TypeMeta: tc.typeMeta, ObjectMeta: metav1.ObjectMeta{Name: "d", Namespace: "ns", UID: "uid-1"}}
			ref := datasetControllerOwnerReference(ds)
			if ref.Kind != tc.expectedKind {
				t.Errorf("Kind: expected %q (base behavior), got %q", tc.expectedKind, ref.Kind)
			}
			if ref.APIVersion != tc.expectedAPIVersion {
				t.Errorf("APIVersion: expected %q (base behavior), got %q", tc.expectedAPIVersion, ref.APIVersion)
			}
			if ref.Name != "d" || string(ref.UID) != "uid-1" {
				t.Errorf("Name/UID mismatch: %+v", ref)
			}
			if ref.Controller == nil || !*ref.Controller {
				t.Errorf("Controller must be pointer to true, got %v", ref.Controller)
			}
			if ref.BlockOwnerDeletion != nil {
				t.Errorf("BlockOwnerDeletion must stay nil (base behavior), got %v", *ref.BlockOwnerDeletion)
			}
		})
	}
}
