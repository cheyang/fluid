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

// ADDITIVE integration-layer verification for PR #6223 (Reviewer B / Codex).
// Runs the PR's CacheRuntime CRD against a REAL API server (envtest) and
// exercises the scale subresource exactly the way kubectl / an HPA would:
//
//	GET  /apis/data.fluid.io/v1alpha1/namespaces/<ns>/cacheruntimes/<name>/scale
//	PUT  ... /scale   (via `kubectl scale`, the same write path an HPA takes)
//
// The repo's own e2e (test/gha-e2e/curvine/test.sh) skips the scale-WRITE path
// on the single-node kind cluster used in CI, so this envtest suite is the only
// executed coverage of that path.
//
// Requires KUBEBUILDER_ASSETS (etcd + kube-apiserver + kubectl binaries).
// Polarity: CONTRACT tests. On the base CRD (no scale subresource) the GET
// asserts fail, which doubles as the premise check for the CRD half.

package codexhpa

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

const verifyNS = "verify-ns"

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func writeKubeconfig(t *testing.T, cfgHost, caB64, certB64, keyB64 string) string {
	t.Helper()
	kc := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: envtest
  cluster:
    server: %s
    certificate-authority-data: %s
users:
- name: envtest
  user:
    client-certificate-data: %s
    client-key-data: %s
contexts:
- name: envtest
  context:
    cluster: envtest
    user: envtest
current-context: envtest
`, cfgHost, caB64, certB64, keyB64)
	path := filepath.Join(t.TempDir(), "kubeconfig")
	if err := os.WriteFile(path, []byte(kc), 0600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	return path
}

func kubectl(t *testing.T, kubeconfig string, allowFailure bool, args ...string) (string, error) {
	t.Helper()
	kubectlBin := filepath.Join(os.Getenv("KUBEBUILDER_ASSETS"), "kubectl")
	args = append([]string{"--kubeconfig", kubeconfig}, args...)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, kubectlBin, args...)
	out, err := cmd.CombinedOutput()
	t.Logf("$ kubectl %s\n%s", strings.Join(args, " "), strings.TrimSpace(string(out)))
	if err != nil && !allowFailure {
		t.Fatalf("kubectl %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out), err
}

func getScale(t *testing.T, kubeconfig, name string) (string, error) {
	return kubectl(t, kubeconfig, true, "get", "--raw",
		fmt.Sprintf("/apis/data.fluid.io/v1alpha1/namespaces/%s/cacheruntimes/%s/scale", verifyNS, name))
}

type scaleTestEnv struct {
	client     client.Client
	kubeconfig string
}

func (e *scaleTestEnv) getScale(t *testing.T, name string) (string, error) {
	return getScale(t, e.kubeconfig, name)
}

// startScaleEnv boots envtest with the PR's CRDs (or CODEX_CRD_DIR when set,
// used by the base-CRD premise check) and returns a client + kubeconfig.
func startScaleEnv(t *testing.T) *scaleTestEnv {
	t.Helper()
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS not set")
	}

	crdDir := os.Getenv("CODEX_CRD_DIR")
	if crdDir == "" {
		crdDir = filepath.Join("..", "..", "..", "config", "crd", "bases")
	}
	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{crdDir},
	}
	cfg, err := testEnv.Start()
	if err != nil {
		t.Fatalf("start envtest: %v", err)
	}
	t.Cleanup(func() {
		if err := testEnv.Stop(); err != nil {
			t.Logf("stop envtest: %v", err)
		}
	})

	scheme := runtime.NewScheme()
	if err := datav1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add corev1 scheme: %v", err)
	}
	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	if err := c.Create(context.TODO(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: verifyNS}}); err != nil {
		t.Fatalf("create namespace: %v", err)
	}

	return &scaleTestEnv{
		client:     c,
		kubeconfig: writeKubeconfig(t, cfg.Host, b64(cfg.CAData), b64(cfg.CertData), b64(cfg.KeyData)),
	}
}

func TestCacheRuntimeScaleSubresource(t *testing.T) {
	env := startScaleEnv(t)
	c := env.client
	kubeconfig := env.kubeconfig
	ctx := context.TODO()

	newRuntime := func(name string, workerSpec *datav1alpha1.CacheRuntimeWorkerSpec) *datav1alpha1.CacheRuntime {
		r := &datav1alpha1.CacheRuntime{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: verifyNS},
			Spec: datav1alpha1.CacheRuntimeSpec{
				RuntimeClassName: "curvine",
			},
		}
		if workerSpec != nil {
			r.Spec.Worker = *workerSpec
		}
		return r
	}

	selectorStr := "cacheruntime.fluid.io/component-name=hpa-demo-worker,cacheruntime.fluid.io/name=hpa-demo"

	t.Run("worker runtime: GET scale serves spec+status+selector", func(t *testing.T) {
		replicas := int32(2)
		r := newRuntime("hpa-demo", &datav1alpha1.CacheRuntimeWorkerSpec{Replicas: replicas})
		if err := c.Create(ctx, r); err != nil {
			t.Fatalf("create: %v", err)
		}

		// simulate the engine's status sync: selector + worker.currentReplicas
		latest := &datav1alpha1.CacheRuntime{}
		if err := c.Get(ctx, types.NamespacedName{Name: "hpa-demo", Namespace: verifyNS}, latest); err != nil {
			t.Fatalf("get: %v", err)
		}
		latest.Status.Selector = selectorStr
		latest.Status.Worker = datav1alpha1.RuntimeComponentStatus{
			Phase:           datav1alpha1.RuntimePhaseReady,
			CurrentReplicas: 2,
			DesiredReplicas: 2,
			ReadyReplicas:   2,
		}
		if err := c.Status().Update(ctx, latest); err != nil {
			t.Fatalf("status update: %v", err)
		}

		out, err := getScale(t, kubeconfig, "hpa-demo")
		if err != nil {
			t.Fatalf("GET /scale failed for a worker-enabled runtime: %v\n%s", err, out)
		}
		if !strings.Contains(out, `"spec":{"replicas":2}`) {
			t.Fatalf("scale spec.replicas mismatch: %s", out)
		}
		if !strings.Contains(out, `"status":{"replicas":2`) {
			t.Fatalf("scale status.replicas mismatch (statusReplicasPath=.status.worker.currentReplicas): %s", out)
		}
		if !strings.Contains(out, fmt.Sprintf(`"selector":%q`, selectorStr)) {
			t.Fatalf("scale status.selector mismatch (labelSelectorPath=.status.selector): %s", out)
		}
	})

	t.Run("worker runtime: kubectl scale writes through the subresource", func(t *testing.T) {
		if _, err := kubectl(t, kubeconfig, false, "scale", "cacheruntime/hpa-demo", "--replicas=4", "-n", verifyNS); err != nil {
			t.Fatalf("kubectl scale failed: %v", err)
		}
		// read the CR back: the PUT must have landed on .spec.worker.replicas,
		// which is what syncRuntimeSpec propagates to the worker AdvancedStatefulSet
		latest := &datav1alpha1.CacheRuntime{}
		if err := c.Get(ctx, types.NamespacedName{Name: "hpa-demo", Namespace: verifyNS}, latest); err != nil {
			t.Fatalf("get: %v", err)
		}
		if latest.Spec.Worker.Replicas != 4 {
			t.Fatalf("kubectl scale did not land on .spec.worker.replicas: got %d, want 4", latest.Spec.Worker.Replicas)
		}
		out, err := getScale(t, kubeconfig, "hpa-demo")
		if err != nil {
			t.Fatalf("GET /scale after scale-up failed: %v\n%s", err, out)
		}
		if !strings.Contains(out, `"spec":{"replicas":4}`) {
			t.Fatalf("scale spec.replicas after write mismatch: %s", out)
		}
	})

	t.Run("negative scale replicas rejected by CRD validation", func(t *testing.T) {
		out, err := kubectl(t, kubeconfig, true, "scale", "cacheruntime/hpa-demo", "--replicas=-1", "-n", verifyNS)
		if err == nil {
			t.Fatalf("kubectl scale --replicas=-1 unexpectedly succeeded: %s", out)
		}
		t.Logf("negative replicas rejected as expected: %s", strings.TrimSpace(out))
	})

	t.Run("worker-less runtime: defaulting + GET scale behavior (pins PR body claim)", func(t *testing.T) {
		r := newRuntime("no-worker", nil)
		if err := c.Create(ctx, r); err != nil {
			t.Fatalf("create: %v", err)
		}
		latest := &datav1alpha1.CacheRuntime{}
		if err := c.Get(ctx, types.NamespacedName{Name: "no-worker", Namespace: verifyNS}, latest); err != nil {
			t.Fatalf("get: %v", err)
		}
		raw, _ := json.Marshal(latest.Spec)
		t.Logf("OBSERVED spec after API-server defaulting (no worker block submitted): %s", raw)
		t.Logf("OBSERVED spec.worker.replicas defaulted value: %d", latest.Spec.Worker.Replicas)

		out, err := getScale(t, kubeconfig, "no-worker")
		if err != nil {
			t.Logf("OBSERVED: GET /scale on worker-less runtime FAILS (PR body claim): %v\n%s", err, out)
		} else {
			t.Logf("OBSERVED: GET /scale on worker-less runtime SUCCEEDS: %s", out)
		}

		scaleOut, scaleErr := kubectl(t, kubeconfig, true, "scale", "cacheruntime/no-worker", "--replicas=3", "-n", verifyNS)
		if scaleErr != nil {
			t.Logf("OBSERVED: kubectl scale on worker-less runtime FAILS: %v\n%s", scaleErr, scaleOut)
		} else {
			t.Logf("OBSERVED: kubectl scale on worker-less runtime SUCCEEDS: %s", scaleOut)
			after := &datav1alpha1.CacheRuntime{}
			if err := c.Get(ctx, types.NamespacedName{Name: "no-worker", Namespace: verifyNS}, after); err != nil {
				t.Fatalf("get: %v", err)
			}
			t.Logf("OBSERVED .spec.worker.replicas after scaling a worker-less runtime: %d (controller skips sync because topology/disabled guard)", after.Spec.Worker.Replicas)
		}
	})

	t.Run("fresh runtime before first status sync: GET scale behavior (HPA-created-early scenario)", func(t *testing.T) {
		replicas := int32(1)
		r := newRuntime("fresh", &datav1alpha1.CacheRuntimeWorkerSpec{Replicas: replicas})
		if err := c.Create(ctx, r); err != nil {
			t.Fatalf("create: %v", err)
		}
		out, err := getScale(t, kubeconfig, "fresh")
		if err != nil {
			t.Logf("OBSERVED: GET /scale before any status write FAILS: %v\n%s", err, out)
		} else {
			t.Logf("OBSERVED: GET /scale before any status write SUCCEEDS: %s", out)
			if strings.Contains(out, `"selector":""`) {
				t.Logf("OBSERVED: selector is the empty string until the engine writes status.selector (HPA sees an unparseable/empty selector until first sync)")
			}
		}
	})
}

// Guard against a subtle JSON-tag drift that would silently break
// statusReplicasPath (.status.worker.currentReplicas): the field must keep its
// json name.
func TestCacheRuntimeScalePathsMatchGoTags(t *testing.T) {
	s := datav1alpha1.CacheRuntimeStatus{}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	m := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := m["worker"]; !ok {
		t.Fatalf("status serializes without a 'worker' key; statusReplicasPath .status.worker.currentReplicas would never resolve")
	}
	worker := map[string]json.RawMessage{}
	if err := json.Unmarshal(m["worker"], &worker); err != nil {
		t.Fatalf("unmarshal worker: %v", err)
	}
	if _, ok := worker["currentReplicas"]; !ok {
		t.Logf("zero-value worker status omits currentReplicas (omitempty); GET /scale on a fresh runtime serves status.replicas=0 until the engine writes it")
	}
	var w datav1alpha1.RuntimeComponentStatus
	w.CurrentReplicas = 5
	rawW, _ := json.Marshal(w)
	var um map[string]interface{}
	if err := json.Unmarshal(rawW, &um); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := um["currentReplicas"]; !ok {
		t.Fatalf("RuntimeComponentStatus.CurrentReplicas lost its json name; scale statusReplicasPath is broken")
	}
	_ = unstructured.Unstructured{}
}
