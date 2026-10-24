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

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	workloadv1alpha1 "github.com/fluid-cloudnative/advanced-statefulset/api/workload/v1alpha1"
	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	cruntime "github.com/fluid-cloudnative/fluid/pkg/runtime"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// tieredStoreDriftHarness builds a CacheEngine against a fake cluster, creates
// the master/worker/client workloads the way Setup does (so tieredStore volumes
// carry their creation-time values), and returns everything the finding tests
// need to drive a spec edit through the sync path.
type tieredStoreDriftHarness struct {
	engine   *CacheEngine
	recorder *record.FakeRecorder
	ctx      cruntime.ReconcileRequestContext
}

func newTieredStoreDriftHarness(t *testing.T, workerLevels, clientLevels []datav1alpha1.RuntimeTieredStoreLevel) *tieredStoreDriftHarness {
	t.Helper()
	const (
		name      = "test-runtime"
		namespace = "default"
	)

	dataset := &datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: "test-dataset-uid"},
		Spec:       datav1alpha1.DatasetSpec{},
	}
	runtimeObj := &datav1alpha1.CacheRuntime{
		TypeMeta: metav1.TypeMeta{APIVersion: "data.fluid.io/v1alpha1", Kind: "CacheRuntime"},
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: namespace, UID: "test-runtime-uid",
		},
		Spec: datav1alpha1.CacheRuntimeSpec{
			RuntimeClassName: "test-class",
			Master:           datav1alpha1.CacheRuntimeMasterSpec{Replicas: 1},
			Worker: datav1alpha1.CacheRuntimeWorkerSpec{
				Replicas:    2,
				TieredStore: datav1alpha1.RuntimeTieredStore{Levels: workerLevels},
			},
			Client: datav1alpha1.CacheRuntimeClientSpec{
				TieredStore: datav1alpha1.RuntimeTieredStore{Levels: clientLevels},
			},
		},
	}
	runtimeClass := &datav1alpha1.CacheRuntimeClass{
		ObjectMeta:     metav1.ObjectMeta{Name: "test-class"},
		FileSystemType: "test-fs",
		Topology: &datav1alpha1.RuntimeTopology{
			Master: &datav1alpha1.RuntimeComponentDefinition{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "master", Image: "test-master:latest"}}},
				},
			},
			Worker: &datav1alpha1.RuntimeComponentDefinition{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "worker", Image: "test-worker:latest"}}},
				},
			},
			Client: &datav1alpha1.RuntimeComponentDefinition{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "client", Image: "test-client:latest"}}},
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(CacheEngineTestScheme).
		WithObjects(dataset, runtimeObj, runtimeClass).
		WithStatusSubresource(dataset, runtimeObj).
		Build()

	recorder := record.NewFakeRecorder(100)
	engine := &CacheEngine{
		name:      name,
		namespace: namespace,
		Client:    fakeClient,
		Recorder:  recorder,
		Log:       ctrl.Log.WithName("verify-test"),
	}
	ctx := cruntime.ReconcileRequestContext{
		Client:         fakeClient,
		Context:        context.Background(),
		Log:            ctrl.Log.WithName("verify-test"),
		RuntimeType:    "cache",
		NamespacedName: types.NamespacedName{Name: name, Namespace: namespace},
	}

	rt, err := engine.getRuntime()
	if err != nil {
		t.Fatalf("getRuntime: %v", err)
	}
	value, err := engine.transform(dataset, rt, runtimeClass)
	if err != nil {
		t.Fatalf("transform: %v", err)
	}
	if _, err = engine.SetupMasterComponent(value.Master); err != nil {
		t.Fatalf("SetupMasterComponent: %v", err)
	}
	if _, err = engine.SetupWorkerComponent(value.Worker); err != nil {
		t.Fatalf("SetupWorkerComponent: %v", err)
	}
	if _, err = engine.SetupClientComponent(value.Client); err != nil {
		t.Fatalf("SetupClientComponent: %v", err)
	}

	return &tieredStoreDriftHarness{
		engine:   engine,
		recorder: recorder,
		ctx:      ctx,
	}
}

// editWorkerTieredStore mutates the stored CacheRuntime's worker tieredStore
// through the API client, the way kubectl patch would.
func (h *tieredStoreDriftHarness) editRuntime(t *testing.T, mutate func(spec *datav1alpha1.CacheRuntimeSpec)) {
	t.Helper()
	rt, err := h.engine.getRuntime()
	if err != nil {
		t.Fatalf("getRuntime: %v", err)
	}
	mutate(&rt.Spec)
	if err := h.engine.Client.Update(h.ctx.Context, rt); err != nil {
		t.Fatalf("update runtime: %v", err)
	}
}

