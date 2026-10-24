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
	"reflect"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	sigsyaml "sigs.k8s.io/yaml"

	"github.com/fluid-cloudnative/fluid/pkg/webhook/plugins"
	yamlv2 "gopkg.in/yaml.v2"
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

	// renderWithLookup renders the chart with the real helm CLI against the envtest API
	// server, so the chart's lookup sees the seeded ConfigMap exactly as `helm upgrade`
	// would. Falls back to upgrade --dry-run=server when plain template does not resolve
	// lookups with this helm version.
	renderWithLookup := func() string {
		out := helmTemplate(t, chartDir, kubeconfig)
		cmDoc := documentNamed(t, out, "ConfigMap", "webhook-plugins")
		if !strings.Contains(cmDoc, "LegacyOnlyMarker") {
			// fall back to the server dry-run path, which definitely performs lookups
			t.Log("plain template did not resolve lookup (marker missing); trying upgrade --dry-run=server")
			helmPath, _ := exec.LookPath("helm")
			upOut, err := exec.Command(helmPath, "upgrade", "--install", "--dry-run=server", "fluid", chartDir,
				"--namespace", "fluid-system", "--kubeconfig", kubeconfig).Output()
			if err != nil {
				if ee, ok := err.(*exec.ExitError); ok {
					t.Fatalf("helm upgrade --dry-run=server failed: %v\n%s", err, ee.Stderr)
				}
				t.Fatalf("helm upgrade --dry-run=server failed: %v", err)
			}
			out = string(upOut)
		}
		return out
	}

	rendered := renderWithLookup()
	cmDoc := documentNamed(t, rendered, "ConfigMap", "webhook-plugins")

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
	var dep struct {
		Spec struct {
			Template struct {
				Metadata struct {
					Annotations map[string]string `yaml:"annotations"`
				} `yaml:"metadata"`
			} `yaml:"template"`
		} `yaml:"spec"`
	}
	if err := sigsyaml.Unmarshal([]byte(depDoc), &dep); err != nil {
		t.Fatalf("parse rendered fluid-webhook deployment: %v", err)
	}
	firstChecksum := dep.Spec.Template.Metadata.Annotations["checksum/plugins-profile"]
	if firstChecksum == "" {
		t.Fatalf("webhook Deployment lost the checksum/plugins-profile value:\n%s", depDoc)
	}

	// B-F6, with the real helm CLI and a real API server: simulate the second upgrade by
	// storing the merged profile exactly as the API server persisted it after the first
	// upgrade, then re-render. The keep branch must reproduce the first upgrade's render
	// byte-for-byte — otherwise the checksum annotation changes and the webhook rolls a
	// second time for no reason. Red on 6d3a6d7e (trailing newline re-indented into a
	// whitespace-only line), green at 268012a5.
	current, err := clientset.CoreV1().ConfigMaps("fluid-system").Get(ctx, "webhook-plugins", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get seeded webhook-plugins: %v", err)
	}
	// The profile the API server persists after the first upgrade ends with exactly one
	// newline: the rendered manifest terminates every block-scalar line, and `|` clip
	// chomping keeps the final line break. (merged itself lacks it only because this test
	// splits helm's printed stream on the "---" separator, which eats the last newline.)
	current.Data["pluginsProfile"] = merged + "\n"
	if _, err := clientset.CoreV1().ConfigMaps("fluid-system").Update(ctx, current, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("store merged profile: %v", err)
	}

	second := renderWithLookup()
	secondCmDoc := documentNamed(t, second, "ConfigMap", "webhook-plugins")
	var secondCm struct {
		Data map[string]string `yaml:"data"`
	}
	if err := sigsyaml.Unmarshal([]byte(secondCmDoc), &secondCm); err != nil {
		t.Fatalf("parse second-render webhook-plugins: %v", err)
	}
	if secondCm.Data["pluginsProfile"] != merged {
		t.Fatalf("second upgrade changed the stored profile content:\n%q\nvs\n%q",
			secondCm.Data["pluginsProfile"], merged)
	}
	secondDepDoc := documentNamed(t, second, "Deployment", "fluid-webhook")
	var secondDep struct {
		Spec struct {
			Template struct {
				Metadata struct {
					Annotations map[string]string `yaml:"annotations"`
				} `yaml:"metadata"`
			} `yaml:"template"`
		} `yaml:"spec"`
	}
	if err := sigsyaml.Unmarshal([]byte(secondDepDoc), &secondDep); err != nil {
		t.Fatalf("parse second-render fluid-webhook deployment: %v", err)
	}
	if got := secondDep.Spec.Template.Metadata.Annotations["checksum/plugins-profile"]; got != firstChecksum {
		t.Fatalf("B-F6 PRESENT against real helm+apiserver: checksum churns on the second upgrade (%s -> %s), webhook would roll again for no reason",
			firstChecksum, got)
	}
}

