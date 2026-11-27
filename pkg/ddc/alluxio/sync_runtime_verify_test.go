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

package alluxio

// Verification harness for PR #6288 (Sync Alluxio imagePullSecrets changes).
//
// These are CONTRACT tests (see docs/verification/alluxio-sync-pullsecrets-claude/):
// they assert the intended correct behavior. On the PR head they must PASS.
// On the merge-base (SyncRuntime is a no-op there) they FAIL, which is both the
// premise reproduction (P0) and the harness-bites check.
//
// TestV01: spec-level and per-component secrets propagate to the master/worker
// StatefulSets and the fuse DaemonSet; idempotent on steady state; unrelated
// pod-spec fields are preserved.
// TestV02: creation-parity matrix — the values SyncRuntime writes must equal
// what the helm charts would render for the same runtime spec/env at creation
// time (charts/alluxio/templates/{master,worker}/statefulset.yaml,
// charts/alluxio/templates/fuse/daemonset.yaml):
//   master/worker: spec.<comp>.imagePullSecrets -> values.imagePullSecrets -> none
//   fuse:          spec.fuse.imagePullSecrets    -> values.imagePullSecrets -> none
//   values.imagePullSecrets (common) = spec.ImagePullSecrets, else env IMAGE_PULL_SECRETS
//   values.fuse.imagePullSecrets     = spec.Fuse.ImagePullSecrets, else env IMAGE_PULL_SECRETS
//   (parseFuseImage's env fallback means fuse prefers env over spec-common when
//   spec.Fuse is empty — matches the chart's `if fuse.imagePullSecrets` branch.)
// TestV03: a missing workload surfaces as an error (retried by the reconciler),
// not as a silent skip.

import (
	"context"
	"reflect"
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	cruntime "github.com/fluid-cloudnative/fluid/pkg/runtime"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type vWorkloads struct {
	master *appsv1.StatefulSet
	worker *appsv1.StatefulSet
	fuse   *appsv1.DaemonSet
}

func vNewWorkloads(name, namespace string, initial []corev1.LocalObjectReference) vWorkloads {
	podSpec := func() corev1.PodSpec {
		return corev1.PodSpec{
			ImagePullSecrets: initial,
			NodeSelector:     map[string]string{"preserve-me": "true"},
			Containers:       []corev1.Container{{Name: "main", Image: "img:1"}},
			Volumes:          []corev1.Volume{{Name: "keep"}},
		}
	}
	return vWorkloads{
		master: &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{Name: name + "-master", Namespace: namespace},
			Spec:       appsv1.StatefulSetSpec{Template: corev1.PodTemplateSpec{Spec: podSpec()}},
		},
		worker: &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{Name: name + "-worker", Namespace: namespace},
			Spec:       appsv1.StatefulSetSpec{Template: corev1.PodTemplateSpec{Spec: podSpec()}},
		},
		fuse: &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Name: name + "-fuse", Namespace: namespace},
			Spec:       appsv1.DaemonSetSpec{Template: corev1.PodTemplateSpec{Spec: podSpec()}},
		},
	}
}

func vGetSecrets(t *testing.T, c client.Client, w vWorkloads) (master, worker, fuse []corev1.LocalObjectReference) {
	t.Helper()
	m := &appsv1.StatefulSet{}
	wo := &appsv1.StatefulSet{}
	f := &appsv1.DaemonSet{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: w.master.Namespace, Name: w.master.Name}, m); err != nil {
		t.Fatalf("get master: %v", err)
	}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: w.worker.Namespace, Name: w.worker.Name}, wo); err != nil {
		t.Fatalf("get worker: %v", err)
	}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: w.fuse.Namespace, Name: w.fuse.Name}, f); err != nil {
		t.Fatalf("get fuse: %v", err)
	}
	return m.Spec.Template.Spec.ImagePullSecrets, wo.Spec.Template.Spec.ImagePullSecrets, f.Spec.Template.Spec.ImagePullSecrets
}

func vAssertPodFieldsPreserved(t *testing.T, c client.Client, w vWorkloads) {
	t.Helper()
	m := &appsv1.StatefulSet{}
	f := &appsv1.DaemonSet{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: w.master.Namespace, Name: w.master.Name}, m); err != nil {
		t.Fatalf("get master: %v", err)
	}
	if got := m.Spec.Template.Spec.NodeSelector; !reflect.DeepEqual(got, map[string]string{"preserve-me": "true"}) {
		t.Errorf("master nodeSelector not preserved: %v", got)
	}
	if len(m.Spec.Template.Spec.Containers) != 1 || m.Spec.Template.Spec.Containers[0].Name != "main" || m.Spec.Template.Spec.Containers[0].Image != "img:1" {
		t.Errorf("master containers not preserved: %v", m.Spec.Template.Spec.Containers)
	}
	if len(m.Spec.Template.Spec.Volumes) != 1 || m.Spec.Template.Spec.Volumes[0].Name != "keep" {
		t.Errorf("master volumes not preserved: %v", m.Spec.Template.Spec.Volumes)
	}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: w.fuse.Namespace, Name: w.fuse.Name}, f); err != nil {
		t.Fatalf("get fuse: %v", err)
	}
	if len(f.Spec.Template.Spec.Containers) != 1 || f.Spec.Template.Spec.Containers[0].Image != "img:1" {
		t.Errorf("fuse containers not preserved: %v", f.Spec.Template.Spec.Containers)
	}
}

