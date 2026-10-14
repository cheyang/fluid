// Verification harness for the review of https://github.com/fluid-cloudnative/fluid/pull/6206
// (fixes https://github.com/fluid-cloudnative/fluid/issues/6205).
//
// Additive reviewer harness only — no production code is modified.
//
// Polarity legend:
//
//	CONTRACT  — asserts the intended correct behavior. FAILS on the base (pre-fix) code,
//	            PASSES on the PR head. A red result on base IS the reproduction of issue #6205.
//	GUARD     — asserts behavior that must stay identical before and after the fix
//	            (regression guard). Expected to PASS on both base and head.
//
// Layer: L1 (deterministic unit).
package transformer

import (
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TestVerifyPR6206GroupLessAPIVersionContract is the P0 premise check, CONTRACT polarity.
//
// Issue #6205: an owner object whose TypeMeta carries a group-less apiVersion
// (e.g. TypeMeta{Kind: "Dataset", APIVersion: "v1alpha1"}) parses with non-empty
// Kind and Version, so base code skips scheme recovery and yields an ownerReference
// with apiVersion "v1alpha1" (group missing). The intended behavior is
// apiVersion "data.fluid.io/v1alpha1".
//
// Runs against every fluid owner type used by the 16 production callers of
// GenerateOwnerReferenceFromObject (runtimes / DataLoad / DataMigrate / DataProcess / Dataset).
func TestVerifyPR6206GroupLessAPIVersionContract(t *testing.T) {
	cases := []struct {
		name string
		obj  client.Object
		kind string
	}{
		{"dataset", &datav1alpha1.Dataset{TypeMeta: metav1.TypeMeta{Kind: "Dataset", APIVersion: "v1alpha1"},
			ObjectMeta: metav1.ObjectMeta{Name: "d", Namespace: "default", UID: "uid-d"}}, "Dataset"},
		{"alluxio-runtime", &datav1alpha1.AlluxioRuntime{TypeMeta: metav1.TypeMeta{Kind: "AlluxioRuntime", APIVersion: "v1alpha1"},
			ObjectMeta: metav1.ObjectMeta{Name: "r", Namespace: "default", UID: "uid-r"}}, "AlluxioRuntime"},
		{"thin-runtime", &datav1alpha1.ThinRuntime{TypeMeta: metav1.TypeMeta{Kind: "ThinRuntime", APIVersion: "v1alpha1"},
			ObjectMeta: metav1.ObjectMeta{Name: "t", Namespace: "default", UID: "uid-t"}}, "ThinRuntime"},
		{"dataload", &datav1alpha1.DataLoad{TypeMeta: metav1.TypeMeta{Kind: "DataLoad", APIVersion: "v1alpha1"},
			ObjectMeta: metav1.ObjectMeta{Name: "l", Namespace: "default", UID: "uid-l"}}, "DataLoad"},
		{"datamigrate", &datav1alpha1.DataMigrate{TypeMeta: metav1.TypeMeta{Kind: "DataMigrate", APIVersion: "v1alpha1"},
			ObjectMeta: metav1.ObjectMeta{Name: "m", Namespace: "default", UID: "uid-m"}}, "DataMigrate"},
		{"dataprocess", &datav1alpha1.DataProcess{TypeMeta: metav1.TypeMeta{Kind: "DataProcess", APIVersion: "v1alpha1"},
			ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default", UID: "uid-p"}}, "DataProcess"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ref := GenerateOwnerReferenceFromObject(c.obj)
			t.Logf("generated ownerReference: apiVersion=%q kind=%q", ref.APIVersion, ref.Kind)
			if ref.Kind != c.kind {
				t.Errorf("kind: got %q, want %q", ref.Kind, c.kind)
			}
			// The contract from issue #6205: a group-less apiVersion must be normalized
			// to the full <group>/<version> of the registered fluid type.
			if want := datav1alpha1.GroupVersion.String(); ref.APIVersion != want {
				t.Errorf("apiVersion: got %q, want %q (group must be recovered from the scheme)", ref.APIVersion, want)
			}
		})
	}
}

// TestVerifyPR6206GroupLessAPIVersionNoKindContract is CONTRACT polarity: group-less
// apiVersion with Kind also missing. Kind is recovered on base too, but the group
// must additionally be filled so the apiVersion is not left as bare "v1alpha1".
func TestVerifyPR6206GroupLessAPIVersionNoKindContract(t *testing.T) {
	obj := &datav1alpha1.DataLoad{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1alpha1"},
		ObjectMeta: metav1.ObjectMeta{Name: "kindless", Namespace: "default", UID: "uid-k"},
	}
	ref := GenerateOwnerReferenceFromObject(obj)
	t.Logf("generated ownerReference: apiVersion=%q kind=%q", ref.APIVersion, ref.Kind)
	if ref.Kind != "DataLoad" {
		t.Errorf("kind: got %q, want %q", ref.Kind, "DataLoad")
	}
	if want := datav1alpha1.GroupVersion.String(); ref.APIVersion != want {
		t.Errorf("apiVersion: got %q, want %q (group must be recovered from the scheme)", ref.APIVersion, want)
	}
}

// TestVerifyPR6206WatchGroupMatchUnitContract is CONTRACT polarity and proves the
// impact mechanism of issue #6205 at the unit layer, mirroring exactly what
// controller-runtime's handler.enqueueRequestForOwner.getOwnerReconcileRequest does:
//
//	refGV, _ := schema.ParseGroupVersion(ref.APIVersion)
//	if ref.Kind == e.groupKind.Kind && refGV.Group == e.groupKind.Group { enqueue }
//
// An ownerReference whose apiVersion has no group never matches the owner GroupKind
// (data.fluid.io/Dataset), so the reconcile request is silently dropped.
func TestVerifyPR6206WatchGroupMatchUnitContract(t *testing.T) {
	owner := &datav1alpha1.Dataset{
		TypeMeta:   metav1.TypeMeta{Kind: "Dataset", APIVersion: "v1alpha1"},
		ObjectMeta: metav1.ObjectMeta{Name: "watched", Namespace: "default", UID: "uid-w"},
	}
	ref := GenerateOwnerReferenceFromObject(owner)

	kinds, _, err := fluidScheme.ObjectKinds(&datav1alpha1.Dataset{})
	if err != nil || len(kinds) != 1 {
		t.Fatalf("cannot resolve owner GroupKind from scheme: kinds=%v err=%v", kinds, err)
	}
	ownerGK := kinds[0].GroupKind()

	refGV, err := schema.ParseGroupVersion(ref.APIVersion)
	if err != nil {
		t.Fatalf("generated apiVersion %q is not parseable: %v", ref.APIVersion, err)
	}
	t.Logf("owner GroupKind=%s, ownerReference parsed group=%q kind=%q", ownerGK.String(), refGV.Group, ref.Kind)
	if ref.Kind != ownerGK.Kind || refGV.Group != ownerGK.Group {
		t.Errorf("owner-based watch would DROP this reference: ref {group:%q kind:%q} does not match owner GroupKind %s",
			refGV.Group, ref.Kind, ownerGK.String())
	}
}

// TestVerifyPR6206CoreTypeRegressionGuard is GUARD polarity (must pass on base AND head).
// A fully typed core-group object (Kind: Pod, APIVersion: v1) legitimately has an empty
// group. The generated reference must keep apiVersion "v1" — the widened recovery trigger
// on the PR head must not rewrite (or drop) it. (On the head the lookup against the fluid
// scheme fails and logs an error; the output must still be unchanged.)
func TestVerifyPR6206CoreTypeRegressionGuard(t *testing.T) {
	pod := &corev1.Pod{
		TypeMeta:   metav1.TypeMeta{Kind: "Pod", APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{Name: "core", Namespace: "default", UID: "uid-c"},
	}
	ref := GenerateOwnerReferenceFromObject(pod)
	t.Logf("generated ownerReference: apiVersion=%q kind=%q", ref.APIVersion, ref.Kind)
	if ref.APIVersion != "v1" || ref.Kind != "Pod" {
		t.Errorf("core type reference mangled: got apiVersion=%q kind=%q, want apiVersion=%q kind=%q",
			ref.APIVersion, ref.Kind, "v1", "Pod")
	}
}

// TestVerifyPR6206FullyPopulatedFluidTypeGuard is GUARD polarity: a fluid object whose
// TypeMeta already carries the full data.fluid.io/v1alpha1 must pass through untouched.
func TestVerifyPR6206FullyPopulatedFluidTypeGuard(t *testing.T) {
	ds := &datav1alpha1.Dataset{
		TypeMeta:   metav1.TypeMeta{Kind: "Dataset", APIVersion: datav1alpha1.GroupVersion.String()},
		ObjectMeta: metav1.ObjectMeta{Name: "full", Namespace: "default", UID: "uid-f"},
	}
	ref := GenerateOwnerReferenceFromObject(ds)
	if ref.APIVersion != datav1alpha1.GroupVersion.String() || ref.Kind != "Dataset" {
		t.Errorf("fully populated reference changed: got apiVersion=%q kind=%q", ref.APIVersion, ref.Kind)
	}
}
