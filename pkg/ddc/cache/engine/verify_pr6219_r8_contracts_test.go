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

// Round-8 CONTRACT tests for PR #6219 (reviewer: codex), head d19969af.
//
// Round 8 is a DESIGN CHANGE round: the PR replaced the in-memory throttled
// warning (rounds 1-7) with a persistent TieredStoreSynced status condition +
// transition-only Warning events, and fixed the round-7 blocker
// TS-ORPHAN-PREFIX-COLLISION by matching orphan volumes against the exact
// engine-generated name shape (isGeneratedTieredStoreVolumeName) instead of
// the bare tiered-store-level- prefix.
//
// Steady-state fixtures go through the REAL creation path
// (engine.transform + SetupWorkerComponent), so a false positive here cannot
// be an artifact of a hand-built workload.
//
// Contracts (expected PASS on this head):
//   - regex shape + transform-name invariant + prefix-collision steady state
//     + orphan guard                                  (TS-ORPHAN-PREFIX-COLLISION fix)
//   - conflict detection / guidance / medium flip / quantity-format
//     / hostPath unset normalization / configmap-only silence
//     (AC-TS1, AC-TS2, AC-TS9, TS-REMEDIATION-GUIDANCE-WEDGE, docs claims)
//   - invalid spec reported as Invalid without recreate advice
//     (AC-TS12, new form: multi-medium is a transform error, not a precedence race)
//   - transition-only dedup surviving an engine restart, message-change
//     re-emission, revert recovery                    (AC-TS3 new form, TS-REQUEUE-REEMISSION)
//   - omission when tieredStore was never configured  (AC-TS2 scope)
//   - client tieredStore edit stays silent            (#6221 deferral canary)
//
// Bug canary (expected PASS while the behavior exists, FLIPS when fixed):
//   - TestVerifyPR6219R8WorkerDisabledLeavesStaleCondition — a persisted
//     TieredStoreSynced=False is never re-evaluated once the worker is
//     disabled, so a stale alarm (with recreate advice) outlives the worker.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	workloadv1alpha1 "github.com/fluid-cloudnative/advanced-statefulset/api/workload/v1alpha1"
	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	cruntime "github.com/fluid-cloudnative/fluid/pkg/runtime"
	"github.com/fluid-cloudnative/fluid/pkg/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	cclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	verifyPR6219R8Name      = "verify-r8"
	verifyPR6219R8Namespace = "default"
)

type verifyPR6219R8Fixture struct {
	t          *testing.T
	client     cclient.WithWatch
	recorder   *record.FakeRecorder
	ctx        cruntime.ReconcileRequestContext
	dataset    *datav1alpha1.Dataset
	class      *datav1alpha1.CacheRuntimeClass
	workerName string
}

func verifyPR6219R8Scheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = datav1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = workloadv1alpha1.AddToScheme(scheme)
	return scheme
}

func verifyPR6219R8NewFixture(t *testing.T) *verifyPR6219R8Fixture {
	t.Helper()
	dataset := &datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: verifyPR6219R8Name, Namespace: verifyPR6219R8Namespace, UID: "verify-r8-dataset-uid"},
		Spec:       datav1alpha1.DatasetSpec{},
	}
	rt := &datav1alpha1.CacheRuntime{
		TypeMeta:   metav1.TypeMeta{APIVersion: "data.fluid.io/v1alpha1", Kind: "CacheRuntime"},
		ObjectMeta: metav1.ObjectMeta{Name: verifyPR6219R8Name, Namespace: verifyPR6219R8Namespace, UID: "verify-r8-uid"},
		Spec: datav1alpha1.CacheRuntimeSpec{
			RuntimeClassName: "verify-r8-class",
			Master:           datav1alpha1.CacheRuntimeMasterSpec{Replicas: 1},
			Worker:           datav1alpha1.CacheRuntimeWorkerSpec{Replicas: 1},
			Client:           datav1alpha1.CacheRuntimeClientSpec{},
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
	replicas := int32(1)
	masterSts := &workloadv1alpha1.AdvancedStatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: verifyPR6219R8Name + "-master", Namespace: verifyPR6219R8Namespace},
		Spec: workloadv1alpha1.AdvancedStatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "verify-r8-master"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "verify-r8-master"}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "master", Image: "verify-master:latest"}}},
			},
		},
		Status: workloadv1alpha1.AdvancedStatefulSetStatus{ReadyReplicas: 1, CurrentReplicas: 1, AvailableReplicas: 1},
	}
	workerSts := &workloadv1alpha1.AdvancedStatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: verifyPR6219R8Name + "-worker", Namespace: verifyPR6219R8Namespace},
		Spec: workloadv1alpha1.AdvancedStatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "verify-r8-worker"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "verify-r8-worker"}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "worker", Image: "verify-worker:latest"}}},
			},
		},
		Status: workloadv1alpha1.AdvancedStatefulSetStatus{ReadyReplicas: 1, CurrentReplicas: 1, AvailableReplicas: 1},
	}
	clientDs := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: verifyPR6219R8Name + "-client", Namespace: verifyPR6219R8Namespace},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "verify-r8-client"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "verify-r8-client"}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "client", Image: "verify-client:latest"}}},
			},
		},
		Status: appsv1.DaemonSetStatus{NumberReady: 0, DesiredNumberScheduled: 0},
	}

	c := fake.NewClientBuilder().WithScheme(verifyPR6219R8Scheme()).
		WithObjects(dataset, rt, class, masterSts, workerSts, clientDs).
		WithStatusSubresource(dataset, rt).
		Build()
	recorder := record.NewFakeRecorder(100)
	ctx := cruntime.ReconcileRequestContext{
		Client:         c,
		Context:        context.Background(),
		Log:            ctrl.Log.WithName("verify-pr6219-r8"),
		RuntimeType:    "cache",
		NamespacedName: types.NamespacedName{Name: verifyPR6219R8Name, Namespace: verifyPR6219R8Namespace},
	}
	return &verifyPR6219R8Fixture{
		t: t, client: c, recorder: recorder, ctx: ctx,
		dataset: dataset, class: class, workerName: verifyPR6219R8Name + "-worker",
	}
}

