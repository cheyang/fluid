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

// Verification harness for PR #6200 review (Reviewer B / Codex). Additive only.
//
// Integration layer (envtest, real kube-apiserver): measures actual API-server
// GET requests for the worker AdvancedStatefulSet during
// CheckAndUpdateRuntimeStatus cycles, for the two client configurations that
// matter:
//
//	A. direct (uncached) client - every client.Get reaches the API server.
//	B. informer-cached client - what fluid actually deploys for the cache
//	   controller (cmd/cache/app/cache.go wires NewFluidControllerClient;
//	   controller-runtime always sets clientOpts.Cache.Reader to the manager
//	   cache, and only Secrets are in DisableFor). Reads hit the local
//	   informer cache and never reach the API server.
//
// Claim under test (issue #5879 / PR body): the per-cycle GetNodeAffinity call
// "adds unnecessary API server load". If B shows 0 API-server GETs per cycle,
// that claim does not hold for the production-default client configuration;
// the saving is informer-cache CPU (lookup + deepcopy), not API-server load.
//
// This file uses only symbols present on the base branch so it can be grafted
// there as well. Skip when envtest binaries are unavailable.

package engine

import (
	"context"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"

	workloadv1alpha1 "github.com/fluid-cloudnative/advanced-statefulset/api/workload/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// verifyCountingTransport counts single-object API-server GETs of the worker
// AdvancedStatefulSet (list/watch requests and other resources are ignored).
type verifyCountingTransport struct {
	base       http.RoundTripper
	mu         sync.Mutex
	workerGets int
}

func (c *verifyCountingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method == http.MethodGet &&
		strings.Contains(req.URL.Path, "advancedstatefulsets/"+testStatusWorker) &&
		req.URL.Query().Get("watch") == "" {
		c.mu.Lock()
		c.workerGets++
		c.mu.Unlock()
	}
	return c.base.RoundTrip(req)
}

func (c *verifyCountingTransport) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.workerGets
}

func (c *verifyCountingTransport) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.workerGets = 0
}

func verifyCountingConfig(cfg *rest.Config, counter *verifyCountingTransport) *rest.Config {
	out := rest.CopyConfig(cfg)
	out.WrapTransport = func(rt http.RoundTripper) http.RoundTripper {
		counter.base = rt
		return counter
	}
	return out
}


// verifyEnvtestASTS builds a schema-valid AdvancedStatefulSet for envtest
// (the CRD requires spec.selector and spec.template; the fake client does not).
func verifyEnvtestASTS(name string, nodeSelector map[string]string) *workloadv1alpha1.AdvancedStatefulSet {
	replicas := int32(1)
	labels := map[string]string{"app": name}
	return &workloadv1alpha1.AdvancedStatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testStatusNamespace},
		Spec: workloadv1alpha1.AdvancedStatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					NodeSelector: nodeSelector,
					Containers:   []corev1.Container{{Name: "worker", Image: "busybox"}},
				},
			},
		},
	}
}

func TestVerifyAPIServerLoadPremise(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS not set; skipping envtest integration layer")
	}

	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{"../../../../charts/fluid/fluid/crds"},
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

	// Seed objects through a plain direct client.
	setupCounter := &verifyCountingTransport{}
	setupClient, err := ctrlclient.New(verifyCountingConfig(cfg, setupCounter), ctrlclient.Options{Scheme: CacheEngineTestScheme})
	if err != nil {
		t.Fatalf("setup client: %v", err)
	}
	ctx := context.Background()
	for _, obj := range []ctrlclient.Object{
		newStatusTestRuntime(),
		verifyEnvtestASTS(testStatusMaster, nil),
		verifyEnvtestASTS(testStatusWorker, map[string]string{"disktype": "ssd"}),
	} {
		if err := setupClient.Create(ctx, obj); err != nil {
			t.Fatalf("seed %T: %v", obj, err)
		}
	}

	// Scenario A: direct (uncached) client. Every client.Get is an API-server GET.
	counterA := &verifyCountingTransport{}
	directClient, err := ctrlclient.New(verifyCountingConfig(cfg, counterA), ctrlclient.Options{Scheme: CacheEngineTestScheme})
	if err != nil {
		t.Fatalf("direct client: %v", err)
	}
	engineA, _ := newStatusTestEngineWithClient(directClient)
	counterA.reset()
	for i := 0; i < 2; i++ {
		if _, err := engineA.CheckAndUpdateRuntimeStatus(newStatusTestRuntimeValue(false)); err != nil {
			t.Fatalf("scenario A cycle %d: %v", i, err)
		}
	}
	directGets := counterA.count()
	t.Logf("scenario A (direct client): API-server GETs of worker AdvancedStatefulSet across 2 status cycles = %d", directGets)
	if directGets < 2 {
		t.Errorf("expected >= 2 API-server GETs across 2 cycles with a direct client (>= 1 per cycle for ConstructComponentStatus), got %d", directGets)
	}

	// Scenario B: informer-cached client (fluid's production default for the
	// cache controller). Reads should be served from the local cache.
	counterB := &verifyCountingTransport{}
	cachedCfg := verifyCountingConfig(cfg, counterB)
	informerCache, err := cache.New(cachedCfg, cache.Options{Scheme: CacheEngineTestScheme})
	if err != nil {
		t.Fatalf("cache.New: %v", err)
	}
	cacheCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		if err := informerCache.Start(cacheCtx); err != nil {
			t.Logf("informer cache stopped: %v", err)
		}
	}()
	informerCache.WaitForCacheSync(cacheCtx)

	cachedClient, err := ctrlclient.New(cachedCfg, ctrlclient.Options{
		Scheme: CacheEngineTestScheme,
		Cache:  &ctrlclient.CacheOptions{Reader: informerCache},
	})
	if err != nil {
		t.Fatalf("cached client: %v", err)
	}

	// Prime the informers (runtime + worker ASTS) so the measured region below
	// contains no lazy informer startup.
	if err := cachedClient.Get(ctx, types.NamespacedName{Name: testStatusRuntime, Namespace: testStatusNamespace}, newStatusTestRuntime()); err != nil {
		t.Fatalf("prime runtime informer: %v", err)
	}
	if err := cachedClient.Get(ctx, types.NamespacedName{Name: testStatusWorker, Namespace: testStatusNamespace}, verifyEnvtestASTS(testStatusWorker, nil)); err != nil {
		t.Fatalf("prime worker informer: %v", err)
	}

	engineB, _ := newStatusTestEngineWithClient(cachedClient)
	counterB.reset()
	for i := 0; i < 2; i++ {
		if _, err := engineB.CheckAndUpdateRuntimeStatus(newStatusTestRuntimeValue(false)); err != nil {
			t.Fatalf("scenario B cycle %d: %v", i, err)
		}
	}
	cachedGets := counterB.count()
	t.Logf("scenario B (informer-cached client, production default): API-server GETs of worker AdvancedStatefulSet across 2 status cycles = %d", cachedGets)
	if cachedGets != 0 {
		t.Errorf("expected 0 API-server GETs across 2 cycles with the informer-cached client, got %d", cachedGets)
	}

	t.Logf("premise impact: direct client = %d API GETs / 2 cycles; cached client = %d API GETs / 2 cycles. "+
		"The per-cycle GetNodeAffinity fetch is an informer-cache read in fluid's default deployment, not API-server load.",
		directGets, cachedGets)
}
