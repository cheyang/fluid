/*
Additive review-verification harness for PR #6288 (reviewer: codex).
Production code is untouched by this file.
See docs/verification/alluxio-sync-imagepullsecrets-codex/README.md.

L2 (integration) layer of finding F1: run SyncRuntime against a REAL API
server (envtest). The fake client cannot fully reproduce the wire semantics
that make F1 bite: the helm chart omits imagePullSecrets when none are
configured, and the API server therefore serves the field as nil, while
SyncRuntime computes a non-nil empty desired slice. This test skips when
KUBEBUILDER_ASSETS is not set, so plain unit runs are unaffected.
*/

package alluxio

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/runtime"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apimachineryRuntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func TestVerifyCodexPR6288SyncRuntimeAgainstRealAPIServer(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS not set; skipping envtest layer (run docs/verification/alluxio-sync-imagepullsecrets-codex/scripts/re-verify.sh)")
	}
	verifyCodexUnsetPullSecretEnv(t)

	scheme := apimachineryRuntime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add corev1: %v", err)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add appsv1: %v", err)
	}
	if err := datav1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add datav1alpha1: %v", err)
	}

	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
	}
	cfg, err := testEnv.Start()
	if err != nil {
		t.Fatalf("failed to start envtest: %v", err)
	}
	defer func() {
		if err := testEnv.Stop(); err != nil {
			t.Logf("failed to stop envtest: %v", err)
		}
	}()

	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: verifyCodexNamespace}}
	if err := c.Create(ctx, ns); err != nil && !apierrors.IsAlreadyExists(err) {
		t.Fatalf("failed to create namespace: %v", err)
	}

	// The AlluxioRuntime with NO imagePullSecrets at any level: the default
	// configuration most users run.
	alluxioRuntime := &datav1alpha1.AlluxioRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: verifyCodexName, Namespace: verifyCodexNamespace},
		Spec:       datav1alpha1.AlluxioRuntimeSpec{},
	}
	if err := c.Create(ctx, alluxioRuntime); err != nil {
		t.Fatalf("failed to create AlluxioRuntime: %v", err)
	}

	// The workloads exactly as the helm chart renders them for that runtime:
	// the imagePullSecrets field is omitted from the pod templates.
	master, worker, fuse := verifyCodexNewWorkloads(nil)
	if err := c.Create(ctx, master); err != nil {
		t.Fatalf("failed to create master StatefulSet: %v", err)
	}
	if err := c.Create(ctx, worker); err != nil {
		t.Fatalf("failed to create worker StatefulSet: %v", err)
	}
	if err := c.Create(ctx, fuse); err != nil {
		t.Fatalf("failed to create fuse DaemonSet: %v", err)
	}

	// Production-fidelity assertion: the API server serves the omitted field
	// back as nil.
	served := &appsv1.StatefulSet{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: verifyCodexNamespace, Name: verifyCodexName + "-master"}, served); err != nil {
		t.Fatalf("failed to read back master StatefulSet: %v", err)
	}
	if served.Spec.Template.Spec.ImagePullSecrets != nil {
		t.Fatalf("API server served imagePullSecrets = %#v, want nil; F1 premise does not hold", served.Spec.Template.Spec.ImagePullSecrets)
	}

	engine := &AlluxioEngine{
		name:      verifyCodexName,
		namespace: verifyCodexNamespace,
		Client:    c,
		Log:       fake.NullLogger(),
	}
	reconcileCtx := runtime.ReconcileRequestContext{}

	// Contract: SyncRuntime is a stable no-op for the default configuration.
	for i := 1; i <= 3; i++ {
		changed, err := engine.SyncRuntime(reconcileCtx)
		if err != nil {
			t.Errorf("call %d: SyncRuntime() error = %v, want nil", i, err)
			continue
		}
		if changed {
			t.Errorf("call %d: SyncRuntime() changed = true, want false (nothing to sync: no secrets configured, workloads carry none)", i)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// And the stored workloads must be untouched.
	got := &appsv1.StatefulSet{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: verifyCodexNamespace, Name: verifyCodexName + "-master"}, got); err != nil {
		t.Fatalf("failed to get master StatefulSet: %v", err)
	}
	if got.Spec.Template.Spec.ImagePullSecrets != nil {
		t.Errorf("master imagePullSecrets = %#v after sync, want nil", got.Spec.Template.Spec.ImagePullSecrets)
	}
}
