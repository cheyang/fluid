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

// Package defaultingtest is the L2 (integration) layer of the PR #6243
// verification harness (reviewer: Codex). It runs the real CacheRuntime CRD
// against a real API server (envtest) and proves the two API-server facts the
// F1 mechanism rests on, then composes them with the webhook's validator:
//
//  1. A CacheRuntime created from a payload without spec.client is stored
//     WITHOUT spec.client (CRD defaulting does not materialize absent parents).
//  2. A full-object Update from a typed Go client — which marshals
//     "client":{} — causes the API server to materialize
//     spec.client.cleanPolicy="OnRuntimeDeleted" in the stored object.
//  3. Therefore the old/new pair a validating webhook sees for a metadata-only
//     update differs in spec.client.cleanPolicy, and (on the PR head)
//     cacheruntime.ValidateSpecUpdate forbids the update.
//
// The test is skipped when no envtest assets are available
// (KUBEBUILDER_ASSETS unset and no ~/.local/share/kubebuilder-envtest).
package defaultingtest

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/webhook/handler/validating/cacheruntime"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

var cacheRuntimeGVK = schema.GroupVersionKind{Group: "data.fluid.io", Version: "v1alpha1", Kind: "CacheRuntime"}

func envtestAssets(t *testing.T) string {
	t.Helper()
	if assets := os.Getenv("KUBEBUILDER_ASSETS"); assets != "" {
		return assets
	}
	matches, _ := filepath.Glob(filepath.Join(os.Getenv("HOME"), ".local/share/kubebuilder-envtest/k8s/*"))
	if len(matches) == 0 {
		t.Skip("no envtest assets: set KUBEBUILDER_ASSETS or install ~/.local/share/kubebuilder-envtest/k8s/*")
	}
	sort.Strings(matches)
	return matches[len(matches)-1]
}

func TestClientlessCacheRuntimeDefaultingAsymmetry(t *testing.T) {
	t.Setenv("KUBEBUILDER_ASSETS", envtestAssets(t))

	scheme := runtime.NewScheme()
	if err := datav1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add datav1alpha1 scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add corev1 scheme: %v", err)
	}

	env := &envtest.Environment{
		Scheme:                scheme,
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "..", "..", "..", "charts", "fluid", "fluid", "crds")},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := env.Start()
	if err != nil {
		t.Fatalf("start envtest: %v", err)
	}
	defer func() {
		if err := env.Stop(); err != nil {
			t.Errorf("stop envtest: %v", err)
		}
	}()

	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "verify-pr6243"}}
	if err := c.Create(ctx, ns); err != nil {
		t.Fatalf("create namespace: %v", err)
	}

	// Create the CacheRuntime from an UNSTRUCTURED payload with no spec.client,
	// exactly what `kubectl create -f test/gha-e2e/mooncake/cacheruntime.yaml` sends.
	stored := &unstructured.Unstructured{}
	stored.SetGroupVersionKind(cacheRuntimeGVK)
	stored.SetName("mooncake-demo")
	stored.SetNamespace("verify-pr6243")
	stored.Object["spec"] = map[string]interface{}{
		"runtimeClassName": "mooncake-demo",
		"master":           map[string]interface{}{"replicas": int64(1)},
		"worker": map[string]interface{}{
			"replicas": int64(1),
			"tieredStore": map[string]interface{}{
				"levels": []interface{}{map[string]interface{}{
					"low": "0.5", "high": "0.8",
					"emptyDir": map[string]interface{}{"quota": "1Gi"},
				}},
			},
		},
	}
	if err := c.Create(ctx, stored); err != nil {
		t.Fatalf("create client-less CacheRuntime: %v", err)
	}

	beforeRaw, err := json.Marshal(stored)
	if err != nil {
		t.Fatalf("marshal pre-update stored form: %v", err)
	}

	// Fact 1: the stored form has NO spec.client at all.
	if _, found, _ := unstructured.NestedMap(stored.Object, "spec", "client"); found {
		t.Fatalf("model broken: stored form unexpectedly has spec.client: %s", beforeRaw)
	}

	// Fact 2: a typed full-object Update (what the CacheRuntime controller does
	// to add a finalizer or an owner reference) materializes the CRD default
	// spec.client.cleanPolicy=OnRuntimeDeleted in the stored object.
	typed := &datav1alpha1.CacheRuntime{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: "verify-pr6243", Name: "mooncake-demo"}, typed); err != nil {
		t.Fatalf("typed get: %v", err)
	}
	typed.Finalizers = append(typed.Finalizers, "fluid-cacheruntime-controller-finalizer")
	if err := c.Update(ctx, typed); err != nil {
		t.Fatalf("typed metadata-only update: %v", err)
	}

	after := &unstructured.Unstructured{}
	after.SetGroupVersionKind(cacheRuntimeGVK)
	if err := c.Get(ctx, client.ObjectKey{Namespace: "verify-pr6243", Name: "mooncake-demo"}, after); err != nil {
		t.Fatalf("get after update: %v", err)
	}
	cleanPolicy, found, err := unstructured.NestedString(after.Object, "spec", "client", "cleanPolicy")
	if err != nil || !found || cleanPolicy != "OnRuntimeDeleted" {
		t.Fatalf("model broken: expected API server to materialize spec.client.cleanPolicy=OnRuntimeDeleted, got %q (found=%v, err=%v)", cleanPolicy, found, err)
	}
	t.Logf("fact 2 confirmed: after a typed full-object update the stored object gained spec.client.cleanPolicy=%q", cleanPolicy)

	// Composition: the old/new pair a validating webhook receives for this
	// metadata-only update (pre-update stored form vs post-defaulting form).
	afterRaw, err := json.Marshal(after)
	if err != nil {
		t.Fatalf("marshal post-update form: %v", err)
	}
	oldObj := &datav1alpha1.CacheRuntime{}
	if err := json.Unmarshal(beforeRaw, oldObj); err != nil {
		t.Fatalf("decode old: %v", err)
	}
	newObj := &datav1alpha1.CacheRuntime{}
	if err := json.Unmarshal(afterRaw, newObj); err != nil {
		t.Fatalf("decode new: %v", err)
	}

	errs := cacheruntime.ValidateSpecUpdate(&oldObj.Spec, &newObj.Spec)
	if len(errs) > 0 {
		t.Errorf("F1 reproduced against a real API server: metadata-only update of a client-less CacheRuntime is denied: %v", errs)
	}
}
