/*
Additive review-verification harness for PR #6288 (reviewer: codex).
Production code is untouched by this file.
See docs/verification/alluxio-sync-imagepullsecrets-codex/README.md.

Finding F1 (contract polarity): with the default configuration (no
imagePullSecrets at any level of the AlluxioRuntime and IMAGE_PULL_SECRETS
unset), the helm chart renders master/worker StatefulSets and the fuse
DaemonSet WITHOUT the imagePullSecrets field, so the API server serves it as
nil. SyncRuntime must then be a stable no-op: changed=false on every
reconcile. If it reports changed=true, base.Sync short-circuits
(`if updated { return }`) and health checks, runtime/dataset status updates,
dataset-cache updates and dataset-mount syncs are skipped on every one of the
runtime's periodic (default 90s) reconciles.
*/

package alluxio

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/runtime"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	verifyCodexName      = "verify-codex"
	verifyCodexNamespace = "default"
)

// verifyCodexNewWorkloads builds the master/worker StatefulSets and the fuse
// DaemonSet the way the helm chart renders them when no imagePullSecrets are
// configured: the pod template simply omits the field (nil slice).
func verifyCodexNewWorkloads(imagePullSecrets []corev1.LocalObjectReference) (master, worker *appsv1.StatefulSet, fuse *appsv1.DaemonSet) {
	newStatefulSet := func(name, containerName string) *appsv1.StatefulSet {
		labels := map[string]string{"app": "alluxio", "role": containerName}
		return &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{Name: verifyCodexName + "-" + name, Namespace: verifyCodexNamespace},
			Spec: appsv1.StatefulSetSpec{
				Selector:    &metav1.LabelSelector{MatchLabels: labels},
				ServiceName: verifyCodexName + "-" + name,
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: labels},
					Spec: corev1.PodSpec{
						ImagePullSecrets: imagePullSecrets,
						Containers:       []corev1.Container{{Name: containerName, Image: "alluxio/alluxio:2.9.3"}},
					},
				},
			},
		}
	}
	master = newStatefulSet("master", "master")
	worker = newStatefulSet("worker", "worker")
	fuseLabels := map[string]string{"app": "alluxio", "role": "fuse"}
	fuse = &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: verifyCodexName + "-fuse", Namespace: verifyCodexNamespace},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: fuseLabels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: fuseLabels},
				Spec: corev1.PodSpec{
					ImagePullSecrets: imagePullSecrets,
					Containers:       []corev1.Container{{Name: "fuse", Image: "alluxio/alluxio-fuse:2.9.3"}},
				},
			},
		},
	}
	return master, worker, fuse
}

func verifyCodexUnsetPullSecretEnv(t *testing.T) {
	t.Helper()
	prev, existed := os.LookupEnv(common.EnvImagePullSecretsKey)
	if existed {
		t.Logf("%s was set to %q; unsetting for the duration of the test", common.EnvImagePullSecretsKey, prev)
	}
	if err := os.Unsetenv(common.EnvImagePullSecretsKey); err != nil {
		t.Fatalf("failed to unset %s: %v", common.EnvImagePullSecretsKey, err)
	}
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv(common.EnvImagePullSecretsKey, prev)
		}
	})
}

func deepEqualForVerifyCodex(a, b []corev1.LocalObjectReference) bool {
	return reflect.DeepEqual(a, b)
}

// TestVerifyCodexPR6288DefaultConfigStaysNoOp is the F1 contract test:
// default configuration => SyncRuntime must report changed=false on every
// call. On the PR head this FAILS: changed=true is reported on every call
// even though the written value equals the stored value (nil).
func TestVerifyCodexPR6288DefaultConfigStaysNoOp(t *testing.T) {
	verifyCodexUnsetPullSecretEnv(t)

	alluxioRuntime := &datav1alpha1.AlluxioRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: verifyCodexName, Namespace: verifyCodexNamespace},
		Spec:       datav1alpha1.AlluxioRuntimeSpec{},
	}
	master, worker, fuse := verifyCodexNewWorkloads(nil)

	client := fake.NewFakeClientWithScheme(testScheme, alluxioRuntime, master, worker, fuse)
	engine := &AlluxioEngine{
		name:      verifyCodexName,
		namespace: verifyCodexNamespace,
		Client:    client,
		Log:       fake.NullLogger(),
	}
	ctx := runtime.ReconcileRequestContext{}

	for i := 1; i <= 3; i++ {
		changed, err := engine.SyncRuntime(ctx)
		if err != nil {
			t.Fatalf("call %d: SyncRuntime() error = %v, want nil", i, err)
		}
		if changed {
			t.Errorf("call %d: SyncRuntime() changed = true, want false (no imagePullSecrets configured anywhere; workloads already carry none)", i)
		}
	}

	// The workload specs must remain exactly as the chart rendered them.
	got := &appsv1.StatefulSet{}
	if err := client.Get(context.Background(), types.NamespacedName{Namespace: verifyCodexNamespace, Name: verifyCodexName + "-master"}, got); err != nil {
		t.Fatalf("failed to get master StatefulSet: %v", err)
	}
	if got.Spec.Template.Spec.ImagePullSecrets != nil {
		t.Errorf("master imagePullSecrets = %v, want nil", got.Spec.Template.Spec.ImagePullSecrets)
	}
}