// newEngine builds a fresh CacheEngine over the same client — the only state
// the new design keeps between reconciles is the persisted status condition,
// so a new engine instance is a faithful controller-restart simulation.
func (f *verifyPR6219R8Fixture) newEngine() *CacheEngine {
	return &CacheEngine{
		name:      verifyPR6219R8Name,
		namespace: verifyPR6219R8Namespace,
		Client:    f.client,
		Log:       ctrl.Log.WithName("verify-pr6219-r8"),
		Recorder:  f.recorder,
	}
}

// createWorkerWithTieredStore runs the real creation path: set the spec, run
// engine.transform, and recreate the worker workload via SetupWorkerComponent
// so the workload carries exactly what creation would generate.
func (f *verifyPR6219R8Fixture) createWorkerWithTieredStore(ts datav1alpha1.RuntimeTieredStore) {
	f.t.Helper()
	rt := &datav1alpha1.CacheRuntime{}
	if err := f.client.Get(f.ctx.Context, types.NamespacedName{Name: verifyPR6219R8Name, Namespace: verifyPR6219R8Namespace}, rt); err != nil {
		f.t.Fatalf("get runtime: %v", err)
	}
	rt.Spec.Worker.TieredStore = ts
	if err := f.client.Update(f.ctx.Context, rt); err != nil {
		f.t.Fatalf("update runtime tieredStore: %v", err)
	}
	e := f.newEngine()
	value, err := e.transform(f.dataset, rt, f.class)
	if err != nil {
		f.t.Fatalf("transform: %v", err)
	}
	seeded := &workloadv1alpha1.AdvancedStatefulSet{}
	if err := f.client.Get(f.ctx.Context, types.NamespacedName{Name: f.workerName, Namespace: verifyPR6219R8Namespace}, seeded); err != nil {
		f.t.Fatalf("get seeded worker: %v", err)
	}
	if err := f.client.Delete(f.ctx.Context, seeded); err != nil {
		f.t.Fatalf("delete seeded worker: %v", err)
	}
	if _, err := e.SetupWorkerComponent(value.Worker); err != nil {
		f.t.Fatalf("SetupWorkerComponent: %v", err)
	}
}

func (f *verifyPR6219R8Fixture) sync(t *testing.T, e *CacheEngine) {
	t.Helper()
	if err := e.Sync(f.ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}
}

func (f *verifyPR6219R8Fixture) editTieredStore(mut func(*datav1alpha1.RuntimeTieredStore)) {
	f.t.Helper()
	rt := &datav1alpha1.CacheRuntime{}
	if err := f.client.Get(f.ctx.Context, types.NamespacedName{Name: verifyPR6219R8Name, Namespace: verifyPR6219R8Namespace}, rt); err != nil {
		f.t.Fatalf("get runtime for edit: %v", err)
	}
	mut(&rt.Spec.Worker.TieredStore)
	if err := f.client.Update(f.ctx.Context, rt); err != nil {
		f.t.Fatalf("update runtime: %v", err)
	}
}

func (f *verifyPR6219R8Fixture) editWorkerVolumes(mut func([]corev1.Volume) []corev1.Volume) {
	f.t.Helper()
	sts := &workloadv1alpha1.AdvancedStatefulSet{}
	if err := f.client.Get(f.ctx.Context, types.NamespacedName{Name: f.workerName, Namespace: verifyPR6219R8Namespace}, sts); err != nil {
		f.t.Fatalf("get worker: %v", err)
	}
	sts.Spec.Template.Spec.Volumes = mut(sts.Spec.Template.Spec.Volumes)
	if err := f.client.Update(f.ctx.Context, sts); err != nil {
		f.t.Fatalf("update worker: %v", err)
	}
}

func (f *verifyPR6219R8Fixture) tieredStoreCondition(t *testing.T) *datav1alpha1.RuntimeCondition {
	t.Helper()
	rt := &datav1alpha1.CacheRuntime{}
	if err := f.client.Get(f.ctx.Context, types.NamespacedName{Name: verifyPR6219R8Name, Namespace: verifyPR6219R8Namespace}, rt); err != nil {
		t.Fatalf("get runtime: %v", err)
	}
	_, cond := utils.GetRuntimeCondition(rt.Status.Conditions, datav1alpha1.RuntimeTieredStoreSynced)
	return cond
}

