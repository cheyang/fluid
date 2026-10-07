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

// Verification harness for the review of
// https://github.com/fluid-cloudnative/fluid/pull/6175 (additive test file; no
// production code is touched). Requires envtest binaries (KUBEBUILDER_ASSETS
// or the default ~/.local/share/kubebuilder-envtest path); the test skips
// cleanly when they are absent.
//
// Two integration-level checks against a real API server (envtest):
//
//  1. Manifest acceptance: the mooncake e2e manifests are created against the
//     real CRD schemas (server-side dry run with strict field validation), so
//     a typo'd field name in cacheruntimeclass.yaml / cacheruntime.yaml /
//     dataset.yaml / rw_job.yaml / bad_mount_pod.yaml fails here instead of in
//     CI.
//
//  2. Finding F2 (stale FailedMount events): check_pvc_not_mountable selects
//     events by involvedObject.name and reason only. This test shows that after
//     the bad-mount pod is deleted, the exact kubectl query from test.sh still
//     returns the old pod's FailedMount events (they outlive the pod), so a
//     repeated run on the same cluster can pass on stale evidence; and that
//     adding involvedObject.uid=<current pod uid> excludes them.
package mooncake

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// test/gha-e2e/mooncake/<this file> -> repo root is 4 levels up
	return filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(thisFile))))
}

func startEnv(t *testing.T) (*envtest.Environment, client.Client, string) {
	t.Helper()
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		candidate := filepath.Join(os.Getenv("HOME"), ".local/share/kubebuilder-envtest/k8s/1.36.2-linux-amd64")
		if _, err := os.Stat(filepath.Join(candidate, "kube-apiserver")); err == nil {
			os.Setenv("KUBEBUILDER_ASSETS", candidate)
		} else {
			t.Skip("envtest binaries not found; set KUBEBUILDER_ASSETS")
		}
	}
	scheme := kruntime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	env := &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join(repoRoot(t), "config", "crd", "bases")},
		Scheme:            scheme,
	}
	cfg, err := env.Start()
	if err != nil {
		t.Fatalf("failed to start envtest: %v", err)
	}
	t.Cleanup(func() { env.Stop() })

	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}

	// Write a kubeconfig so the tests can invoke the very same kubectl
	// invocations that test.sh runs, against this API server.
	kubeconfig := filepath.Join(t.TempDir(), "kubeconfig")
	kc := clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{"envtest": {
			Server:                   cfg.Host,
			CertificateAuthorityData: cfg.CAData,
		}},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{"envtest": {
			ClientCertificateData: cfg.CertData,
			ClientKeyData:         cfg.KeyData,
		}},
		Contexts: map[string]*clientcmdapi.Context{"envtest": {
			Cluster:   "envtest",
			AuthInfo:  "envtest",
			Namespace: "default",
		}},
		CurrentContext: "envtest",
	}
	if err := clientcmd.WriteToFile(kc, kubeconfig); err != nil {
		t.Fatal(err)
	}

	if err := c.Create(t.Context(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}); err != nil && !apierrors.IsAlreadyExists(err) {
		t.Fatal(err)
	}
	return env, c, kubeconfig
}

func kubectlBinary(t *testing.T) string {
	t.Helper()
	p := filepath.Join(os.Getenv("KUBEBUILDER_ASSETS"), "kubectl")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("kubectl not found in KUBEBUILDER_ASSETS: %v", err)
	}
	return p
}

func TestManifestsAcceptedByAPIServer_Verify(t *testing.T) {
	_, _, kubeconfig := startEnv(t)
	kubectl := kubectlBinary(t)
	dir := repoRoot(t)
	manifests := []string{
		"test/gha-e2e/mooncake/cacheruntimeclass.yaml",
		"test/gha-e2e/mooncake/dataset.yaml",
		"test/gha-e2e/mooncake/cacheruntime.yaml",
		"test/gha-e2e/mooncake/rw_job.yaml",
		"test/gha-e2e/mooncake/bad_mount_pod.yaml",
	}
	for _, m := range manifests {
		out, err := exec.Command(kubectl, "--kubeconfig", kubeconfig,
			"apply", "-f", filepath.Join(dir, m),
			"--dry-run=server", "--validate=strict").CombinedOutput()
		if err != nil {
			t.Fatalf("manifest %s rejected by API server: %v\n%s", m, err, out)
		}
		t.Logf("%s: %s", m, out)
	}
}

func TestStaleFailedMountEventsMatchPodNameSelector_F2Verify(t *testing.T) {
	_, c, kubeconfig := startEnv(t)
	kubectl := kubectlBinary(t)
	ctx := t.Context()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "mooncake-bad-mount", Namespace: "default"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app", Image: "busybox:1.36"}},
		},
	}
	if err := c.Create(ctx, pod); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, pod); err != nil {
		t.Fatal(err)
	}

	// The event the CSI-side failure produces on the first run.
	evt := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{Name: "mooncake-bad-mount.stale", Namespace: "default"},
		InvolvedObject: corev1.ObjectReference{
			Kind:      "Pod",
			Name:      pod.Name,
			Namespace: pod.Namespace,
			UID:       pod.UID,
		},
		Reason:  "FailedMount",
		Message: `MountVolume.SetUp failed for volume "default-mooncake-demo" : rpc error: code = Internal desc = timeout waiting for FUSE mount point to be ready`,
		Source:  corev1.EventSource{Component: "kubelet"},
		Type:    "Warning",
	}
	if err := c.Create(ctx, evt); err != nil {
		t.Fatal(err)
	}

	// test.sh force-deletes the pod at the end of check_pvc_not_mountable.
	if err := c.Delete(ctx, pod); err != nil {
		t.Fatal(err)
	}

	// The exact query from check_pvc_not_mountable:
	//   kubectl get events --field-selector "involvedObject.name=mooncake-bad-mount,reason=FailedMount" -ojsonpath='{.items[*].message}'
	out, err := exec.Command(kubectl, "--kubeconfig", kubeconfig, "get", "events",
		"--field-selector", "involvedObject.name=mooncake-bad-mount,reason=FailedMount",
		"-ojsonpath={.items[*].message}").CombinedOutput()
	if err != nil {
		t.Fatalf("kubectl events query failed: %v\n%s", err, out)
	}
	if len(out) == 0 {
		t.Fatal("expected the stale FailedMount event to still match the name+reason selector after the pod is gone; got nothing")
	}
	t.Logf("stale event still matched after pod deletion (this is the F2 mechanism): %s", out)

	// The fix: scope to the current pod's UID. A re-created pod has a different
	// UID, so a uid-scoped selector excludes the stale event.
	var evList corev1.EventList
	uidSelector := fields.ParseSelectorOrDie("involvedObject.name=mooncake-bad-mount,reason=FailedMount,involvedObject.uid=some-other-uid")
	if err := c.List(ctx, &evList, client.InNamespace("default"), client.MatchingFieldsSelector{Selector: uidSelector}); err != nil {
		t.Fatalf("uid-scoped event list failed: %v", err)
	}
	if len(evList.Items) != 0 {
		t.Fatalf("uid-scoped selector should exclude the stale event, got %d items", len(evList.Items))
	}
	t.Log("uid-scoped selector excludes the stale event: the fix works")
}