// TestV01_SyncRuntime_PropagatesSecretChanges — CONTRACT test (PR head: PASS, base: FAIL).
func TestV01_SyncRuntime_PropagatesSecretChanges(t *testing.T) {
	t.Setenv(common.EnvImagePullSecretsKey, "")
	const name, namespace = "v-runtime", "default"

	runtime := &datav1alpha1.AlluxioRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: datav1alpha1.AlluxioRuntimeSpec{
			ImagePullSecrets: []corev1.LocalObjectReference{{Name: "common-secret"}},
			Master:           datav1alpha1.AlluxioCompTemplateSpec{ImagePullSecrets: []corev1.LocalObjectReference{{Name: "master-secret"}}},
			Worker:           datav1alpha1.AlluxioCompTemplateSpec{ImagePullSecrets: []corev1.LocalObjectReference{{Name: "worker-secret"}}},
			Fuse:             datav1alpha1.AlluxioFuseSpec{ImagePullSecrets: []corev1.LocalObjectReference{{Name: "fuse-secret"}}},
		},
	}
	w := vNewWorkloads(name, namespace, []corev1.LocalObjectReference{{Name: "old-secret"}})
	client := fake.NewFakeClientWithScheme(testScheme, runtime, w.master, w.worker, w.fuse)
	engine := &AlluxioEngine{name: name, namespace: namespace, Client: client, Log: fake.NullLogger()}

	changed, err := engine.SyncRuntime(cruntime.ReconcileRequestContext{})
	if err != nil {
		t.Fatalf("SyncRuntime() error = %v", err)
	}
	if !changed {
		t.Fatalf("SyncRuntime() changed = false, want true (secrets differ)")
	}
	m, wo, f := vGetSecrets(t, client, w)
	if want := []corev1.LocalObjectReference{{Name: "master-secret"}}; !reflect.DeepEqual(m, want) {
		t.Errorf("master secrets = %v, want %v", m, want)
	}
	if want := []corev1.LocalObjectReference{{Name: "worker-secret"}}; !reflect.DeepEqual(wo, want) {
		t.Errorf("worker secrets = %v, want %v", wo, want)
	}
	if want := []corev1.LocalObjectReference{{Name: "fuse-secret"}}; !reflect.DeepEqual(f, want) {
		t.Errorf("fuse secrets = %v, want %v", f, want)
	}
	vAssertPodFieldsPreserved(t, client, w)

	// steady state: no further change
	changed, err = engine.SyncRuntime(cruntime.ReconcileRequestContext{})
	if err != nil {
		t.Fatalf("second SyncRuntime() error = %v", err)
	}
	if changed {
		t.Fatalf("second SyncRuntime() changed = true, want false (steady state)")
	}

	// removal of every secret: desired becomes nil everywhere (env unset),
	// so the pod templates must be cleared.
	runtime.Spec.ImagePullSecrets = nil
	runtime.Spec.Master.ImagePullSecrets = nil
	runtime.Spec.Worker.ImagePullSecrets = nil
	runtime.Spec.Fuse.ImagePullSecrets = nil
	if err := client.Update(context.Background(), runtime); err != nil {
		t.Fatalf("update runtime: %v", err)
	}
	changed, err = engine.SyncRuntime(cruntime.ReconcileRequestContext{})
	if err != nil || !changed {
		t.Fatalf("SyncRuntime() after removal = (%v, %v), want (true, nil)", changed, err)
	}
	m, wo, f = vGetSecrets(t, client, w)
	if m != nil || wo != nil || f != nil {
		t.Errorf("secrets after removal = (%v, %v, %v), want all nil", m, wo, f)
	}
}

