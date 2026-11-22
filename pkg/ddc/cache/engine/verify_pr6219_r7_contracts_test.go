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

// Round-7 CONTRACT tests for PR #6219 (reviewer: codex), head 59cd3ab3.
//
// Round 7 covers one new hypothesis (raised by Reviewer C / Copilot comment
// r4657407203 on the round-7 head, independently verified here): the orphan
// detection in checkWorkerTieredStoreDrift treats EVERY worker-pod volume whose
// name starts with tieredStoreVolumeNamePrefix ("tiered-store-level-") as
// engine-generated, and flags any such volume that is not an expected generated
// name as drift. But the prefix is not reserved anywhere: spec.volumes entries
// referenced by spec.worker.volumeMounts are injected into the worker pod
// unchanged (transformRuntimeSpecVolumes), and CacheRuntimeClass template
// volumes ride the same path. A user (or class author) volume named e.g.
// "tiered-store-level-config" therefore produces a RECURRING false-positive
// RuntimeTieredStoreConflict warning at steady state — no edit needed — whose
// remediation advice ("Delete and recreate the CacheRuntime") cannot resolve
// it, because recreation re-injects the same user volume.
//
//   - TestVerifyPR6219UserPrefixVolumeReachesWorkerPod  CONTRACT (reachability,
//     expected PASS on head): the injection path carries a prefix-colliding
//     user volume into the worker pod spec unchanged.
//   - TestVerifyPR6219UserPrefixVolumeSteadyStateNoWarning  CONTRACT (the
//     finding itself, expected RED on head until fixed): steady state + a user
//     prefix volume must NOT warn.
//   - TestVerifyPR6219OrphanedGeneratedVolumeStillDetected  CONTRACT (guard,
//     expected PASS on head): an orphaned volume with the exact generated name
//     shape must still warn, so a fix that narrows the match to the generated
//     shape (tiered-store-level-<N>-index-<M> / -memory) cannot silently
//     disable orphan detection.

import (
	"strings"
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// Reachability: a spec.volumes entry named with the tiered-store prefix is
// injected into the worker pod spec unchanged when a worker volumeMount
// references it. This is a supported CacheRuntime feature (transformVolumes
// step 3, wired for the worker at transform_worker.go).
func TestVerifyPR6219UserPrefixVolumeReachesWorkerPod(t *testing.T) {
	e, _, _ := verifyPR6219Engine(t)
	podSpec := &corev1.PodSpec{Containers: []corev1.Container{{Name: "worker"}}}
	userVol := corev1.Volume{
		Name: "tiered-store-level-config",
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: "user-tiered-store-conf"},
			},
		},
	}
	err := e.transformRuntimeSpecVolumes(
		[]corev1.Volume{userVol},
		[]corev1.VolumeMount{{Name: "tiered-store-level-config", MountPath: "/etc/tiered-store"}},
		podSpec,
	)
	if err != nil {
		t.Fatalf("transformRuntimeSpecVolumes rejected a legal user volume: %v", err)
	}
	found := findVolume(podSpec.Volumes, "tiered-store-level-config")
	if found == nil || found.ConfigMap == nil {
		t.Fatalf("user prefix volume was not injected into the worker pod spec: %+v", podSpec.Volumes)
	}
}

// The finding: steady state (spec matches the generated volume exactly) plus a
// user-supplied configMap volume sharing the prefix must NOT warn. RED on the
// round-7 head = the false positive reproduces.
func TestVerifyPR6219UserPrefixVolumeSteadyStateNoWarning(t *testing.T) {
	e, recorder, ctx := verifyPR6219Engine(t)

	// Steady state: disk-backed emptyDir level 0 (2Gi); pod carries the
	// matching generated volume tiered-store-level-0-index-0 AND a user
	// configMap volume named tiered-store-level-config (injected via
	// spec.volumes + worker volumeMounts, proven reachable by the test above).
	q := resource.MustParse("2Gi")
	verifyPR6219SetLevels(t, e, ctx, []datav1alpha1.RuntimeTieredStoreLevel{
		{EmptyDir: &datav1alpha1.EmptyDirMediumSource{Quota: q}},
	})
	verifyPR6219ReplaceWorkerVolumes(t, e, ctx, []corev1.Volume{
		{
			Name: "tiered-store-level-0-index-0",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{Medium: "", SizeLimit: &q},
			},
		},
		{
			Name: "tiered-store-level-config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: "user-tiered-store-conf"},
				},
			},
		},
	})

	verifyPR6219SyncOnce(t, e, ctx)
	for _, ev := range verifyPR6219DrainEvents(recorder) {
		if strings.Contains(ev, common.RuntimeTieredStoreConflict) {
			t.Errorf("steady state with a user-supplied prefix volume must not warn, got: %s", ev)
		}
	}
}

// Guard: an orphaned volume with the exact GENERATED name shape must still be
// flagged, so a fix that narrows orphan detection to the generated shape keeps
// the PR's intended detection. PASS on the round-7 head.
func TestVerifyPR6219OrphanedGeneratedVolumeStillDetected(t *testing.T) {
	e, recorder, ctx := verifyPR6219Engine(t)

	q := resource.MustParse("2Gi")
	verifyPR6219SetLevels(t, e, ctx, []datav1alpha1.RuntimeTieredStoreLevel{
		{EmptyDir: &datav1alpha1.EmptyDirMediumSource{Quota: q}},
	})
	verifyPR6219ReplaceWorkerVolumes(t, e, ctx, []corev1.Volume{
		{
			Name: "tiered-store-level-0-index-0",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{Medium: "", SizeLimit: &q},
			},
		},
		// orphaned generated-shape volume: level 1 was removed from the spec
		// (or the runtime was recreated with fewer levels against a stale pod).
		{
			Name: "tiered-store-level-1-index-0",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{Medium: "", SizeLimit: &q},
			},
		},
	})

	verifyPR6219SyncOnce(t, e, ctx)
	found := false
	for _, ev := range verifyPR6219DrainEvents(recorder) {
		if strings.Contains(ev, common.RuntimeTieredStoreConflict) && strings.Contains(ev, "tiered-store-level-1-index-0") {
			found = true
		}
	}
	if !found {
		t.Errorf("orphaned generated-shape volume tiered-store-level-1-index-0 must still warn")
	}
}
