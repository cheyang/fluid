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

// Verification harness for PR #6250 (reviewer: codex).
// These tests are ADDITIVE reviewer evidence; they are not part of the PR.
// They only call generateDataLoadValueFile / generateDataBackupValueFile, whose
// signatures are identical on the base and the PR head, so the same file can be
// grafted onto either tree.
//
// Polarity:
//   - TestVerifyDataLoadUsesRuntimeImagePullSecrets     : contract (fails on base, passes on head)
//   - TestVerifyDataLoadFallsBackToEnvImagePullSecrets  : contract (passes on base AND head = no regression)
//   - TestVerifyDataLoadRuntimeSecretsReplaceEnv        : contract (fails on base, passes on head)
//   - TestVerifyDataBackupUsesRuntimeImagePullSecrets   : contract (fails on base, passes on head)
//   - TestVerifyDataBackupFallsBackToEnvImagePullSecrets: contract (passes on base AND head = no regression)
//   - TestVerifyDataLoadMissingRuntimeNowErrors         : behavior documentation; passes on head,
//                                                         fails on base (base did not need the runtime)

package alluxio

import (
	"os"
	"strings"
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	cdatabackup "github.com/fluid-cloudnative/fluid/pkg/databackup"
	cdataload "github.com/fluid-cloudnative/fluid/pkg/dataload"
	"github.com/fluid-cloudnative/fluid/pkg/ddc/base"
	cruntime "github.com/fluid-cloudnative/fluid/pkg/runtime"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/yaml"
	goyaml "gopkg.in/yaml.v2"
)

const (
	verifyDatasetName      = "verify-dataset"
	verifyDatasetNamespace = "verify-ns"
	verifyRuntimeSecret    = "runtime-registry-secret"
	verifyEnvSecret        = "env-controller-secret"
)

func verifyDataLoadObjs(runtimeSecrets []corev1.LocalObjectReference) []runtime.Object {
	return []runtime.Object{
		&datav1alpha1.Dataset{
			ObjectMeta: metav1.ObjectMeta{Name: verifyDatasetName, Namespace: verifyDatasetNamespace, UID: "verify-uid"},
		},
		&datav1alpha1.AlluxioRuntime{
			ObjectMeta: metav1.ObjectMeta{Name: verifyDatasetName, Namespace: verifyDatasetNamespace},
			Spec:       datav1alpha1.AlluxioRuntimeSpec{ImagePullSecrets: runtimeSecrets},
		},
	}
}

func verifyDataLoad() *datav1alpha1.DataLoad {
	return &datav1alpha1.DataLoad{
		ObjectMeta: metav1.ObjectMeta{Name: "verify-dataload", Namespace: verifyDatasetNamespace},
		Spec: datav1alpha1.DataLoadSpec{
			Dataset: datav1alpha1.TargetDataset{Name: verifyDatasetName, Namespace: verifyDatasetNamespace},
			Target:  []datav1alpha1.TargetPath{{Path: "/verify", Replicas: 1}},
		},
	}
}

func genDataLoadValueFile(t *testing.T, objs []runtime.Object, dataload *datav1alpha1.DataLoad) *cdataload.DataLoadValue {
	t.Helper()
	client := fake.NewFakeClientWithScheme(testScheme, objs...)
	engine := &AlluxioEngine{Client: client, Log: fake.NullLogger()}
	ctx := cruntime.ReconcileRequestContext{Client: client, Log: fake.NullLogger()}

	valueFileName, err := engine.generateDataLoadValueFile(ctx, dataload)
	if err != nil {
		t.Fatalf("generateDataLoadValueFile returned error: %v", err)
	}
	defer os.Remove(valueFileName)

	data, err := os.ReadFile(valueFileName)
	if err != nil {
		t.Fatalf("failed to read generated value file %s: %v", valueFileName, err)
	}
	value := &cdataload.DataLoadValue{}
	if err := yaml.Unmarshal(data, value); err != nil {
		t.Fatalf("failed to unmarshal generated value file: %v\ncontent:\n%s", err, string(data))
	}
	return value
}

