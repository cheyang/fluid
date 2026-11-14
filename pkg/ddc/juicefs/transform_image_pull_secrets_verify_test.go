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

// Verification harness for fluid-cloudnative/fluid PR #6245
// (hierarchical imagePullSecrets for JuiceFSRuntime). Additive only: this file
// does not modify any production code. Plain go tests (no ginkgo) so that
// re-verify.sh can match them precisely with -run 'TestVerifyPr6245'.

package juicefs

import (
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/ddc/base"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
)

func pr6245Secrets(names ...string) []corev1.LocalObjectReference {
	refs := make([]corev1.LocalObjectReference, 0, len(names))
	for _, name := range names {
		refs = append(refs, corev1.LocalObjectReference{Name: name})
	}
	return refs
}

func pr6245Dataset() *datav1alpha1.Dataset {
	return &datav1alpha1.Dataset{
		Spec: datav1alpha1.DatasetSpec{
			Mounts: []datav1alpha1.Mount{{
				MountPoint: "juicefs:///mnt/test",
				Name:       "test",
				EncryptOptions: []datav1alpha1.EncryptOption{{
					Name: "metaurl",
					ValueFrom: datav1alpha1.EncryptOptionSource{
						SecretKeyRef: datav1alpha1.SecretKeySelector{Name: "test", Key: "metaurl"},
					},
				}},
			}},
		},
	}
}

func pr6245Engine(t *testing.T, jfsRuntime *datav1alpha1.JuiceFSRuntime) *JuiceFSEngine {
	t.Helper()
	juicefsSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "fluid"},
		Data:       map[string][]byte{"metaurl": []byte("test")},
	}
	fakeClient := fake.NewFakeClientWithScheme(testScheme, []runtime.Object{juicefsSecret}...)
	runtimeInfo, err := base.BuildRuntimeInfo("test", "fluid", "juicefs")
	if err != nil {
		t.Fatalf("BuildRuntimeInfo: %v", err)
	}
	return &JuiceFSEngine{
		name:        "test",
		namespace:   "fluid",
		Client:      fakeClient,
		Log:         fake.NullLogger(),
		runtime:     jfsRuntime,
		runtimeInfo: runtimeInfo,
	}
}

// Contract: with only IMAGE_PULL_SECRETS set on the controller and nothing on
// the runtime, the rendered values keep the env default at the top level and
// leave the component overrides empty (pre-PR behavior preserved).
func TestVerifyPr6245EnvDefaultStillApplies(t *testing.T) {
	t.Setenv(common.EnvImagePullSecretsKey, "env-secret")
	rt := &datav1alpha1.JuiceFSRuntime{}
	value := &JuiceFS{Edition: CommunityEdition}

	if err := pr6245Engine(t, rt).transformWorkers(rt, pr6245Dataset(), value); err != nil {
		t.Fatalf("transformWorkers: %v", err)
	}
	if err := pr6245Engine(t, rt).transformFuse(rt, pr6245Dataset(), value); err != nil {
		t.Fatalf("transformFuse: %v", err)
	}

	if got := value.ImagePullSecrets; len(got) != 1 || got[0].Name != "env-secret" {
		t.Errorf("top-level ImagePullSecrets = %v, want [env-secret]", got)
	}
	if len(value.Worker.ImagePullSecrets) != 0 {
		t.Errorf("Worker.ImagePullSecrets = %v, want empty (chart falls back to top-level)", value.Worker.ImagePullSecrets)
	}
	if len(value.Fuse.ImagePullSecrets) != 0 {
		t.Errorf("Fuse.ImagePullSecrets = %v, want empty (chart falls back to top-level)", value.Fuse.ImagePullSecrets)
	}
}

// Contract: spec.imagePullSecrets replaces the controller env default at the
// top level of the helm values.
func TestVerifyPr6245RuntimeOverridesEnv(t *testing.T) {
	t.Setenv(common.EnvImagePullSecretsKey, "env-secret")
	rt := &datav1alpha1.JuiceFSRuntime{
		Spec: datav1alpha1.JuiceFSRuntimeSpec{ImagePullSecrets: pr6245Secrets("runtime-secret")},
	}
	value := &JuiceFS{Edition: CommunityEdition}

	if err := pr6245Engine(t, rt).transformWorkers(rt, pr6245Dataset(), value); err != nil {
		t.Fatalf("transformWorkers: %v", err)
	}
	if got := value.ImagePullSecrets; len(got) != 1 || got[0].Name != "runtime-secret" {
		t.Errorf("top-level ImagePullSecrets = %v, want [runtime-secret]", got)
	}
}

// Contract: spec.worker.imagePullSecrets and spec.fuse.imagePullSecrets land on
// the per-component values while the top level still carries the runtime-level
// list (chart-side fallback target).
func TestVerifyPr6245WorkerAndFuseOverrides(t *testing.T) {
	rt := &datav1alpha1.JuiceFSRuntime{
		Spec: datav1alpha1.JuiceFSRuntimeSpec{
			ImagePullSecrets: pr6245Secrets("runtime-secret"),
			Worker:           datav1alpha1.JuiceFSCompTemplateSpec{ImagePullSecrets: pr6245Secrets("worker-secret")},
			Fuse:             datav1alpha1.JuiceFSFuseSpec{ImagePullSecrets: pr6245Secrets("fuse-secret")},
		},
	}
	value := &JuiceFS{Edition: CommunityEdition}

	if err := pr6245Engine(t, rt).transformWorkers(rt, pr6245Dataset(), value); err != nil {
		t.Fatalf("transformWorkers: %v", err)
	}
	if err := pr6245Engine(t, rt).transformFuse(rt, pr6245Dataset(), value); err != nil {
		t.Fatalf("transformFuse: %v", err)
	}

	if got := value.ImagePullSecrets; len(got) != 1 || got[0].Name != "runtime-secret" {
		t.Errorf("top-level ImagePullSecrets = %v, want [runtime-secret]", got)
	}
	if got := value.Worker.ImagePullSecrets; len(got) != 1 || got[0].Name != "worker-secret" {
		t.Errorf("Worker.ImagePullSecrets = %v, want [worker-secret]", got)
	}
	if got := value.Fuse.ImagePullSecrets; len(got) != 1 || got[0].Name != "fuse-secret" {
		t.Errorf("Fuse.ImagePullSecrets = %v, want [fuse-secret]", got)
	}
}