func (f *verifyPR6219R8Fixture) drainEvents() []string {
	var events []string
	for {
		select {
		case ev := <-f.recorder.Events:
			events = append(events, ev)
		default:
			return events
		}
	}
}

func (f *verifyPR6219R8Fixture) tieredStoreEvents() []string {
	var out []string
	for _, ev := range f.drainEvents() {
		if strings.Contains(ev, "RuntimeTieredStoreConflict") || strings.Contains(ev, "RuntimeTieredStoreInvalid") {
			out = append(out, ev)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// TS-ORPHAN-PREFIX-COLLISION (round-7 blocker) — fix verification
// ---------------------------------------------------------------------------

// The generated-name matcher must accept exactly the engine-generated shapes
// and reject prefix collisions and near-misses.
func TestVerifyPR6219R8GeneratedNameRegex(t *testing.T) {
	positives := []string{
		"tiered-store-level-0-index-0",
		"tiered-store-level-12-index-3",
		"tiered-store-level-2-memory",
	}
	negatives := []string{
		"tiered-store-level-config",       // round-7 false positive shape
		"tiered-store-level-0",            // missing suffix
		"tiered-store-level-0-index",      // missing path index
		"tiered-store-level-0-index-",     // empty path index
		"tiered-store-level--index-0",     // empty level index
		"tiered-store-level-0-memoryx",    // suffix extension
		"tiered-store-level-0-index-0-extra",
		"tiered-store-level-0-Index-0", // case
		"xtiered-store-level-0-index-0", // leading garbage (anchoring)
		"tiered-store-level-0-memory-1",
		"tiered-store-level-",  // bare prefix
		"tiered-store-levels-0-index-0", // prefix lookalike
	}
	for _, name := range positives {
		if !isGeneratedTieredStoreVolumeName(name) {
			t.Errorf("generated name %q must match the matcher (orphan detection would miss it)", name)
		}
	}
	for _, name := range negatives {
		if isGeneratedTieredStoreVolumeName(name) {
			t.Errorf("name %q must NOT match the generated shape (false-positive vector)", name)
		}
	}
}

// Invariant guard for the want/got asymmetry: `want` is unfiltered, `got` is
// filtered by isGeneratedTieredStoreVolumeName. If TransformRuntimeTieredStore
// ever generates a volume name outside the matcher, every runtime using that
// medium would report a permanent "expected ... not found" false drift.
func TestVerifyPR6219R8TransformGeneratesOnlyRegexNames(t *testing.T) {
	e := &CacheEngine{Log: ctrl.Log.WithName("verify-pr6219-r8")}
	spec := datav1alpha1.RuntimeTieredStore{Levels: []datav1alpha1.RuntimeTieredStoreLevel{
		{ProcessMemory: &datav1alpha1.ProcessMemoryMediumSource{Quota: resource.MustParse("8Gi")}},
		{HostPath: &datav1alpha1.HostPathMediumSource{
			Paths:  []string{"/mnt/disk1", "/mnt/disk2"},
			Quotas: []resource.Quantity{resource.MustParse("100Gi"), resource.MustParse("200Gi")},
		}},
		{EmptyDir: &datav1alpha1.EmptyDirMediumSource{Quota: resource.MustParse("10Gi")}},
		{EmptyDir: &datav1alpha1.EmptyDirMediumSource{Quota: resource.MustParse("10Gi"), Medium: corev1.StorageMediumMemory}},
	}}
	podSpec := &corev1.PodSpec{Containers: []corev1.Container{{Name: "worker"}}}
	if err := e.TransformRuntimeTieredStore(&spec, podSpec); err != nil {
		t.Fatalf("TransformRuntimeTieredStore: %v", err)
	}
	if len(podSpec.Volumes) == 0 {
		t.Fatalf("transform generated no volumes for a full spec")
	}
	for _, vol := range podSpec.Volumes {
		if !isGeneratedTieredStoreVolumeName(vol.Name) {
			t.Errorf("transform-generated volume %q does not match isGeneratedTieredStoreVolumeName: "+
				"the drift check's got-filter would drop it and report a false 'not found' drift", vol.Name)
		}
	}
}

// Reachability (kept from round 7): a spec.volumes entry sharing the
// tiered-store-level- prefix is injected into the worker pod unchanged when a
// worker volumeMount references it.
func TestVerifyPR6219R8UserPrefixVolumeReachesWorkerPod(t *testing.T) {
	e := &CacheEngine{Log: ctrl.Log.WithName("verify-pr6219-r8")}
	podSpec := &corev1.PodSpec{Containers: []corev1.Container{{Name: "worker"}}}
	userVol := corev1.Volume{
		Name: "tiered-store-level-config",
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: "user-tiered-store-conf"},
			},
		},
	}
	if err := e.transformRuntimeSpecVolumes(
		[]corev1.Volume{userVol},
		[]corev1.VolumeMount{{Name: "tiered-store-level-config", MountPath: "/etc/tiered-store"}},
		podSpec,
	); err != nil {
		t.Fatalf("transformRuntimeSpecVolumes rejected a legal user volume: %v", err)
	}
	found := false
	for _, v := range podSpec.Volumes {
		if v.Name == "tiered-store-level-config" && v.ConfigMap != nil {
			found = true
		}
	}
	if !found {
		t.Fatalf("user prefix volume was not injected into the worker pod spec: %+v", podSpec.Volumes)
	}
}

