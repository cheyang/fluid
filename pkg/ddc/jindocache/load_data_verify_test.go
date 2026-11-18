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

package jindocache

// Reviewer verification harness (PR #6266). Contract test, polarity: contract.
// C1: when JindoRuntime.spec.imagePullSecrets is set, the generated DataLoad
// values must carry the runtime secrets (replacing the controller env
// default); when the runtime list is empty, the env value remains the
// fallback. This test FAILS on the pre-PR code (env-only).

import (
	"reflect"
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
)

func TestVerifyImagePullSecretsPriority(t *testing.T) {
	t.Setenv(common.EnvImagePullSecretsKey, "env-secret")

	targetDataset := &datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: "test-dataset", Namespace: "fluid"},
	}
	dataload := &datav1alpha1.DataLoad{
		ObjectMeta: metav1.ObjectMeta{Name: "test-dataload", Namespace: "fluid"},
		Spec: datav1alpha1.DataLoadSpec{
			Dataset: datav1alpha1.TargetDataset{Name: "test-dataset", Namespace: "fluid"},
		},
	}
	engine := JindoCacheEngine{
		Client:    fake.NewFakeClientWithScheme(testScheme),
		namespace: "fluid",
		name:      "test-dataset",
		Log:       fake.NullLogger(),
	}

	// 1. runtime secrets set (env also set) -> runtime replaces env
	runtime := &datav1alpha1.JindoRuntime{}
	runtime.Spec.ImagePullSecrets = []corev1.LocalObjectReference{{Name: "runtime-secret"}}
	value, err := engine.genDataLoadValue("fluid:v0.0.1", runtime, targetDataset, dataload)
	if err != nil {
		t.Fatalf("genDataLoadValue with runtime secrets: %v", err)
	}
	if want := []corev1.LocalObjectReference{{Name: "runtime-secret"}}; !reflect.DeepEqual(value.DataLoadInfo.ImagePullSecrets, want) {
		t.Errorf("runtime+env set: got %v, want %v (runtime must replace env default)", value.DataLoadInfo.ImagePullSecrets, want)
	}

	// 2. runtime empty -> env fallback preserved
	runtime.Spec.ImagePullSecrets = nil
	value, err = engine.genDataLoadValue("fluid:v0.0.1", runtime, targetDataset, dataload)
	if err != nil {
		t.Fatalf("genDataLoadValue with env-only: %v", err)
	}
	if want := []corev1.LocalObjectReference{{Name: "env-secret"}}; !reflect.DeepEqual(value.DataLoadInfo.ImagePullSecrets, want) {
		t.Errorf("runtime empty: got %v, want %v (env fallback)", value.DataLoadInfo.ImagePullSecrets, want)
	}

	// 3. neither set -> empty list, no error
	t.Setenv(common.EnvImagePullSecretsKey, "")
	value, err = engine.genDataLoadValue("fluid:v0.0.1", runtime, targetDataset, dataload)
	if err != nil {
		t.Fatalf("genDataLoadValue with no secrets: %v", err)
	}
	if got := value.DataLoadInfo.ImagePullSecrets; len(got) != 0 {
		t.Errorf("no secrets anywhere: got %v, want empty", got)
	}
}