// Contract: the serialized helm values expose exactly the value paths the
// chart templates read: imagePullSecrets, worker.imagePullSecrets,
// fuse.imagePullSecrets.
func TestVerifyPr6245HelmValuesShape(t *testing.T) {
	value := &JuiceFS{}
	value.ImagePullSecrets = pr6245Secrets("runtime-secret")
	value.Worker.ImagePullSecrets = pr6245Secrets("worker-secret")
	value.Fuse.ImagePullSecrets = pr6245Secrets("fuse-secret")

	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var top []struct{ Name string }
	if err := json.Unmarshal(decoded["imagePullSecrets"], &top); err != nil || len(top) != 1 || top[0].Name != "runtime-secret" {
		t.Errorf("values.imagePullSecrets = %s, want [{name:runtime-secret}]", decoded["imagePullSecrets"])
	}
	var worker struct {
		ImagePullSecrets []struct{ Name string } `json:"imagePullSecrets"`
	}
	if err := json.Unmarshal(decoded["worker"], &worker); err != nil || len(worker.ImagePullSecrets) != 1 || worker.ImagePullSecrets[0].Name != "worker-secret" {
		t.Errorf("values.worker.imagePullSecrets missing/wrong in %s", decoded["worker"])
	}
	var fuse struct {
		ImagePullSecrets []struct{ Name string } `json:"imagePullSecrets"`
	}
	if err := json.Unmarshal(decoded["fuse"], &fuse); err != nil || len(fuse.ImagePullSecrets) != 1 || fuse.ImagePullSecrets[0].Name != "fuse-secret" {
		t.Errorf("values.fuse.imagePullSecrets missing/wrong in %s", decoded["fuse"])
	}
}

// Canary (documents current behavior): secrets set on spec.master /
// spec.jobWorker are accepted by the API but dropped by the transform - no
// chart value consumes them. If a future change wires them up, this test
// flips red and must be inverted/promoted to a contract test.
func TestVerifyPr6245MasterAndJobWorkerSecretsInert(t *testing.T) {
	rt := &datav1alpha1.JuiceFSRuntime{
		Spec: datav1alpha1.JuiceFSRuntimeSpec{
			Master:    datav1alpha1.JuiceFSCompTemplateSpec{ImagePullSecrets: pr6245Secrets("master-secret")},
			JobWorker: datav1alpha1.JuiceFSCompTemplateSpec{ImagePullSecrets: pr6245Secrets("jobworker-secret")},
		},
	}
	value := &JuiceFS{Edition: CommunityEdition}

	if err := pr6245Engine(t, rt).transformWorkers(rt, pr6245Dataset(), value); err != nil {
		t.Fatalf("transformWorkers: %v", err)
	}
	if err := pr6245Engine(t, rt).transformFuse(rt, pr6245Dataset(), value); err != nil {
		t.Fatalf("transformFuse: %v", err)
	}

	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// NB: common.ImageInfo.ImagePullSecrets has no omitempty, so the key is
	// always present; "leak" means a non-empty list.
	var top []struct{ Name string }
	if raw, ok := decoded["imagePullSecrets"]; ok && string(raw) != "null" {
		if err := json.Unmarshal(raw, &top); err != nil {
			t.Fatalf("decode top-level imagePullSecrets: %v", err)
		}
	}
	if len(top) != 0 {
		t.Errorf("master/jobWorker secrets leaked into top-level imagePullSecrets: %+v", top)
	}
	if _, ok := decoded["master"]; ok {
		t.Errorf("chart values unexpectedly contain a master section: %s", decoded["master"])
	}
	if _, ok := decoded["jobWorker"]; ok {
		t.Errorf("chart values unexpectedly contain a jobWorker section: %s", decoded["jobWorker"])
	}
}

// Contract: the regenerated deepcopy performs an isolated copy of the three
// new API fields (no aliasing of the original slices).
func TestVerifyPr6245DeepCopyIsolated(t *testing.T) {
	rt := &datav1alpha1.JuiceFSRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "fluid"},
		Spec: datav1alpha1.JuiceFSRuntimeSpec{
			ImagePullSecrets: pr6245Secrets("runtime-secret"),
			Worker:           datav1alpha1.JuiceFSCompTemplateSpec{ImagePullSecrets: pr6245Secrets("worker-secret")},
			Fuse:             datav1alpha1.JuiceFSFuseSpec{ImagePullSecrets: pr6245Secrets("fuse-secret")},
		},
	}
	dup := rt.DeepCopy()
	dup.Spec.ImagePullSecrets[0].Name = "mutated"
	dup.Spec.Worker.ImagePullSecrets[0].Name = "mutated"
	dup.Spec.Fuse.ImagePullSecrets[0].Name = "mutated"

	if rt.Spec.ImagePullSecrets[0].Name != "runtime-secret" ||
		rt.Spec.Worker.ImagePullSecrets[0].Name != "worker-secret" ||
		rt.Spec.Fuse.ImagePullSecrets[0].Name != "fuse-secret" {
		t.Errorf("DeepCopy aliases the original: %+v", rt.Spec)
	}
}