// TestHelmUpgradeMergesClientlessDefaultWhenValuesLackIt is the be4e30d3 contract against
// the real helm CLI and a real API server, with zero fidelity compromises: it installs the
// BASE chart (whose values predate the clientless group), then runs the real
// `helm upgrade --reuse-values` of the PR chart. --reuse-values hands the old release's
// values to the new templates, so `.Values.webhook.pluginsProfile.plugins.clientless` is
// nil; 268012a5 merged that nil into the preserved ConfigMap (`clientless: null`, no
// plugin behind the always-rendered rule). The fix falls back to the chart's own group.
// Hooks are skipped: the chart's pre-upgrade CRD Job can never complete without a kubelet.
// Red on 268012a5, green at 41b47141.
func TestHelmUpgradeMergesClientlessDefaultWhenValuesLackIt(t *testing.T) {
	assets := findEnvtestAssets()
	if assets == "" {
		t.Skip("no envtest assets available")
	}
	helmPath, err := exec.LookPath("helm")
	if err != nil {
		t.Skip("helm binary not available")
	}

	env := &envtest.Environment{BinaryAssetsDirectory: assets}
	cfg, err := env.Start()
	if err != nil {
		t.Skipf("envtest did not start: %v", err)
	}
	defer func() { _ = env.Stop() }()

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

	// the base chart (what the pre-upgrade release runs), extracted from git
	baseChart := filepath.Join(t.TempDir(), "base-chart")
	out, err := exec.Command("git", "-C", repoRoot(t), "archive", baseSHA, "charts").Output()
	if err != nil {
		t.Skipf("base %s not available in this clone: %v", baseSHA, err)
	}
	tar := filepath.Join(t.TempDir(), "base-charts.tar")
	if err := os.WriteFile(tar, out, 0644); err != nil {
		t.Fatalf("write base charts tar: %v", err)
	}
	if err := os.MkdirAll(baseChart, 0755); err != nil {
		t.Fatalf("mkdir base chart dir: %v", err)
	}
	if err := exec.Command("tar", "-x", "-f", tar, "-C", baseChart).Run(); err != nil {
		t.Fatalf("extract base chart: %v", err)
	}
	baseChartDir := filepath.Join(baseChart, "charts/fluid/fluid")
	if _, err := os.Stat(filepath.Join(baseChartDir, "Chart.yaml")); err != nil {
		t.Fatalf("base chart not extracted: %v", err)
	}

	runHelm := func(args ...string) string {
		args = append(args, "--kubeconfig", kubeconfig)
		out, err := exec.Command(helmPath, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("helm %s failed: %v\n%s", strings.Join(args, " "), err, out)
		}
		return string(out)
	}

	// the release to upgrade: base chart, default values (no clientless group anywhere)
	runHelm("install", "fluid", baseChartDir, "--namespace", "fluid-system", "--create-namespace")

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("clientset: %v", err)
	}
	ctx := context.TODO()
	before, err := clientset.CoreV1().ConfigMaps("fluid-system").Get(ctx, "webhook-plugins", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("the base install must create webhook-plugins: %v", err)
	}
	if strings.Contains(before.Data["pluginsProfile"], "clientless") {
		t.Fatalf("fixture broken: the base release already has a clientless group")
	}

	// the upgrade under review: reuse the old release's values verbatim
	runHelm("upgrade", "fluid", filepath.Join(repoRoot(t), "charts/fluid/fluid"),
		"--namespace", "fluid-system", "--reuse-values", "--no-hooks")

	after, err := clientset.CoreV1().ConfigMaps("fluid-system").Get(ctx, "webhook-plugins", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get webhook-plugins after upgrade: %v", err)
	}
	merged := after.Data["pluginsProfile"]
	if strings.Contains(merged, "clientless: null") {
		t.Fatalf("268012a5 BEHAVIOR under real --reuse-values: merge wrote `clientless: null` — no plugin behind the clientless rule:\n%s", merged)
	}
	mergedMap := profileAsMap(t, merged)
	pluginsMap, ok := mergedMap["plugins"].(map[string]interface{})
	if !ok {
		t.Fatalf("upgraded profile plugins is not a map:\n%s", merged)
	}
	clientless, ok := pluginsMap["clientless"].(map[string]interface{})
	if !ok {
		t.Fatalf("upgraded profile has no clientless group:\n%s", merged)
	}
	if !reflect.DeepEqual(clientless, valuesClientlessGroup(t)) {
		t.Fatalf("merged clientless group is not the chart default:\n%v\nvs\n%v", clientless, valuesClientlessGroup(t))
	}
	for _, kept := range []string{"FilePrefetcher", "NodeAffinityWithCache", "FuseSidecar", "PreferNodesWithoutCache", "pluginConfig"} {
		if !strings.Contains(merged, kept) {
			t.Fatalf("upgraded profile lost the pre-existing entry %q:\n%s", kept, merged)
		}
	}
	var parsed plugins.PluginsProfile
	if err := yamlv2.Unmarshal([]byte(merged), &parsed); err != nil {
		t.Fatalf("upgraded profile does not unmarshal into plugins.PluginsProfile: %v", err)
	}
	if len(parsed.Plugins.Clientless.WithDataset) != 1 || parsed.Plugins.Clientless.WithDataset[0] != "RuntimeConfigInjector" {
		t.Fatalf("webhook would not register RuntimeConfigInjector from the upgraded profile: %+v", parsed.Plugins.Clientless)
	}
}
