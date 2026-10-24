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

// Integration layer (L2) for the PR #6219 harness: runs the drift check against
// a real API server (envtest) instead of the fake client.
//
// What this layer adds over L1:
//   1. False-positive guard: quantities and tieredStore levels round-trip
//      through real CRD schema validation and etcd storage. A steady-state
//      runtime must produce NO RuntimeTieredStoreConflict event after the
//      round-trip (e.g. "8Gi" stored and re-served must Cmp-equal the spec).
//   2. The full split scenario against real persistence: edit the CR, sync,
//      observe the warning event and the ConfigMap content served by the API
//      server.
//
// Skipped unless envtest binaries are available (KUBEBUILDER_ASSETS or the
// setup-envtest default cache).

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	workloadv1alpha1 "github.com/fluid-cloudnative/advanced-statefulset/api/workload/v1alpha1"
	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	cruntime "github.com/fluid-cloudnative/fluid/pkg/runtime"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func verifyPR6219EnvtestAssets(t *testing.T) (string, bool) {
	t.Helper()
	if dir := os.Getenv("KUBEBUILDER_ASSETS"); dir != "" {
		return dir, true
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}
	matches, _ := filepath.Glob(filepath.Join(home, ".local", "share", "kubebuilder-envtest", "k8s", "*"))
	if len(matches) == 0 {
		return "", false
	}
	// pick the lexicographically last (newest) version dir
	dir := matches[len(matches)-1]
	if _, err := os.Stat(filepath.Join(dir, "kube-apiserver")); err != nil {
		return "", false
	}
	return dir, true
}

