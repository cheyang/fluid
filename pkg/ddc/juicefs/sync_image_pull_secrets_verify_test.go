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

// Verification harness for PR #6245 round 3 (head d7e316803).
// Reviewer: Codex (Reviewer B). Additive only - production code untouched.
//
// Round 3 added imagePullSecrets propagation to SyncRuntime
// (checkAndSetWorkerChanges / checkAndSetFuseChanges + the caller's
// globalSecretsChanged bookkeeping). The PR's own test covers the leaf
// functions; this harness drives the real entry point SyncRuntime with a fake
// client and asserts end-to-end convergence:
//
//	S1 (contract): adding spec.imagePullSecrets to an existing runtime updates
//	   the worker sts + fuse ds pod secrets, bumps the fuse generation, records
//	   the new value in the helm-values configmap, and a second SyncRuntime is
//	   a no-op (idempotent).
//	S2 (contract): with a worker override in place, changing only the runtime-wide
//	   secrets leaves the worker pod untouched and updates only the fuse.
//	S3 (contract): removing spec.imagePullSecrets entirely clears the pod secrets.
//	F-worker-lost-update-missing-scriptcm (contract, RED on head d7e316803):
//	   when the worker-script configmap is absent, syncWorkerSpec early-returns
//	   AFTER checkAndSetWorkerChanges flagged the change but BEFORE the sts
//	   update, while SyncRuntime still persists the mutated values configmap -
//	   the recorded state permanently diverges from the actual sts (a second
//	   reconcile detects nothing). Pre-existing structure inherited by the new
//	   field.
package juicefs

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/net"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/ddc/base"
	"github.com/fluid-cloudnative/fluid/pkg/ddc/base/portallocator"
	cruntime "github.com/fluid-cloudnative/fluid/pkg/runtime"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
)

type verifySyncFixture struct {
	engine *JuiceFSEngine
	client client.Client
}

