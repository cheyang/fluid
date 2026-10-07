package utils

// Verification harness for PR #6199 (reviewer: codex). Additive only.
// L2 integration layer: run the real production path (typed client read -> TypeMeta
// stripped -> datasetControllerOwnerReference -> API server create) against a real
// API server (envtest). Requires KUBEBUILDER_ASSETS or setup-envtest binaries.

import (
	"context"
	"path/filepath"
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func TestVerifyCreateRuntimeForReferenceDatasetEnvtest(t *testing.T) {
	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := testEnv.Start()
	if err != nil {
		t.Fatalf("failed to start envtest: %v", err)
	}
	defer func() {
		if err := testEnv.Stop(); err != nil {
			t.Errorf("failed to stop envtest: %v", err)
		}
	}()

	s := runtime.NewScheme()
	if err := datav1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("failed to register scheme: %v", err)
	}
	c, err := client.New(cfg, client.Options{Scheme: s})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	ctx := context.TODO()

	// Create the Dataset the way the API server hands it back; a typed client read
	// strips TypeMeta, which is exactly the path that needs scheme-based recovery.
	ds := &datav1alpha1.Dataset{ObjectMeta: metav1.ObjectMeta{Name: "ref-dataset", Namespace: "default"}}
	if err := c.Create(ctx, ds); err != nil {
		t.Fatalf("failed to create dataset: %v", err)
	}
	fetched := &datav1alpha1.Dataset{}
	if err := c.Get(ctx, types.NamespacedName{Name: "ref-dataset", Namespace: "default"}, fetched); err != nil {
		t.Fatalf("failed to get dataset: %v", err)
	}
	t.Logf("typed-client read back TypeMeta: kind=%q apiVersion=%q (empty proves the recovery path is exercised)",
		fetched.TypeMeta.Kind, fetched.TypeMeta.APIVersion)

	if err := CreateRuntimeForReferenceDatasetIfNotExist(c, fetched); err != nil {
		t.Fatalf("CreateRuntimeForReferenceDatasetIfNotExist failed: %v", err)
	}

	thin := &datav1alpha1.ThinRuntime{}
	if err := c.Get(ctx, types.NamespacedName{Name: "ref-dataset", Namespace: "default"}, thin); err != nil {
		t.Fatalf("ThinRuntime was not created: %v", err)
	}
	if len(thin.GetOwnerReferences()) != 1 {
		t.Fatalf("expected exactly 1 ownerReference, got %v", thin.GetOwnerReferences())
	}
	ref := thin.GetOwnerReferences()[0]
	if ref.Kind != "Dataset" || ref.APIVersion != "data.fluid.io/v1alpha1" {
		t.Errorf("ownerReference not resolved to the Dataset GVK: %+v", ref)
	}
	if ref.UID != fetched.UID || ref.Name != fetched.Name {
		t.Errorf("ownerReference does not point at the dataset: %+v vs %s/%s", ref, fetched.Name, fetched.UID)
	}
	if ref.Controller == nil || !*ref.Controller {
		t.Errorf("ownerReference must be a controller reference, got %v", ref.Controller)
	}

	// A second reconcile must be a no-op: an unstable ownerReference would flip
	// reflect.DeepEqual every time and cause an update loop.
	rvBefore := thin.GetResourceVersion()
	if err := CreateRuntimeForReferenceDatasetIfNotExist(c, fetched); err != nil {
		t.Fatalf("second CreateRuntimeForReferenceDatasetIfNotExist failed: %v", err)
	}
	thinAfter := &datav1alpha1.ThinRuntime{}
	if err := c.Get(ctx, types.NamespacedName{Name: "ref-dataset", Namespace: "default"}, thinAfter); err != nil {
		t.Fatalf("failed to re-get ThinRuntime: %v", err)
	}
	if thinAfter.GetResourceVersion() != rvBefore {
		t.Errorf("second reconcile mutated the ThinRuntime (rv %s -> %s): ownerReference is not stable",
			rvBefore, thinAfter.GetResourceVersion())
	}
}
