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

// Round-8 verification contracts (head d19969af, PR #6219).
//
// TS6  TestVerifyUserPrefixVolumeSteadyStateNoWarning — the round-7 blocker
//      TS-ORPHAN-PREFIX-COLLISION as a regression contract: user-supplied
//      worker-pod volumes that merely share the tiered-store-level- prefix
//      must not trigger TieredStoreSynced=False or a Warning event, while a
//      genuinely orphaned engine-generated volume must still be flagged.
// TS7  TestVerifyQuantityRoundTripAndHostPathUnsetNoDrift — steady-state
//      guards against false positives: equal quantities in different
//      formats (the API-server round-trip shape) and HostPath.Type nil vs
//      HostPathUnset must compare equal.
// TS8  TestVerifyInvalidSpecConditionAndGuidance — invalid specs report
//      RuntimeTieredStoreInvalid with fix-the-spec guidance (never the
//      destructive recreate advice).
// TS9  TestVerifyUnconfiguredTieredStoreNoCondition — runtimes that never
//      configured tieredStore get no condition and no event.

import (
	"strings"
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// TS6 — round-7 blocker TS-ORPHAN-PREFIX-COLLISION regression contract.
func TestVerifyUserPrefixVolumeSteadyStateNoWarning(t *testing.T) {
	h := newTieredStoreDriftHarness(t, []datav1alpha1.RuntimeTieredStoreLevel{
		{ProcessMemory: &datav1alpha1.ProcessMemoryMediumSource{Quota: resource.MustParse("8Gi")}},
	}, nil)

	// Inject a user volume that shares the tiered-store-level- prefix, the way
	// CacheRuntime spec.volumes + spec.worker.volumeMounts or a class-template
	// volume would put it into the worker pod (transform appends user volumes
	// verbatim; the injection here is the steady-state pod shape either path
	// produces).
	asts := h.workerASTS(t)
	asts.Spec.Template.Spec.Volumes = append(asts.Spec.Template.Spec.Volumes, corev1.Volume{
		Name: "tiered-store-level-config",
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: "custom"}},
		},
	})
	if err := h.engine.Client.Update(h.ctx.Context, asts); err != nil {
		t.Fatalf("update worker asts: %v", err)
	}

	h.drainEvents()
	for i := 0; i < 3; i++ {
		if err := h.engine.Sync(h.ctx); err != nil {
			t.Fatalf("Sync #%d: %v", i, err)
		}
	}
	if events := h.drainEvents(); len(events) != 0 {
		t.Fatalf("steady-state runtime with a prefix-colliding user volume emitted events: %v", events)
	}
	cond := h.tieredStoreCondition(t)
	if cond == nil {
		// No tieredStore condition at all is acceptable only if no generated
		// volume drifted; here the processMemory level IS configured, so the
		// condition must exist and be True.
		t.Fatalf("TieredStoreSynced condition missing on a configured runtime at steady state")
	}
	if cond.Status != corev1.ConditionTrue {
		t.Fatalf("TieredStoreSynced = %s (%s) with a prefix-colliding user volume, want True: %q",
			cond.Status, cond.Reason, cond.Message)
	}

	// Guard: a genuinely orphaned engine-generated volume is still detected.
	asts = h.workerASTS(t)
	asts.Spec.Template.Spec.Volumes = append(asts.Spec.Template.Spec.Volumes, corev1.Volume{
		Name: getTieredStoreVolumeName(5, 3),
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: resource.NewQuantity(1, resource.BinarySI)},
		},
	})
	if err := h.engine.Client.Update(h.ctx.Context, asts); err != nil {
		t.Fatalf("update worker asts (orphan): %v", err)
	}
	h.drainEvents()
	if err := h.engine.Sync(h.ctx); err != nil {
		t.Fatalf("Sync (orphan): %v", err)
	}
	cond = h.tieredStoreCondition(t)
	if cond == nil || cond.Status != corev1.ConditionFalse {
		t.Fatalf("orphaned generated volume not detected: cond = %+v", cond)
	}
	found := false
	for _, e := range h.drainEvents() {
		if strings.Contains(e, common.RuntimeTieredStoreConflict) {
			found = true
		}
	}
	if !found {
		t.Fatalf("orphaned generated volume did not emit a %s event", common.RuntimeTieredStoreConflict)
	}
}

