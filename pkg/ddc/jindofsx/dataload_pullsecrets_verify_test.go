/*
Verification harness for PR #6266 (Reviewer B / Codex). Additive test file: production code untouched.

Polarity: CONTRACT. These tests assert the intended behavior:
  runtime.spec.imagePullSecrets wins when set; controller IMAGE_PULL_SECRETS env is the fallback.
On the base branch (env-only behavior) the TestVerifyJindoFSxRuntimePullSecrets* tests FAIL
(that failure is the premise P0 reproduction); on the PR head they PASS.
*/
package jindofsx

import (
	"os"
	"reflect"
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

func buildVerifyFSxEngine() JindoFSxEngine {
	return JindoFSxEngine{
		Client:    fake.NewFakeClientWithScheme(testScheme),
		namespace: "fluid",
		name:      "test-dataset",
		Log:       fake.NullLogger(),
	}
}

func verifyFSxDataloadInputs() (*datav1alpha1.Dataset, *datav1alpha1.DataLoad) {
	return &datav1alpha1.Dataset{
			ObjectMeta: metav1.ObjectMeta{Name: "test-dataset", Namespace: "fluid"},
		}, &datav1alpha1.DataLoad{
			ObjectMeta: metav1.ObjectMeta{Name: "test-dataload", Namespace: "fluid"},
			Spec: datav1alpha1.DataLoadSpec{
				Dataset: datav1alpha1.TargetDataset{Name: "test-dataset", Namespace: "fluid"},
			},
		}
}

// P0 / F1 contract: a runtime-level imagePullSecrets entry must be honored by the DataLoad values.
// Fails on base (proves the premise), passes on the PR head.
func TestVerifyJindoFSxRuntimePullSecretsWin(t *testing.T) {
	t.Setenv(common.EnvImagePullSecretsKey, "env-secret")
	engine := buildVerifyFSxEngine()
	ds, dl := verifyFSxDataloadInputs()
	rt := &datav1alpha1.JindoRuntime{
		Spec: datav1alpha1.JindoRuntimeSpec{
			ImagePullSecrets: []corev1.LocalObjectReference{{Name: "runtime-secret"}},
		},
	}
	v, err := engine.genDataLoadValue("fluid:v0.0.1", rt, ds, dl)
	if err != nil {
		t.Fatalf("genDataLoadValue: %v", err)
	}
	want := []corev1.LocalObjectReference{{Name: "runtime-secret"}}
	if !reflect.DeepEqual(v.DataLoadInfo.ImagePullSecrets, want) {
		t.Fatalf("runtime imagePullSecrets not honored: got %v, want %v", v.DataLoadInfo.ImagePullSecrets, want)
	}
}

// P0 companion: runtime secrets must be honored even when the controller env var is unset.
func TestVerifyJindoFSxRuntimePullSecretsEnvUnset(t *testing.T) {
	t.Setenv(common.EnvImagePullSecretsKey, "")
	engine := buildVerifyFSxEngine()
	ds, dl := verifyFSxDataloadInputs()
	rt := &datav1alpha1.JindoRuntime{
		Spec: datav1alpha1.JindoRuntimeSpec{
			ImagePullSecrets: []corev1.LocalObjectReference{{Name: "runtime-secret"}},
		},
	}
	v, err := engine.genDataLoadValue("fluid:v0.0.1", rt, ds, dl)
	if err != nil {
		t.Fatalf("genDataLoadValue: %v", err)
	}
	want := []corev1.LocalObjectReference{{Name: "runtime-secret"}}
	if !reflect.DeepEqual(v.DataLoadInfo.ImagePullSecrets, want) {
		t.Fatalf("runtime imagePullSecrets not honored with env unset: got %v, want %v", v.DataLoadInfo.ImagePullSecrets, want)
	}
}

// Backward-compat contract: with no runtime secrets, the controller env default still applies
// (passes on base AND head; guards the fallback path against regression).
func TestVerifyJindoFSxEnvFallbackWhenRuntimeEmpty(t *testing.T) {
	t.Setenv(common.EnvImagePullSecretsKey, "env-secret")
	engine := buildVerifyFSxEngine()
	ds, dl := verifyFSxDataloadInputs()
	rt := &datav1alpha1.JindoRuntime{}
	v, err := engine.genDataLoadValue("fluid:v0.0.1", rt, ds, dl)
	if err != nil {
		t.Fatalf("genDataLoadValue: %v", err)
	}
	want := []corev1.LocalObjectReference{{Name: "env-secret"}}
	if !reflect.DeepEqual(v.DataLoadInfo.ImagePullSecrets, want) {
		t.Fatalf("env fallback broken: got %v, want %v", v.DataLoadInfo.ImagePullSecrets, want)
	}
}

// Both empty => no imagePullSecrets at all (chart must then omit the key).
func TestVerifyJindoFSxNoSecretsAnywhere(t *testing.T) {
	t.Setenv(common.EnvImagePullSecretsKey, "")
	engine := buildVerifyFSxEngine()
	ds, dl := verifyFSxDataloadInputs()
	rt := &datav1alpha1.JindoRuntime{}
	v, err := engine.genDataLoadValue("fluid:v0.0.1", rt, ds, dl)
	if err != nil {
		t.Fatalf("genDataLoadValue: %v", err)
	}
	if len(v.DataLoadInfo.ImagePullSecrets) != 0 {
		t.Fatalf("expected no imagePullSecrets, got %v", v.DataLoadInfo.ImagePullSecrets)
	}
}

// TestVerifyEmitJindoFSxDataLoadValues renders real genDataLoadValue output to $VERIFY_EMIT_DIR
// so scripts/helm_render_check.sh can feed it to `helm template` (integration layer).
func TestVerifyEmitJindoFSxDataLoadValues(t *testing.T) {
	dir := os.Getenv("VERIFY_EMIT_DIR")
	if dir == "" {
		t.Skip("VERIFY_EMIT_DIR not set; emission only runs under the helm_render_check.sh harness")
	}
	engine := buildVerifyFSxEngine()
	ds, dl := verifyFSxDataloadInputs()

	rt := &datav1alpha1.JindoRuntime{
		Spec: datav1alpha1.JindoRuntimeSpec{
			ImagePullSecrets: []corev1.LocalObjectReference{{Name: "runtime-secret"}},
		},
	}
	withSecrets, err := engine.genDataLoadValue("fluid:v0.0.1", rt, ds, dl)
	if err != nil {
		t.Fatalf("genDataLoadValue: %v", err)
	}
	data, err := yaml.Marshal(withSecrets)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(dir+"/jindofsx-values-runtime-secret.yaml", data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	rtEmpty := &datav1alpha1.JindoRuntime{}
	noSecrets, err := engine.genDataLoadValue("fluid:v0.0.1", rtEmpty, ds, dl)
	if err != nil {
		t.Fatalf("genDataLoadValue: %v", err)
	}
	data, err = yaml.Marshal(noSecrets)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(dir+"/jindofsx-values-no-secret.yaml", data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}
