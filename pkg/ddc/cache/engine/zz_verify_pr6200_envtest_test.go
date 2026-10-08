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

// Verification harness for PR #6200 (reviewer artifact; additive, test-only).
//
// Integration layer (L2): run CheckAndUpdateRuntimeStatus against a REAL API
// server (envtest) with the shipped CacheRuntime + AdvancedStatefulSet CRDs.
// Proves end-to-end at API level that:
//   1. exactly 1 worker workload Get happens per status cycle;
//   2. status.CacheAffinity derived from the worker pod template survives the
//      status-subresource round trip through the real API server;
//   3. an out-of-band nodeSelector change is reflected on the very next cycle
//      (the PR's "zero staleness" claim).
//
// Polarity: CONTRACT. Skips when KUBEBUILDER_ASSETS is unset.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	workloadv1alpha1 "github.com/fluid-cloudnative/advanced-statefulset/api/workload/v1alpha1"
	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
)

func TestVerifyWorkerAffinitySingleReadEnvtest(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS not set; skipping envtest integration layer")
	}

	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{
		datav1alpha1.AddToScheme,
		workloadv1alpha1.AddToScheme,
		corev1.AddToScheme,
	} {
		if err := add(scheme); err != nil {
			t.Fatalf("add scheme: %v", err)
		}
	}

	repoRoot := filepath.Join("..", "..", "..", "..")
	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join(repoRoot, "config", "crd", "bases"),
			filepath.Join(repoRoot, "charts", "fluid", "fluid", "crds"),
		},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := testEnv.Start()
	if err != nil {
		t.Fatalf("start envtest: %v", err)
	}
	defer func() {
		if err := testEnv.Stop(); err != nil {
			t.Logf("stop envtest: %v", err)
		}
	}()

	realClient, err := ctrlclient.New(cfg, ctrlclient.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	ctx := context.TODO()

	if err := realClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testStatusNamespace}}); err != nil && !apierrors.IsAlreadyExists(err) {
		t.Fatalf("create namespace: %v", err)
	}

	cr := &datav1alpha1.CacheRuntime{
		ObjectMeta: metav1.ObjectMeta{
			Name:              testStatusRuntime,
			Namespace:         testStatusNamespace,
			CreationTimestamp: metav1.NewTime(time.Now().Add(-time.Minute)),
		},
	}
	if err := realClient.Create(ctx, cr); err != nil {
		t.Fatalf("create CacheRuntime: %v", err)
	}

	replicas := int32(1)
	mkASTS := func(name string, nodeSelector map[string]string) *workloadv1alpha1.AdvancedStatefulSet {
		labels := map[string]string{"app": name}
		return &workloadv1alpha1.AdvancedStatefulSet{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testStatusNamespace},
			Spec: workloadv1alpha1.AdvancedStatefulSetSpec{
				Replicas: &replicas,
				Selector: &metav1.LabelSelector{MatchLabels: labels},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: labels},
					Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "c", Image: "busybox"}}, NodeSelector: nodeSelector},
				},
			},
		}
	}
	if err := realClient.Create(ctx, mkASTS(testStatusMaster, nil)); err != nil {
		t.Fatalf("create master asts: %v", err)
	}
	worker := mkASTS(testStatusWorker, map[string]string{"disktype": "ssd"})
	if err := realClient.Create(ctx, worker); err != nil {
		t.Fatalf("create worker asts: %v", err)
	}
	// envtest has no kubelet/controllers: seed the workload .status by hand.
	for _, name := range []string{testStatusMaster, testStatusWorker} {
		asts := &workloadv1alpha1.AdvancedStatefulSet{}
		if err := realClient.Get(ctx, types.NamespacedName{Name: name, Namespace: testStatusNamespace}, asts); err != nil {
			t.Fatalf("get asts %s: %v", name, err)
		}
		asts.Status.ReadyReplicas = 1
		asts.Status.CurrentReplicas = 1
		asts.Status.AvailableReplicas = 1
		if err := realClient.Status().Update(ctx, asts); err != nil {
			t.Fatalf("seed asts %s status: %v", name, err)
		}
	}

	counting := &verifyWorkerGetCountingClient{Client: realClient}
	engine := &CacheEngine{
		Client:    counting,
		name:      testStatusRuntime,
		namespace: testStatusNamespace,
		Log:       fake.NullLogger(),
	}

	getCacheAffinity := func() *corev1.NodeAffinity {
		got := &datav1alpha1.CacheRuntime{}
		if err := realClient.Get(ctx, types.NamespacedName{Name: testStatusRuntime, Namespace: testStatusNamespace}, got); err != nil {
			t.Fatalf("re-get CacheRuntime: %v", err)
		}
		return got.Status.CacheAffinity
	}

	// Cycle 1
	ready, err := engine.CheckAndUpdateRuntimeStatus(newStatusTestRuntimeValue(false))
	if err != nil {
		t.Fatalf("cycle 1: %v", err)
	}
	if !ready {
		t.Fatalf("cycle 1: expected runtime ready")
	}
	if counting.workerGets != 1 {
		t.Fatalf("cycle 1: expected exactly 1 worker workload Get, observed %d", counting.workerGets)
	}
	affinity := getCacheAffinity()
	if affinity == nil || affinity.RequiredDuringSchedulingIgnoredDuringExecution == nil ||
		len(affinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms) != 1 {
		t.Fatalf("cycle 1: CacheAffinity did not round-trip through the API server: %+v", affinity)
	}
	exprs := affinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions
	found := false
	for _, e := range exprs {
		if e.Key == "disktype" && len(e.Values) == 1 && e.Values[0] == "ssd" {
			found = true
		}
	}
	if !found {
		t.Fatalf("cycle 1: expected disktype=ssd in CacheAffinity, got %+v", exprs)
	}

	// Out-of-band nodeSelector update, then cycle 2 must reflect it immediately.
	if err := realClient.Get(ctx, types.NamespacedName{Name: testStatusWorker, Namespace: testStatusNamespace}, worker); err != nil {
		t.Fatalf("re-get worker: %v", err)
	}
	worker.Spec.Template.Spec.NodeSelector = map[string]string{"disktype": "nvme"}
	if err := realClient.Update(ctx, worker); err != nil {
		t.Fatalf("out-of-band worker update: %v", err)
	}

	ready, err = engine.CheckAndUpdateRuntimeStatus(newStatusTestRuntimeValue(false))
	if err != nil {
		t.Fatalf("cycle 2: %v", err)
	}
	if !ready {
		t.Fatalf("cycle 2: expected runtime ready")
	}
	if counting.workerGets != 2 {
		t.Fatalf("cycle 2: expected 1 worker workload Get per cycle (total 2), observed %d", counting.workerGets)
	}
	affinity = getCacheAffinity()
	exprs = affinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions
	foundNVME, foundSSD := false, false
	for _, e := range exprs {
		if e.Key == "disktype" && len(e.Values) == 1 && e.Values[0] == "nvme" {
			foundNVME = true
		}
		if e.Key == "disktype" && len(e.Values) == 1 && e.Values[0] == "ssd" {
			foundSSD = true
		}
	}
	if !foundNVME || foundSSD {
		t.Fatalf("cycle 2: expected zero-staleness CacheAffinity disktype=nvme only, got %+v", exprs)
	}
	t.Logf("envtest: 1 worker Get/cycle, CacheAffinity round-trips via status subresource, out-of-band nodeSelector reflected next cycle")
}