// TS7 — steady-state guards: equal quantities in different textual forms and
// HostPath.Type nil vs HostPathUnset must not read as drift (the API server
// round-trips both shapes).
func TestVerifyQuantityRoundTripAndHostPathUnsetNoDrift(t *testing.T) {
	t.Run("quantity-format", func(t *testing.T) {
		h := newTieredStoreDriftHarness(t, []datav1alpha1.RuntimeTieredStoreLevel{
			{EmptyDir: &datav1alpha1.EmptyDirMediumSource{Quota: resource.MustParse("1Gi")}},
		}, nil)
		// Rewrite the workload volume's sizeLimit in the canonicalized
		// plain-integer form the API server would return for 1Gi.
		asts := h.workerASTS(t)
		vol := volumeBySpec(t, asts.Spec.Template.Spec.Volumes, getTieredStoreVolumeName(0, 0))
		vol.EmptyDir.SizeLimit = resource.NewQuantity(1073741824, resource.BinarySI)
		if err := h.engine.Client.Update(h.ctx.Context, asts); err != nil {
			t.Fatalf("update worker asts: %v", err)
		}
		h.drainEvents()
		if err := h.engine.Sync(h.ctx); err != nil {
			t.Fatalf("Sync: %v", err)
		}
		if events := h.drainEvents(); len(events) != 0 {
			t.Fatalf("equal quantity in round-tripped format reported as drift: %v", events)
		}
		if cond := h.tieredStoreCondition(t); cond == nil || cond.Status != corev1.ConditionTrue {
			t.Fatalf("TieredStoreSynced for round-tripped quantity = %+v, want True", cond)
		}
	})

	t.Run("hostpath-type-unset", func(t *testing.T) {
		h := newTieredStoreDriftHarness(t, []datav1alpha1.RuntimeTieredStoreLevel{
			{HostPath: &datav1alpha1.HostPathMediumSource{
				Paths:  []string{"/mnt/disk1"},
				Quotas: []resource.Quantity{resource.MustParse("100Gi")},
			}},
		}, nil)
		// The spec side leaves Type nil; the workload side carries the
		// API-server-defaulted HostPathUnset.
		asts := h.workerASTS(t)
		vol := volumeBySpec(t, asts.Spec.Template.Spec.Volumes, getTieredStoreVolumeName(0, 0))
		unset := corev1.HostPathUnset
		vol.HostPath.Type = &unset
		if err := h.engine.Client.Update(h.ctx.Context, asts); err != nil {
			t.Fatalf("update worker asts: %v", err)
		}
		h.drainEvents()
		if err := h.engine.Sync(h.ctx); err != nil {
			t.Fatalf("Sync: %v", err)
		}
		if events := h.drainEvents(); len(events) != 0 {
			t.Fatalf("HostPathUnset vs nil Type reported as drift: %v", events)
		}
		if cond := h.tieredStoreCondition(t); cond == nil || cond.Status != corev1.ConditionTrue {
			t.Fatalf("TieredStoreSynced for HostPathUnset vs nil = %+v, want True", cond)
		}
	})
}

// TS8 — invalid specs get RuntimeTieredStoreInvalid and fix-the-spec guidance.
func TestVerifyInvalidSpecConditionAndGuidance(t *testing.T) {
	drive := func(t *testing.T, mutate func(spec *datav1alpha1.CacheRuntimeSpec)) {
		h := newTieredStoreDriftHarness(t, []datav1alpha1.RuntimeTieredStoreLevel{
			{EmptyDir: &datav1alpha1.EmptyDirMediumSource{Quota: resource.MustParse("2Gi"), Medium: corev1.StorageMediumMemory}},
		}, nil)
		h.editRuntime(t, mutate)
		h.drainEvents()
		if err := h.engine.Sync(h.ctx); err != nil {
			t.Fatalf("Sync: %v", err)
		}
		cond := h.tieredStoreCondition(t)
		if cond == nil || cond.Status != corev1.ConditionFalse {
			t.Fatalf("TieredStoreSynced for invalid spec = %+v, want False", cond)
		}
		if cond.Reason != common.RuntimeTieredStoreInvalid {
			t.Fatalf("reason for invalid spec = %q, want %q", cond.Reason, common.RuntimeTieredStoreInvalid)
		}
		if !strings.Contains(cond.Message, "Fix spec.worker.tieredStore") {
			t.Fatalf("invalid-spec message should direct at fixing the spec: %q", cond.Message)
		}
		if strings.Contains(cond.Message, "Delete and recreate") {
			t.Fatalf("invalid-spec message must not advise recreating (the spec itself is broken): %q", cond.Message)
		}
		found := false
		for _, e := range h.drainEvents() {
			if strings.Contains(e, common.RuntimeTieredStoreInvalid) && strings.Contains(e, corev1.EventTypeWarning) {
				found = true
			}
		}
		if !found {
			t.Fatalf("invalid spec did not emit a %s Warning event", common.RuntimeTieredStoreInvalid)
		}
	}
	t.Run("multi-medium", func(t *testing.T) {
		drive(t, func(spec *datav1alpha1.CacheRuntimeSpec) {
			spec.Worker.TieredStore.Levels[0].HostPath = &datav1alpha1.HostPathMediumSource{
				Paths:  []string{"/mnt/cache"},
				Quotas: []resource.Quantity{resource.MustParse("2Gi")},
			}
		})
	})
	t.Run("multi-processMemory", func(t *testing.T) {
		drive(t, func(spec *datav1alpha1.CacheRuntimeSpec) {
			// Two processMemory levels across all levels is invalid.
			spec.Worker.TieredStore.Levels[0] = datav1alpha1.RuntimeTieredStoreLevel{
				ProcessMemory: &datav1alpha1.ProcessMemoryMediumSource{Quota: resource.MustParse("2Gi")},
			}
			spec.Worker.TieredStore.Levels = append(spec.Worker.TieredStore.Levels,
				datav1alpha1.RuntimeTieredStoreLevel{
					ProcessMemory: &datav1alpha1.ProcessMemoryMediumSource{Quota: resource.MustParse("4Gi")},
				})
		})
	})
}

