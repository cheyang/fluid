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

package engine

// Verification harness for PR #6242 (reviewer: codex). Additive only; production code untouched.
// Head-only: these tests exercise symbols introduced by the PR (getBoundRuntimeClassReadOnly),
// so this file does not compile against the base branch and is not part of the P0 base run.
//
// TestVerifyPR6242DataLoadWaitsForSnapshot is a CONTRACT test: a DataLoad whose runtime has no
// snapshot yet must fail with a "not bound" error (the dataset controller retries it) instead of
// falling back to the live class, and must not create a snapshot itself.

import (
	"context"
	"strings"
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	cruntime "github.com/fluid-cloudnative/fluid/pkg/runtime"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestVerifyPR6242DataLoadWaitsForSnapshot(t *testing.T) {
	const (
		runtimeName = "test-runtime"
		namespace   = "default"
	)
	dataset := &datav1alpha1.Dataset{ObjectMeta: metav1.ObjectMeta{Name: runtimeName, Namespace: namespace, UID: "dataset-uid"}}
	runtimeObj := &datav1alpha1.CacheRuntime{
		TypeMeta:   metav1.TypeMeta{APIVersion: datav1alpha1.GroupVersion.String(), Kind: datav1alpha1.CacheRuntimeKind},
		ObjectMeta: metav1.ObjectMeta{Name: runtimeName, Namespace: namespace, UID: "runtime-uid"},
		Spec: datav1alpha1.CacheRuntimeSpec{
			RuntimeClassName: "test-class",
			Worker:           datav1alpha1.CacheRuntimeWorkerSpec{Replicas: 2},
		},
	}
	runtimeClass := &datav1alpha1.CacheRuntimeClass{
		ObjectMeta:     metav1.ObjectMeta{Name: "test-class"},
		FileSystemType: "test-fs",
		Topology: &datav1alpha1.RuntimeTopology{
			Worker: verifyPR6242Component("worker", "test-worker:v1", corev1.ResourceRequirements{}),
		},
	}
	dataload := &datav1alpha1.DataLoad{
		ObjectMeta: metav1.ObjectMeta{Name: "test-dataload", Namespace: namespace},
		Spec:       datav1alpha1.DataLoadSpec{Dataset: datav1alpha1.TargetDataset{Name: runtimeName, Namespace: namespace}},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(CacheEngineTestScheme).
		WithObjects(dataset, runtimeObj, runtimeClass, dataload).
		WithStatusSubresource(dataset, runtimeObj).
		Build()
	engine := &CacheEngine{
		name:      runtimeName,
		namespace: namespace,
		Client:    fakeClient,
		Log:       ctrl.Log.WithName("verify-pr6242"),
		Recorder:  record.NewFakeRecorder(20),
	}
	ctx := cruntime.ReconcileRequestContext{
		Context:        context.Background(),
		Client:         fakeClient,
		Log:            ctrl.Log.WithName("verify-pr6242"),
		NamespacedName: types.NamespacedName{Name: runtimeName, Namespace: namespace},
		Recorder:       record.NewFakeRecorder(20),
	}

	_, err := engine.generateDataLoadValueFile(ctx, dataload)
	if err == nil {
		t.Fatal("expected a not-bound error for a runtime without a snapshot, got nil")
	}
	if !strings.Contains(err.Error(), "not bound") {
		t.Fatalf("expected a not-bound error, got: %v", err)
	}

	// The read-only path must not bind the runtime itself.
	snapshot := &corev1.ConfigMap{}
	getErr := fakeClient.Get(context.Background(),
		types.NamespacedName{Name: common.GetCacheRuntimeClassSnapshotConfigMapName(runtimeName), Namespace: namespace}, snapshot)
	if getErr == nil {
		t.Fatal("the read-only data operation path created a snapshot; only the runtime controller may bind")
	}
}