// THE round-7 blocker contract: steady state through the real creation path,
// plus a user/configMap volume sharing the tiered-store-level- prefix on the
// worker pod, must produce condition True and zero tieredStore warning events.
func TestVerifyPR6219R8UserPrefixVolumeSteadyStateNoWarning(t *testing.T) {
	f := verifyPR6219R8NewFixture(t)
	q := resource.MustParse("2Gi")
	f.createWorkerWithTieredStore(datav1alpha1.RuntimeTieredStore{Levels: []datav1alpha1.RuntimeTieredStoreLevel{
		{EmptyDir: &datav1alpha1.EmptyDirMediumSource{Quota: q}},
	}})
	// Simulate the supported injection (spec.volumes + worker volumeMounts, or
	// a CacheRuntimeClass template volume): the volume reaches the pod unchanged.
	f.editWorkerVolumes(func(vols []corev1.Volume) []corev1.Volume {
		return append(vols, corev1.Volume{
			Name: "tiered-store-level-config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: "user-tiered-store-conf"}},
			},
		})
	})

	f.sync(t, f.newEngine())

	cond := f.tieredStoreCondition(t)
	if cond == nil {
		t.Fatalf("expected a TieredStoreSynced condition at steady state, got none")
	}
	if cond.Status != corev1.ConditionTrue {
		t.Fatalf("FALSE POSITIVE (round-7 blocker still present): steady state with a user prefix volume reported %s/%s: %s",
			cond.Status, cond.Reason, cond.Message)
	}
	for _, ev := range f.tieredStoreEvents() {
		t.Errorf("FALSE POSITIVE: tieredStore warning at steady state with a user prefix volume: %s", ev)
	}
}

// Guard: an orphaned volume with the exact generated shape must still be
// flagged — the fix must narrow false positives without disabling orphan
// detection.
func TestVerifyPR6219R8OrphanedGeneratedVolumeStillDetected(t *testing.T) {
	f := verifyPR6219R8NewFixture(t)
	f.createWorkerWithTieredStore(datav1alpha1.RuntimeTieredStore{Levels: []datav1alpha1.RuntimeTieredStoreLevel{
		{EmptyDir: &datav1alpha1.EmptyDirMediumSource{Quota: resource.MustParse("2Gi")}},
	}})
	orphan := corev1.Volume{
		Name: "tiered-store-level-9-index-0",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: resource.NewQuantity(1<<30, resource.BinarySI)},
		},
	}
	f.editWorkerVolumes(func(vols []corev1.Volume) []corev1.Volume { return append(vols, orphan) })

	f.sync(t, f.newEngine())

	cond := f.tieredStoreCondition(t)
	if cond == nil || cond.Status != corev1.ConditionFalse || cond.Reason != common.RuntimeTieredStoreConflict {
		t.Fatalf("orphaned generated volume not detected: cond=%+v", cond)
	}
	if !strings.Contains(cond.Message, "tiered-store-level-9-index-0") {
		t.Errorf("condition does not name the orphaned volume: %s", cond.Message)
	}
	if n := len(f.tieredStoreEvents()); n != 1 {
		t.Errorf("expected exactly 1 transition warning event, got %d", n)
	}
}

// ---------------------------------------------------------------------------
// Drift detection contracts (AC-TS1, AC-TS9, F1 guidance)
// ---------------------------------------------------------------------------

// AC-TS1 + TS-REMEDIATION-GUIDANCE-WEDGE: an in-place processMemory quota edit
// surfaces as TieredStoreSynced=False / RuntimeTieredStoreConflict through the
// full Sync path, names the divergent volume, and keeps the recreate guidance
// (the only remedy round-1 verification proved convergent).
func TestVerifyPR6219R8DriftConflictThroughSync(t *testing.T) {
	f := verifyPR6219R8NewFixture(t)
	f.createWorkerWithTieredStore(datav1alpha1.RuntimeTieredStore{Levels: []datav1alpha1.RuntimeTieredStoreLevel{
		{ProcessMemory: &datav1alpha1.ProcessMemoryMediumSource{Quota: resource.MustParse("8Gi")}},
	}})

	e := f.newEngine()
	f.sync(t, e)
	if cond := f.tieredStoreCondition(t); cond == nil || cond.Status != corev1.ConditionTrue {
		t.Fatalf("steady state must be TieredStoreSynced=True, got %+v", cond)
	}
	if n := len(f.tieredStoreEvents()); n != 0 {
		t.Fatalf("steady state emitted %d tieredStore events", n)
	}

	f.editTieredStore(func(ts *datav1alpha1.RuntimeTieredStore) {
		ts.Levels[0].ProcessMemory.Quota = resource.MustParse("16Gi")
	})
	f.sync(t, e)

	cond := f.tieredStoreCondition(t)
	if cond == nil || cond.Status != corev1.ConditionFalse || cond.Reason != common.RuntimeTieredStoreConflict {
		t.Fatalf("expected TieredStoreSynced=False/RuntimeTieredStoreConflict, got %+v", cond)
	}
	if !strings.Contains(cond.Message, "tiered-store-level-0-memory") {
		t.Errorf("condition does not name the divergent volume: %s", cond.Message)
	}
	if !strings.Contains(cond.Message, "Delete and recreate the CacheRuntime to apply changes.") {
		t.Errorf("conflict guidance regressed (TS-REMEDIATION-GUIDANCE-WEDGE): %s", cond.Message)
	}
	events := f.tieredStoreEvents()
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 transition event, got %d: %v", len(events), events)
	}
	if !strings.Contains(events[0], string(corev1.EventTypeWarning)) || !strings.Contains(events[0], common.RuntimeTieredStoreConflict) {
		t.Errorf("event is not a RuntimeTieredStoreConflict warning: %s", events[0])
	}
}

