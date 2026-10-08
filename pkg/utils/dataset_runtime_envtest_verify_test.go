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

package utils

// Review-verification harness for PR #6199, integration layer (envtest, real API server).
//
// Proves the end-to-end property the helper exists for: a ThinRuntime created/adopted for a
// reference dataset carries an ownerReference the API server accepts AND that resolves back
// to the Dataset through the RESTMapper (the mechanism of the owner-based watch). Also proves
// the premise's mechanism with counterfactuals: an ownerReference with empty kind/apiVersion
// (or with the malformed "data.fluid.io/" shape the legacy code copied verbatim) is rejected
// by the real API server.
//
// Polarity: contract. Skips when no envtest binaries are available.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func envtestAssetsAvailable() bool {
	if dir := os.Getenv("KUBEBUILDER_ASSETS"); dir != "" {
		_, err := os.Stat(filepath.Join(dir, "kube-apiserver"))
		return err == nil
	}
	_, err := os.Stat("/usr/local/kubebuilder/bin/kube-apiserver")
	return err == nil
}

func TestVerifyOwnerRefAgainstRealAPIServer(t *testing.T) {
	if !envtestAssetsAvailable() {
		t.Skip("envtest binaries not found (set KUBEBUILDER_ASSETS); skipping integration layer")
	}

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

	testScheme := runtime.NewScheme()
	if err := datav1alpha1.AddToScheme(testScheme); err != nil {
		t.Fatalf("failed to register scheme: %v", err)
	}
	c, err := client.New(cfg, client.Options{Scheme: testScheme})
	if err != nil {
		t.Fatalf("failed to build client: %v", err)
	}
	ctx := context.TODO()

	// 1. Create a reference dataset through the real API server and read it back with a typed
	//    client, the same way the dataset controller does.
	dataset := &datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: "verify-ref-dataset", Namespace: "default"},
	}
	if err := c.Create(ctx, dataset); err != nil {
		t.Fatalf("failed to create dataset: %v", err)
	}
	got := &datav1alpha1.Dataset{}
	if err := c.Get(ctx, types.NamespacedName{Name: "verify-ref-dataset", Namespace: "default"}, got); err != nil {
		t.Fatalf("failed to get dataset: %v", err)
	}
	t.Logf("typed-client Get returned TypeMeta: kind=%q apiVersion=%q (empty TypeMeta is what motivates the recovery logic)",
		got.TypeMeta.Kind, got.TypeMeta.APIVersion)

	// 2. Run the function under review exactly as the dataset controller calls it.
	if err := CreateRuntimeForReferenceDatasetIfNotExist(c, got); err != nil {
		t.Fatalf("CreateRuntimeForReferenceDatasetIfNotExist failed: %v", err)
	}

	// 3. The persisted ThinRuntime must carry a complete controller ownerReference. The real
	//    API server accepted the create, which already proves server-side validation passed.
	thin := &datav1alpha1.ThinRuntime{}
	if err := c.Get(ctx, types.NamespacedName{Name: "verify-ref-dataset", Namespace: "default"}, thin); err != nil {
		t.Fatalf("failed to get created thinRuntime: %v", err)
	}
	refs := thin.GetOwnerReferences()
	if len(refs) != 1 {
		t.Fatalf("expected exactly 1 ownerReference, got %d: %v", len(refs), refs)
	}
	ref := refs[0]
	if ref.Kind != "Dataset" || ref.APIVersion != "data.fluid.io/v1alpha1" {
		t.Errorf("ownerReference GVK = {%s %s}, want {Dataset data.fluid.io/v1alpha1}", ref.Kind, ref.APIVersion)
	}
	if ref.Controller == nil || !*ref.Controller {
		t.Errorf("ownerReference Controller = %v, want true", ref.Controller)
	}
	if ref.UID != got.GetUID() {
		t.Errorf("ownerReference UID = %q, want dataset UID %q", ref.UID, got.GetUID())
	}

	// 4. The owner-based watch resolves a dependent back to its owner through the RESTMapper;
	//    prove the produced GVK is resolvable on the real API server.
	httpClient, err := rest.HTTPClientFor(cfg)
	if err != nil {
		t.Fatalf("failed to build http client: %v", err)
	}
	mapper, err := apiutil.NewDynamicRESTMapper(cfg, httpClient)
	if err != nil {
		t.Fatalf("failed to build RESTMapper: %v", err)
	}
	mapping, err := mapper.RESTMapping(schema.GroupKind{Group: "data.fluid.io", Kind: ref.Kind}, "v1alpha1")
	if err != nil {
		t.Errorf("owner GVK of the persisted reference is not resolvable via RESTMapper: %v", err)
	} else {
		t.Logf("owner GVK resolves to resource %s", mapping.Resource.Resource)
	}

	// 5. Counterfactuals: the shapes the recovery logic exists to avoid must be rejected by
	//    the real API server. This validates the premise's mechanism ("an ownerReference
	//    missing its kind or its apiVersion is rejected by the API server").
	mkThin := func(name, kind, apiVersion string) *datav1alpha1.ThinRuntime {
		return &datav1alpha1.ThinRuntime{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "default",
				OwnerReferences: []metav1.OwnerReference{{
					Kind:       kind,
					APIVersion: apiVersion,
					Name:       "verify-ref-dataset",
					UID:        got.GetUID(),
					Controller: ptr.To(true),
				}},
			},
		}
	}
	counterfactuals := []struct {
		name       string
		kind       string
		apiVersion string
		wantErrSub string
	}{
		{name: "cf-empty-gvk", kind: "", apiVersion: "", wantErrSub: "must not be empty"},
		{name: "cf-empty-version", kind: "Dataset", apiVersion: "data.fluid.io/", wantErrSub: "version must not be empty"},
	}
	for _, cf := range counterfactuals {
		err := c.Create(ctx, mkThin(cf.name, cf.kind, cf.apiVersion))
		if err == nil {
			t.Errorf("counterfactual %s: API server ACCEPTED ownerReference {kind=%q apiVersion=%q}; expected rejection",
				cf.name, cf.kind, cf.apiVersion)
			continue
		}
		t.Logf("counterfactual %s rejected as expected: %v", cf.name, err)
		if cf.wantErrSub != "" && !strings.Contains(err.Error(), cf.wantErrSub) {
			t.Errorf("counterfactual %s: rejection %q does not contain %q", cf.name, err.Error(), cf.wantErrSub)
		}
	}
}
