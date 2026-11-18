/*
Verification harness for PR #6266 (Reviewer B / Codex). Additive test file: production code untouched.

Polarity: CONTRACT. Same three contracts as the jindofsx harness, for JindoCacheEngine.
On the base branch (env-only behavior) the TestVerifyJindoCacheRuntimePullSecrets* tests FAIL
(premise P0 reproduction); on the PR head they PASS.
*/
package jindocache

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

func buildVerifyCacheEngine() JindoCacheEngine {
	return JindoCacheEngine{
		Client:    fake.NewFakeClientWithScheme(testScheme),
		namespace: "fluid",
		name:      "test-dataset",
		Log:       fake.NullLogger(),
	}
}

func verifyCacheDataloadInputs() (*datav1alpha1.Dataset, *datav1alpha1.DataLoad) {
	return &datav1alpha1.Dataset{
			ObjectMeta: metav1.ObjectMeta{Name: "test-dataset", Namespace: "fluid"},
		}, &datav1alpha1.DataLoad{
			ObjectMeta: metav1.ObjectMeta{Name: "test-dataload", Namespace: "fluid"},
			Spec: datav1alpha1.DataLoadSpec{
				Dataset: datav1alpha1.TargetDataset{Name: "test-dataset", Namespace: "fluid"},
			},
		}
}

// P0 / F1 contract: runtime imagePullSecrets must win over the env default. Fails on base.
func TestVerifyJindoCacheRuntimePullSecretsWin(t *testing.T) {
	t.Setenv(common.EnvImagePullSecretsKey, "env-secret")
	engine := buildVerifyCacheEngine()
	ds, dl := verifyCacheDataloadInputs()
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
func TestVerifyJindoCacheRuntimePullSecretsEnvUnset(t *testing.T) {
	t.Setenv(common.EnvImagePullSecretsKey, "")
	engine := buildVerifyCacheEngine()
	ds, dl := verifyCacheDataloadInputs()
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

// Backward-compat contract: env fallback when the runtime list is empty. Passes on base AND head.
func TestVerifyJindoCacheEnvFallbackWhenRuntimeEmpty(t *testing.T) {
	t.Setenv(common.EnvImagePullSecretsKey, "env-secret")
	engine := buildVerifyCacheEngine()
	ds, dl := verifyCacheDataloadInputs()
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

// TestVerifyEmitJindoCacheDataLoadValues renders real genDataLoadValue output to $VERIFY_EMIT_DIR
// for scripts/helm_render_check.sh (integration layer).
func TestVerifyEmitJindoCacheDataLoadValues(t *testing.T) {
	dir := os.Getenv("VERIFY_EMIT_DIR")
	if dir == "" {
		t.Skip("VERIFY_EMIT_DIR not set; emission only runs under the helm_render_check.sh harness")
	}
	engine := buildVerifyCacheEngine()
	ds, dl := verifyCacheDataloadInputs()

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
	if err := os.WriteFile(dir+"/jindocache-values-runtime-secret.yaml", data, 0o600); err != nil {
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
	if err := os.WriteFile(dir+"/jindocache-values-no-secret.yaml", data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}
