/*
  Verification harness for PR #6200 (Reviewer B / Codex) — integration layer.

  Runs CacheEngine.CheckAndUpdateRuntimeStatus against a real API server
  (controller-runtime envtest) instead of the fake client, proving:

    - exactly 1 worker AdvancedStatefulSet Get per status cycle on the PR head
      (the same test observes 2 per cycle when grafted onto the merge-base —
      that red result IS the premise reproduction for issue #5879);
    - status.CacheAffinity is persisted through the real status subresource;
    - an out-of-band nodeSelector update is reflected on the next cycle.

  Requires envtest binaries (etcd + kube-apiserver). Set KUBEBUILDER_ASSETS or
  rely on the standard ~/.local/share/kubebuilder-envtest layout; the test
  skips cleanly when no assets are available.
*/

package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	workloadv1alpha1 "github.com/fluid-cloudnative/advanced-statefulset/api/workload/v1alpha1"
	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// zzVerifyEnvtestAssetsDir locates envtest binaries (etcd + kube-apiserver),
// returning "" when none are available. Honors KUBEBUILDER_ASSETS first, then
// the standard ~/.local/share/kubebuilder-envtest layout (newest version last
// in lexical order wins).
func zzVerifyEnvtestAssetsDir() string {
	if dir := os.Getenv("KUBEBUILDER_ASSETS"); dir != "" {
		if _, err := os.Stat(filepath.Join(dir, "kube-apiserver")); err == nil {
			return dir
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	root := filepath.Join(home, ".local", "share", "kubebuilder-envtest", "k8s")
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	found := ""
	for _, e := range entries {
		if _, err := os.Stat(filepath.Join(root, e.Name(), "kube-apiserver")); err == nil {
			found = filepath.Join(root, e.Name())
		}
	}
	return found
}

func TestZZVerifyPR6200EnvtestSingleRead(t *testing.T) {
	assetsDir := zzVerifyEnvtestAssetsDir()
	if assetsDir == "" {
		t.Skip("envtest binaries not available; skipping integration layer")
	}

	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{
		corev1.AddToScheme,
		datav1alpha1.AddToScheme,
		workloadv1alpha1.AddToScheme,
	} {
		if err := add(scheme); err != nil {
			t.Fatalf("scheme: %v", err)
		}
	}

	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "..", "config", "crd", "bases"),
			filepath.Join("..", "..", "..", "..", "charts", "fluid", "fluid", "crds"),
		},
		Scheme:                scheme,
		BinaryAssetsDirectory: assetsDir,
	}
	cfg, err := testEnv.Start()
	if err != nil {
		t.Fatalf("start envtest: %v", err)
	}
	defer func() {
		if err := testEnv.Stop(); err != nil {
			t.Logf("stop envtest: %v", err)
		}
	}()

	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	ctx := context.TODO()

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: zzVerifyNS}}
	if err := k8sClient.Create(ctx, ns); err != nil && !apierrors.IsAlreadyExists(err) {
		t.Fatalf("create namespace: %v", err)
	}

	runtimeObj := zzVerifyRuntimeObj()
	runtimeObj.Spec.RuntimeClassName = "curvine"
	if err := k8sClient.Create(ctx, runtimeObj); err != nil {
		t.Fatalf("create CacheRuntime: %v", err)
	}

	mkASTS := func(name string, desired, ready int32, nodeSelector map[string]string) *workloadv1alpha1.AdvancedStatefulSet {
		asts := zzVerifyASTS(name, desired, ready, nodeSelector, nil)
		// CRD-required fields
		asts.Spec.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}}
		asts.Spec.Template.ObjectMeta = metav1.ObjectMeta{Labels: map[string]string{"app": name}}
		asts.Spec.Template.Spec.Containers = []corev1.Container{{Name: "c", Image: "img:latest"}}
		return asts
	}

	for _, asts := range []*workloadv1alpha1.AdvancedStatefulSet{
		mkASTS(zzVerifyMaster, 1, 1, nil),
		mkASTS(zzVerifyWorker, 1, 1, map[string]string{"disktype": "ssd"}),
	} {
		// Capture the intended status before Create: the client decodes the
		// server response into asts, and the API server zeroes .status on
		// create (status subresource), so asts.Status is empty afterwards.
		wantStatus := asts.Status
		if err := k8sClient.Create(ctx, asts); err != nil {
			t.Fatalf("create %s: %v", asts.Name, err)
		}
		// status is a subresource on a real API server: set it explicitly
		latest := &workloadv1alpha1.AdvancedStatefulSet{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: asts.Name, Namespace: zzVerifyNS}, latest); err != nil {
			t.Fatalf("re-get %s: %v", asts.Name, err)
		}
		latest.Status = wantStatus
		if err := k8sClient.Status().Update(ctx, latest); err != nil {
			t.Fatalf("status update %s: %v", asts.Name, err)
		}
	}

	counting := &zzVerifyWorkerGetCounter{Client: k8sClient}
	e := &CacheEngine{Client: counting, name: zzVerifyRuntime, namespace: zzVerifyNS, Log: fake.NullLogger()}

	ready, err := e.CheckAndUpdateRuntimeStatus(zzVerifyStatusValue())
	if err != nil {
		t.Fatalf("cycle 1: %v", err)
	}
	if !ready {
		t.Fatalf("cycle 1: expected ready")
	}
	if counting.workerGets != 1 {
		t.Errorf("P0 premise check (envtest): expected exactly 1 worker Get in cycle 1, observed %d", counting.workerGets)
	}

	r1 := zzVerifyGetRuntime(t, k8sClient)
	if r1.Status.CacheAffinity == nil || r1.Status.CacheAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		t.Fatalf("cycle 1: CacheAffinity not persisted through status subresource: %+v", r1.Status.CacheAffinity)
	}
	terms1 := r1.Status.CacheAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	if len(terms1) != 1 || len(terms1[0].MatchExpressions) != 1 || terms1[0].MatchExpressions[0].Values[0] != "ssd" {
		t.Fatalf("cycle 1: unexpected affinity terms: %+v", terms1)
	}

	// out-of-band nodeSelector update, then next cycle must reflect it
	worker := &workloadv1alpha1.AdvancedStatefulSet{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: zzVerifyWorker, Namespace: zzVerifyNS}, worker); err != nil {
		t.Fatalf("get worker: %v", err)
	}
	worker.Spec.Template.Spec.NodeSelector = map[string]string{"disktype": "nvme"}
	if err := k8sClient.Update(ctx, worker); err != nil {
		t.Fatalf("update worker: %v", err)
	}

	if _, err := e.CheckAndUpdateRuntimeStatus(zzVerifyStatusValue()); err != nil {
		t.Fatalf("cycle 2: %v", err)
	}
	if counting.workerGets != 2 {
		t.Errorf("P0 premise check (envtest): expected exactly 1 worker Get per cycle (2 total), observed %d", counting.workerGets)
	}

	r2 := zzVerifyGetRuntime(t, k8sClient)
	terms2 := r2.Status.CacheAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	if len(terms2) != 1 || len(terms2[0].MatchExpressions) != 1 || terms2[0].MatchExpressions[0].Values[0] != "nvme" {
		t.Fatalf("cycle 2: expected updated affinity disktype=nvme, got %+v", terms2)
	}

	t.Logf("envtest observed worker Gets: %d over 2 cycles; affinity updated ssd->nvme", counting.workerGets)
}
