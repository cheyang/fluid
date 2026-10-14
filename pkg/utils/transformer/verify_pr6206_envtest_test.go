// Verification harness for the review of https://github.com/fluid-cloudnative/fluid/pull/6206
// (fixes https://github.com/fluid-cloudnative/fluid/issues/6205).
//
// Additive reviewer harness only — no production code is modified.
//
// Layer: L2 (integration). Boots envtest with the real fluid CRDs and a real
// discovery RESTMapper, then drives the REAL controller-runtime owner-event handler
// (handler.EnqueueRequestForOwner, the same one wired in pkg/ctrl/watch/manager.go)
// with a dependent Pod whose controller ownerReference was produced by
// GenerateOwnerReferenceFromObject for a group-less TypeMeta input.
//
// CONTRACT polarity: asserts the intended correct behavior — the owner event MUST
// enqueue a reconcile request for the Dataset. On base code the generated reference
// carries apiVersion "v1alpha1" (no group), the handler's Group match fails, the
// request is silently dropped, and this test FAILS (= reproduction of the issue's
// impact). On the PR head it PASSES.
package transformer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
)

// findEnvtestAssets locates kube-apiserver/etcd binaries: $KUBEBUILDER_ASSETS first,
// then a few well-known local caches. Returns "" when nothing is found (test skips).
func findEnvtestAssets() string {
	candidates := []string{os.Getenv("KUBEBUILDER_ASSETS"), "/usr/local/kubebuilder/bin"}
	for _, root := range []string{os.Getenv("HOME") + "/envtest-assets/k8s", "/tmp/envtest-assets/k8s", "/usr/local/envtest/k8s"} {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		// newest-looking version last in listing order wins by appending later; keep it simple and take them all
		for _, e := range entries {
			candidates = append(candidates, filepath.Join(root, e.Name()))
		}
	}
	for _, dir := range candidates {
		if dir == "" {
			continue
		}
		if st, err := os.Stat(filepath.Join(dir, "kube-apiserver")); err == nil && !st.IsDir() {
			if _, err := os.Stat(filepath.Join(dir, "etcd")); err == nil {
				return dir
			}
		}
	}
	return ""
}

func TestVerifyPR6206OwnerWatchEnqueueIntegration(t *testing.T) {
	assets := findEnvtestAssets()
	if assets == "" {
		t.Skip("envtest binaries (kube-apiserver, etcd) not found; set KUBEBUILDER_ASSETS")
	}

	scheme := runtime.NewScheme()
	if err := datav1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add fluid scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}

	env := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
		BinaryAssetsDirectory: assets,
	}
	cfg, err := env.Start()
	if err != nil {
		t.Fatalf("start envtest: %v", err)
	}
	defer func() {
		if err := env.Stop(); err != nil {
			t.Logf("stop envtest: %v", err)
		}
	}()

	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// The owner object, as handed over with a group-less apiVersion (the shape from issue #6205).
	owner := &datav1alpha1.Dataset{
		TypeMeta:   metav1.TypeMeta{Kind: "Dataset", APIVersion: "v1alpha1"},
		ObjectMeta: metav1.ObjectMeta{Name: "groupless-owner", Namespace: "default"},
	}
	if err := c.Create(ctx, owner); err != nil {
		t.Fatalf("create owner dataset: %v", err)
	}

	// Generate the reference from the group-less-shaped TypeMeta. On base code this
	// yields apiVersion "v1alpha1"; on the PR head "data.fluid.io/v1alpha1".
	genRef := GenerateOwnerReferenceFromObject(&datav1alpha1.Dataset{
		TypeMeta:   metav1.TypeMeta{Kind: "Dataset", APIVersion: "v1alpha1"},
		ObjectMeta: metav1.ObjectMeta{Name: owner.Name, Namespace: owner.Namespace, UID: owner.UID},
	})
	t.Logf("generated ownerReference: apiVersion=%q kind=%q", genRef.APIVersion, genRef.Kind)

	// Premise claim from issue #6205: the API server accepts the group-less reference
	// syntactically on write. Create the dependent Pod carrying exactly the generated
	// reference and require the write to succeed (GUARD: must hold on base and head).
	dependent := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dependent-pod",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: genRef.APIVersion,
				Kind:       genRef.Kind,
				Name:       genRef.Name,
				UID:        types.UID(genRef.UID),
				Controller: &genRef.Controller,
			}},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "pause", Image: "registry.k8s.io/pause:3.9"}}},
	}
	if err := c.Create(ctx, dependent); err != nil {
		t.Fatalf("API server must accept the generated ownerReference syntactically, got: %v", err)
	}

	// Real production wiring: pkg/ctrl/watch/manager.go uses
	// handler.EnqueueRequestForOwner(mgr.GetScheme(), mgr.GetRESTMapper(), r.ManagedResource(), OnlyControllerOwner()).
	httpClient, err := rest.HTTPClientFor(cfg)
	if err != nil {
		t.Fatalf("build http client: %v", err)
	}
	mapper, err := apiutil.NewDynamicRESTMapper(cfg, httpClient)
	if err != nil {
		t.Fatalf("build discovery RESTMapper: %v", err)
	}
	eh := handler.EnqueueRequestForOwner(scheme, mapper, &datav1alpha1.Dataset{}, handler.OnlyControllerOwner())
	q := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
	eh.Create(ctx, event.CreateEvent{Object: dependent}, q)

	got := q.Len()
	t.Logf("enqueued reconcile requests for the Dataset: %d", got)
	if got != 1 {
		t.Errorf("owner-based watch dropped the event: enqueued %d requests, want 1 "+
			"(ownerReference apiVersion %q does not resolve to GroupKind data.fluid.io/Dataset)", got, genRef.APIVersion)
	}
}
