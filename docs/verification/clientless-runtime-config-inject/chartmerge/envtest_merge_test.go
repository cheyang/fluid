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

// Integration layer for F1 with zero fidelity assumptions: the real helm CLI renders the
// PR chart against a real API server (envtest), so the chart's `lookup` of the preserved
// webhook-plugins ConfigMap executes exactly as it does during `helm upgrade`.
//
// Skipped when helm or envtest assets (KUBEBUILDER_ASSETS, or a known local path) are not
// available; the unit-fidelity harness in chart_merge_test.go covers the same branches
// with a stubbed lookup.
package chartmerge

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	sigsyaml "sigs.k8s.io/yaml"
)

func findEnvtestAssets() string {
	if v := os.Getenv("KUBEBUILDER_ASSETS"); v != "" {
		return v
	}
	for _, candidate := range []string{
		"/tmp/envtest-assets/k8s/1.29.0-linux-amd64",
		"/tmp/rbg-verify-bin/k8s/1.33.0-linux-amd64",
	} {
		if _, err := os.Stat(filepath.Join(candidate, "kube-apiserver")); err == nil {
			return candidate
		}
	}
	return ""
}

// helmTemplate renders the PR chart with the real helm binary against the given
// kubeconfig (empty kubeconfig = client-only render).
func helmTemplate(t *testing.T, chartDir, kubeconfig string) string {
	t.Helper()
	helmPath, err := exec.LookPath("helm")
	if err != nil {
		t.Skip("helm binary not available")
	}
	args := []string{"template", "fluid", chartDir}
	if kubeconfig != "" {
		args = append(args, "--kubeconfig", kubeconfig)
	}
	out, err := exec.Command(helmPath, args...).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			t.Fatalf("helm template failed: %v\n%s", err, ee.Stderr)
		}
		t.Fatalf("helm template failed: %v", err)
	}
	return string(out)
}

func documentNamed(t *testing.T, rendered, kind, name string) string {
	t.Helper()
	for _, doc := range strings.Split(rendered, "\n---") {
		var meta struct {
			Kind     string `yaml:"kind"`
			Metadata struct {
				Name string `yaml:"name"`
			} `yaml:"metadata"`
		}
		if err := sigsyaml.Unmarshal([]byte(doc), &meta); err != nil {
			continue
		}
		if meta.Kind == kind && meta.Metadata.Name == name {
			return doc
		}
	}
	t.Fatalf("no %s named %s in the render", kind, name)
	return ""
}

// TestHelmUpgradeMergesClientlessProfileAgainstRealApiServer seeds a webhook-plugins
// ConfigMap holding the pre-upgrade (legacy) profile into an envtest API server, then
// renders the PR chart with the real helm CLI so the chart's lookup finds it - the exact
// `helm upgrade` code path.
func TestHelmUpgradeMergesClientlessProfileAgainstRealApiServer(t *testing.T) {
	assets := findEnvtestAssets()
	if assets == "" {
		t.Skip("no envtest assets available")
	}
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm binary not available")
	}

	env := &envtest.Environment{BinaryAssetsDirectory: assets}
	cfg, err := env.Start()
	if err != nil {
		t.Skipf("envtest did not start: %v", err)
	}
	defer func() { _ = env.Stop() }()

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("clientset: %v", err)
	}
	ctx := context.TODO()
	if _, err := clientset.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "fluid-system"},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create namespace: %v", err)
	}

	// pre-upgrade state: the ConfigMap a release without the clientless group leaves behind
	// The ConfigMap is owned by the earlier helm release, as a real upgrade finds it.
	seed := func(profile string) {
		_, err := clientset.CoreV1().ConfigMaps("fluid-system").Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "webhook-plugins",
				Namespace: "fluid-system",
				Labels:    map[string]string{"app.kubernetes.io/managed-by": "Helm"},
				Annotations: map[string]string{
					"meta.helm.sh/release-name":      "fluid",
					"meta.helm.sh/release-namespace": "fluid-system",
				},
			},
			Data: map[string]string{"pluginsProfile": profile},
		}, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("seed webhook-plugins: %v", err)
		}
	}
	// The marker distinguishes the merge branch (which preserves the stored profile) from
	// the fresh-install branch (which renders values.yaml): only the merge branch keeps it.
	legacyWithMarker := strings.Replace(legacyProfile, "- PreferNodesWithoutCache",
		"- PreferNodesWithoutCache\n    - LegacyOnlyMarker", 1)
	seed(legacyWithMarker)

	// kubeconfig for the helm CLI
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
			Cluster: "envtest", AuthInfo: "envtest",
		}},
		CurrentContext: "envtest",
	}
	if err := clientcmd.WriteToFile(kc, kubeconfig); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}

	chartDir := filepath.Join(repoRoot(t), "charts/fluid/fluid")

	// does this helm do lookups during template at all? If not, try upgrade --dry-run=server.
	rendered := helmTemplate(t, chartDir, kubeconfig)
	cmDoc := documentNamed(t, rendered, "ConfigMap", "webhook-plugins")
	if !strings.Contains(cmDoc, "LegacyOnlyMarker") {
		// fall back to the server dry-run path, which definitely performs lookups
		t.Log("plain template did not resolve lookup (marker missing); trying upgrade --dry-run=server")
		helmPath, _ := exec.LookPath("helm")
		out, err := exec.Command(helmPath, "upgrade", "--install", "--dry-run=server", "fluid", chartDir,
			"--namespace", "fluid-system", "--kubeconfig", kubeconfig).Output()
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				t.Fatalf("helm upgrade --dry-run=server failed: %v\n%s", err, ee.Stderr)
			}
			t.Fatalf("helm upgrade --dry-run=server failed: %v", err)
		}
		rendered = string(out)
		cmDoc = documentNamed(t, rendered, "ConfigMap", "webhook-plugins")
	}

	// F1 contract against real helm + real lookup: the preserved legacy profile must gain
	// the clientless group and keep its own entries.
	var cm struct {
		Data map[string]string `yaml:"data"`
	}
	if err := sigsyaml.Unmarshal([]byte(cmDoc), &cm); err != nil {
		t.Fatalf("parse rendered webhook-plugins: %v", err)
	}
	merged := cm.Data["pluginsProfile"]
	if !strings.Contains(merged, "LegacyOnlyMarker") {
		t.Fatalf("lookup did not drive the merge: the stored profile's marker is gone, so the fresh-install branch rendered instead:\n%s", merged)
	}
	if !strings.Contains(merged, "clientless") || !strings.Contains(merged, "RuntimeConfigInjector") {
		t.Fatalf("F1 STILL PRESENT against real helm+apiserver: merged profile lacks clientless:\n%s", merged)
	}
	for _, kept := range []string{"FilePrefetcher", "NodeAffinityWithCache", "FuseSidecar", "PreferNodesWithoutCache", "pluginConfig"} {
		if !strings.Contains(merged, kept) {
			t.Fatalf("merged profile lost %q:\n%s", kept, merged)
		}
	}

	// the webhook Deployment must roll with the profile change
	depDoc := documentNamed(t, rendered, "Deployment", "fluid-webhook")
	if !strings.Contains(depDoc, "checksum/plugins-profile") {
		t.Fatalf("webhook Deployment has no checksum/plugins-profile annotation:\n%s", depDoc)
	}
}