// buildVerifySyncEngine builds a fake-client engine with every object
// SyncRuntime touches: runtime CR, dataset+secret (transform), helm-values
// configmap (oldValue), worker sts (OnDelete), fuse ds (OnDelete, generation 1),
// fuse-script cm, worker-script cm (unless omitWorkerScriptCM), and the PVC the
// fuse generation bump patches. Worker/fuse script cms are seeded with the
// commands the transform generates so only the secrets diff drives the sync.
func buildVerifySyncEngine(t *testing.T, name, namespace string, spec datav1alpha1.JuiceFSRuntimeSpec, oldValue *JuiceFS, omitWorkerScriptCM bool) *verifySyncFixture {
	t.Helper()
	if err := os.Unsetenv(common.EnvImagePullSecretsKey); err != nil {
		t.Fatalf("unsetenv: %v", err)
	}

	rt := &datav1alpha1.JuiceFSRuntime{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}, Spec: spec}
	dataset := &datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: datav1alpha1.DatasetSpec{Mounts: []datav1alpha1.Mount{{
			MountPoint: "juicefs:///mnt/test",
			Name:       "test",
			EncryptOptions: []datav1alpha1.EncryptOption{{
				Name: "metaurl",
				ValueFrom: datav1alpha1.EncryptOptionSource{
					SecretKeyRef: datav1alpha1.SecretKeySelector{Name: name, Key: "metaurl"},
				},
			}},
		}}},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Data:       map[string][]byte{"metaurl": []byte("redis://127.0.0.1:6379/0")},
	}

	s := runtime.NewScheme()
	_ = datav1alpha1.AddToScheme(s)
	_ = appsv1.AddToScheme(s)
	_ = corev1.AddToScheme(s)

	c := fake.NewFakeClientWithScheme(s, rt, dataset, secret)
	runtimeInfo, err := base.BuildRuntimeInfo(name, namespace, "juicefs")
	if err != nil {
		t.Fatalf("BuildRuntimeInfo: %v", err)
	}
	engine := &JuiceFSEngine{
		name:        name,
		namespace:   namespace,
		Client:      c,
		Log:         fake.NullLogger(),
		runtime:     rt,
		runtimeInfo: runtimeInfo,
		engineImpl:  "juicefs",
	}

	pr := net.ParsePortRangeOrDie("14000-15999")
	if err := portallocator.SetupRuntimePortAllocator(nil, pr, "bitmap", dummy); err != nil {
		t.Fatalf("port allocator: %v", err)
	}

	latestValue, err := engine.transform(rt)
	if err != nil {
		t.Fatalf("transform: %v", err)
	}

	oldBytes, err := yaml.Marshal(oldValue)
	if err != nil {
		t.Fatalf("marshal old value: %v", err)
	}
	valuesCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: engine.getHelmValuesConfigMapName(), Namespace: namespace},
		Data:       map[string]string{"data": string(oldBytes)},
	}

	oldWorkerSecrets := effectiveImagePullSecrets(oldValue.Worker.ImagePullSecrets, oldValue.ImagePullSecrets)
	oldFuseSecrets := effectiveImagePullSecrets(oldValue.Fuse.ImagePullSecrets, oldValue.ImagePullSecrets)

	workerSTS := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: engine.getWorkerName(), Namespace: namespace},
		Spec: appsv1.StatefulSetSpec{
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{Type: appsv1.OnDeleteStatefulSetStrategyType},
			Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
				ImagePullSecrets: oldWorkerSecrets,
				Containers:       []corev1.Container{{Name: JuiceFSWorkerContainerName}},
			}},
		},
	}
	fuseDS := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: engine.getFuseName(), Namespace: namespace},
		Spec: appsv1.DaemonSetSpec{
			UpdateStrategy: appsv1.DaemonSetUpdateStrategy{Type: appsv1.OnDeleteDaemonSetStrategyType},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{common.LabelRuntimeFuseGeneration: "1"}},
				Spec: corev1.PodSpec{
					ImagePullSecrets: oldFuseSecrets,
					Containers:       []corev1.Container{{Name: JuiceFSFuseContainerName}},
				},
			},
		},
	}
	fuseScriptCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: engine.getFuseScriptName(), Namespace: namespace},
		Data:       map[string]string{"script.sh": "#!/bin/bash\n\n" + latestValue.Fuse.Command},
	}
	pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}

	objs := []runtime.Object{valuesCM, workerSTS, fuseDS, fuseScriptCM, pvc}
	if !omitWorkerScriptCM {
		objs = append(objs, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: engine.getWorkerScriptName(), Namespace: namespace},
			Data:       map[string]string{"script.sh": "#!/bin/bash\n\n" + latestValue.Worker.Command},
		})
	}
	for _, o := range objs {
		if err := c.Create(context.TODO(), o.(client.Object)); err != nil {
			t.Fatalf("seed %T: %v", o, err)
		}
	}
	return &verifySyncFixture{engine: engine, client: c}
}

func refs(names ...string) []corev1.LocalObjectReference {
	var out []corev1.LocalObjectReference
	for _, n := range names {
		out = append(out, corev1.LocalObjectReference{Name: n})
	}
	return out
}

func (f *verifySyncFixture) workerPodSecrets(t *testing.T) []corev1.LocalObjectReference {
	t.Helper()
	sts := &appsv1.StatefulSet{}
	if err := f.client.Get(context.TODO(), types.NamespacedName{Name: f.engine.getWorkerName(), Namespace: f.engine.namespace}, sts); err != nil {
		t.Fatalf("get worker sts: %v", err)
	}
	return sts.Spec.Template.Spec.ImagePullSecrets
}

func (f *verifySyncFixture) fusePodSecrets(t *testing.T) []corev1.LocalObjectReference {
	t.Helper()
	ds := &appsv1.DaemonSet{}
	if err := f.client.Get(context.TODO(), types.NamespacedName{Name: f.engine.getFuseName(), Namespace: f.engine.namespace}, ds); err != nil {
		t.Fatalf("get fuse ds: %v", err)
	}
	return ds.Spec.Template.Spec.ImagePullSecrets
}