// TestVerifyCodexPR6288EmptyStateIdempotentAfterRemoval is the second half of
// the F1 contract: after a real change (secrets configured -> all removed) is
// synced, the NEXT reconcile must be a stable no-op. On the PR head this
// FAILS: the reconcile after the removal still reports changed=true.
func TestVerifyCodexPR6288EmptyStateIdempotentAfterRemoval(t *testing.T) {
	verifyCodexUnsetPullSecretEnv(t)

	secret := []corev1.LocalObjectReference{{Name: "registry-secret"}}
	alluxioRuntime := &datav1alpha1.AlluxioRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: verifyCodexName, Namespace: verifyCodexNamespace},
		Spec:       datav1alpha1.AlluxioRuntimeSpec{ImagePullSecrets: secret},
	}
	master, worker, fuse := verifyCodexNewWorkloads(secret)

	client := fake.NewFakeClientWithScheme(testScheme, alluxioRuntime, master, worker, fuse)
	engine := &AlluxioEngine{
		name:      verifyCodexName,
		namespace: verifyCodexNamespace,
		Client:    client,
		Log:       fake.NullLogger(),
	}
	ctx := runtime.ReconcileRequestContext{}

	// Baseline: everything in sync, no change expected.
	if changed, err := engine.SyncRuntime(ctx); err != nil || changed {
		t.Fatalf("baseline: SyncRuntime() = (changed=%v, err=%v), want (false, nil)", changed, err)
	}

	// Remove every imagePullSecrets from the runtime spec.
	alluxioRuntime.Spec.ImagePullSecrets = nil
	if err := client.Update(context.Background(), alluxioRuntime); err != nil {
		t.Fatalf("failed to remove runtime imagePullSecrets: %v", err)
	}

	// First sync after removal performs the real change.
	if changed, err := engine.SyncRuntime(ctx); err != nil || !changed {
		t.Fatalf("removal sync: SyncRuntime() = (changed=%v, err=%v), want (true, nil)", changed, err)
	}

	// The next reconcile must be a no-op.
	for i := 1; i <= 2; i++ {
		changed, err := engine.SyncRuntime(ctx)
		if err != nil {
			t.Fatalf("post-removal call %d: SyncRuntime() error = %v, want nil", i, err)
		}
		if changed {
			t.Errorf("post-removal call %d: SyncRuntime() changed = true, want false (secrets already cleared; nothing left to do)", i)
		}
	}
}

