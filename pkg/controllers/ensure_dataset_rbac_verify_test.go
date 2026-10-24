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

package controllers

// Review verification harness for PR #5674 (fluid-cloudnative/fluid), L2 integration layer.
// Finding F5 (contract polarity).
//
// Question: does the real API server accept the Dataset Create issued by
// RuntimeReconciler.ensureDatasetForRuntime when the creating identity has exactly the
// permissions the shipped runtime-controller ClusterRoles grant (charts/fluid/fluid/
// templates/role/<runtime>/rbac.yaml: create/update/patch/get/list/watch on datasets,
// nothing on <runtime>/finalizers)?
//
// ensureDatasetForRuntime builds the Dataset with controllerutil.SetControllerReference,
// which sets Controller=true AND BlockOwnerDeletion=true on the ownerReference to the
// Runtime. Kubernetes rejects creates that set blockOwnerDeletion unless the creator is
// allowed to update the owner's finalizers subresource. This test runs that exact Create
// against a real (envtest) API server, once as the limited creator and once as a control
// after granting <runtime>/finalizers, so the failure and its fix are both observable.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	rbacv1 "k8s.io/api/rbac/v1"
	corev1 "k8s.io/api/core/v1"
)

func boolPtr(b bool) *bool { return &b }

func TestAutoCreateDatasetCreateRBACContract(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS not set; envtest unavailable, skipping L2 integration check")
	}

	ctx := context.Background()
	testEnv := &envtest.Environment{
		UseExistingCluster: boolPtr(false),
		CRDDirectoryPaths:  []string{filepath.Join("..", "..", "config", "crd", "bases")},
	}
	cfg, err := testEnv.Start()
	if err != nil {
		t.Fatalf("failed to start envtest: %v", err)
	}
	defer func() { _ = testEnv.Stop() }()

	sch := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(sch)
	_ = datav1alpha1.AddToScheme(sch)

	admin, err := client.New(cfg, client.Options{Scheme: sch})
	if err != nil {
		t.Fatalf("failed to build admin client: %v", err)
	}

	// The limited creator mirrors the shipped alluxioruntime-controller ClusterRole:
	// full CRUD on datasets, NO permission on alluxioruntimes/finalizers.
	limitedCfg, err := testEnv.AddUser(envtest.User{Name: "verify-limited-creator"}, cfg)
	if err != nil {
		t.Fatalf("failed to add limited user: %v", err)
	}
	limited, err := client.New(limitedCfg.Config(), client.Options{Scheme: sch})
	if err != nil {
		t.Fatalf("failed to build limited client: %v", err)
	}

	cr := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: "verify-dataset-creator"},
		Rules: []rbacv1.PolicyRule{{
			APIGroups: []string{"data.fluid.io"},
			Resources: []string{"datasets"},
			Verbs:     []string{"create", "get", "list", "watch", "update", "patch"},
		}},
	}
	if err := admin.Create(ctx, cr); err != nil {
		t.Fatalf("failed to create ClusterRole: %v", err)
	}
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "verify-dataset-creator"},
		RoleRef:    rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "ClusterRole", Name: cr.Name},
		Subjects:   []rbacv1.Subject{{Kind: rbacv1.UserKind, APIGroup: "rbac.authorization.k8s.io", Name: "verify-limited-creator"}},
	}
	if err := admin.Create(ctx, crb); err != nil {
		t.Fatalf("failed to create ClusterRoleBinding: %v", err)
	}

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "verify-autocreate"}}
	if err := admin.Create(ctx, ns); err != nil {
		t.Fatalf("failed to create namespace: %v", err)
	}

	runtimeObj := &datav1alpha1.AlluxioRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: ns.Name},
	}
	if err := admin.Create(ctx, runtimeObj); err != nil {
		t.Fatalf("failed to create runtime: %v", err)
	}

	buildDataset := func(name string) *datav1alpha1.Dataset {
		ds := &datav1alpha1.Dataset{
			ObjectMeta: metav1.ObjectMeta{
				Name:        name,
				Namespace:   ns.Name,
				Annotations: map[string]string{"fluid.io/dataset-policy": "auto-create"},
			},
		}
		if err := controllerutil.SetControllerReference(runtimeObj, ds, sch); err != nil {
			t.Fatalf("failed to set controller reference: %v", err)
		}
		return ds
	}

	// Negative control: the limited user must NOT be able to create unrelated
	// objects, proving RBAC is actually enforced for this identity (otherwise the
	// refutation below would be meaningless).
	if err := limited.Create(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "should-be-forbidden", Namespace: ns.Name}}); err == nil {
		t.Fatalf("negative control failed: limited user could create a ConfigMap, RBAC is not enforced in this envtest")
	}

	// The exact Create ensureDatasetForRuntime performs, as the limited creator.
	err = limited.Create(ctx, buildDataset("demo"))
	if err != nil {
		t.Errorf("F5 CONFIRMED: Dataset Create with blockOwnerDeletion ownerReference rejected for identity without <runtime>/finalizers update permission: %v", err)
	} else {
		t.Logf("F5 REFUTED: limited creator could create the Dataset (no blockOwnerDeletion admission check hit)")
	}

	// Control: same create as cluster-admin (what the fake-client unit tests effectively do).
	if err := admin.Create(ctx, buildDataset("demo-admin")); err != nil {
		t.Errorf("control create as admin failed: %v", err)
	}

	// Fix check: grant update on alluxioruntimes/finalizers and retry as limited creator.
	crFix := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: "verify-runtime-finalizers"},
		Rules: []rbacv1.PolicyRule{{
			APIGroups: []string{"data.fluid.io"},
			Resources: []string{"alluxioruntimes/finalizers"},
			Verbs:     []string{"update"},
		}},
	}
	if err := admin.Create(ctx, crFix); err != nil {
		t.Fatalf("failed to create fix ClusterRole: %v", err)
	}
	crbFix := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "verify-runtime-finalizers"},
		RoleRef:    rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "ClusterRole", Name: crFix.Name},
		Subjects:   []rbacv1.Subject{{Kind: rbacv1.UserKind, APIGroup: "rbac.authorization.k8s.io", Name: "verify-limited-creator"}},
	}
	if err := admin.Create(ctx, crbFix); err != nil {
		t.Fatalf("failed to create fix ClusterRoleBinding: %v", err)
	}

	if err := limited.Create(ctx, buildDataset("demo-fixed")); err != nil {
		t.Errorf("create still failed after granting <runtime>/finalizers update: %v", err)
	} else {
		t.Logf("after granting <runtime>/finalizers update the create succeeds")
	}

}