func (f *verifySyncFixture) fuseGeneration(t *testing.T) string {
	t.Helper()
	ds := &appsv1.DaemonSet{}
	if err := f.client.Get(context.TODO(), types.NamespacedName{Name: f.engine.getFuseName(), Namespace: f.engine.namespace}, ds); err != nil {
		t.Fatalf("get fuse ds: %v", err)
	}
	return ds.Spec.Template.Labels[common.LabelRuntimeFuseGeneration]
}

func (f *verifySyncFixture) recordedValue(t *testing.T) *JuiceFS {
	t.Helper()
	v, err := f.engine.GetValueFromConfigmap()
	if err != nil {
		t.Fatalf("GetValueFromConfigmap: %v", err)
	}
	return v
}

// latestValue re-runs the engine transform for the current CR state (used to
// rebuild fixtures that must match what a fresh SyncRuntime would compute).
func (f *verifySyncFixture) latestValue(t *testing.T) *JuiceFS {
	t.Helper()
	rt := &datav1alpha1.JuiceFSRuntime{}
	if err := f.client.Get(context.TODO(), types.NamespacedName{Name: f.engine.name, Namespace: f.engine.namespace}, rt); err != nil {
		t.Fatalf("get runtime: %v", err)
	}
	v, err := f.engine.transform(rt)
	if err != nil {
		t.Fatalf("transform: %v", err)
	}
	return v
}

func TestVerifyPr6245SyncRuntimePropagatesSecrets(t *testing.T) {
	old := constructBaseRuntimeValue()
	fx := buildVerifySyncEngine(t, "verifysync", "default", datav1alpha1.JuiceFSRuntimeSpec{
		ImagePullSecrets: refs("reg-a"),
	}, old, false)

	changed, err := fx.engine.SyncRuntime(cruntime.ReconcileRequestContext{})
	if err != nil {
		t.Fatalf("SyncRuntime: %v", err)
	}
	if !changed {
		t.Fatalf("SyncRuntime changed = false, want true after adding spec.imagePullSecrets")
	}
	if got := fx.workerPodSecrets(t); !refsEqual(got, refs("reg-a")) {
		t.Errorf("worker pod secrets = %v, want [reg-a]", got)
	}
	if got := fx.fusePodSecrets(t); !refsEqual(got, refs("reg-a")) {
		t.Errorf("fuse pod secrets = %v, want [reg-a]", got)
	}
	if got := fx.fuseGeneration(t); got != "2" {
		t.Errorf("fuse generation = %q, want \"2\" (bumped so fuse pods pick up the new pull secrets)", got)
	}
	recorded := fx.recordedValue(t)
	if !refsEqual(recorded.ImagePullSecrets, refs("reg-a")) {
		t.Errorf("recorded global secrets = %v, want [reg-a]", recorded.ImagePullSecrets)
	}

	// idempotency: a second pass must be a no-op
	changed2, err := fx.engine.SyncRuntime(cruntime.ReconcileRequestContext{})
	if err != nil {
		t.Fatalf("second SyncRuntime: %v", err)
	}
	if changed2 {
		t.Errorf("second SyncRuntime changed = true, want false (converged)")
	}
	if got := fx.fuseGeneration(t); got != "2" {
		t.Errorf("fuse generation after second pass = %q, want \"2\" (no double bump)", got)
	}
}