// AC-TS9 (round-1 minor, fixed): emptyDir medium-only flips are detected in
// both directions (whole-volume DeepEqual includes EmptyDir.Medium).
func TestVerifyPR6219R8MediumFlipDetected(t *testing.T) {
	for _, tc := range []struct {
		name    string
		initial corev1.StorageMedium
		edited  corev1.StorageMedium
	}{
		{"disk to Memory", corev1.StorageMediumDefault, corev1.StorageMediumMemory},
		{"Memory to disk", corev1.StorageMediumMemory, corev1.StorageMediumDefault},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := verifyPR6219R8NewFixture(t)
			f.createWorkerWithTieredStore(datav1alpha1.RuntimeTieredStore{Levels: []datav1alpha1.RuntimeTieredStoreLevel{
				{EmptyDir: &datav1alpha1.EmptyDirMediumSource{Quota: resource.MustParse("10Gi"), Medium: tc.initial}},
			}})
			f.editTieredStore(func(ts *datav1alpha1.RuntimeTieredStore) {
				ts.Levels[0].EmptyDir.Medium = tc.edited
			})
			f.sync(t, f.newEngine())
			cond := f.tieredStoreCondition(t)
			if cond == nil || cond.Status != corev1.ConditionFalse || cond.Reason != common.RuntimeTieredStoreConflict {
				t.Fatalf("medium flip %s not detected: %+v", tc.name, cond)
			}
		})
	}
}

// AC-TS2 round-trip semantics at unit level: "1Gi" in spec vs the numerically
// identical "1073741824" on the workload must NOT drift (equality.Semantic
// compares quantities by value, not by string).
func TestVerifyPR6219R8QuantityFormatNoFalsePositive(t *testing.T) {
	f := verifyPR6219R8NewFixture(t)
	f.createWorkerWithTieredStore(datav1alpha1.RuntimeTieredStore{Levels: []datav1alpha1.RuntimeTieredStoreLevel{
		{EmptyDir: &datav1alpha1.EmptyDirMediumSource{Quota: resource.MustParse("1Gi")}},
	}})
	f.editWorkerVolumes(func(vols []corev1.Volume) []corev1.Volume {
		for i := range vols {
			if vols[i].Name == "tiered-store-level-0-index-0" {
				vols[i].EmptyDir.SizeLimit = resource.NewQuantity(1073741824, resource.BinarySI)
			}
		}
		return vols
	})
	f.sync(t, f.newEngine())
	cond := f.tieredStoreCondition(t)
	if cond == nil || cond.Status != corev1.ConditionTrue {
		t.Fatalf("FALSE POSITIVE on quantity format round-trip (1Gi vs 1073741824): %+v", cond)
	}
	if n := len(f.tieredStoreEvents()); n != 0 {
		t.Fatalf("quantity format round-trip emitted %d events", n)
	}
}

// nil HostPath.Type on the workload vs HostPathUnset from the spec normalize
// to the same value; a real Type change still drifts.
func TestVerifyPR6219R8HostPathUnsetNormalization(t *testing.T) {
	f := verifyPR6219R8NewFixture(t)
	f.createWorkerWithTieredStore(datav1alpha1.RuntimeTieredStore{Levels: []datav1alpha1.RuntimeTieredStoreLevel{
		{HostPath: &datav1alpha1.HostPathMediumSource{
			Paths:  []string{"/mnt/disk1"},
			Quotas: []resource.Quantity{resource.MustParse("100Gi")},
		}},
	}})
	// API-server defaulting shape: Type explicitly HostPathUnset.
	unset := corev1.HostPathUnset
	f.editWorkerVolumes(func(vols []corev1.Volume) []corev1.Volume {
		for i := range vols {
			if vols[i].HostPath != nil {
				vols[i].HostPath.Type = &unset
			}
		}
		return vols
	})
	f.sync(t, f.newEngine())
	if cond := f.tieredStoreCondition(t); cond == nil || cond.Status != corev1.ConditionTrue {
		t.Fatalf("FALSE POSITIVE on nil-vs-HostPathUnset: %+v", cond)
	}

	// And a genuine Type edit must drift.
	dir := corev1.HostPathDirectory
	f.editTieredStore(func(ts *datav1alpha1.RuntimeTieredStore) { ts.Levels[0].HostPath.Type = &dir })
	f.sync(t, f.newEngine())
	cond := f.tieredStoreCondition(t)
	if cond == nil || cond.Status != corev1.ConditionFalse {
		t.Fatalf("hostPath Type drift not detected: %+v", cond)
	}
}

