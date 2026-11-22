package thin

// Verification harness for PR #6274 / issue #6273 (reviewer: codex).
// Polarity: CONTRACT tests — they assert the intended correct behavior.
// On the base branch (896d21e) F1/F2 are expected to FAIL (bug reproduces);
// on the PR head (6fed668) they are expected to PASS (fix works).

import (
	"reflect"
	"strings"
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

func buildEngineForFuseOptionVerify(accessModes []corev1.PersistentVolumeAccessMode, profileOpts, runtimeOpts map[string]string) (*ThinEngine, *datav1alpha1.ThinRuntime, *datav1alpha1.ThinRuntimeProfile) {
	dataset, thinruntime, profile := mockFluidObjectsForTests(types.NamespacedName{Name: "verify-dataset", Namespace: "default"})
	dataset.Spec.AccessModes = accessModes
	profile.Spec.Fuse.Options = profileOpts
	thinruntime.Spec.Fuse.Options = runtimeOpts

	engine := mockThinEngineForTests(dataset, thinruntime, profile)
	resources := []runtime.Object{dataset, thinruntime, profile}
	engine.Client = fake.NewFakeClientWithScheme(datav1alpha1.UnitTestScheme, resources...)
	return engine, thinruntime, profile
}

func hasToken(options, token string) bool {
	for _, o := range strings.Split(options, ",") {
		if o == token {
			return true
		}
	}
	return false
}

// F1: read-only dataset (empty accessModes defaults to ReadOnlyMany) + profile
// with fuse options => "ro" must survive in MOUNT_OPTIONS.
func TestVerifyRoKeptWhenProfileDefinesOptions(t *testing.T) {
	engine, thinruntime, profile := buildEngineForFuseOptionVerify(
		nil, // empty accessModes -> default ReadOnlyMany
		map[string]string{"profile-opt1": "p1", "profile-opt2": "p2"},
		map[string]string{"runtime-opt": "r1"},
	)

	options, err := engine.parseFuseOptions(thinruntime, profile, nil)
	if err != nil {
		t.Fatalf("parseFuseOptions returned error: %v", err)
	}
	t.Logf("MOUNT_OPTIONS = %q", options)
	if !hasToken(options, "ro") {
		t.Errorf("read-only dataset: expected token %q in MOUNT_OPTIONS, got %q", "ro", options)
	}
	for _, want := range []string{"profile-opt1=p1", "profile-opt2=p2", "runtime-opt=r1"} {
		if !hasToken(options, want) {
			t.Errorf("expected option %q in MOUNT_OPTIONS, got %q", want, options)
		}
	}
}

// F2: runtime options must not be written into the profile's own map.
func TestVerifyProfileOptionsMapNotMutated(t *testing.T) {
	engine, thinruntime, profile := buildEngineForFuseOptionVerify(
		nil,
		map[string]string{"profile-opt1": "p1"},
		map[string]string{"runtime-opt": "r1"},
	)

	before := map[string]string{"profile-opt1": "p1"}
	if _, err := engine.parseFuseOptions(thinruntime, profile, nil); err != nil {
		t.Fatalf("parseFuseOptions returned error: %v", err)
	}
	if !reflect.DeepEqual(profile.Spec.Fuse.Options, before) {
		t.Errorf("profile.Spec.Fuse.Options mutated: got %v, want %v", profile.Spec.Fuse.Options, before)
	}
}

// F3 (guard): writable dataset (ReadWriteMany) + profile options => no "ro".
func TestVerifyNoRoForWritableDataset(t *testing.T) {
	engine, thinruntime, profile := buildEngineForFuseOptionVerify(
		[]corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany},
		map[string]string{"profile-opt1": "p1"},
		nil,
	)

	options, err := engine.parseFuseOptions(thinruntime, profile, nil)
	if err != nil {
		t.Fatalf("parseFuseOptions returned error: %v", err)
	}
	t.Logf("MOUNT_OPTIONS = %q", options)
	if hasToken(options, "ro") {
		t.Errorf("writable dataset: unexpected token %q in MOUNT_OPTIONS %q", "ro", options)
	}
	if !hasToken(options, "profile-opt1=p1") {
		t.Errorf("expected option %q in MOUNT_OPTIONS, got %q", "profile-opt1=p1", options)
	}
}

// F4 (observation, non-blocking): RO dataset + profile that sets "rw" =>
// MOUNT_OPTIONS contains BOTH "ro" and "rw" in random (map iteration) order.
// libfuse applies the last one wins, so the effective mode is nondeterministic.
// Asserts the observed behavior on the PR head; passes pre- and post-fix.
func TestVerifyRoAndRwCoexistWhenProfileSetsRw(t *testing.T) {
	engine, thinruntime, profile := buildEngineForFuseOptionVerify(
		nil, // empty accessModes -> default ReadOnlyMany
		map[string]string{"rw": ""},
		nil,
	)

	options, err := engine.parseFuseOptions(thinruntime, profile, nil)
	if err != nil {
		t.Fatalf("parseFuseOptions returned error: %v", err)
	}
	t.Logf("MOUNT_OPTIONS = %q", options)
	if !hasToken(options, "ro") || !hasToken(options, "rw") {
		t.Errorf("expected both ro and rw tokens in MOUNT_OPTIONS, got %q", options)
	}
}
