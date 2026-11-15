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

// Verification harness for PR #6250, DataBackup half: the DataBackup value
// file must carry the AlluxioRuntime's spec.imagePullSecrets when set.
// Contract test: FAILS on the merge-base (which always used the controller
// env IMAGE_PULL_SECRETS), PASSES with the PR applied.
//
// Layer: L1/L2 (fake client + real value-file generation), no cluster needed.

import (
	"os"
	"reflect"
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	cdatabackup "github.com/fluid-cloudnative/fluid/pkg/databackup"
	"github.com/fluid-cloudnative/fluid/pkg/ddc/base"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	cruntime "github.com/fluid-cloudnative/fluid/pkg/runtime"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

func verifyBackupEngine(t *testing.T, runtime *datav1alpha1.AlluxioRuntime) (*AlluxioEngine, cruntime.ReconcileRequestContext) {
	t.Helper()

	dataset := &datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testDatasetName,
			Namespace: testDatasetNamespace,
			UID:       "verify-uid",
		},
	}
	masterPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-dataset-master-0",
			Namespace: testDatasetNamespace,
		},
		Spec: corev1.PodSpec{
			NodeName: "verify-node",
			Containers: []corev1.Container{
				{
					Name: "alluxio-master",
					Ports: []corev1.ContainerPort{
						{Name: "rpc", ContainerPort: 19998},
					},
				},
			},
		},
		Status: corev1.PodStatus{
			PodIP: "10.0.0.1",
		},
	}

	client := fake.NewFakeClientWithScheme(testScheme, runtime, dataset, masterPod)
	runtimeInfo, err := base.BuildRuntimeInfo(testDatasetName, testDatasetNamespace, "alluxio")
	if err != nil {
		t.Fatalf("failed to build runtime info: %v", err)
	}
	engine := &AlluxioEngine{
		Client:      client,
		name:        testDatasetName,
		namespace:   testDatasetNamespace,
		runtime:     runtime,
		runtimeInfo: runtimeInfo,
		Log:         fake.NullLogger(),
	}
	ctx := cruntime.ReconcileRequestContext{Log: fake.NullLogger()}
	return engine, ctx
}

func verifyBackupObject() *datav1alpha1.DataBackup {
	return &datav1alpha1.DataBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "verify-backup",
			Namespace: testDatasetNamespace,
		},
		Spec: datav1alpha1.DataBackupSpec{
			Dataset:    testDatasetName,
			BackupPath: "pvc://verify-pvc/data",
		},
	}
}

func verifyDataBackupValue(t *testing.T, engine *AlluxioEngine, ctx cruntime.ReconcileRequestContext, databackup *datav1alpha1.DataBackup) *cdatabackup.DataBackupValue {
	t.Helper()
	fileName, err := engine.generateDataBackupValueFile(ctx, databackup)
	if err != nil {
		t.Fatalf("generateDataBackupValueFile failed: %v", err)
	}
	if fileName == "" {
		t.Fatal("expected non-empty value file name")
	}
	defer os.Remove(fileName)

	data, err := os.ReadFile(fileName)
	if err != nil {
		t.Fatalf("failed to read value file %s: %v", fileName, err)
	}
	var value cdatabackup.DataBackupValue
	if err := yaml.Unmarshal(data, &value); err != nil {
		t.Fatalf("failed to unmarshal value file %s: %v\ncontent:\n%s", fileName, err, string(data))
	}
	return &value
}

// TestPR6250DataBackupValueFileUsesRuntimePullSecrets asserts the DataBackup
// half of the PR: runtime spec.imagePullSecrets must land in the generated
// DataBackup value file, replacing the controller env default.
func TestPR6250DataBackupValueFileUsesRuntimePullSecrets(t *testing.T) {
	t.Setenv(common.EnvImagePullSecretsKey, "env-secret-should-be-replaced")

	runtimeWithSecrets := &datav1alpha1.AlluxioRuntime{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testDatasetName,
			Namespace: testDatasetNamespace,
		},
		Spec: datav1alpha1.AlluxioRuntimeSpec{
			Replicas: 1,
			ImagePullSecrets: []corev1.LocalObjectReference{
				{Name: "runtime-secret"},
			},
		},
	}

	engine, ctx := verifyBackupEngine(t, runtimeWithSecrets)
	value := verifyDataBackupValue(t, engine, ctx, verifyBackupObject())
	want := []corev1.LocalObjectReference{{Name: "runtime-secret"}}
	if !reflect.DeepEqual(value.DataBackup.ImagePullSecrets, want) {
		t.Errorf("DataBackup value file imagePullSecrets = %v, want %v (runtime secrets must replace the env default)",
			value.DataBackup.ImagePullSecrets, want)
	}
}

// TestPR6250DataBackupValueFileFallsBackToEnvPullSecrets asserts the fallback
// for the DataBackup half: no runtime secrets -> controller env value.
func TestPR6250DataBackupValueFileFallsBackToEnvPullSecrets(t *testing.T) {
	t.Setenv(common.EnvImagePullSecretsKey, "env-secret")

	runtimeNoSecrets := &datav1alpha1.AlluxioRuntime{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testDatasetName,
			Namespace: testDatasetNamespace,
		},
		Spec: datav1alpha1.AlluxioRuntimeSpec{
			Replicas: 1,
		},
	}

	engine, ctx := verifyBackupEngine(t, runtimeNoSecrets)
	value := verifyDataBackupValue(t, engine, ctx, verifyBackupObject())
	want := []corev1.LocalObjectReference{{Name: "env-secret"}}
	if !reflect.DeepEqual(value.DataBackup.ImagePullSecrets, want) {
		t.Errorf("DataBackup value file imagePullSecrets = %v, want %v (env fallback)", value.DataBackup.ImagePullSecrets, want)
	}
}