func TestVerifyPR6219Envtest_DriftRoundTrip(t *testing.T) {
	assets, ok := verifyPR6219EnvtestAssets(t)
	if !ok {
		t.Skip("envtest binaries not available; set KUBEBUILDER_ASSETS")
	}
	os.Setenv("KUBEBUILDER_ASSETS", assets)

	// The AdvancedStatefulSet CRD lives under charts/, separate from
	// config/crd/bases; stage it alone in a temp dir to avoid duplicate CRDs.
	tmp := t.TempDir()
	crdSrc := filepath.Join("..", "..", "..", "..", "charts", "fluid", "fluid", "crds", "workload.fluid.io_advancedstatefulsets.yaml")
	crdBytes, err := os.ReadFile(crdSrc)
	if err != nil {
		t.Fatalf("read ASTS CRD: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "workload.fluid.io_advancedstatefulsets.yaml"), crdBytes, 0644); err != nil {
		t.Fatalf("stage ASTS CRD: %v", err)
	}

	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{
		datav1alpha1.AddToScheme, corev1.AddToScheme,
		workloadv1alpha1.AddToScheme, apiextensionsv1.AddToScheme,
	} {
		if err := add(scheme); err != nil {
			t.Fatalf("scheme: %v", err)
		}
	}

	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "..", "config", "crd", "bases"),
			tmp,
		},
		Scheme: scheme,
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

	// The production controller reads through the manager's CACHED client, and
	// cached reads preserve the object's TypeMeta (the delegating client only
	// resets GVK on the uncached path). Using a cached client here matters:
	// syncRuntimeValueConfigMap builds the ConfigMap owner reference from
	// runtime.APIVersion/Kind, and a direct (uncached) read returns them empty,
	// which a real API server rejects. Wire the harness like production.
	writeClient, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	cl, err := cluster.New(cfg, func(o *cluster.Options) { o.Scheme = scheme })
	if err != nil {
		t.Fatalf("cluster: %v", err)
	}
	clusterCtx, stopCluster := context.WithCancel(context.Background())
	defer stopCluster()
	go func() {
		if err := cl.Start(clusterCtx); err != nil {
			t.Logf("cluster stopped: %v", err)
		}
	}()
	if !cl.GetCache().WaitForCacheSync(clusterCtx) {
		t.Fatal("cache sync")
	}
	c := cl.GetClient()
	ctx := context.Background()

	// CRDs need a moment to become established; poll until reads succeed.
	deadline := time.Now().Add(60 * time.Second)
	for {
		var list datav1alpha1.CacheRuntimeList
		if err := c.List(ctx, &list); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("CacheRuntime CRD never became ready")
		}
		time.Sleep(500 * time.Millisecond)
	}

	if err := writeClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: verifyPR6219Namespace}}); err != nil && !apierrors.IsAlreadyExists(err) {
		t.Fatalf("create namespace: %v", err)
	}
	dataset := &datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: verifyPR6219RuntimeName, Namespace: verifyPR6219Namespace},
		Spec:       datav1alpha1.DatasetSpec{},
	}
	if err := c.Create(ctx, dataset); err != nil {
		t.Fatalf("create dataset: %v", err)
	}
	if err := c.Create(ctx, verifyPR6219RuntimeClass()); err != nil {
		t.Fatalf("create runtimeclass: %v", err)
	}
	if err := c.Create(ctx, verifyPR6219Runtime()); err != nil {
		t.Fatalf("create cacheruntime: %v", err)
	}
	if err := c.Create(ctx, verifyPR6219MasterASTS()); err != nil {
		t.Fatalf("create master asts: %v", err)
	}
	if err := c.Create(ctx, verifyPR6219WorkerASTS("8Gi")); err != nil {
		t.Fatalf("create worker asts: %v", err)
	}

	recorder := record.NewFakeRecorder(100)
	e := &CacheEngine{
		name:      verifyPR6219RuntimeName,
		namespace: verifyPR6219Namespace,
		Client:    c,
		Log:       ctrl.Log.WithName("verify-pr6219-envtest"),
		Recorder:  recorder,
	}
	reqCtx := cruntime.ReconcileRequestContext{
		Client:         c,
		Context:        ctx,
		Log:            ctrl.Log.WithName("verify-pr6219-envtest"),
		RuntimeType:    "cache",
		NamespacedName: types.NamespacedName{Name: verifyPR6219RuntimeName, Namespace: verifyPR6219Namespace},
	}

	// Reads for assertions go through the write client (always fresh); the
	// engine itself reads through the cache, exactly like production.
	readEngine := &CacheEngine{name: verifyPR6219RuntimeName, namespace: verifyPR6219Namespace, Client: writeClient, Log: e.Log}

	// Steady state after a REAL API-server round-trip, allowing for cache lag:
	// sync must eventually succeed, and no drift warning may EVER appear while
	// spec and workload agree (false-positive guard).
	syncOK := false
	deadline = time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if err := verifyPR6219TrySync(e, reqCtx); err == nil {
			syncOK = true
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !syncOK {
		t.Fatalf("steady-state sync never succeeded against envtest")
	}
	for _, ev := range verifyPR6219DrainEvents(recorder) {
		if strings.Contains(ev, common.RuntimeTieredStoreConflict) {
			t.Fatalf("FALSE POSITIVE: drift warning on steady-state runtime after API round-trip: %s", ev)
		}
	}

	// In-place edit through the real API server (write client, retry on conflict).
	editOK := false
	for time.Now().Before(deadline) {
		rt := &datav1alpha1.CacheRuntime{}
		if err := writeClient.Get(ctx, types.NamespacedName{Name: verifyPR6219RuntimeName, Namespace: verifyPR6219Namespace}, rt); err != nil {
			time.Sleep(300 * time.Millisecond)
			continue
		}
		rt.Spec.Worker.TieredStore.Levels[0].ProcessMemory.Quota = resource.MustParse("16Gi")
		if err := writeClient.Update(ctx, rt); err == nil {
			editOK = true
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if !editOK {
		t.Fatalf("failed to apply the in-place quota edit")
	}

	// The drift warning must appear once the cache catches up.
	found := ""
	for time.Now().Before(deadline) && found == "" {
		_ = verifyPR6219TrySync(e, reqCtx)
		for _, ev := range verifyPR6219DrainEvents(recorder) {
			if strings.Contains(ev, common.RuntimeTieredStoreConflict) {
				found = ev
			}
		}
		if found == "" {
			time.Sleep(500 * time.Millisecond)
		}
	}
	if found == "" {
		t.Fatalf("expected %s warning after in-place edit against envtest", common.RuntimeTieredStoreConflict)
	}
	if !strings.Contains(found, "16Gi") || !strings.Contains(found, "8Gi") {
		t.Errorf("event does not name both quotas: %s", found)
	}

	// The ConfigMap as served by the API server shows the new quota while the
	// ASTS volume still shows the old one: the split, end to end.
	if got := verifyPR6219ConfigMapQuota(t, readEngine, reqCtx); got != "16Gi" {
		t.Errorf("configmap quota = %s, want 16Gi", got)
	}
	if got := verifyPR6219WorkerVolumeSize(t, readEngine, reqCtx); got != "8Gi" {
		t.Errorf("worker volume sizeLimit = %s, want 8Gi", got)
	}
}

// verifyPR6219TrySync is verifyPR6219SyncOnce with errors returned instead of
// fatal, so callers can poll through informer-cache lag.
func verifyPR6219TrySync(e *CacheEngine, ctx cruntime.ReconcileRequestContext) error {
	rt, err := e.getRuntime()
	if err != nil {
		return err
	}
	class, err := e.getRuntimeClass(rt.Spec.RuntimeClassName)
	if err != nil {
		return err
	}
	if err := e.syncRuntimeValueConfigMap(ctx, rt); err != nil {
		return err
	}
	return e.syncRuntimeSpec(ctx, rt, class)
}
