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

// Reviewer-private verification harness for fluid-cloudnative/fluid#6183.
//
// PR #6183 is a docs-only change that promotes `replicas` from the
// "unsupported / requires redeploy" list to a documented in-place-updatable
// field of CacheRuntime (section 3.3 of cacheruntime_spec_update.md). The
// whole PR is correct only if the controller really does sync
// spec.{master,worker}.replicas into the backing AdvancedStatefulSet on every
// reconcile, without a redeploy. This file proves that claim at the cheapest
// layer that decides it (fake client + the real syncRuntimeSpec code path).
//
// POLARITY: every test here is a CONTRACT test asserting the behaviour the docs
// promise. On the code under review they PASS (claim confirmed). If someone
// later removes the `Replicas:` field from ComponentSpec in syncRuntimeSpec,
// these tests go RED -- that is the reproduction of the doc becoming a lie.
//
// This harness is additive: it does not modify any production code.

import (
	"context"
	"testing"

	workloadv1alpha1 "github.com/fluid-cloudnative/advanced-statefulset/api/workload/v1alpha1"
	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/ddc/cache/component"
	cruntime "github.com/fluid-cloudnative/fluid/pkg/runtime"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	cclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// verify6183Fixture builds a CacheEngine + fake client with a Master ASTS at
// masterReplicas and a Worker ASTS at workerReplicas, no resources declared
// anywhere (so the resources sync path is inert and cannot mask the replicas
// signal).
func verify6183Fixture(t *testing.T, masterReplicas, workerReplicas int32) (*CacheEngine, *datav1alpha1.CacheRuntime, *datav1alpha1.CacheRuntimeClass, cruntime.ReconcileRequestContext, cclient.Client) {
	t.Helper()
	scheme := CacheEngineTestScheme

	runtimeObj := &datav1alpha1.CacheRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: "test-runtime", Namespace: "default", UID: "test-runtime-uid"},
		Spec: datav1alpha1.CacheRuntimeSpec{
			RuntimeClassName: "test-class",
			Master:           datav1alpha1.CacheRuntimeMasterSpec{Replicas: masterReplicas},
			Worker:           datav1alpha1.CacheRuntimeWorkerSpec{Replicas: workerReplicas},
		},
	}

	runtimeClass := &datav1alpha1.CacheRuntimeClass{
		ObjectMeta:     metav1.ObjectMeta{Name: "test-class"},
		FileSystemType: "test-fs",
		Topology: &datav1alpha1.RuntimeTopology{
			Master: &datav1alpha1.RuntimeComponentDefinition{
				Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "master", Image: "test-master:latest"}},
				}},
			},
			Worker: &datav1alpha1.RuntimeComponentDefinition{
				Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "worker", Image: "test-worker:latest"}},
				}},
			},
		},
	}

	mr := masterReplicas
	masterSts := &workloadv1alpha1.AdvancedStatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "test-runtime-master", Namespace: "default"},
		Spec: workloadv1alpha1.AdvancedStatefulSetSpec{
			Replicas: &mr,
			Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "master", Image: "test-master:latest"}},
			}},
		},
		Status: workloadv1alpha1.AdvancedStatefulSetStatus{
			ReadyReplicas: masterReplicas, CurrentReplicas: masterReplicas, AvailableReplicas: masterReplicas,
		},
	}
	wr := workerReplicas
	workerSts := &workloadv1alpha1.AdvancedStatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "test-runtime-worker", Namespace: "default"},
		Spec: workloadv1alpha1.AdvancedStatefulSetSpec{
			Replicas: &wr,
			Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "worker", Image: "test-worker:latest"}},
			}},
		},
		Status: workloadv1alpha1.AdvancedStatefulSetStatus{
			ReadyReplicas: workerReplicas, CurrentReplicas: workerReplicas, AvailableReplicas: workerReplicas,
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(runtimeObj, runtimeClass, masterSts, workerSts).
		WithStatusSubresource(runtimeObj).
		Build()

	engine := &CacheEngine{
		name:      "test-runtime",
		namespace: "default",
		Client:    fakeClient,
		Log:       ctrl.Log.WithName("verify-6183"),
	}
	ctx := cruntime.ReconcileRequestContext{
		Client:         fakeClient,
		Context:        context.Background(),
		Log:            ctrl.Log.WithName("verify-6183"),
		RuntimeType:    "cache",
		NamespacedName: types.NamespacedName{Name: "test-runtime", Namespace: "default"},
	}
	return engine, runtimeObj, runtimeClass, ctx, fakeClient
}

