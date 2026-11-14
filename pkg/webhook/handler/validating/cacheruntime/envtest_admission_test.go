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

package cacheruntime_test

// F1 — integration (envtest) reproduction for PR #6243.
//
// Claim (contract): a metadata-only UPDATE of a CacheRuntime that was created
// WITHOUT an explicit `spec.client` object (e.g. the mooncake client-less
// topology, or any manifest that omits the client block) must be allowed by the
// `update.cacheruntime.fluid.io` webhook, because only `spec` differences may be
// denied.
//
// Suspected mechanism (matches the kind-e2e CI failure of this PR on all five
// k8s versions, "mooncake cache runtime (client-less topology) e2e"):
//
//   1. The CRD defaults `spec.client.cleanPolicy` to `OnRuntimeDeleted`, but
//      structural-schema defaulting only fills defaults INSIDE objects that are
//      present. A manifest without `client:` is therefore stored with no client
//      object at all (kubectl describe shows no Client section).
//   2. The controller does a typed Get -> mutate metadata (ownerReference /
//      finalizer, see RuntimeReconciler.AddOwnerAndRequeue /
//      AddFinalizerAndRequeue) -> full-object Update. The typed client always
//      serializes the zero `CacheRuntimeClientSpec` struct as `"client":{}` —
//      an object that IS present — so the API server now applies the
//      cleanPolicy default to the request body.
//   3. Admission compares oldObject (no client, cleanPolicy "") with the
//      defaulted newObject (cleanPolicy "OnRuntimeDeleted") and denies:
//      "spec.client.cleanPolicy: Forbidden: field is immutable".
//      The dataset then never reaches Bound.
//
// This is the red that reproduces the bug. After a fix it must be green.
// Subtests "control" and "guard" pin the surrounding behavior so a red here
// cannot be a wiring false-negative:
//   control — the same metadata-only update on a CacheRuntime WITH an explicit
//             client object is allowed (cleanPolicy was defaulted at create).
//   guard   — a spec.worker.tieredStore change IS denied (the webhook is wired
//             and enforcing).

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/webhook/handler/validating/cacheruntime"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var envtestScheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(envtestScheme))
	utilruntime.Must(datav1alpha1.AddToScheme(envtestScheme))
}

// findEnvtestAssets locates the envtest control-plane binaries, honoring
// KUBEBUILDER_ASSETS and the setup-envtest download directories.
func findEnvtestAssets(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("KUBEBUILDER_ASSETS"); v != "" {
		return v
	}
	roots := []string{
		filepath.Join(os.Getenv("HOME"), ".local/share/kubebuilder-envtest/k8s"),
		"/usr/local/kubebuilder/bin",
	}
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		// prefer the newest-looking version directories
		sortStringsDesc(names)
		for _, n := range names {
			dir := filepath.Join(root, n)
			if _, err := os.Stat(filepath.Join(dir, "kube-apiserver")); err == nil {
				return dir
			}
		}
	}
	t.Fatalf("envtest assets not found: set KUBEBUILDER_ASSETS or install them with `setup-envtest use <version>`")
	return ""
}

func sortStringsDesc(s []string) {
	// simple insertion sort descending (tiny lists)
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] > s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

type tLogWriter struct{ t *testing.T }

