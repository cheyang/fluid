package utils

// Integration layer (envtest, real API server) for the PR #6199 verification harness.
// Skipped unless KUBEBUILDER_ASSETS is set. Proves the premise chain of issue #6140 end to end:
// the apiVersion the pre-#6199 implementation could pass through ("data.fluid.io/", version
// missing) is rejected by a real API server, while the converged implementation's output is
// accepted. Polarity: CONTRACT on the patched code.

import (
	"context"
	"os"
	"strings"
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func TestVerifyOwnerRefAPIServerValidation(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS not set; skipping envtest integration layer")
	}

	env := &envtest.Environment{}
	cfg, err := env.Start()
	if err != nil {
		t.Fatalf("failed to start envtest: %v", err)
	}
	defer func() {
		if err := env.Stop(); err != nil {
			t.Logf("failed to stop envtest: %v", err)
		}
	}()

	k8sClient, err := client.New(cfg, client.Options{})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	ctx := context.TODO()

	// What the pre-#6199 local helper produced for TypeMeta{Kind:"Dataset", APIVersion:"data.fluid.io/"}.
	malformed := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "verify-malformed-ownerref",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "data.fluid.io/",
				Kind:       "Dataset",
				Name:       "some-dataset",
				UID:        types.UID("uid-verify"),
				Controller: func() *bool { b := true; return &b }(),
			}},
		},
	}
	err = k8sClient.Create(ctx, malformed)
	if err == nil {
		t.Errorf("expected the API server to reject an ownerReference with apiVersion %q (version missing), but create succeeded", "data.fluid.io/")
	} else {
		t.Logf("malformed ownerReference rejected as expected: %v", err)
		if !strings.Contains(err.Error(), "data.fluid.io/") {
			t.Errorf("rejection reason does not mention the malformed apiVersion: %v", err)
		}
	}

	// What the converged implementation produces for the same input shape.
	dataset := &datav1alpha1.Dataset{
		TypeMeta:   metav1.TypeMeta{Kind: "Dataset", APIVersion: "data.fluid.io/"},
		ObjectMeta: metav1.ObjectMeta{Name: "some-dataset", Namespace: "default", UID: "uid-verify"},
	}
	ref := datasetControllerOwnerReference(dataset)
	wellformed := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "verify-wellformed-ownerref",
			Namespace:       "default",
			OwnerReferences: []metav1.OwnerReference{ref},
		},
	}
	if err := k8sClient.Create(ctx, wellformed); err != nil {
		t.Errorf("expected the API server to accept the converged ownerReference %+v, got error: %v", ref, err)
	}
}