func TestVerifyPr6245SyncRuntimeOverrideShieldsComponent(t *testing.T) {
	old := constructBaseRuntimeValue()
	old.ImagePullSecrets = refs("g1")
	old.Worker.ImagePullSecrets = refs("w")
	fx := buildVerifySyncEngine(t, "verifyshield", "default", datav1alpha1.JuiceFSRuntimeSpec{
		ImagePullSecrets: refs("g2"),
		Worker:           datav1alpha1.JuiceFSCompTemplateSpec{ImagePullSecrets: refs("w")},
	}, old, false)

	if _, err := fx.engine.SyncRuntime(cruntime.ReconcileRequestContext{}); err != nil {
		t.Fatalf("SyncRuntime: %v", err)
	}
	if got := fx.workerPodSecrets(t); !refsEqual(got, refs("w")) {
		t.Errorf("worker pod secrets = %v, want [w] (override shields the worker from the global change)", got)
	}
	if got := fx.fusePodSecrets(t); !refsEqual(got, refs("g2")) {
		t.Errorf("fuse pod secrets = %v, want [g2]", got)
	}
	recorded := fx.recordedValue(t)
	if !refsEqual(recorded.ImagePullSecrets, refs("g2")) || !refsEqual(recorded.Worker.ImagePullSecrets, refs("w")) {
		t.Errorf("recorded value global=%v worker=%v, want global=[g2] worker=[w]", recorded.ImagePullSecrets, recorded.Worker.ImagePullSecrets)
	}
}

func TestVerifyPr6245SyncRuntimeSecretsRemoved(t *testing.T) {
	old := constructBaseRuntimeValue()
	old.ImagePullSecrets = refs("g")
	fx := buildVerifySyncEngine(t, "verifyremove", "default", datav1alpha1.JuiceFSRuntimeSpec{}, old, false)

	changed, err := fx.engine.SyncRuntime(cruntime.ReconcileRequestContext{})
	if err != nil {
		t.Fatalf("SyncRuntime: %v", err)
	}
	if !changed {
		t.Fatalf("SyncRuntime changed = false, want true after removing spec.imagePullSecrets")
	}
	if got := fx.workerPodSecrets(t); len(got) != 0 {
		t.Errorf("worker pod secrets = %v, want empty", got)
	}
	if got := fx.fusePodSecrets(t); len(got) != 0 {
		t.Errorf("fuse pod secrets = %v, want empty", got)
	}
	if got := fx.recordedValue(t).ImagePullSecrets; len(got) != 0 {
		t.Errorf("recorded global secrets = %v, want empty", got)
	}
}