// Contract: when the AlluxioRuntime sets spec.imagePullSecrets and the controller
// IMAGE_PULL_SECRETS env is unset, the DataLoad value file must carry the runtime's
// secrets. Fails on base (base only reads the env), passes on the PR head.
func TestVerifyDataLoadUsesRuntimeImagePullSecrets(t *testing.T) {
	os.Unsetenv(common.EnvImagePullSecretsKey)

	value := genDataLoadValueFile(t,
		verifyDataLoadObjs([]corev1.LocalObjectReference{{Name: verifyRuntimeSecret}}),
		verifyDataLoad())

	got := value.DataLoadInfo.ImagePullSecrets
	if len(got) != 1 || got[0].Name != verifyRuntimeSecret {
		t.Errorf("expected dataload imagePullSecrets=[%s], got %v", verifyRuntimeSecret, got)
	}
}

// Contract (regression guard): when the runtime sets no secrets, the controller env
// default must still be used. Must pass on BOTH base and head.
func TestVerifyDataLoadFallsBackToEnvImagePullSecrets(t *testing.T) {
	t.Setenv(common.EnvImagePullSecretsKey, verifyEnvSecret)

	value := genDataLoadValueFile(t, verifyDataLoadObjs(nil), verifyDataLoad())

	got := value.DataLoadInfo.ImagePullSecrets
	if len(got) != 1 || got[0].Name != verifyEnvSecret {
		t.Errorf("expected dataload imagePullSecrets=[%s], got %v", verifyEnvSecret, got)
	}
}

// Contract: when both runtime secrets and the env default exist, the runtime's
// secrets replace the env default (same semantics as master/worker/fuse via
// parseRuntimeImage). Fails on base (base emits env secret), passes on head.
func TestVerifyDataLoadRuntimeSecretsReplaceEnv(t *testing.T) {
	t.Setenv(common.EnvImagePullSecretsKey, verifyEnvSecret)

	value := genDataLoadValueFile(t,
		verifyDataLoadObjs([]corev1.LocalObjectReference{{Name: verifyRuntimeSecret}}),
		verifyDataLoad())

	got := value.DataLoadInfo.ImagePullSecrets
	if len(got) != 1 || got[0].Name != verifyRuntimeSecret {
		t.Errorf("expected dataload imagePullSecrets=[%s] (replace, not merge), got %v", verifyRuntimeSecret, got)
	}
	for _, s := range got {
		if s.Name == verifyEnvSecret {
			t.Errorf("env secret %s should have been replaced, got %v", verifyEnvSecret, got)
		}
	}
}

// Behavior documentation (head contract): when the AlluxioRuntime object bound to the
// target dataset does not exist, generateDataLoadValueFile now returns an error
// instead of silently using the env default. On base this call succeeds.
func TestVerifyDataLoadMissingRuntimeNowErrors(t *testing.T) {
	os.Unsetenv(common.EnvImagePullSecretsKey)

	objs := []runtime.Object{
		&datav1alpha1.Dataset{
			ObjectMeta: metav1.ObjectMeta{Name: verifyDatasetName, Namespace: verifyDatasetNamespace},
		},
	}
	client := fake.NewFakeClientWithScheme(testScheme, objs...)
	engine := &AlluxioEngine{Client: client, Log: fake.NullLogger()}
	ctx := cruntime.ReconcileRequestContext{Client: client, Log: fake.NullLogger()}

	_, err := engine.generateDataLoadValueFile(ctx, verifyDataLoad())
	if err == nil {
		t.Errorf("expected error when the AlluxioRuntime is missing, got nil (pre-PR behavior)")
	} else if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected a not-found error, got: %v", err)
	}
}

