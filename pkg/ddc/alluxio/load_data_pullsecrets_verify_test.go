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

package alluxio

// Verification harness for PR #6250 (feat(alluxio): use runtime imagePullSecrets
// for DataLoad and DataBackup). These are contract tests: each asserts the
// *intended correct* behavior, so it FAILS on code where the behavior is absent
// (e.g. the merge-base, where the DataLoad value file always used the
// controller env IMAGE_PULL_SECRETS and ignored runtime spec) and PASSES once
// the runtime secrets are wired through.
//
// Layer: L1/L2 (fake client + real value-file generation), no cluster needed.

import (
	"os"
	"reflect"
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	cdataload "github.com/fluid-cloudnative/fluid/pkg/dataload"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	cruntime "github.com/fluid-cloudnative/fluid/pkg/runtime"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

const (
	verifyDatasetName      = "verify-dataset"
	verifyDatasetNamespace = "fluid"
)

func verifyDataLoadValue(t *testing.T, engine AlluxioEngine, ctx cruntime.ReconcileRequestContext, dataload *datav1alpha1.DataLoad) *cdataload.DataLoadValue {
	t.Helper()
	fileName, err := engine.generateDataLoadValueFile(ctx, dataload)
	if err != nil {
		t.Fatalf("generateDataLoadValueFile failed: %v", err)
	}
	if fileName == "" {
		t.Fatal("expected non-empty value file name")
	}
	defer os.Remove(fileName)

	data, err := os.ReadFile(fileName)
	if err != nil {
		t.Fatalf("failed to read value file %s: %v", fileName, err)
	}
	var value cdataload.DataLoadValue
	if err := yaml.Unmarshal(data, &value); err != nil {
		t.Fatalf("failed to unmarshal value file %s: %v\ncontent:\n%s", fileName, err, string(data))
	}
	return &value
}

func verifyDataLoadObject() *datav1alpha1.DataLoad {
	return &datav1alpha1.DataLoad{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "verify-dataload",
			Namespace: verifyDatasetNamespace,
		},
		Spec: datav1alpha1.DataLoadSpec{
			Dataset: datav1alpha1.TargetDataset{
				Name:      verifyDatasetName,
				Namespace: verifyDatasetNamespace,
			},
			Target: []datav1alpha1.TargetPath{
				{Path: "/verify", Replicas: 1},
			},
		},
	}
}

func verifyDatasetObject() *datav1alpha1.Dataset {
	return &datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{
			Name:      verifyDatasetName,
			Namespace: verifyDatasetNamespace,
			UID:       "verify-uid",
		},
	}
}

// TestPR6250DataLoadValueFileUsesRuntimePullSecrets asserts the core claim of
// the PR: when the AlluxioRuntime sets spec.imagePullSecrets, the generated
// DataLoad value file carries those secrets (and they are not merged with the
// controller env default).
func TestPR6250DataLoadValueFileUsesRuntimePullSecrets(t *testing.T) {
	t.Setenv(common.EnvImagePullSecretsKey, "env-secret-should-be-replaced")

	runtimeWithSecrets := &datav1alpha1.AlluxioRuntime{
		ObjectMeta: metav1.ObjectMeta{
			Name:      verifyDatasetName,
			Namespace: verifyDatasetNamespace,
		},
		Spec: datav1alpha1.AlluxioRuntimeSpec{
			ImagePullSecrets: []corev1.LocalObjectReference{{Name: "runtime-secret"}},
		},
	}

	client := fake.NewFakeClientWithScheme(testScheme, verifyDatasetObject(), runtimeWithSecrets)
	engine := AlluxioEngine{}
	ctx := cruntime.ReconcileRequestContext{Client: client}

	value := verifyDataLoadValue(t, engine, ctx, verifyDataLoadObject())
	want := []corev1.LocalObjectReference{{Name: "runtime-secret"}}
	if !reflect.DeepEqual(value.DataLoadInfo.ImagePullSecrets, want) {
		t.Errorf("DataLoad value file imagePullSecrets = %v, want %v (runtime secrets must replace the env default)",
			value.DataLoadInfo.ImagePullSecrets, want)
	}
}

// TestPR6250DataLoadValueFileFallsBackToEnvPullSecrets asserts the fallback:
// with no runtime spec secrets, the controller env value is used.
func TestPR6250DataLoadValueFileFallsBackToEnvPullSecrets(t *testing.T) {
	t.Setenv(common.EnvImagePullSecretsKey, "env-secret")

	runtimeNoSecrets := &datav1alpha1.AlluxioRuntime{
		ObjectMeta: metav1.ObjectMeta{
			Name:      verifyDatasetName,
			Namespace: verifyDatasetNamespace,
		},
	}

	client := fake.NewFakeClientWithScheme(testScheme, verifyDatasetObject(), runtimeNoSecrets)
	engine := AlluxioEngine{}
	ctx := cruntime.ReconcileRequestContext{Client: client}

	value := verifyDataLoadValue(t, engine, ctx, verifyDataLoadObject())
	want := []corev1.LocalObjectReference{{Name: "env-secret"}}
	if !reflect.DeepEqual(value.DataLoadInfo.ImagePullSecrets, want) {
		t.Errorf("DataLoad value file imagePullSecrets = %v, want %v (env fallback)", value.DataLoadInfo.ImagePullSecrets, want)
	}
}

// TestPR6250DataLoadValueFileRequiresRuntime documents the new hard dependency
// introduced by the PR: the DataLoad value file generation now fails when the
// AlluxioRuntime object cannot be found (previously it did not need the
// runtime object at all). Contract test for the new intended behavior.
func TestPR6250DataLoadValueFileRequiresRuntime(t *testing.T) {
	client := fake.NewFakeClientWithScheme(testScheme, verifyDatasetObject())
	engine := AlluxioEngine{}
	ctx := cruntime.ReconcileRequestContext{Client: client}

	fileName, err := engine.generateDataLoadValueFile(ctx, verifyDataLoadObject())
	if err == nil {
		os.Remove(fileName)
		t.Fatal("expected an error when the AlluxioRuntime is missing, got nil")
	}
	if fileName != "" {
		t.Errorf("expected empty value file name on error, got %q", fileName)
	}
}