// Contract test for the lost-update path (round 3 finding
// F-worker-lost-update-missing-scriptcm), re-scoped in round 4: head
// be3337dd00 fixes the permanence by comparing desired secrets against the
// LIVE pod template instead of the recorded value. The accepted contract is
// no longer "apply in one pass or surface an error" but "the drift is never
// lost": while the worker-script configmap is missing the worker sts cannot be
// updated, yet every SyncRuntime must keep reporting the drift (changed=true,
// so the reconcile retries), and once the blocker clears the worker must
// converge - the recorded value leading the live template by one pass is
// harmless bookkeeping because diffing no longer trusts it.
//
// RED on previous head d7e316803: there pass 2 detects nothing (recorded value
// already matches the spec) and pass 3 leaves the worker on [g1] forever.
func TestVerifyPr6245WorkerSecretsRetryAndConvergeWhenScriptConfigmapMissing(t *testing.T) {
	old := constructBaseRuntimeValue()
	old.ImagePullSecrets = refs("g1")
	fx := buildVerifySyncEngine(t, "verifylost", "default", datav1alpha1.JuiceFSRuntimeSpec{
		ImagePullSecrets: refs("g2"),
	}, old, true)

	// pass 1: worker update blocked by the missing script configmap
	changed, err := fx.engine.SyncRuntime(cruntime.ReconcileRequestContext{})
	if err != nil {
		t.Fatalf("SyncRuntime: %v", err)
	}
	if !changed {
		t.Errorf("pass 1: changed = false, want true (fuse synced, worker drift pending)")
	}
	if got := fx.workerPodSecrets(t); !refsEqual(got, refs("g1")) {
		t.Fatalf("pass 1: worker pod secrets = %v, want [g1] (script cm missing blocks the sts update)", got)
	}

	// pass 2: blocker still present - the drift must still be detected (the
	// pre-fix code reported changed=false here and never retried)
	changed, err = fx.engine.SyncRuntime(cruntime.ReconcileRequestContext{})
	if err != nil {
		t.Fatalf("pass 2 SyncRuntime: %v", err)
	}
	if !changed {
		t.Fatalf("pass 2: changed = false - the pending worker secrets update was LOST " +
			"(recorded value masks the live-template drift); want true (keeps retrying)")
	}
	if got := fx.workerPodSecrets(t); !refsEqual(got, refs("g1")) {
		t.Fatalf("pass 2: worker pod secrets = %v, want [g1] (still blocked)", got)
	}

	// blocker cleared: restore the worker-script configmap, the next pass must
	// converge the worker
	lv := fx.latestValue(t)
	scriptCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: fx.engine.getWorkerScriptName(), Namespace: "default"},
		Data:       map[string]string{"script.sh": "#!/bin/bash\n\n" + lv.Worker.Command},
	}
	if err := fx.client.Create(context.TODO(), scriptCM); err != nil {
		t.Fatalf("restore worker script cm: %v", err)
	}
	if _, err := fx.engine.SyncRuntime(cruntime.ReconcileRequestContext{}); err != nil {
		t.Fatalf("pass 3 SyncRuntime: %v", err)
	}
	if got := fx.workerPodSecrets(t); !refsEqual(got, refs("g2")) {
		t.Fatalf("pass 3: worker pod secrets = %v, want [g2] (must converge once the blocker clears)", got)
	}

	// pass 4: converged, no-op
	changed, err = fx.engine.SyncRuntime(cruntime.ReconcileRequestContext{})
	if err != nil {
		t.Fatalf("pass 4 SyncRuntime: %v", err)
	}
	if changed {
		t.Errorf("pass 4: changed = true, want false (converged)")
	}
}

// Contract test for round 3 finding F-sync-strategy-flip-advance: when the
// worker StatefulSet has a non-OnDelete update strategy (abnormal - the chart
// ships OnDelete), syncWorkerSpec flips the strategy and skips the template
// sync for that pass while the recorded global value advances. The worker
// must catch up on the NEXT reconcile (triggered by the strategy update
// event) because the diff is taken against the live template.
//
// RED on previous head d7e316803: the second pass compared the recorded value
// against the spec, saw no diff, and left the worker on [a] permanently.
func TestVerifyPr6245WorkerCatchesUpAfterStrategyFlip(t *testing.T) {
	old := constructBaseRuntimeValue()
	old.ImagePullSecrets = refs("a")
	fx := buildVerifySyncEngine(t, "verifyflip", "default", datav1alpha1.JuiceFSRuntimeSpec{
		ImagePullSecrets: refs("b"),
	}, old, false)

	// abnormal state: worker sts on RollingUpdate
	sts := &appsv1.StatefulSet{}
	if err := fx.client.Get(context.TODO(), types.NamespacedName{Name: fx.engine.getWorkerName(), Namespace: "default"}, sts); err != nil {
		t.Fatalf("get worker sts: %v", err)
	}
	sts.Spec.UpdateStrategy = appsv1.StatefulSetUpdateStrategy{Type: appsv1.RollingUpdateStatefulSetStrategyType}
	if err := fx.client.Update(context.TODO(), sts); err != nil {
		t.Fatalf("flip worker sts strategy: %v", err)
	}

	// pass 1: strategy flipped to OnDelete, worker template not synced this pass
	if _, err := fx.engine.SyncRuntime(cruntime.ReconcileRequestContext{}); err != nil {
		t.Fatalf("pass 1 SyncRuntime: %v", err)
	}
	if got := fx.workerPodSecrets(t); !refsEqual(got, refs("a")) {
		t.Fatalf("pass 1: worker pod secrets = %v, want [a] (sync deferred by the strategy flip)", got)
	}

	// pass 2 (the reconcile the strategy update event triggers): worker must converge
	if _, err := fx.engine.SyncRuntime(cruntime.ReconcileRequestContext{}); err != nil {
		t.Fatalf("pass 2 SyncRuntime: %v", err)
	}
	if got := fx.workerPodSecrets(t); !refsEqual(got, refs("b")) {
		t.Fatalf("pass 2: worker pod secrets = %v, want [b] (catch-up via the live-template diff)", got)
	}
}