func (h *tieredStoreDriftHarness) drainEvents() []string {
	var events []string
	for {
		select {
		case e := <-h.recorder.Events:
			events = append(events, e)
		default:
			return events
		}
	}
}

func (h *tieredStoreDriftHarness) workerASTS(t *testing.T) *workloadv1alpha1.AdvancedStatefulSet {
	t.Helper()
	asts := &workloadv1alpha1.AdvancedStatefulSet{}
	if err := h.engine.Client.Get(h.ctx.Context, types.NamespacedName{Name: "test-runtime-worker", Namespace: "default"}, asts); err != nil {
		t.Fatalf("get worker asts: %v", err)
	}
	return asts
}

func (h *tieredStoreDriftHarness) clientDS(t *testing.T) *appsv1.DaemonSet {
	t.Helper()
	ds := &appsv1.DaemonSet{}
	if err := h.engine.Client.Get(h.ctx.Context, types.NamespacedName{Name: "test-runtime-client", Namespace: "default"}, ds); err != nil {
		t.Fatalf("get client daemonset: %v", err)
	}
	return ds
}

func (h *tieredStoreDriftHarness) configQuota(t *testing.T, component string) string {
	t.Helper()
	cm := &corev1.ConfigMap{}
	if err := h.engine.Client.Get(h.ctx.Context, types.NamespacedName{Name: "fluid-runtime-config-test-runtime", Namespace: "default"}, cm); err != nil {
		t.Fatalf("get configmap: %v", err)
	}
	raw, ok := cm.Data["runtime.json"]
	if !ok {
		t.Fatalf("runtime.json missing from configmap: %v", cm.Data)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("unmarshal runtime.json: %v", err)
	}
	comp, _ := cfg[component].(map[string]interface{})
	levels, _ := comp["tieredStoreLevels"].([]interface{})
	if len(levels) == 0 {
		t.Fatalf("no tieredStoreLevels for %s in runtime.json: %s", component, raw)
	}
	level, _ := levels[0].(map[string]interface{})
	quotas, _ := level["quotas"].([]interface{})
	if len(quotas) != 1 {
		t.Fatalf("unexpected quotas for %s in runtime.json: %s", component, raw)
	}
	q, _ := quotas[0].(string)
	return q
}

func volumeBySpec(t *testing.T, volumes []corev1.Volume, name string) *corev1.Volume {
	t.Helper()
	for i := range volumes {
		if volumes[i].Name == name {
			return &volumes[i]
		}
	}
	t.Fatalf("volume %s not found", name)
	return nil
}