func (w tLogWriter) Write(p []byte) (int, error) {
	w.t.Log(strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

func TestValidatingWebhookAdmissionAgainstRealAPIServer(t *testing.T) {
	logf.SetLogger(zap.New(zap.WriteTo(tLogWriter{t}), zap.UseDevMode(true)))

	testdata, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatal(err)
	}
	crdDir, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "..", "config", "crd", "bases"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(crdDir); err != nil {
		t.Fatalf("CRD dir not found (run from the package dir): %v", err)
	}

	env := &envtest.Environment{
		CRDDirectoryPaths:     []string{crdDir},
		BinaryAssetsDirectory: findEnvtestAssets(t),
		ErrorIfCRDPathMissing: true,
		WebhookInstallOptions: envtest.WebhookInstallOptions{
			Paths: []string{testdata},
		},
	}
	cfg, err := env.Start()
	if err != nil {
		t.Fatalf("envtest start failed: %v", err)
	}
	defer func() {
		_ = env.Stop()
	}()

	// ---- run the real webhook handler on the envtest-provided endpoint ----
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:  envtestScheme,
		Metrics: metricsserver.Options{BindAddress: "0"},
		WebhookServer: webhook.NewServer(webhook.Options{
			Host:    env.WebhookInstallOptions.LocalServingHost,
			Port:    env.WebhookInstallOptions.LocalServingPort,
			CertDir: env.WebhookInstallOptions.LocalServingCertDir,
		}),
	})
	if err != nil {
		t.Fatalf("manager setup failed: %v", err)
	}

	h := &cacheruntime.SpecUpdateHandler{}
	h.Setup(nil, nil, admission.NewDecoder(envtestScheme))
	mgr.GetWebhookServer().Register("/"+common.WebhookValidateCacheRuntimePath, &webhook.Admission{Handler: h})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		if err := mgr.Start(ctx); err != nil {
			t.Errorf("manager exited: %v", err)
		}
	}()

	// wait for the webhook server to accept connections
	addr := net.JoinHostPort(env.WebhookInstallOptions.LocalServingHost, fmt.Sprintf("%d", env.WebhookInstallOptions.LocalServingPort))
	deadline := time.Now().Add(30 * time.Second)
	for {
		conn, dialErr := net.DialTimeout("tcp", addr, time.Second)
		if dialErr == nil {
			_ = conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("webhook server never came up on %s", addr)
		}
		time.Sleep(200 * time.Millisecond)
	}

	k8sCli, err := client.New(cfg, client.Options{Scheme: envtestScheme})
	if err != nil {
		t.Fatalf("client setup failed: %v", err)
	}

	vwc := &admissionregistrationv1.ValidatingWebhookConfiguration{}
	if err := k8sCli.Get(ctx, types.NamespacedName{Name: "fluid-cacheruntime-validating-webhook"}, vwc); err != nil {
		t.Fatalf("validating webhook configuration not installed: %v", err)
	}

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "cacheruntime-envtest"}}
	if err := k8sCli.Create(ctx, ns); err != nil {
		t.Fatalf("namespace create failed: %v", err)
	}

	// createRaw creates a CacheRuntime whose JSON keeps exactly the keys of the
	// manifest (in particular, it can omit `client` entirely — the typed client
	// would always serialize the zero struct as `"client":{}`).
	createRaw := func(t *testing.T, name, specJSON string) {
		t.Helper()
		obj := map[string]interface{}{}
		raw := fmt.Sprintf(`{"apiVersion":"data.fluid.io/v1alpha1","kind":"CacheRuntime","metadata":{"name":%q,"namespace":"cacheruntime-envtest"},"spec":%s}`, name, specJSON)
		if err := json.Unmarshal([]byte(raw), &obj); err != nil {
			t.Fatal(err)
		}
		u := &unstructured.Unstructured{Object: obj}
		if err := k8sCli.Create(ctx, u); err != nil {
			t.Fatalf("create %s failed: %v", name, err)
		}
	}

	// ---- F1: clientless CacheRuntime (mooncake topology: no `client` key at all) ----
	t.Run("F1: metadata-only update of a clientless CacheRuntime is allowed", func(t *testing.T) {
		spec := `{"runtimeClassName":"test-class","master":{"replicas":1},"worker":{"replicas":1,"tieredStore":{"levels":[{"emptyDir":{"quota":"1Gi"}}]}}}`
		createRaw(t, "no-client", spec)

		// Mimic the controller: typed Get -> mutate metadata -> full-object Update.
		got := &datav1alpha1.CacheRuntime{}
		if err := k8sCli.Get(ctx, types.NamespacedName{Name: "no-client", Namespace: "cacheruntime-envtest"}, got); err != nil {
			t.Fatal(err)
		}
		got.SetFinalizers([]string{"example.fluid.io/test"})
		err := k8sCli.Update(ctx, got)
		if err != nil {
			t.Fatalf("metadata-only update was denied: %v\nPR #6243: the CRD default of spec.client.cleanPolicy materializes on the defaulted update request body while the stored object has no client object, so the webhook sees a spec difference the user never made", err)
		}
	})

	// ---- control: CacheRuntime WITH an explicit client object ----
	t.Run("control: metadata-only update with explicit client spec is allowed", func(t *testing.T) {
		spec := `{"runtimeClassName":"test-class","master":{"replicas":1},"worker":{"replicas":1},"client":{"options":{"debug":"false"}}}`
		createRaw(t, "with-client", spec)

		got := &datav1alpha1.CacheRuntime{}
		if err := k8sCli.Get(ctx, types.NamespacedName{Name: "with-client", Namespace: "cacheruntime-envtest"}, got); err != nil {
			t.Fatal(err)
		}
		got.SetFinalizers([]string{"example.fluid.io/test"})
		if err := k8sCli.Update(ctx, got); err != nil {
			t.Fatalf("metadata-only update was denied even with an explicit client object: %v", err)
		}
	})

	// ---- guard: the webhook is wired and denying real spec changes ----
	t.Run("guard: tieredStore change is denied", func(t *testing.T) {
		spec := `{"runtimeClassName":"test-class","master":{"replicas":1},"worker":{"replicas":1,"tieredStore":{"levels":[{"emptyDir":{"quota":"1Gi"}}]}}}`
		createRaw(t, "tieredstore-change", spec)

		got := &datav1alpha1.CacheRuntime{}
		if err := k8sCli.Get(ctx, types.NamespacedName{Name: "tieredstore-change", Namespace: "cacheruntime-envtest"}, got); err != nil {
			t.Fatal(err)
		}
		got.Spec.Worker.TieredStore.Levels[0].EmptyDir.Quota = resource.MustParse("4Gi")
		err := k8sCli.Update(ctx, got)
		if err == nil {
			t.Fatal("a tieredStore change was allowed: the webhook is not wired, so the F1 result above is a wiring false-negative")
		}
		if !strings.Contains(err.Error(), "spec.worker.tieredStore") {
			t.Fatalf("tieredStore change denied for an unexpected reason: %v", err)
		}
	})
}