// Contract test for round 3 finding F-sync-override-not-recorded: rotating the
// runtime-wide secrets a->b while adding a worker override pinned to a keeps
// the worker on a (no effective change), and the override must still be
// recorded - so that a later override removal falls the worker back to b.
//
// RED on previous head d7e316803: the override was only persisted when the
// effective list changed, so pass 1 recorded no worker override and pass 2
// left the worker on [a] permanently.
func TestVerifyPr6245RotationThenOverrideRemovalConverges(t *testing.T) {
	old := constructBaseRuntimeValue()
	old.ImagePullSecrets = refs("a")
	fx := buildVerifySyncEngine(t, "verifyrot", "default", datav1alpha1.JuiceFSRuntimeSpec{
		ImagePullSecrets: refs("b"),
		Worker:           datav1alpha1.JuiceFSCompTemplateSpec{ImagePullSecrets: refs("a")},
	}, old, false)

	// pass 1: effective worker list unchanged (override a == old global a)
	if _, err := fx.engine.SyncRuntime(cruntime.ReconcileRequestContext{}); err != nil {
		t.Fatalf("pass 1 SyncRuntime: %v", err)
	}
	if got := fx.workerPodSecrets(t); !refsEqual(got, refs("a")) {
		t.Errorf("pass 1: worker pod secrets = %v, want [a] (override shields the worker)", got)
	}
	if got := fx.fusePodSecrets(t); !refsEqual(got, refs("b")) {
		t.Errorf("pass 1: fuse pod secrets = %v, want [b]", got)
	}
	recorded := fx.recordedValue(t)
	if !refsEqual(recorded.Worker.ImagePullSecrets, refs("a")) {
		t.Fatalf("pass 1: recorded worker override = %v, want [a] - the override edit was LOST "+
			"(only effective-list changes were persisted)", recorded.Worker.ImagePullSecrets)
	}
	if !refsEqual(recorded.ImagePullSecrets, refs("b")) {
		t.Errorf("pass 1: recorded global secrets = %v, want [b]", recorded.ImagePullSecrets)
	}

	// remove the override; the worker must fall back to the global b
	rt := &datav1alpha1.JuiceFSRuntime{}
	if err := fx.client.Get(context.TODO(), types.NamespacedName{Name: "verifyrot", Namespace: "default"}, rt); err != nil {
		t.Fatalf("get runtime: %v", err)
	}
	rt.Spec.Worker.ImagePullSecrets = nil
	if err := fx.client.Update(context.TODO(), rt); err != nil {
		t.Fatalf("remove worker override: %v", err)
	}
	if _, err := fx.engine.SyncRuntime(cruntime.ReconcileRequestContext{}); err != nil {
		t.Fatalf("pass 2 SyncRuntime: %v", err)
	}
	if got := fx.workerPodSecrets(t); !refsEqual(got, refs("b")) {
		t.Fatalf("pass 2: worker pod secrets = %v, want [b] (override removed, fall back to the global list)", got)
	}
	if got := fx.recordedValue(t).Worker.ImagePullSecrets; len(got) != 0 {
		t.Errorf("pass 2: recorded worker override = %v, want empty", got)
	}
}

func refsEqual(a, b []corev1.LocalObjectReference) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Contract: user-facing sample docs must not claim imagePullSecrets changes
// need a runtime recreate - round 3 wires them into SyncRuntime, so they
// propagate on reconcile (pods pick them up when recreated under OnDelete).
func TestVerifyPr6245DocsFreshness(t *testing.T) {
	root := pr6245RepoRoot(t)
	script := filepath.Join(root, "docs/verification/juicefs-imagepullsecrets-codex/scripts/docs_staleness_check.sh")
	cmd := exec.Command("bash", script, root)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("docs_staleness_check.sh failed: %v\n%s", err, out)
	}
}