// TestV02_SyncRuntime_CreationParity — CONTRACT test (PR head: PASS, base: FAIL).
// The desired value must equal what the charts render at creation for the same
// runtime spec and controller env, otherwise SyncRuntime drifts workloads away
// from (or flaps against) freshly created ones.
func TestV02_SyncRuntime_CreationParity(t *testing.T) {
	secret := func(names ...string) []corev1.LocalObjectReference {
		var out []corev1.LocalObjectReference
		for _, n := range names {
			out = append(out, corev1.LocalObjectReference{Name: n})
		}
		return out
	}

	cases := []struct {
		name       string
		env        string
		specCommon []corev1.LocalObjectReference
		specMaster []corev1.LocalObjectReference
		specWorker []corev1.LocalObjectReference
		specFuse   []corev1.LocalObjectReference
		wantMaster []corev1.LocalObjectReference // chart-rendered value at creation
		wantWorker []corev1.LocalObjectReference
		wantFuse   []corev1.LocalObjectReference
	}{
		{
			// charts: comps empty -> common everywhere (fuse value empty -> falls to common)
			name:       "common-only",
			specCommon: secret("A"),
			wantMaster: secret("A"), wantWorker: secret("A"), wantFuse: secret("A"),
		},
		{
			// charts: per-component set -> component value everywhere
			name:       "component-only",
			specMaster: secret("M"), specWorker: secret("W"), specFuse: secret("F"),
			wantMaster: secret("M"), wantWorker: secret("W"), wantFuse: secret("F"),
		},
		{
			// charts: component wins where set; common fills the rest; fuse with
			// empty spec falls to common (env unset)
			name:       "mixed",
			specCommon: secret("A"), specMaster: secret("M"), specFuse: secret("F"),
			wantMaster: secret("M"), wantWorker: secret("A"), wantFuse: secret("F"),
		},
		{
			// env fallback: values.imagePullSecrets = env; fuse value = env
			// (parseFuseImage env fallback), so all three get env
			name:       "env-only",
			env:        "B",
			wantMaster: secret("B"), wantWorker: secret("B"), wantFuse: secret("B"),
		},
		{
			// env + common: master/worker get spec common (A); fuse.imagePullSecrets
			// is pre-filled with env (B) by parseFuseImage, so the chart's
			// `if fuse.imagePullSecrets` branch wins over the common fallback.
			// SyncRuntime must reproduce the same value (it does: parseFuseImage
			// env fallback pre-empts the common fallback).
			name: "env-and-common",
			env:  "B", specCommon: secret("A"),
			wantMaster: secret("A"), wantWorker: secret("A"), wantFuse: secret("B"),
		},
		{
			name:       "empty",
			wantMaster: nil, wantWorker: nil, wantFuse: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(common.EnvImagePullSecretsKey, tc.env)
			const name, namespace = "v-parity", "default"
			runtime := &datav1alpha1.AlluxioRuntime{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
				Spec: datav1alpha1.AlluxioRuntimeSpec{
					ImagePullSecrets: tc.specCommon,
					Master:           datav1alpha1.AlluxioCompTemplateSpec{ImagePullSecrets: tc.specMaster},
					Worker:           datav1alpha1.AlluxioCompTemplateSpec{ImagePullSecrets: tc.specWorker},
					Fuse:             datav1alpha1.AlluxioFuseSpec{ImagePullSecrets: tc.specFuse},
				},
			}
			w := vNewWorkloads(name, namespace, []corev1.LocalObjectReference{{Name: "initial-secret"}})
			client := fake.NewFakeClientWithScheme(testScheme, runtime, w.master, w.worker, w.fuse)
			engine := &AlluxioEngine{name: name, namespace: namespace, Client: client, Log: fake.NullLogger()}

			_, err := engine.SyncRuntime(cruntime.ReconcileRequestContext{})
			if err != nil {
				t.Fatalf("SyncRuntime() error = %v", err)
			}
			m, wo, f := vGetSecrets(t, client, w)
			if !reflect.DeepEqual(m, tc.wantMaster) {
				t.Errorf("master secrets = %v, want (creation-parity) %v", m, tc.wantMaster)
			}
			if !reflect.DeepEqual(wo, tc.wantWorker) {
				t.Errorf("worker secrets = %v, want (creation-parity) %v", wo, tc.wantWorker)
			}
			if !reflect.DeepEqual(f, tc.wantFuse) {
				t.Errorf("fuse secrets = %v, want (creation-parity) %v", f, tc.wantFuse)
			}
		})
	}
}

// TestV03_SyncRuntime_MissingWorkloadSurfacesError — CONTRACT test:
// a missing fuse DaemonSet must surface as an error (so the reconciler retries
// and raises a Warning event) rather than being silently ignored.
func TestV03_SyncRuntime_MissingWorkloadSurfacesError(t *testing.T) {
	t.Setenv(common.EnvImagePullSecretsKey, "")
	const name, namespace = "v-missing", "default"
	runtime := &datav1alpha1.AlluxioRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       datav1alpha1.AlluxioRuntimeSpec{ImagePullSecrets: []corev1.LocalObjectReference{{Name: "A"}}},
	}
	w := vNewWorkloads(name, namespace, nil)
	// note: fuse DaemonSet deliberately NOT registered
	client := fake.NewFakeClientWithScheme(testScheme, runtime, w.master, w.worker)
	engine := &AlluxioEngine{name: name, namespace: namespace, Client: client, Log: fake.NullLogger()}

	_, err := engine.SyncRuntime(cruntime.ReconcileRequestContext{})
	if err == nil {
		t.Fatalf("SyncRuntime() with missing fuse DaemonSet returned nil error, want error")
	}
}