// TestVerifyCodexPR6288ConfiguredSecretsSync is the positive control: when
// secrets are configured and differ, SyncRuntime applies them once and then
// goes quiet. Must PASS on the PR head; it guards the harness against false
// positives.
func TestVerifyCodexPR6288ConfiguredSecretsSync(t *testing.T) {
	verifyCodexUnsetPullSecretEnv(t)

	common := []corev1.LocalObjectReference{{Name: "common-secret"}}
	masterSecret := []corev1.LocalObjectReference{{Name: "master-secret"}}
	old := []corev1.LocalObjectReference{{Name: "old-secret"}}
	alluxioRuntime := &datav1alpha1.AlluxioRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: verifyCodexName, Namespace: verifyCodexNamespace},
		Spec: datav1alpha1.AlluxioRuntimeSpec{
			ImagePullSecrets: common,
			Master:           datav1alpha1.AlluxioCompTemplateSpec{ImagePullSecrets: masterSecret},
		},
	}
	master, worker, fuse := verifyCodexNewWorkloads(old)

	client := fake.NewFakeClientWithScheme(testScheme, alluxioRuntime, master, worker, fuse)
	engine := &AlluxioEngine{
		name:      verifyCodexName,
		namespace: verifyCodexNamespace,
		Client:    client,
		Log:       fake.NullLogger(),
	}
	ctx := runtime.ReconcileRequestContext{}

	if changed, err := engine.SyncRuntime(ctx); err != nil || !changed {
		t.Fatalf("first sync: SyncRuntime() = (changed=%v, err=%v), want (true, nil)", changed, err)
	}

	gotMaster := &appsv1.StatefulSet{}
	if err := client.Get(context.Background(), types.NamespacedName{Namespace: verifyCodexNamespace, Name: verifyCodexName + "-master"}, gotMaster); err != nil {
		t.Fatalf("failed to get master StatefulSet: %v", err)
	}
	if len(gotMaster.Spec.Template.Spec.ImagePullSecrets) != 1 || gotMaster.Spec.Template.Spec.ImagePullSecrets[0].Name != "master-secret" {
		t.Errorf("master imagePullSecrets = %v, want [master-secret]", gotMaster.Spec.Template.Spec.ImagePullSecrets)
	}
	gotWorker := &appsv1.StatefulSet{}
	if err := client.Get(context.Background(), types.NamespacedName{Namespace: verifyCodexNamespace, Name: verifyCodexName + "-worker"}, gotWorker); err != nil {
		t.Fatalf("failed to get worker StatefulSet: %v", err)
	}
	if len(gotWorker.Spec.Template.Spec.ImagePullSecrets) != 1 || gotWorker.Spec.Template.Spec.ImagePullSecrets[0].Name != "common-secret" {
		t.Errorf("worker imagePullSecrets = %v, want [common-secret] (fallback to runtime-level)", gotWorker.Spec.Template.Spec.ImagePullSecrets)
	}

	if changed, err := engine.SyncRuntime(ctx); err != nil || changed {
		t.Errorf("steady-state sync: SyncRuntime() = (changed=%v, err=%v), want (false, nil)", changed, err)
	}
}

// TestVerifyCodexPR6288APIServerServesNilForOmittedSecrets proves the
// production-fidelity link of F1 without a cluster: PodSpec.ImagePullSecrets
// is `json:"imagePullSecrets,omitempty"`, so a workload rendered by helm
// without the field (or updated with an empty value) round-trips through the
// API server as a nil slice, while the desired value computed by SyncRuntime
// for the default config is a non-nil empty slice
// (docker.GetImagePullSecretsFromEnv returns []corev1.LocalObjectReference{}).
// reflect.DeepEqual(nil, []) == false is what keeps changed=true forever.
func TestVerifyCodexPR6288APIServerServesNilForOmittedSecrets(t *testing.T) {
	// 1. A pod spec without imagePullSecrets serializes without the field.
	raw, err := json.Marshal(corev1.PodSpec{Containers: []corev1.Container{{Name: "c"}}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var asMap map[string]interface{}
	if err := json.Unmarshal(raw, &asMap); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}
	if _, exists := asMap["imagePullSecrets"]; exists {
		t.Errorf("serialized PodSpec unexpectedly contains imagePullSecrets: %s", raw)
	}

	// 2. Decoding such a document yields a nil slice.
	var decoded corev1.PodSpec
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.ImagePullSecrets != nil {
		t.Errorf("decoded ImagePullSecrets = %#v, want nil", decoded.ImagePullSecrets)
	}

	// 3. An explicit empty list also round-trips back to nil (omitempty drops it).
	rawEmpty, err := json.Marshal(corev1.PodSpec{
		Containers:       []corev1.Container{{Name: "c"}},
		ImagePullSecrets: []corev1.LocalObjectReference{},
	})
	if err != nil {
		t.Fatalf("marshal empty: %v", err)
	}
	var decodedEmpty corev1.PodSpec
	if err := json.Unmarshal(rawEmpty, &decodedEmpty); err != nil {
		t.Fatalf("unmarshal empty: %v", err)
	}
	if decodedEmpty.ImagePullSecrets != nil {
		t.Errorf("decoded empty ImagePullSecrets = %#v, want nil (omitempty drops empty slices on the wire)", decodedEmpty.ImagePullSecrets)
	}

	// 4. And nil-vs-empty is exactly what reflect.DeepEqual distinguishes.
	empty := make([]corev1.LocalObjectReference, 0)
	var nilSlice []corev1.LocalObjectReference
	if deepEqualForVerifyCodex(nilSlice, empty) {
		t.Error("reflect.DeepEqual(nil, []) unexpectedly true; the premise of F1 is gone")
	}
}