// ---------------------------------------------------------------------------
// Round 5 (head 2150b49ef): F-docs-fuse-restart-overstated.
//
// The round-5 docs commit replaced the stale "needs the runtime to be
// recreated" sentence with "the fuse pods are restarted" (en) /
// "fuse pod 会重启" (zh). That overstates what the sync path does on the
// default configuration:
//
//   - charts/juicefs renders the fuse DaemonSet with updateStrategy OnDelete
//     (values.yaml fuse.updateStrategy.type), and syncFuseSpec forcibly flips
//     any other strategy back to OnDelete before touching the template
//     (sync_runtime.go, early return after UpdateDaemonSetUpdateStrategy).
//     With OnDelete, the API server never restarts running pods on a template
//     update - the new secrets reach a fuse pod only when that pod is
//     recreated.
//   - The fuse generation bump is not a rollout trigger either: the label is
//     consumed only by the CSI node server's NodeUnstage lazy clean
//     (nodeserver.go getCleanFuseFunc / checkIfFuseNeedUpdate), and only under
//     cleanPolicy OnFuseChanged or OnDemand. SetupFuseCleanPolicy maps the
//     zero value (NoneCleanPolicy, what a JuiceFSRuntime gets when
//     spec.fuse.cleanPolicy is unset) to OnRuntimeDeleted, under which the CSI
//     never cleans fuse pods eagerly.
//
// So on a default runtime, changing imagePullSecrets updates the fuse
// DaemonSet template and bumps the generation, but the running fuse pods keep
// the old secrets until they are recreated for another reason - exactly like
// the worker pods the same sentence describes correctly.

// TestVerifyPr6245FuseSyncNeverRestartsRunningPods pins the mechanism (GREEN
// on head): the default fuse clean policy is OnRuntimeDeleted and the sync
// path enforces an OnDelete fuse DaemonSet, so no sync pass can restart a
// running fuse pod.
func TestVerifyPr6245FuseSyncNeverRestartsRunningPods(t *testing.T) {
	// Part A: a JuiceFSRuntime that leaves spec.fuse.cleanPolicy unset gets
	// OnRuntimeDeleted (this is the exact call base.GetRuntimeInfo makes in
	// pkg/ddc/base/runtime.go). Under OnRuntimeDeleted the CSI node server
	// never cleans fuse pods on a generation change, so the generation bump
	// SyncRuntime performs cannot restart fuse pods.
	rt := &datav1alpha1.JuiceFSRuntime{}
	if rt.Spec.Fuse.CleanPolicy != datav1alpha1.NoneCleanPolicy {
		t.Fatalf("zero-value JuiceFSRuntime spec.fuse.cleanPolicy = %q, want NoneCleanPolicy", rt.Spec.Fuse.CleanPolicy)
	}
	info, err := base.BuildRuntimeInfo("verifypolicy", "default", "juicefs")
	if err != nil {
		t.Fatalf("BuildRuntimeInfo: %v", err)
	}
	info.SetupFuseCleanPolicy(rt.Spec.Fuse.CleanPolicy)
	if got := info.GetFuseCleanPolicy(); got != datav1alpha1.OnRuntimeDeletedCleanPolicy {
		t.Errorf("default fuse clean policy = %q, want %q (OnRuntimeDeleted = CSI never cleans fuse pods eagerly)", got, datav1alpha1.OnRuntimeDeletedCleanPolicy)
	}

	// Part B: even when the fuse DaemonSet starts out RollingUpdate, the sync
	// path flips it to OnDelete before applying template changes, and the
	// secrets change then lands on the template only - running pods are never
	// restarted by a template update under OnDelete.
	fx := buildVerifySyncEngine(t, "verifyfuseroll", "default", datav1alpha1.JuiceFSRuntimeSpec{
		ImagePullSecrets: refs("reg-a"),
	}, constructBaseRuntimeValue(), false)
	dsKey := types.NamespacedName{Name: fx.engine.getFuseName(), Namespace: fx.engine.namespace}
	ds := &appsv1.DaemonSet{}
	if err := fx.client.Get(context.TODO(), dsKey, ds); err != nil {
		t.Fatalf("get fuse ds: %v", err)
	}
	ds.Spec.UpdateStrategy.Type = appsv1.RollingUpdateDaemonSetStrategyType
	if err := fx.client.Update(context.TODO(), ds); err != nil {
		t.Fatalf("set fuse ds strategy RollingUpdate: %v", err)
	}

	// Pass 1 flips the strategy and defers the template sync (early return);
	// pass 2 applies the template change under the enforced OnDelete strategy.
	for pass := 1; pass <= 2; pass++ {
		if _, err := fx.engine.SyncRuntime(cruntime.ReconcileRequestContext{}); err != nil {
			t.Fatalf("SyncRuntime pass %d: %v", pass, err)
		}
	}

	if err := fx.client.Get(context.TODO(), dsKey, ds); err != nil {
		t.Fatalf("re-get fuse ds: %v", err)
	}
	if got := ds.Spec.UpdateStrategy.Type; got != appsv1.OnDeleteDaemonSetStrategyType {
		t.Errorf("fuse ds update strategy after sync = %q, want %q (with OnDelete a template update never restarts running pods)", got, appsv1.OnDeleteDaemonSetStrategyType)
	}
	if got := ds.Spec.Template.Spec.ImagePullSecrets; !refsEqual(got, refs("reg-a")) {
		t.Errorf("fuse pod template secrets = %v, want [reg-a] (template converged under OnDelete without restarting pods)", got)
	}
}