// TS9 — runtimes that never configured tieredStore must not gain a condition
// or emit events (no noise from the new check).
func TestVerifyUnconfiguredTieredStoreNoCondition(t *testing.T) {
	h := newTieredStoreDriftHarness(t, nil, nil)
	h.drainEvents()
	for i := 0; i < 2; i++ {
		if err := h.engine.Sync(h.ctx); err != nil {
			t.Fatalf("Sync #%d: %v", i, err)
		}
	}
	if events := h.drainEvents(); len(events) != 0 {
		t.Fatalf("unconfigured tieredStore emitted events: %v", events)
	}
	if cond := h.tieredStoreCondition(t); cond != nil {
		t.Fatalf("unconfigured tieredStore gained a condition: %+v", cond)
	}
}

// TestVerifyDriftCheckNilPodSpec pins the nil-podSpec guard.
func TestVerifyDriftCheckNilPodSpec(t *testing.T) {
	h := newTieredStoreDriftHarness(t, nil, nil)
	rt, err := h.engine.getRuntime()
	if err != nil {
		t.Fatalf("getRuntime: %v", err)
	}
	if got := h.engine.checkWorkerTieredStoreDrift(rt, nil); got != nil {
		t.Fatalf("checkWorkerTieredStoreDrift(nil podSpec) = %+v, want nil", got)
	}
}

// TestVerifyIsGeneratedTieredStoreVolumeName pins the round-7 fix's regex
// directly (the util the orphan filter now depends on).
func TestVerifyIsGeneratedTieredStoreVolumeName(t *testing.T) {
	generated := []string{
		getTieredStoreVolumeName(0, 0),
		getTieredStoreVolumeName(3, 7),
		getTieredStoreVolumeName(12, 0),
		getMemoryTieredStoreVolumeName(0),
		getMemoryTieredStoreVolumeName(4),
	}
	for _, name := range generated {
		if !isGeneratedTieredStoreVolumeName(name) {
			t.Fatalf("isGeneratedTieredStoreVolumeName(%q) = false, want true", name)
		}
	}
	user := []string{
		"tiered-store-level-config",
		"tiered-store-level-data",
		"tiered-store-level-",
		"tiered-store-level-0",
		"tiered-store-level-0-index",
		"tiered-store-level-memory",
		"tiered-store-level-0-index-x",
		"my-custom-volume",
	}
	for _, name := range user {
		if isGeneratedTieredStoreVolumeName(name) {
			t.Fatalf("isGeneratedTieredStoreVolumeName(%q) = true, want false", name)
		}
	}
}

// TestVerifyClassTemplatePrefixVolumeNoWarning reproduces the second injection
// path of TS-ORPHAN-PREFIX-COLLISION at the transform level: a
// CacheRuntimeClass worker template volume sharing the prefix reaches the
// worker pod verbatim and must be ignored by the drift check.
func TestVerifyClassTemplatePrefixVolumeNoWarning(t *testing.T) {
	h := newTieredStoreDriftHarness(t, []datav1alpha1.RuntimeTieredStoreLevel{
		{ProcessMemory: &datav1alpha1.ProcessMemoryMediumSource{Quota: resource.MustParse("8Gi")}},
	}, nil)

	// Simulate the class-template volume the way the creation path would
	// deliver it: same shape as a template volume appended before the engine's
	// generated ones.
	asts := h.workerASTS(t)
	vols := []corev1.Volume{{
		Name: "tiered-store-level-ssd",
		VolumeSource: corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{Path: "/mnt/ssd"},
		},
	}}
	vols = append(vols, asts.Spec.Template.Spec.Volumes...)
	asts.Spec.Template.Spec.Volumes = vols
	if err := h.engine.Client.Update(h.ctx.Context, asts); err != nil {
		t.Fatalf("update worker asts: %v", err)
	}

	h.drainEvents()
	if err := h.engine.Sync(h.ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if events := h.drainEvents(); len(events) != 0 {
		t.Fatalf("class-template prefix-colliding volume emitted events: %v", events)
	}
	if cond := h.tieredStoreCondition(t); cond == nil || cond.Status != corev1.ConditionTrue {
		t.Fatalf("TieredStoreSynced with class-template prefix volume = %+v, want True", cond)
	}
}