func verifyBackupEngine(t *testing.T, runtimeSecrets []corev1.LocalObjectReference) (*AlluxioEngine, cruntime.ReconcileRequestContext) {
	t.Helper()
	runtimeObj := &datav1alpha1.AlluxioRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: verifyDatasetName, Namespace: verifyDatasetNamespace},
		Spec:       datav1alpha1.AlluxioRuntimeSpec{Replicas: 1, ImagePullSecrets: runtimeSecrets},
	}
	dataset := &datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: verifyDatasetName, Namespace: verifyDatasetNamespace, UID: "verify-uid"},
	}
	masterPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: verifyDatasetName + "-master-0", Namespace: verifyDatasetNamespace},
		Spec: corev1.PodSpec{
			NodeName: "verify-node",
			Containers: []corev1.Container{{
				Name:  "alluxio-master",
				Ports: []corev1.ContainerPort{{Name: "rpc", ContainerPort: 19998}},
			}},
		},
		Status: corev1.PodStatus{PodIP: "10.0.0.1"},
	}

	client := fake.NewFakeClientWithScheme(testScheme, runtimeObj, dataset, masterPod)
	runtimeInfo, err := base.BuildRuntimeInfo(verifyDatasetName, verifyDatasetNamespace, "alluxio")
	if err != nil {
		t.Fatalf("failed to build runtime info: %v", err)
	}
	engine := &AlluxioEngine{
		Client:      client,
		name:        verifyDatasetName,
		namespace:   verifyDatasetNamespace,
		runtime:     runtimeObj,
		runtimeInfo: runtimeInfo,
		Log:         fake.NullLogger(),
	}
	ctx := cruntime.ReconcileRequestContext{Log: fake.NullLogger()}
	return engine, ctx
}

func genDataBackupValueFile(t *testing.T, engine *AlluxioEngine, ctx cruntime.ReconcileRequestContext) *cdatabackup.DataBackupValue {
	t.Helper()
	dataBackup := &datav1alpha1.DataBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "verify-backup", Namespace: verifyDatasetNamespace},
		Spec: datav1alpha1.DataBackupSpec{
			Dataset:    verifyDatasetName,
			BackupPath: "pvc://backup-pvc/data",
			RunAs:      &datav1alpha1.User{UID: ptr.To(int64(0)), GID: ptr.To(int64(0))},
		},
	}

	valueFileName, err := engine.generateDataBackupValueFile(ctx, dataBackup)
	if err != nil {
		t.Fatalf("generateDataBackupValueFile returned error: %v", err)
	}
	defer os.Remove(valueFileName)

	data, err := os.ReadFile(valueFileName)
	if err != nil {
		t.Fatalf("failed to read generated value file %s: %v", valueFileName, err)
	}
	value := &cdatabackup.DataBackupValue{}
	if err := goyaml.Unmarshal(data, value); err != nil {
		t.Fatalf("failed to unmarshal generated value file: %v\ncontent:\n%s", err, string(data))
	}
	return value
}

// Contract: DataBackup value file must carry the runtime's spec.imagePullSecrets.
// Fails on base (base only reads the env), passes on the PR head.
func TestVerifyDataBackupUsesRuntimeImagePullSecrets(t *testing.T) {
	os.Unsetenv(common.EnvImagePullSecretsKey)

	engine, ctx := verifyBackupEngine(t, []corev1.LocalObjectReference{{Name: verifyRuntimeSecret}})
	value := genDataBackupValueFile(t, engine, ctx)

	got := value.DataBackup.ImagePullSecrets
	if len(got) != 1 || got[0].Name != verifyRuntimeSecret {
		t.Errorf("expected databackup imagePullSecrets=[%s], got %v", verifyRuntimeSecret, got)
	}
}

// Contract (regression guard): with no runtime secrets, the env default still applies
// for DataBackup. Must pass on BOTH base and head.
func TestVerifyDataBackupFallsBackToEnvImagePullSecrets(t *testing.T) {
	t.Setenv(common.EnvImagePullSecretsKey, verifyEnvSecret)

	engine, ctx := verifyBackupEngine(t, nil)
	value := genDataBackupValueFile(t, engine, ctx)

	got := value.DataBackup.ImagePullSecrets
	if len(got) != 1 || got[0].Name != verifyEnvSecret {
		t.Errorf("expected databackup imagePullSecrets=[%s], got %v", verifyEnvSecret, got)
	}
}