// TestVerifyPr6245DocsFuseRestartClaim is the contract test for
// F-docs-fuse-restart-overstated: the sample docs must not claim that a
// secrets change restarts the fuse pods. RED on head 2150b49ef (both language
// samples claim exactly that); GREEN once the sentence describes the actual
// behavior (template updated on the next sync; fuse pods pick up the new
// secrets when they are recreated, or lazily via cleanPolicy OnFuseChanged).
func TestVerifyPr6245DocsFuseRestartClaim(t *testing.T) {
	root := pr6245RepoRoot(t)
	script := filepath.Join(root, "docs/verification/juicefs-imagepullsecrets-codex/scripts/docs_fuse_restart_claim_check.sh")
	cmd := exec.Command("bash", script, root)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("docs_fuse_restart_claim_check.sh failed: %v\n%s", err, out)
	}
}

// TestVerifyPr6245DocsAntecedentDisambiguated is the contract test for
// F-docs-antecedent-ambiguous (round 6): the sample-docs closing paragraph
// declares spec.master.imagePullSecrets and spec.jobWorker.imagePullSecrets
// inert, so the next sentence must not use the bare demonstrative "these
// fields" (en) / "这些字段" (zh) - its nearest antecedent is the inert
// master/jobWorker pair, which the sync path never consumes (desired
// worker/fuse secrets derive from spec.imagePullSecrets + worker/fuse
// overrides + the controller env default only). RED on heads 2150b49ef and
// a0baafae0 (bare demonstrative in both languages); GREEN once the referent
// is disambiguated ("the imagePullSecrets fields above" / "上述
// imagePullSecrets 字段"), as in round-7 head e7a93e775.
func TestVerifyPr6245DocsAntecedentDisambiguated(t *testing.T) {
	root := pr6245RepoRoot(t)
	script := filepath.Join(root, "docs/verification/juicefs-imagepullsecrets-codex/scripts/docs_antecedent_check.sh")
	cmd := exec.Command("bash", script, root)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("docs_antecedent_check.sh failed: %v\n%s", err, out)
	}
}
