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

// Integration layer (L2) for PR #6200: run CheckAndUpdateRuntimeStatus against
// a REAL API server (envtest) with the shipped CRDs installed, and prove that
//   (a) the worker AdvancedStatefulSet is read exactly once per status cycle,
//   (b) status.CacheAffinity survives a real API-server round trip (the CRD
//       structural schema does not prune it), and
//   (c) an out-of-band nodeSelector change is reflected on the next cycle.
// Polarity: contract. Skipped when KUBEBUILDER_ASSETS is unset.
//
// The file only uses symbols that also exist on the merge-base, so it can be
// grafted onto either side (on base, expectation (a) fails with 2 reads —
// that is the L2-side premise reproduction).

package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func zzRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}

func TestVerifyPR6200EnvtestCacheAffinityRoundTrip(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS not set; skipping envtest layer")
	}
	root := zzRepoRoot(t)
	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join(root, "config", "crd", "bases"),
			filepath.Join(root, "charts", "fluid", "fluid", "crds"),
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

	scheme := zzVerifyScheme(t)
	realClient, err := ctrlclient.New(cfg, ctrlclient.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := realClient.Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: zzVerifyNS},
	}); err != nil {
		t.Fatalf("create namespace: %v", err)
	}

	rt := zzVerifyRuntimeObj()
	rt.Spec = datav1alpha1.CacheRuntimeSpec{RuntimeClassName: "zz-verify-class"}
	if err := realClient.Create(ctx, rt); err != nil {
		t.Fatalf("create CacheRuntime: %v", err)
	}

	master := zzVerifyASTS(zzVerifyMaster, nil)
	master.Spec.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{"app": "zz-verify-master"}}
	master.Spec.Template.Labels = map[string]string{"app": "zz-verify-master"}
	if err := realClient.Create(ctx, master); err != nil {
		t.Fatalf("create master ASTS: %v", err)
	}

	worker := zzVerifyASTS(zzVerifyWorker, map[string]string{"disktype": "ssd"})
	worker.Spec.Template.Spec.Affinity = &corev1.Affinity{
		NodeAffinity: &corev1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{
					{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{Key: "kubernetes.io/os", Operator: corev1.NodeSelectorOpIn, Values: []string{"linux"}},
						},
					},
				},
			},
		},
	}
	worker.Spec.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{"app": "zz-verify-worker"}}
	worker.Spec.Template.Labels = map[string]string{"app": "zz-verify-worker"}
	if err := realClient.Create(ctx, worker); err != nil {
		t.Fatalf("create worker ASTS: %v", err)
	}

	counting := &zzWorkerGetCounter{Client: realClient}
	engine := zzVerifyEngine(counting)

	// Cycle 1 against the real API server.
	if _, err := engine.CheckAndUpdateRuntimeStatus(zzVerifyStatusValue()); err != nil {
		t.Fatalf("cycle 1: %v", err)
	}
	if counting.workerGets != 1 {
		t.Fatalf("cycle 1: worker AdvancedStatefulSet Get calls = %d, want exactly 1 (base reads it twice)", counting.workerGets)
	}

	// (b) CacheAffinity must survive the real round trip incl. the CRD schema.
	stored := &datav1alpha1.CacheRuntime{}
	if err := realClient.Get(ctx, types.NamespacedName{Name: zzVerifyRuntime, Namespace: zzVerifyNS}, stored); err != nil {
		t.Fatalf("get runtime: %v", err)
	}
	aff := stored.Status.CacheAffinity
	if aff == nil || aff.RequiredDuringSchedulingIgnoredDuringExecution == nil ||
		len(aff.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms) != 1 {
		t.Fatalf("status.CacheAffinity did not round-trip through the API server: %+v", aff)
	}
	gotKeys := map[string]bool{}
	for _, e := range aff.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions {
		gotKeys[e.Key] = true
	}
	for _, k := range []string{"kubernetes.io/os", "disktype"} {
		if !gotKeys[k] {
			t.Fatalf("status.CacheAffinity lost key %q through the API-server round trip (keys: %v)", k, gotKeys)
		}
	}
	if stored.Status.Worker.DesiredReplicas != 1 {
		t.Fatalf("status.Worker.DesiredReplicas = %d, want 1", stored.Status.Worker.DesiredReplicas)
	}

	// (c) out-of-band nodeSelector change is reflected on the very next cycle.
	fresh := worker.DeepCopy()
	if err := realClient.Get(ctx, types.NamespacedName{Name: zzVerifyWorker, Namespace: zzVerifyNS}, fresh); err != nil {
		t.Fatalf("get worker: %v", err)
	}
	fresh.Spec.Template.Spec.NodeSelector = map[string]string{"disktype": "nvme"}
	if err := realClient.Update(ctx, fresh); err != nil {
		t.Fatalf("update worker nodeSelector: %v", err)
	}
	if _, err := engine.CheckAndUpdateRuntimeStatus(zzVerifyStatusValue()); err != nil {
		t.Fatalf("cycle 2: %v", err)
	}
	if counting.workerGets != 2 {
		t.Fatalf("cycle 2: cumulative worker Gets = %d, want exactly 2 (1 per cycle)", counting.workerGets)
	}
	stored2 := &datav1alpha1.CacheRuntime{}
	if err := realClient.Get(ctx, types.NamespacedName{Name: zzVerifyRuntime, Namespace: zzVerifyNS}, stored2); err != nil {
		t.Fatalf("get runtime after cycle 2: %v", err)
	}
	found := false
	for _, e := range stored2.Status.CacheAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions {
		if e.Key == "disktype" && len(e.Values) == 1 && e.Values[0] == "nvme" {
			found = true
		}
		if e.Key == "disktype" && len(e.Values) == 1 && e.Values[0] == "ssd" {
			t.Fatalf("stale affinity persisted into cycle 2: still has disktype=ssd")
		}
	}
	if !found {
		t.Fatalf("cycle 2 did not pick up out-of-band nodeSelector change (disktype=nvme)")
	}
}
