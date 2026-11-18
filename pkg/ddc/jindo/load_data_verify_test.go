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

package jindo

// Reviewer verification harness (PR #6266). Contract test, polarity: contract.
// C3: the legacy "jindo" engine impl (still selectable via JINDO_ENGINE_TYPE=jindo,
// see pkg/utils/jindo.GetDefaultEngineImpl) serves the same JindoRuntime CRD, and
// its runtime chart honors spec.imagePullSecrets (transform.go: ImagePullSecrets:
// runtime.Spec.ImagePullSecrets). Its DataLoad should therefore honor the runtime
// secrets the same way jindofsx/jindocache now do. This contract test currently
// FAILS on the PR head because pkg/ddc/jindo/load_data.go still reads the env
// only — that failure IS the reproduction of the coverage gap (finding F2 in the
// review). It will pass once the same fix is applied to the legacy engine.

import (
	"reflect"
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
)

func TestVerifyLegacyJindoImagePullSecrets(t *testing.T) {
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
	engine := JindoEngine{
		Client:    fake.NewFakeClientWithScheme(testScheme),
		namespace: "fluid",
		name:      "test-dataset",
		Log:       fake.NullLogger(),
	}

	runtime := &datav1alpha1.JindoRuntime{}
	runtime.Spec.ImagePullSecrets = []corev1.LocalObjectReference{{Name: "runtime-secret"}}
	value, err := engine.genDataLoadValue("fluid:v0.0.1", runtime, targetDataset, dataload)
	if err != nil {
		t.Fatalf("genDataLoadValue: %v", err)
	}
	if want := []corev1.LocalObjectReference{{Name: "runtime-secret"}}; !reflect.DeepEqual(value.DataLoadInfo.ImagePullSecrets, want) {
		t.Errorf("legacy jindo engine: runtime secrets ignored, got %v, want %v", value.DataLoadInfo.ImagePullSecrets, want)
	}
}
