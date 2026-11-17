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

// Contract test for the lost-update path: with the worker-script configmap
// missing, syncWorkerSpec early-returns after checkAndSetWorkerChanges flagged
// the secrets change but before the sts update; SyncRuntime still saves the
// mutated value, so recorded state diverges from the cluster. RED on head
// d7e316803: the worker sts keeps the old secrets while the configmap claims
// the new ones, and a second SyncRuntime is a no-op (divergence is permanent).
func TestVerifyPr6245WorkerSecretsLostWhenScriptConfigmapMissing(t *testing.T) {
	old := constructBaseRuntimeValue()
	old.ImagePullSecrets = refs("g1")
	fx := buildVerifySyncEngine(t, "verifylost", "default", datav1alpha1.JuiceFSRuntimeSpec{
		ImagePullSecrets: refs("g2"),
	}, old, true)

	changed, err := fx.engine.SyncRuntime(cruntime.ReconcileRequestContext{})
	if err != nil {
		// Acceptable fix direction: surface the failure and do NOT advance the
		// recorded value, so a later reconcile retries the worker sync.
		if got := fx.recordedValue(t).ImagePullSecrets; !refsEqual(got, refs("g1")) {
			t.Fatalf("SyncRuntime errored (%v) yet recorded value advanced to %v - divergence", err, got)
		}
		return
	}
	if !changed {
		t.Logf("SyncRuntime changed = false (secrets change dropped silently)")
	}

	if got := fx.workerPodSecrets(t); !refsEqual(got, refs("g2")) {
		t.Fatalf("LOST UPDATE: worker sts secrets = %v, want [g2]; recorded value claims %v - "+
			"syncWorkerSpec returns before Client.Update when the worker-script configmap is missing, "+
			"but SyncRuntime still persists the mutated value", got, fx.recordedValue(t).ImagePullSecrets)
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