func getASTSReplicas(t *testing.T, c cclient.Client, name string) int32 {
	t.Helper()
	asts := &workloadv1alpha1.AdvancedStatefulSet{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: name, Namespace: "default"}, asts); err != nil {
		t.Fatalf("get %s: %v", name, err)
	}
	if asts.Spec.Replicas == nil {
		t.Fatalf("%s: spec.replicas is nil", name)
	}
	return *asts.Spec.Replicas
}

// TestVerify6183_WorkerReplicasSyncedInPlace proves doc section 3.3: patching
// spec.worker.replicas propagates to the Worker AdvancedStatefulSet through the
// reconcile sync path, with no resources declared and no redeploy. CONTRACT.
func TestVerify6183_WorkerReplicasSyncedInPlace(t *testing.T) {
	engine, runtimeObj, runtimeClass, ctx, c := verify6183Fixture(t, 1, 2)

	if got := getASTSReplicas(t, c, "test-runtime-worker"); got != 2 {
		t.Fatalf("precondition: worker ASTS replicas = %d, want 2", got)
	}

	// Simulate `kubectl patch cacheruntime ... {"spec":{"worker":{"replicas":3}}}`
	runtimeObj.Spec.Worker.Replicas = 3
	if err := engine.syncRuntimeSpec(ctx, runtimeObj, runtimeClass); err != nil {
		t.Fatalf("syncRuntimeSpec: %v", err)
	}

	if got := getASTSReplicas(t, c, "test-runtime-worker"); got != 3 {
		t.Fatalf("worker ASTS replicas = %d, want 3 (replicas NOT synced in place -- doc 3.3 would be wrong)", got)
	}
}

// TestVerify6183_MasterReplicasSyncedInPlace proves the doc's summary table
// claim that Master (not just Worker) supports in-place replicas update. CONTRACT.
func TestVerify6183_MasterReplicasSyncedInPlace(t *testing.T) {
	engine, runtimeObj, runtimeClass, ctx, c := verify6183Fixture(t, 1, 2)

	runtimeObj.Spec.Master.Replicas = 3
	if err := engine.syncRuntimeSpec(ctx, runtimeObj, runtimeClass); err != nil {
		t.Fatalf("syncRuntimeSpec: %v", err)
	}

	if got := getASTSReplicas(t, c, "test-runtime-master"); got != 3 {
		t.Fatalf("master ASTS replicas = %d, want 3 (master replicas NOT synced -- doc table would be wrong)", got)
	}
}

// TestVerify6183_ScaleInSynced proves the rollback direction the author says
// they exercised (2 -> 1): scale-in is propagated too, not just scale-out. CONTRACT.
func TestVerify6183_ScaleInSynced(t *testing.T) {
	engine, runtimeObj, runtimeClass, ctx, c := verify6183Fixture(t, 1, 2)

	runtimeObj.Spec.Worker.Replicas = 1
	if err := engine.syncRuntimeSpec(ctx, runtimeObj, runtimeClass); err != nil {
		t.Fatalf("syncRuntimeSpec: %v", err)
	}

	if got := getASTSReplicas(t, c, "test-runtime-worker"); got != 1 {
		t.Fatalf("worker ASTS replicas = %d, want 1 (scale-in NOT synced)", got)
	}
}

// TestVerify6183_ReplicasSyncedWithoutResources proves the distinction the PR
// body draws: Replicas is passed into ComponentSpec as a non-nil pointer on
// every reconcile, UNLIKE resources (which is nil when nothing declares any),
// so replicas is value-driven and always applied. CONTRACT.
func TestVerify6183_ReplicasSyncedWithoutResources(t *testing.T) {
	engine, runtimeObj, runtimeClass, ctx, c := verify6183Fixture(t, 1, 2)

	// No resources anywhere in the fixture. Replicas must still sync.
	runtimeObj.Spec.Worker.Replicas = 5
	if err := engine.syncRuntimeSpec(ctx, runtimeObj, runtimeClass); err != nil {
		t.Fatalf("syncRuntimeSpec: %v", err)
	}
	if got := getASTSReplicas(t, c, "test-runtime-worker"); got != 5 {
		t.Fatalf("worker ASTS replicas = %d, want 5 with no resources declared", got)
	}
}

