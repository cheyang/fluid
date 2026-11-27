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

// Round-8 integration layer (L2) for PR #6219: the full engine.Sync drift
// lifecycle against a REAL API server (envtest with the shipped CRDs).
//
// What this layer adds over the L1 fake-client contracts:
//  1. CRD acceptance: the new TieredStoreSynced condition type is written to
//     status.conditions through the shipped CacheRuntime CRD schema (a schema
//     enum on condition type would reject it — proven accepted here).
//  2. Persistence + restart-proof dedup with real storage: the drift
//     transition emits exactly one RuntimeTieredStoreConflict Warning, further
//     reconciles emit none, and a FRESH engine instance (controller restart)
//     still emits none — dedup comes from the persisted condition.
//  3. Revert recovery through the same path: condition returns to True with no
//     additional tieredStore events.
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
	"github.com/fluid-cloudnative/fluid/pkg/utils"
	appsv1 "k8s.io/api/apps/v1"
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

func verifyPR6219R8EnvtestAssets(t *testing.T) (string, bool) {
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
	dir := matches[len(matches)-1]
	if _, err := os.Stat(filepath.Join(dir, "kube-apiserver")); err != nil {
		return "", false
	}
	return dir, true
}

func TestVerifyPR6219R8Envtest_ConditionLifecycleAcrossRestart(t *testing.T) {
	assets, ok := verifyPR6219R8EnvtestAssets(t)
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
		datav1alpha1.AddToScheme, corev1.AddToScheme, appsv1.AddToScheme,
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

	// The production controller reads through the manager's CACHED client;
	// wire the harness the same way (owner references need TypeMeta, which only
	// cached reads preserve).
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
	deadline := time.Now().Add(90 * time.Second)
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

	if err := writeClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: verifyPR6219R8Namespace}}); err != nil && !apierrors.IsAlreadyExists(err) {
		t.Fatalf("create namespace: %v", err)
	}

	// Fixture mirrors the L1 steady state: worker created through the real
	// creation path so the ASTS carries exactly the generated volumes.
	dataset := &datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: verifyPR6219R8Name, Namespace: verifyPR6219R8Namespace},
		Spec:       datav1alpha1.DatasetSpec{},
	}
	rt := &datav1alpha1.CacheRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: verifyPR6219R8Name, Namespace: verifyPR6219R8Namespace},
		Spec: datav1alpha1.CacheRuntimeSpec{
			RuntimeClassName: "verify-r8-class",
			Master:           datav1alpha1.CacheRuntimeMasterSpec{Replicas: 1},
			Worker: datav1alpha1.CacheRuntimeWorkerSpec{
				Replicas: 1,
				TieredStore: datav1alpha1.RuntimeTieredStore{Levels: []datav1alpha1.RuntimeTieredStoreLevel{
					{ProcessMemory: &datav1alpha1.ProcessMemoryMediumSource{Quota: resource.MustParse("8Gi")}},
				}},
			},
			Client: datav1alpha1.CacheRuntimeClientSpec{},
		},
	}
	class := &datav1alpha1.CacheRuntimeClass{
		ObjectMeta:     metav1.ObjectMeta{Name: "verify-r8-class"},
		FileSystemType: "verifyfs",
		Topology: &datav1alpha1.RuntimeTopology{
			Master: &datav1alpha1.RuntimeComponentDefinition{
				Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "master", Image: "verify-master:latest"}},
				}},
			},
			Worker: &datav1alpha1.RuntimeComponentDefinition{
				Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "worker", Image: "verify-worker:latest"}},
				}},
			},
			Client: &datav1alpha1.RuntimeComponentDefinition{
				Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "client", Image: "verify-client:latest"}},
				}},
			},
		},
	}
	for _, obj := range []client.Object{dataset, rt, class} {
		if err := writeClient.Create(ctx, obj); err != nil {
			t.Fatalf("create %T: %v", obj, err)
		}
	}

	recorder := record.NewFakeRecorder(100)
	newEngine := func() *CacheEngine {
		return &CacheEngine{
			name:      verifyPR6219R8Name,
			namespace: verifyPR6219R8Namespace,
			Client:    c,
			Log:       ctrl.Log.WithName("verify-pr6219-r8-envtest"),
			Recorder:  recorder,
		}
	}
	reqCtx := cruntime.ReconcileRequestContext{
		Client:         c,
		Context:        ctx,
		Log:            ctrl.Log.WithName("verify-pr6219-r8-envtest"),
		RuntimeType:    "cache",
		NamespacedName: types.NamespacedName{Name: verifyPR6219R8Name, Namespace: verifyPR6219R8Namespace},
	}

	// Create the workloads directly, deriving the worker pod template from the
	// REAL transform output so the ASTS carries exactly the volumes the creation
	// path would generate. (Setup*Component gates on Status.*.Phase == None and
	// is not usable against a runtime that has not gone through Setup.)
	setupOK := false
	for time.Now().Before(deadline) {
		cachedRt := &datav1alpha1.CacheRuntime{}
		if err := c.Get(ctx, types.NamespacedName{Name: verifyPR6219R8Name, Namespace: verifyPR6219R8Namespace}, cachedRt); err != nil {
			time.Sleep(300 * time.Millisecond)
			continue
		}
		value, err := newEngine().transform(dataset, cachedRt, class)
		if err != nil {
			time.Sleep(300 * time.Millisecond)
			continue
		}
		replicas := int32(1)
		mkASTS := func(suffix string, tmpl corev1.PodTemplateSpec) *workloadv1alpha1.AdvancedStatefulSet {
			labels := map[string]string{"app": verifyPR6219R8Name + "-" + suffix}
			tmpl.Labels = labels
			return &workloadv1alpha1.AdvancedStatefulSet{
				ObjectMeta: metav1.ObjectMeta{Name: verifyPR6219R8Name + "-" + suffix, Namespace: verifyPR6219R8Namespace},
				Spec: workloadv1alpha1.AdvancedStatefulSetSpec{
					Replicas: &replicas,
					Selector: &metav1.LabelSelector{MatchLabels: labels},
					Template: tmpl,
				},
			}
		}
		masterTmpl := class.Topology.Master.Template
		workerTmpl := value.Worker.PodTemplateSpec
		clientTmpl := class.Topology.Client.Template
		clientLabels := map[string]string{"app": verifyPR6219R8Name + "-client"}
		clientTmpl.Labels = clientLabels
		clientDs := &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Name: verifyPR6219R8Name + "-client", Namespace: verifyPR6219R8Namespace},
			Spec: appsv1.DaemonSetSpec{
				Selector: &metav1.LabelSelector{MatchLabels: clientLabels},
				Template: clientTmpl,
			},
		}
		for _, obj := range []client.Object{mkASTS("master", masterTmpl), mkASTS("worker", workerTmpl), clientDs} {
			if err := writeClient.Create(ctx, obj); err != nil && !apierrors.IsAlreadyExists(err) {
				t.Fatalf("create %T: %v", obj, err)
			}
		}
		setupOK = true
		break
	}
	if !setupOK {
		t.Fatalf("setup path never succeeded")
	}

	// Mark workloads ready (status subresource on the real API server).
	for _, name := range []string{verifyPR6219R8Name + "-master", verifyPR6219R8Name + "-worker"} {
		sts := &workloadv1alpha1.AdvancedStatefulSet{}
		if err := writeClient.Get(ctx, types.NamespacedName{Name: name, Namespace: verifyPR6219R8Namespace}, sts); err != nil {
			t.Fatalf("get %s: %v", name, err)
		}
		sts.Status.ReadyReplicas = 1
		sts.Status.CurrentReplicas = 1
		sts.Status.AvailableReplicas = 1
		if err := writeClient.Status().Update(ctx, sts); err != nil {
			t.Fatalf("ready %s: %v", name, err)
		}
	}

	readCond := func() *datav1alpha1.RuntimeCondition {
		latest := &datav1alpha1.CacheRuntime{}
		if err := writeClient.Get(ctx, types.NamespacedName{Name: verifyPR6219R8Name, Namespace: verifyPR6219R8Namespace}, latest); err != nil {
			return nil
		}
		_, cond := utils.GetRuntimeCondition(latest.Status.Conditions, datav1alpha1.RuntimeTieredStoreSynced)
		return cond
	}
	drainTieredStoreEvents := func() []string {
		var out []string
		for {
			select {
			case ev := <-recorder.Events:
				if strings.Contains(ev, "RuntimeTieredStoreConflict") || strings.Contains(ev, "RuntimeTieredStoreInvalid") {
					out = append(out, ev)
				}
			default:
				return out
			}
		}
	}
	syncUntil := func(want func(*datav1alpha1.RuntimeCondition) bool, what string) {
		t.Helper()
		local := time.Now().Add(90 * time.Second)
		for time.Now().Before(local) {
			_ = newEngine().Sync(reqCtx) // tolerate cache-lag errors while polling
			if cond := readCond(); cond != nil && want(cond) {
				return
			}
			time.Sleep(500 * time.Millisecond)
		}
		cond := readCond()
		t.Fatalf("condition never converged to %s; last=%+v", what, cond)
	}

	// 1. Steady state: condition True written through the shipped CRD schema
	// (acceptance proof for the new condition type), zero tieredStore events.
	syncUntil(func(cond *datav1alpha1.RuntimeCondition) bool {
		return cond.Status == corev1.ConditionTrue && cond.Reason == datav1alpha1.RuntimeTieredStoreSyncedReason
	}, "TieredStoreSynced=True at steady state")
	if evs := drainTieredStoreEvents(); len(evs) != 0 {
		t.Fatalf("FALSE POSITIVE at steady state after real API round-trip: %v", evs)
	}

	// 2. Drift edit (8Gi -> 16Gi) through the real API server: exactly one
	// RuntimeTieredStoreConflict Warning at the transition.
	editOK := false
	for time.Now().Before(deadline) {
		latest := &datav1alpha1.CacheRuntime{}
		if err := writeClient.Get(ctx, types.NamespacedName{Name: verifyPR6219R8Name, Namespace: verifyPR6219R8Namespace}, latest); err != nil {
			time.Sleep(300 * time.Millisecond)
			continue
		}
		latest.Spec.Worker.TieredStore.Levels[0].ProcessMemory.Quota = resource.MustParse("16Gi")
		if err := writeClient.Update(ctx, latest); err == nil {
			editOK = true
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if !editOK {
		t.Fatalf("failed to apply the in-place quota edit")
	}
	syncUntil(func(cond *datav1alpha1.RuntimeCondition) bool {
		return cond.Status == corev1.ConditionFalse && cond.Reason == common.RuntimeTieredStoreConflict
	}, "TieredStoreSynced=False/RuntimeTieredStoreConflict after edit")
	evs := drainTieredStoreEvents()
	if len(evs) != 1 {
		t.Fatalf("expected exactly 1 transition event after drift, got %d: %v", len(evs), evs)
	}
	if !strings.Contains(evs[0], "tiered-store-level-0-memory") || !strings.Contains(evs[0], "Delete and recreate the CacheRuntime") {
		t.Errorf("event missing volume name or recreate guidance: %s", evs[0])
	}

	// 3. Dedup with real persistence: more reconciles emit nothing, and a
	// restart-shaped fresh engine still emits nothing (no in-memory state).
	for i := 0; i < 3; i++ {
		if err := newEngine().Sync(reqCtx); err != nil {
			t.Fatalf("re-sync %d: %v", i, err)
		}
	}
	if evs := drainTieredStoreEvents(); len(evs) != 0 {
		t.Fatalf("persistent drift re-emitted %d events against envtest", len(evs))
	}
	if cond := readCond(); cond == nil || cond.Status != corev1.ConditionFalse {
		t.Fatalf("persisted drift condition lost: %+v", cond)
	}

	// 4. Revert recovers to True with no further tieredStore events.
	for time.Now().Before(deadline) {
		latest := &datav1alpha1.CacheRuntime{}
		if err := writeClient.Get(ctx, types.NamespacedName{Name: verifyPR6219R8Name, Namespace: verifyPR6219R8Namespace}, latest); err != nil {
			time.Sleep(300 * time.Millisecond)
			continue
		}
		latest.Spec.Worker.TieredStore.Levels[0].ProcessMemory.Quota = resource.MustParse("8Gi")
		if err := writeClient.Update(ctx, latest); err == nil {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	syncUntil(func(cond *datav1alpha1.RuntimeCondition) bool {
		return cond.Status == corev1.ConditionTrue
	}, "TieredStoreSynced=True after revert")
	if evs := drainTieredStoreEvents(); len(evs) != 0 {
		t.Fatalf("revert emitted %d tieredStore events (want 0)", len(evs))
	}
}