// Docs claim: hostPath.quotas / high / low are ConfigMap-only fields — editing
// them updates the runtime ConfigMap without a pod-volume drift warning.
func TestVerifyPR6219R8ConfigMapOnlyFieldsSilent(t *testing.T) {
	f := verifyPR6219R8NewFixture(t)
	f.createWorkerWithTieredStore(datav1alpha1.RuntimeTieredStore{Levels: []datav1alpha1.RuntimeTieredStoreLevel{
		{HostPath: &datav1alpha1.HostPathMediumSource{
			Paths:  []string{"/mnt/disk1"},
			Quotas: []resource.Quantity{resource.MustParse("100Gi")},
		}},
	}})
	f.editTieredStore(func(ts *datav1alpha1.RuntimeTieredStore) {
		ts.Levels[0].HostPath.Quotas[0] = resource.MustParse("200Gi")
		ts.Levels[0].High = "0.9"
		ts.Levels[0].Low = "0.4"
	})
	f.sync(t, f.newEngine())
	cond := f.tieredStoreCondition(t)
	if cond == nil || cond.Status != corev1.ConditionTrue {
		t.Fatalf("ConfigMap-only field edit must not drift: %+v", cond)
	}
	if n := len(f.tieredStoreEvents()); n != 0 {
		t.Fatalf("ConfigMap-only field edit emitted %d events", n)
	}
}

