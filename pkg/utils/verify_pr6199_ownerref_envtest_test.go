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

// Integration-layer verification harness for the review of PR #6199 (Reviewer B / Codex).
// Additive only: no production code is modified by this file.
// Runs against a real API server via envtest (KUBEBUILDER_ASSETS must point at
// kube-apiserver+etcd binaries); skipped automatically when assets are absent.
//
// Claim C3 (contract): end to end, CreateRuntimeForReferenceDatasetIfNotExist
// creates and adopts a ThinRuntime whose controller ownerReference carries a
// complete, RESTMapper-resolvable GVK, even though a typed client hands the
// Dataset back with an empty TypeMeta (the production condition).
//
// Claim C4 (observation): whether the API server rejects a ThinRuntime whose
// ownerReference has empty kind/apiVersion — the motivating sub-claim of
// issue #6140 ("rejected by the API server"). Recorded either way.

package utils

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func TestVerifyPR6199EnvtestOwnerReference(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS not set, skipping envtest layer")
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

	sch := runtime.NewScheme()
	if err := datav1alpha1.AddToScheme(sch); err != nil {
		t.Fatalf("failed to register scheme: %v", err)
	}
	c, err := client.New(cfg, client.Options{Scheme: sch})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	ctx := context.TODO()

	// 1. Create a reference Dataset through the real API server.
	dataset := &datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: "verify-pr6199", Namespace: "default"},
		Spec:       datav1alpha1.DatasetSpec{Mounts: []datav1alpha1.Mount{{MountPoint: "pvc://verify-pr6199-pvc", Name: "verify-pr6199-pvc"}}},
	}
	if err := c.Create(ctx, dataset); err != nil {
		t.Fatalf("failed to create dataset: %v", err)
	}

	// 2. Read it back with the typed client: TypeMeta must come back empty,
	//    which is the production condition under which the recovery matters.
	fetched := &datav1alpha1.Dataset{}
	if err := c.Get(ctx, client.ObjectKey{Name: "verify-pr6199", Namespace: "default"}, fetched); err != nil {
		t.Fatalf("failed to get dataset: %v", err)
	}
	gvk := fetched.GetObjectKind().GroupVersionKind()
	if !gvk.Empty() {
		t.Fatalf("precondition violated: typed client returned non-empty TypeMeta %v; the recovery path would not be exercised", gvk)
	}
	t.Logf("confirmed: typed client returns Dataset with empty TypeMeta (uid=%s)", fetched.GetUID())

	// 3. Exercise the function under review end to end with the real client.
	if err := CreateRuntimeForReferenceDatasetIfNotExist(c, fetched); err != nil {
		t.Fatalf("CreateRuntimeForReferenceDatasetIfNotExist failed: %v", err)
	}

	created := &datav1alpha1.ThinRuntime{}
	if err := c.Get(ctx, client.ObjectKey{Name: "verify-pr6199", Namespace: "default"}, created); err != nil {
		t.Fatalf("thinRuntime was not created: %v", err)
	}
	if len(created.GetOwnerReferences()) != 1 {
		t.Fatalf("expected exactly 1 ownerReference on created thinRuntime, got %d: %v", len(created.GetOwnerReferences()), created.GetOwnerReferences())
	}
	ref := created.GetOwnerReferences()[0]
	if ref.Kind != datav1alpha1.Datasetkind || ref.APIVersion != datav1alpha1.GroupVersion.String() {
		t.Errorf("C3: created thinRuntime ownerReference GVK = %s/%s, want %s/%s",
			ref.APIVersion, ref.Kind, datav1alpha1.GroupVersion.String(), datav1alpha1.Datasetkind)
	}
	if ref.Controller == nil || !*ref.Controller {
		t.Errorf("C3: created thinRuntime ownerReference Controller = %v, want true", ref.Controller)
	}
	if ref.UID != fetched.GetUID() || ref.Name != fetched.GetName() {
		t.Errorf("C3: ownerReference identity = %s/%s, want %s/%s", ref.Name, ref.UID, fetched.GetName(), fetched.GetUID())
	}

	// 4. The owner-based watch of the dataset controller resolves dependents via
	//    RESTMapper on the ownerReference GVK; prove the emitted GVK resolves.
	httpClient, err := rest.HTTPClientFor(cfg)
	if err != nil {
		t.Fatalf("failed to build http client: %v", err)
	}
	mapper, err := apiutil.NewDynamicRESTMapper(cfg, httpClient)
	if err != nil {
		t.Fatalf("failed to build discovery RESTMapper: %v", err)
	}
	parsedGV, err := schema.ParseGroupVersion(ref.APIVersion)
	if err != nil {
		t.Fatalf("ownerReference apiVersion %q does not parse: %v", ref.APIVersion, err)
	}
	mapping, err := mapper.RESTMapping(schema.GroupKind{Group: parsedGV.Group, Kind: ref.Kind}, parsedGV.Version)
	if err != nil {
		t.Errorf("C3: ownerReference GVK %s/%s is not resolvable through the RESTMapper: %v", ref.APIVersion, ref.Kind, err)
	} else if mapping.Resource.Resource != "datasets" {
		t.Errorf("C3: ownerReference GVK resolves to resource %q, want %q", mapping.Resource.Resource, "datasets")
	}

	// 5. Adoption / repair path against the real API server: pre-create a
	//    thinRuntime carrying an ownerReference with empty kind/apiVersion
	//    (what the dataset controller produced before #6138), then reconcile.
	legacy := &datav1alpha1.ThinRuntime{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "verify-pr6199-legacy",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "",
				Kind:       "",
				Name:       "verify-pr6199-legacy",
				UID:        "3e108dcc-9aab-4d0b-99dc-9976d5cd6d5a",
				Controller: func() *bool { b := true; return &b }(),
			}},
		},
	}
	createErr := c.Create(ctx, legacy)
	if createErr != nil {
		// C4 observation: API server rejected the malformed ownerReference.
		t.Logf("C4: API server REJECTED thinRuntime with empty ownerReference kind/apiVersion: %v", createErr)
	} else {
		t.Logf("C4: API server ACCEPTED thinRuntime with empty ownerReference kind/apiVersion (uid=%s)", legacy.GetUID())

		legacyDataset := &datav1alpha1.Dataset{
			ObjectMeta: metav1.ObjectMeta{Name: "verify-pr6199-legacy", Namespace: "default", UID: "3e108dcc-9aab-4d0b-99dc-9976d5cd6d5a"},
			Spec:       datav1alpha1.DatasetSpec{Mounts: []datav1alpha1.Mount{{MountPoint: "pvc://verify-pr6199-legacy-pvc", Name: "verify-pr6199-legacy-pvc"}}},
		}
		if err := c.Create(ctx, legacyDataset); err != nil {
			t.Fatalf("failed to create legacy dataset: %v", err)
		}
		gotLegacyDs := &datav1alpha1.Dataset{}
		if err := c.Get(ctx, client.ObjectKey{Name: "verify-pr6199-legacy", Namespace: "default"}, gotLegacyDs); err != nil {
			t.Fatalf("failed to get legacy dataset: %v", err)
		}
		// The dataset UID is assigned by the server; align the scenario with a
		// real leftover whose ownerReference points at a deleted-and-recreated
		// dataset only by name: keep the recorded UID to test pure GVK repair.
		gotLegacyDs.SetUID("3e108dcc-9aab-4d0b-99dc-9976d5cd6d5a")

		if err := CreateRuntimeForReferenceDatasetIfNotExist(c, gotLegacyDs); err != nil {
			t.Fatalf("adoption/repair failed: %v", err)
		}
		repaired := &datav1alpha1.ThinRuntime{}
		if err := c.Get(ctx, client.ObjectKey{Name: "verify-pr6199-legacy", Namespace: "default"}, repaired); err != nil {
			t.Fatalf("failed to get repaired thinRuntime: %v", err)
		}
		if len(repaired.GetOwnerReferences()) != 1 {
			t.Fatalf("expected exactly 1 ownerReference after repair, got %d: %v", len(repaired.GetOwnerReferences()), repaired.GetOwnerReferences())
		}
		rref := repaired.GetOwnerReferences()[0]
		if rref.Kind != datav1alpha1.Datasetkind || rref.APIVersion != datav1alpha1.GroupVersion.String() {
			t.Errorf("C3: repaired ownerReference GVK = %s/%s, want %s/%s",
				rref.APIVersion, rref.Kind, datav1alpha1.GroupVersion.String(), datav1alpha1.Datasetkind)
		}
		if rref.Controller == nil || !*rref.Controller {
			t.Errorf("C3: repaired ownerReference Controller = %v, want true", rref.Controller)
		}
	}

	// Keep cfg referenced in case future assertions need raw discovery.
	var _ *rest.Config = cfg
}
