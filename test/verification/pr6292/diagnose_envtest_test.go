package pr6292

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// TestDescribeRuntimeAgainstEnvtest is a CONTRACT test (integration layer).
//
// It runs the four diagnose scripts end-to-end with the REAL kubectl against a
// REAL API server (envtest) with the fluid CRDs installed. Namespace ns1 holds
// two runtimes of each kind: "demo" (the one passed via -r) and "decoy".
//
// Expected (fixed): the archive's <kind>runtime-demo.yaml describes only "demo".
// Observed on base: the file describes every runtime of that kind, so "decoy"
// leaks into a file named for "demo" — the exact symptom the PR describes.
//
// Polarity: contract — passes on fixed code, fails on the buggy base.
//
// Skips (recorded, never a pass) when envtest binaries are unavailable:
// set KUBEBUILDER_ASSETS or put setup-envtest on PATH.
func TestDescribeRuntimeAgainstEnvtest(t *testing.T) {
	if testing.Short() {
		t.Skip("integration layer: skipped in -short mode")
	}
	assets := os.Getenv("KUBEBUILDER_ASSETS")
	if assets == "" {
		if bin, err := exec.LookPath("setup-envtest"); err == nil {
			out, err := exec.Command(bin, "use", "1.31.0", "-p", "path").Output()
			if err == nil {
				assets = strings.TrimSpace(string(out))
			}
		}
	}
	if assets == "" {
		t.Skip("HARNESS-ENV: no KUBEBUILDER_ASSETS and no setup-envtest on PATH; cannot run integration layer")
	}
	if _, err := exec.LookPath("kubectl"); err != nil {
		t.Skip("HARNESS-ENV: kubectl not on PATH")
	}
	t.Logf("using KUBEBUILDER_ASSETS=%s", assets)
	os.Setenv("KUBEBUILDER_ASSETS", assets)

	root := repoRoot(t)
	env := &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join(root, "config", "crd", "bases")},
	}
	cfg, err := env.Start()
	if err != nil {
		t.Fatalf("start envtest: %v", err)
	}
	defer func() {
		if err := env.Stop(); err != nil {
			t.Logf("stop envtest: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c, err := crclient.New(cfg, crclient.Options{})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	for _, ns := range []string{"ns1", "fluid-system"} {
		if err := c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}); err != nil {
			t.Fatalf("create ns %s: %v", ns, err)
		}
	}

	kindOf := map[string]string{
		"alluxioruntime": "AlluxioRuntime",
		"cacheruntime":   "CacheRuntime",
		"jindoruntime":   "JindoRuntime",
		"juicefsruntime": "JuiceFSRuntime",
	}
	for _, kind := range kindOf {
		for _, name := range []string{"demo", "decoy"} {
			u := &unstructured.Unstructured{}
			u.SetGroupVersionKind(schema.GroupVersionKind{Group: "data.fluid.io", Version: "v1alpha1", Kind: kind})
			u.SetName(name)
			u.SetNamespace("ns1")
			if err := c.Create(ctx, u); err != nil {
				t.Fatalf("create %s/%s: %v", kind, name, err)
			}
		}
	}

	// Serialize a kubeconfig for the envtest apiserver so the scripts' kubectl
	// invocations (via KUBECONFIG) talk to it.
	kubeconfig := clientcmdapi.NewConfig()
	kubeconfig.Clusters["envtest"] = &clientcmdapi.Cluster{
		Server:                   cfg.Host,
		CertificateAuthorityData: cfg.CAData,
	}
	kubeconfig.AuthInfos["envtest"] = &clientcmdapi.AuthInfo{
		ClientCertificateData: cfg.CertData,
		ClientKeyData:         cfg.KeyData,
	}
	kubeconfig.Contexts["envtest"] = &clientcmdapi.Context{Cluster: "envtest", AuthInfo: "envtest"}
	kubeconfig.CurrentContext = "envtest"
	kubeconfigPath := filepath.Join(t.TempDir(), "kubeconfig")
	if err := clientcmd.WriteToFile(*kubeconfig, kubeconfigPath); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}

	for _, tc := range diagnoseScripts {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			bin := filepath.Join(tmp, "bin")
			writeStub(t, bin, "helm", "#!/usr/bin/env bash\nexit 0\n")
			archive := filepath.Join(tmp, "out.tar.gz")
			cmd := exec.Command("bash", filepath.Join(root, "tools", tc.script),
				"collect", "-r", "demo", "-n", "ns1", "--collect-path", archive)
			cmd.Dir = tmp
			cmd.Env = append(os.Environ(),
				"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
				"KUBECONFIG="+kubeconfigPath)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("script %s failed: %v\n%s", tc.script, err, out)
			}

			// Extract the archive and find <kind>runtime-demo.yaml.
			extract := filepath.Join(tmp, "x")
			if err := os.MkdirAll(extract, 0o755); err != nil {
				t.Fatal(err)
			}
			if out, err := exec.Command("tar", "-xzf", archive, "-C", extract).CombinedOutput(); err != nil {
				t.Fatalf("extract archive: %v\n%s", err, out)
			}
			// tar strips the leading slash of the absolute diagnose_dir, so
			// walk the extraction root and find <kind>-demo.yaml.
			want := fmt.Sprintf("%s-demo.yaml", tc.runtimeKind)
			var matches []string
			err = filepath.WalkDir(extract, func(path string, d os.DirEntry, err error) error {
				if err == nil && !d.IsDir() && filepath.Base(path) == want {
					matches = append(matches, path)
				}
				return nil
			})
			if err != nil || len(matches) == 0 {
				t.Fatalf("no %s found under %s (walk err=%v)", want, extract, err)
			}
			content, err := os.ReadFile(matches[0])
			if err != nil {
				t.Fatal(err)
			}
			s := string(content)
			if !strings.Contains(s, "demo") {
				t.Errorf("CONTRACT VIOLATION: %s: %s does not describe the requested runtime \"demo\":\n%.300s", tc.script, matches[0], s)
			}
			if strings.Contains(s, "decoy") {
				t.Errorf("CONTRACT VIOLATION: %s: %s also describes the unrelated runtime \"decoy\" — kubectl describe ran without a name and dumped every %s in ns1:\n%.300s", tc.script, matches[0], tc.runtimeKind, s)
			}
		})
	}
}