// TestVerify6183_NoChangeIsIdempotent proves the doc's claim that the sync is
// value-driven rather than a periodic overwrite: reconciling with an unchanged
// replica count leaves the ASTS spec.replicas as-is (no spurious patch). CONTRACT.
func TestVerify6183_NoChangeIsIdempotent(t *testing.T) {
	engine, runtimeObj, runtimeClass, ctx, c := verify6183Fixture(t, 1, 2)

	// Reconcile without changing anything.
	if err := engine.syncRuntimeSpec(ctx, runtimeObj, runtimeClass); err != nil {
		t.Fatalf("syncRuntimeSpec: %v", err)
	}
	if got := getASTSReplicas(t, c, "test-runtime-worker"); got != 2 {
		t.Fatalf("worker ASTS replicas = %d, want unchanged 2", got)
	}
	if got := getASTSReplicas(t, c, "test-runtime-master"); got != 1 {
		t.Fatalf("master ASTS replicas = %d, want unchanged 1", got)
	}
}

// TestVerify6183_ScalingObservableViaStatus disproves the doc's §3.3 claim that
// scaling "can only be observed from the controller logs". After syncRuntimeSpec
// patches the ASTS spec.replicas, the reconcile status path
// (getRuntimeStatusValue -> CheckAndUpdateRuntimeStatus -> setWorkerComponentStatus
// -> ConstructComponentStatus -> Status().Update) writes the new count into
// CacheRuntime.status.worker.desiredReplicas, which is exactly what the sibling
// curvine_cache_runtime.md doc reads with
// `kubectl get cacheruntime ... -o jsonpath='{.status.worker.readyReplicas}/{.status.worker.desiredReplicas}'`.
// CONTRACT: green means the status IS an observability channel; the doc line is wrong.
func TestVerify6183_ScalingObservableViaStatus(t *testing.T) {
	engine, runtimeObj, runtimeClass, ctx, c := verify6183Fixture(t, 1, 2)

	// Scale worker 2 -> 3 and sync it into the ASTS.
	runtimeObj.Spec.Worker.Replicas = 3
	if err := engine.syncRuntimeSpec(ctx, runtimeObj, runtimeClass); err != nil {
		t.Fatalf("syncRuntimeSpec: %v", err)
	}
	if got := getASTSReplicas(t, c, "test-runtime-worker"); got != 3 {
		t.Fatalf("precondition: worker ASTS replicas = %d, want 3", got)
	}

	// Drive the steady-state status write-back that runs on every reconcile.
	statusValue, err := engine.getRuntimeStatusValue(runtimeObj, runtimeClass)
	if err != nil {
		t.Fatalf("getRuntimeStatusValue: %v", err)
	}
	if _, err := engine.CheckAndUpdateRuntimeStatus(statusValue); err != nil {
		t.Fatalf("CheckAndUpdateRuntimeStatus: %v", err)
	}

	// Read the CacheRuntime CR back and inspect .status.worker, the field the
	// Curvine doc uses to observe scaling.
	got := &datav1alpha1.CacheRuntime{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: "test-runtime", Namespace: "default"}, got); err != nil {
		t.Fatalf("get cacheruntime: %v", err)
	}
	if got.Status.Worker.DesiredReplicas != 3 {
		t.Fatalf("status.worker.desiredReplicas = %d, want 3 (scaling NOT observable via status — doc claim would be right)", got.Status.Worker.DesiredReplicas)
	}
}

// TestVerify6183_ConstructComponentStatusReflectsReplicas is the focused unit
// behind the test above: ConstructComponentStatus derives desiredReplicas from
// the ASTS spec.replicas that syncRuntimeSpec just patched. CONTRACT.
func TestVerify6183_ConstructComponentStatusReflectsReplicas(t *testing.T) {
	engine, runtimeObj, runtimeClass, ctx, c := verify6183Fixture(t, 1, 2)

	runtimeObj.Spec.Worker.Replicas = 4
	if err := engine.syncRuntimeSpec(ctx, runtimeObj, runtimeClass); err != nil {
		t.Fatalf("syncRuntimeSpec: %v", err)
	}

	manager := component.NewComponentHelper(common.ComponentTypeWorker, c)
	st, err := manager.ConstructComponentStatus(context.Background(), &common.ComponentIdentity{
		Name:      common.GetCacheComponentName("test-runtime", common.ComponentTypeWorker),
		Namespace: "default",
	})
	if err != nil {
		t.Fatalf("ConstructComponentStatus: %v", err)
	}
	if st.DesiredReplicas != 4 {
		t.Fatalf("ConstructComponentStatus.DesiredReplicas = %d, want 4", st.DesiredReplicas)
	}
}