// TestVerifyWorkerDriftWarnsThroughFullSync is a contract test for PR #6219's
// core claim, driven through the public Sync entry point (not just
// syncRuntimeSpec): after the worker workload is created, editing the worker
// tieredStore quota must surface a RuntimeTieredStoreConflict Warning event on
// the CacheRuntime.
func TestVerifyWorkerDriftWarnsThroughFullSync(t *testing.T) {
	h := newTieredStoreDriftHarness(t, []datav1alpha1.RuntimeTieredStoreLevel{
		{ProcessMemory: &datav1alpha1.ProcessMemoryMediumSource{Quota: resource.MustParse("8Gi")}},
	}, nil)

	// Creation-time volume sanity.
	vol := volumeBySpec(t, h.workerASTS(t).Spec.Template.Spec.Volumes, getMemoryTieredStoreVolumeName(0))
	if vol.EmptyDir == nil || vol.EmptyDir.SizeLimit == nil || vol.EmptyDir.SizeLimit.Cmp(resource.MustParse("8Gi")) != 0 {
		t.Fatalf("creation-time worker volume = %+v, want 8Gi sizeLimit", vol)
	}

	h.editRuntime(t, func(spec *datav1alpha1.CacheRuntimeSpec) {
		spec.Worker.TieredStore.Levels[0].ProcessMemory.Quota = resource.MustParse("16Gi")
	})

	h.drainEvents()
	if err := h.engine.Sync(h.ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	events := h.drainEvents()
	found := false
	for _, e := range events {
		if strings.Contains(e, common.RuntimeTieredStoreConflict) && strings.Contains(e, corev1.EventTypeWarning) {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a %s Warning event from full Sync, got %v", common.RuntimeTieredStoreConflict, events)
	}
	// The remediation text must point at the remedy that actually converges:
	// round 1 verified that deleting the worker workload wedges sync (NotFound
	// every reconcile) while recreating the CacheRuntime converges, so the event
	// must direct users at the CacheRuntime, not the worker workload.
	for _, e := range events {
		if strings.Contains(e, common.RuntimeTieredStoreConflict) && !strings.Contains(e, "Delete and recreate the CacheRuntime") {
			t.Fatalf("drift event remediation text changed: %q", e)
		}
	}
}

// TestVerifyDriftThrottleAcrossSyncs is a CONTRACT test for the throttle the
// controller relies on: while worker tieredStore drift persists, consecutive
// Sync calls on the same engine (what GetOrCreateEngine's per-runtime engine
// cache gives the real Reconcile loop) must emit exactly one
// RuntimeTieredStoreConflict event; the event must re-appear once the interval
// expires; and the cooldown must reset when the drift is resolved so a fresh
// drift warns immediately.
func TestVerifyDriftThrottleAcrossSyncs(t *testing.T) {
	h := newTieredStoreDriftHarness(t, []datav1alpha1.RuntimeTieredStoreLevel{
		{ProcessMemory: &datav1alpha1.ProcessMemoryMediumSource{Quota: resource.MustParse("8Gi")}},
	}, nil)

	h.editRuntime(t, func(spec *datav1alpha1.CacheRuntimeSpec) {
		spec.Worker.TieredStore.Levels[0].ProcessMemory.Quota = resource.MustParse("16Gi")
	})
	h.drainEvents()

	countDriftEvents := func() int {
		n := 0
		for _, e := range h.drainEvents() {
			if strings.Contains(e, common.RuntimeTieredStoreConflict) {
				n++
			}
		}
		return n
	}

	// Three consecutive syncs while the drift persists: exactly one event.
	for i := 0; i < 3; i++ {
		if err := h.engine.Sync(h.ctx); err != nil {
			t.Fatalf("Sync #%d: %v", i, err)
		}
	}
	if got := countDriftEvents(); got != 1 {
		t.Fatalf("got %d drift events across 3 consecutive Sync calls, want 1 (throttled)", got)
	}

	// After the interval expires the warning is re-emitted.
	h.engine.timeOfLastTieredStoreWarning = time.Now().Add(-defaultTieredStoreWarningInterval - time.Minute)
	if err := h.engine.Sync(h.ctx); err != nil {
		t.Fatalf("Sync after interval: %v", err)
	}
	if got := countDriftEvents(); got != 1 {
		t.Fatalf("got %d drift events after the interval expired, want 1 (re-emitted)", got)
	}

	// Resolving the drift resets the cooldown: the next fresh drift warns
	// immediately instead of waiting out the interval.
	h.editRuntime(t, func(spec *datav1alpha1.CacheRuntimeSpec) {
		spec.Worker.TieredStore.Levels[0].ProcessMemory.Quota = resource.MustParse("8Gi")
	})
	if err := h.engine.Sync(h.ctx); err != nil {
		t.Fatalf("Sync resolve: %v", err)
	}
	if got := countDriftEvents(); got != 0 {
		t.Fatalf("got %d drift events after resolving the drift, want 0", got)
	}
	h.editRuntime(t, func(spec *datav1alpha1.CacheRuntimeSpec) {
		spec.Worker.TieredStore.Levels[0].ProcessMemory.Quota = resource.MustParse("16Gi")
	})
	if err := h.engine.Sync(h.ctx); err != nil {
		t.Fatalf("Sync re-drift: %v", err)
	}
	if got := countDriftEvents(); got != 1 {
		t.Fatalf("got %d drift events for a fresh drift right after resolution, want 1 (cooldown was reset)", got)
	}
}

// TestVerifyClientTieredStoreDriftSilent is a BUG-CANARY pinned to follow-up
// issue #6221: client-side tieredStore drift detection. As of head b56db670
// the docs are scoped to spec.worker.tieredStore (matching the implementation)
// and #6221 tracks the client gap, so the documented scope is honest — but the
// underlying silence remains: editing spec.client.tieredStore rebuilds the
// client's ConfigMap levels while the client DaemonSet keeps its
// creation-time volume, and no RuntimeTieredStoreConflict event is emitted.
//
// Polarity: canary — it passes while client drift stays undetected and FLIPS
// TO RED once #6221 lands (extend detection to the client DaemonSet pod
// spec). Then invert the last assertion into a contract test.
func TestVerifyClientTieredStoreDriftSilent(t *testing.T) {
	h := newTieredStoreDriftHarness(t,
		// worker levels stay consistent so they cannot be the event source
		[]datav1alpha1.RuntimeTieredStoreLevel{
			{ProcessMemory: &datav1alpha1.ProcessMemoryMediumSource{Quota: resource.MustParse("8Gi")}},
		},
		[]datav1alpha1.RuntimeTieredStoreLevel{
			{ProcessMemory: &datav1alpha1.ProcessMemoryMediumSource{Quota: resource.MustParse("4Gi")}},
		})

	// Creation-time client volume sanity.
	vol := volumeBySpec(t, h.clientDS(t).Spec.Template.Spec.Volumes, getMemoryTieredStoreVolumeName(0))
	if vol.EmptyDir == nil || vol.EmptyDir.SizeLimit == nil || vol.EmptyDir.SizeLimit.Cmp(resource.MustParse("4Gi")) != 0 {
		t.Fatalf("creation-time client volume = %+v, want 4Gi sizeLimit", vol)
	}

	h.editRuntime(t, func(spec *datav1alpha1.CacheRuntimeSpec) {
		spec.Client.TieredStore.Levels[0].ProcessMemory.Quota = resource.MustParse("8Gi")
	})

	h.drainEvents()
	if err := h.engine.Sync(h.ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// The ConfigMap follows the edited spec...
	if got := h.configQuota(t, "client"); got != "8Gi" {
		t.Fatalf("ConfigMap client quota = %q, want 8Gi", got)
	}
	// ...while the client DaemonSet volume keeps its creation-time size.
	vol = volumeBySpec(t, h.clientDS(t).Spec.Template.Spec.Volumes, getMemoryTieredStoreVolumeName(0))
	if vol.EmptyDir.SizeLimit.Cmp(resource.MustParse("4Gi")) != 0 {
		t.Fatalf("client volume sizeLimit after sync = %s, want 4Gi (unchanged)", vol.EmptyDir.SizeLimit.String())
	}

	// Canary assertion: nothing reports the split.
	events := h.drainEvents()
	for _, e := range events {
		if strings.Contains(e, common.RuntimeTieredStoreConflict) {
			t.Fatalf("client tieredStore drift was reported (%q); the canary must be inverted", e)
		}
	}
}

// TestVerifyEmptyDirMediumFlipDetected is a CONTRACT test (inverted from the
// round-1 canary TestVerifyEmptyDirMediumFlipSilent, which flipped to red on
// head b56db670 when the Medium comparison landed in checkEmptyDirDrift):
// flipping an emptyDir level's `medium` between disk ("") and "Memory" is a
// real split (the ConfigMap's mediumType and the creation path's memory
// accounting follow the spec while the worker volume stays as created), and
// the drift check must report it in BOTH directions.
func TestVerifyEmptyDirMediumFlipDetected(t *testing.T) {
	drive := func(t *testing.T, from, to corev1.StorageMedium) {
		h := newTieredStoreDriftHarness(t, []datav1alpha1.RuntimeTieredStoreLevel{
			{EmptyDir: &datav1alpha1.EmptyDirMediumSource{Quota: resource.MustParse("10Gi"), Medium: from}},
		}, nil)

		// Creation-time sanity: the volume carries the spec medium verbatim.
		vol := volumeBySpec(t, h.workerASTS(t).Spec.Template.Spec.Volumes, getTieredStoreVolumeName(0, 0))
		if vol.EmptyDir == nil || vol.EmptyDir.Medium != from || vol.EmptyDir.SizeLimit == nil || vol.EmptyDir.SizeLimit.Cmp(resource.MustParse("10Gi")) != 0 {
			t.Fatalf("creation-time worker volume = %+v, want emptyDir 10Gi medium %q", vol, from)
		}

		h.editRuntime(t, func(spec *datav1alpha1.CacheRuntimeSpec) {
			spec.Worker.TieredStore.Levels[0].EmptyDir.Medium = to
		})

		h.drainEvents()
		if err := h.engine.Sync(h.ctx); err != nil {
			t.Fatalf("Sync: %v", err)
		}

		// The worker volume keeps its creation-time medium (not patched in place).
		vol = volumeBySpec(t, h.workerASTS(t).Spec.Template.Spec.Volumes, getTieredStoreVolumeName(0, 0))
		if vol.EmptyDir.Medium != from {
			t.Fatalf("worker volume medium after sync = %q, want %q (unchanged)", vol.EmptyDir.Medium, from)
		}

		// Contract assertion: the medium flip is reported.
		events := h.drainEvents()
		found := false
		for _, e := range events {
			if strings.Contains(e, common.RuntimeTieredStoreConflict) && strings.Contains(e, corev1.EventTypeWarning) {
				found = true
			}
		}
		if !found {
			t.Fatalf("emptyDir medium flip %q -> %q was not reported, got %v", from, to, events)
		}
	}

	t.Run("disk-to-memory", func(t *testing.T) { drive(t, corev1.StorageMedium(""), corev1.StorageMediumMemory) })
	t.Run("memory-to-disk", func(t *testing.T) { drive(t, corev1.StorageMediumMemory, corev1.StorageMedium("")) })
}