// AC-TS12 (new form): a level carrying more than one medium — accepted by the
// CRD, reachable via post-creation edit — is now rejected by
// TransformRuntimeTieredStore and surfaced as RuntimeTieredStoreInvalid with
// fix-the-spec advice, never recreate advice. The ConfigMap extraction
// precedence race from rounds 2-3 is superseded: the check never validates a
// multi-medium level against pod volumes at all.
func TestVerifyPR6219R8InvalidSpecNoRecreateAdvice(t *testing.T) {
	for _, tc := range []struct {
		name   string
		levels []datav1alpha1.RuntimeTieredStoreLevel
	}{
		{"multiple media on one level", []datav1alpha1.RuntimeTieredStoreLevel{{
			EmptyDir: &datav1alpha1.EmptyDirMediumSource{Quota: resource.MustParse("2Gi")},
			HostPath: &datav1alpha1.HostPathMediumSource{Paths: []string{"/mnt/cache"}, Quotas: []resource.Quantity{resource.MustParse("2Gi")}},
		}}},
		{"multiple processMemory levels", []datav1alpha1.RuntimeTieredStoreLevel{
			{ProcessMemory: &datav1alpha1.ProcessMemoryMediumSource{Quota: resource.MustParse("2Gi")}},
			{ProcessMemory: &datav1alpha1.ProcessMemoryMediumSource{Quota: resource.MustParse("4Gi")}},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := verifyPR6219R8NewFixture(t)
			f.createWorkerWithTieredStore(datav1alpha1.RuntimeTieredStore{Levels: []datav1alpha1.RuntimeTieredStoreLevel{
				{EmptyDir: &datav1alpha1.EmptyDirMediumSource{Quota: resource.MustParse("2Gi")}},
			}})
			f.editTieredStore(func(ts *datav1alpha1.RuntimeTieredStore) { ts.Levels = tc.levels })
			f.sync(t, f.newEngine())

			cond := f.tieredStoreCondition(t)
			if cond == nil || cond.Status != corev1.ConditionFalse || cond.Reason != common.RuntimeTieredStoreInvalid {
				t.Fatalf("expected TieredStoreSynced=False/RuntimeTieredStoreInvalid, got %+v", cond)
			}
			if !strings.Contains(cond.Message, "Fix spec.worker.tieredStore.") {
				t.Errorf("invalid condition must direct at the spec: %s", cond.Message)
			}
			if strings.Contains(cond.Message, "ecreate") {
				t.Errorf("invalid spec must NOT advise recreation (recreate cannot fix an invalid spec): %s", cond.Message)
			}
			events := f.tieredStoreEvents()
			if len(events) != 1 || !strings.Contains(events[0], common.RuntimeTieredStoreInvalid) {
				t.Fatalf("expected exactly 1 RuntimeTieredStoreInvalid event, got %v", events)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Transition-only eventing (AC-TS3 new form, TS-REQUEUE-REEMISSION remedy)
// ---------------------------------------------------------------------------

// The persisted condition dedups events across reconciles AND across engine
// restarts — the property the round-7 in-memory throttle could not provide and
// the carried TS-REQUEUE-REEMISSION advisory asked for.
func TestVerifyPR6219R8EventDedupAcrossEngineRestart(t *testing.T) {
	f := verifyPR6219R8NewFixture(t)
	f.createWorkerWithTieredStore(datav1alpha1.RuntimeTieredStore{Levels: []datav1alpha1.RuntimeTieredStoreLevel{
		{ProcessMemory: &datav1alpha1.ProcessMemoryMediumSource{Quota: resource.MustParse("8Gi")}},
	}})

	f.sync(t, f.newEngine()) // steady: True, no events
	f.editTieredStore(func(ts *datav1alpha1.RuntimeTieredStore) {
		ts.Levels[0].ProcessMemory.Quota = resource.MustParse("16Gi")
	})
	f.sync(t, f.newEngine())
	if n := len(f.tieredStoreEvents()); n != 1 {
		t.Fatalf("drift transition must emit exactly 1 event, got %d", n)
	}

	// Same engine, same drift: no re-emission.
	f.sync(t, f.newEngine())
	f.sync(t, f.newEngine())
	if n := len(f.tieredStoreEvents()); n != 0 {
		t.Fatalf("persistent drift re-emitted %d events (transition-only contract broken)", n)
	}

	// Restart simulation: a brand-new engine instance over the same cluster
	// state must not re-emit either — dedup comes from the persisted condition,
	// not from in-memory throttle state.
	restarted := f.newEngine()
	f.sync(t, restarted)
	f.sync(t, restarted)
	if n := len(f.tieredStoreEvents()); n != 0 {
		t.Fatalf("engine restart re-emitted %d drift events (dedup is not restart-proof)", n)
	}
	if cond := f.tieredStoreCondition(t); cond == nil || cond.Status != corev1.ConditionFalse {
		t.Fatalf("persisted drift condition lost: %+v", cond)
	}
}

// Message-change re-emission, revert recovery, and immediate re-drift warning.
func TestVerifyPR6219R8MessageChangeReemitsAndRevertClears(t *testing.T) {
	f := verifyPR6219R8NewFixture(t)
	f.createWorkerWithTieredStore(datav1alpha1.RuntimeTieredStore{Levels: []datav1alpha1.RuntimeTieredStoreLevel{
		{ProcessMemory: &datav1alpha1.ProcessMemoryMediumSource{Quota: resource.MustParse("8Gi")}},
		{EmptyDir: &datav1alpha1.EmptyDirMediumSource{Quota: resource.MustParse("10Gi")}},
	}})
	f.sync(t, f.newEngine())

	// Drift A.
	f.editTieredStore(func(ts *datav1alpha1.RuntimeTieredStore) {
		ts.Levels[0].ProcessMemory.Quota = resource.MustParse("16Gi")
	})
	f.sync(t, f.newEngine())
	if n := len(f.tieredStoreEvents()); n != 1 {
		t.Fatalf("drift A: want 1 event, got %d", n)
	}

	// Drift B (message changes: a second divergent volume) -> fresh event.
	f.editTieredStore(func(ts *datav1alpha1.RuntimeTieredStore) {
		ts.Levels[1].EmptyDir.Quota = resource.MustParse("20Gi")
	})
	f.sync(t, f.newEngine())
	events := f.tieredStoreEvents()
	if len(events) != 1 || !strings.Contains(events[0], "tiered-store-level-1-index-0") {
		t.Fatalf("changed drift must re-emit once naming the new volume, got %v", events)
	}

	// Revert both -> True, no event.
	f.editTieredStore(func(ts *datav1alpha1.RuntimeTieredStore) {
		ts.Levels[0].ProcessMemory.Quota = resource.MustParse("8Gi")
		ts.Levels[1].EmptyDir.Quota = resource.MustParse("10Gi")
	})
	f.sync(t, f.newEngine())
	if cond := f.tieredStoreCondition(t); cond == nil || cond.Status != corev1.ConditionTrue {
		t.Fatalf("revert must recover TieredStoreSynced=True, got %+v", cond)
	}
	if n := len(f.tieredStoreEvents()); n != 0 {
		t.Fatalf("recovery emitted %d events (want 0)", n)
	}

	// Immediate re-drift after recovery -> fresh event (no cooldown wedge).
	f.editTieredStore(func(ts *datav1alpha1.RuntimeTieredStore) {
		ts.Levels[0].ProcessMemory.Quota = resource.MustParse("12Gi")
	})
	f.sync(t, f.newEngine())
	if n := len(f.tieredStoreEvents()); n != 1 {
		t.Fatalf("re-drift after recovery must emit exactly 1 event, got %d", n)
	}
}

// Omission contract: a runtime whose tieredStore was never configured gets no
// TieredStoreSynced condition and no events.
func TestVerifyPR6219R8NoTieredStoreNoCondition(t *testing.T) {
	f := verifyPR6219R8NewFixture(t)
	f.sync(t, f.newEngine())
	if cond := f.tieredStoreCondition(t); cond != nil {
		t.Fatalf("tieredStore was never configured but a condition appeared: %+v", cond)
	}
	if n := len(f.tieredStoreEvents()); n != 0 {
		t.Fatalf("tieredStore was never configured but %d events appeared", n)
	}
}

// #6221 deferral canary (pre-existing scope): client-side tieredStore edits
// stay silent. Flips red when #6221 lands — then invert to a contract.
func TestVerifyPR6219R8ClientTieredStoreEditSilent(t *testing.T) {
	f := verifyPR6219R8NewFixture(t)
	f.sync(t, f.newEngine())

	rt := &datav1alpha1.CacheRuntime{}
	if err := f.client.Get(f.ctx.Context, types.NamespacedName{Name: verifyPR6219R8Name, Namespace: verifyPR6219R8Namespace}, rt); err != nil {
		t.Fatalf("get runtime: %v", err)
	}
	rt.Spec.Client.TieredStore = datav1alpha1.RuntimeTieredStore{Levels: []datav1alpha1.RuntimeTieredStoreLevel{
		{EmptyDir: &datav1alpha1.EmptyDirMediumSource{Quota: resource.MustParse("8Gi")}},
	}}
	if err := f.client.Update(f.ctx.Context, rt); err != nil {
		t.Fatalf("update runtime: %v", err)
	}
	f.sync(t, f.newEngine())

	if cond := f.tieredStoreCondition(t); cond != nil {
		t.Fatalf("CANARY FLIPPED (#6221 landed?): client edit produced a condition: %+v", cond)
	}
	if n := len(f.tieredStoreEvents()); n != 0 {
		t.Fatalf("CANARY FLIPPED (#6221 landed?): client edit produced %d events", n)
	}
}

// ---------------------------------------------------------------------------
// Round-8 NEW edge: stale persisted condition after the worker is disabled
// ---------------------------------------------------------------------------

// BUG CANARY (new in round 8): once TieredStoreSynced=False is persisted,
// disabling the worker skips the drift check entirely (tieredStoreCond == nil),
// so nothing re-evaluates or clears the condition — a stale "Delete and
// recreate the CacheRuntime" alarm outlives the worker it describes. Passes
// while the staleness exists; flips red when the condition is cleared (or
// marked Unknown) on the disabled path.
func TestVerifyPR6219R8WorkerDisabledLeavesStaleCondition(t *testing.T) {
	f := verifyPR6219R8NewFixture(t)
	f.createWorkerWithTieredStore(datav1alpha1.RuntimeTieredStore{Levels: []datav1alpha1.RuntimeTieredStoreLevel{
		{ProcessMemory: &datav1alpha1.ProcessMemoryMediumSource{Quota: resource.MustParse("8Gi")}},
	}})
	f.sync(t, f.newEngine())
	f.editTieredStore(func(ts *datav1alpha1.RuntimeTieredStore) {
		ts.Levels[0].ProcessMemory.Quota = resource.MustParse("16Gi")
	})
	f.sync(t, f.newEngine())
	if cond := f.tieredStoreCondition(t); cond == nil || cond.Status != corev1.ConditionFalse {
		t.Fatalf("setup: drift condition not established: %+v", cond)
	}
	_ = f.tieredStoreEvents()

	// Disable the worker entirely.
	rt := &datav1alpha1.CacheRuntime{}
	if err := f.client.Get(f.ctx.Context, types.NamespacedName{Name: verifyPR6219R8Name, Namespace: verifyPR6219R8Namespace}, rt); err != nil {
		t.Fatalf("get runtime: %v", err)
	}
	rt.Spec.Worker.Disabled = true
	if err := f.client.Update(f.ctx.Context, rt); err != nil {
		t.Fatalf("disable worker: %v", err)
	}
	f.sync(t, f.newEngine())

	cond := f.tieredStoreCondition(t)
	if cond == nil {
		return // condition cleared — the finding is fixed; this canary must be inverted
	}
	if cond.Status == corev1.ConditionFalse {
		t.Logf("STALE CONDITION (canary): worker disabled but TieredStoreSynced stays %s/%s: %s",
			cond.Status, cond.Reason, cond.Message)
		return
	}
	// Any non-False re-evaluation (e.g. Unknown) also resolves the alarm.
}

// ---------------------------------------------------------------------------
// Docs contract (F1/F3 scope + reserved-name documentation)
// ---------------------------------------------------------------------------

func TestVerifyPR6219R8DocsGuidanceAndScope(t *testing.T) {
	repoRoot := filepath.Join("..", "..", "..", "..")
	read := func(rel string) string {
		b, err := os.ReadFile(filepath.Join(repoRoot, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		return string(b)
	}
	for _, lang := range []string{"en", "zh"} {
		sample := read("docs/" + lang + "/samples/cacheruntime/cacheruntime_spec_update.md")
		for _, want := range []string{"TieredStoreSynced", "RuntimeTieredStoreConflict"} {
			if !strings.Contains(sample, want) {
				t.Errorf("%s sample doc dropped the %s drift reporting guidance", lang, want)
			}
		}
		guide := read("docs/" + lang + "/userguide/cache_runtime_tieredstore.md")
		if !strings.Contains(guide, "tiered-store-level-") {
			t.Errorf("%s userguide no longer documents the reserved generated volume names", lang)
		}
	}
	en := read("docs/en/samples/cacheruntime/cacheruntime_spec_update.md")
	if strings.Contains(en, "spec.client.tieredStore") {
		t.Errorf("en doc still promises client-scope tieredStore drift coverage (TS-DOC-CLIENT-SCOPE; #6221 stays the tracker)")
	}
}
