// Verification harness for PR https://github.com/fluid-cloudnative/fluid/pull/6300
// (docs: add the English dataset toleration sample and fix its StatefulSet apiVersion).
//
// This file is ADDITIVE review tooling. It touches no production code.
// All tests are CONTRACT tests: they assert the intended correct behavior of the PR,
// so they PASS on the PR head and FAIL on the merge-base (where the English page and
// the apiVersion fix do not exist).
//
// Run from the repository root:
//
//	go test ./docs/verification/pr-6300-en-data-toleration/ -v
package doctoleration

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/yaml"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
)

// repoRoot resolves the repository root from the package dir (docs/verification/<topic>).
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return root
}

func readDoc(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	// Normalize CRLF (the zh page uses CRLF; the en page uses LF) so the
	// comparison is about content, not line-ending convention.
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

var (
	codeFenceRe = regexp.MustCompile("(?s)```[a-zA-Z]*\n(.*?)```")
	heredocRe   = regexp.MustCompile("(?s)cat<<EOF >(\\S+)\n(.*?)\nEOF")
)

func codeBlocks(t *testing.T, content string) []string {
	t.Helper()
	var out []string
	for _, m := range codeFenceRe.FindAllStringSubmatch(content, -1) {
		out = append(out, m[1])
	}
	return out
}

// A1 (contract, unit): the English page's code blocks must match the fixed Chinese
// page's code blocks exactly — this is the PR body's own verification claim
// ("The code blocks of the English page match the (fixed) Chinese page exactly").
func TestCodeBlocksMatchChineseSource(t *testing.T) {
	en := codeBlocks(t, readDoc(t, "docs/en/samples/data_toleration.md"))
	zh := codeBlocks(t, readDoc(t, "docs/zh/samples/data_toleration.md"))
	if len(en) == 0 {
		t.Fatal("no code blocks found in the English page (does docs/en/samples/data_toleration.md exist?)")
	}
	if len(en) != len(zh) {
		t.Fatalf("code block count differs: en=%d zh=%d", len(en), len(zh))
	}
	for i := range en {
		if en[i] != zh[i] {
			t.Fatalf("code block %d differs between en and zh pages:\n--- en ---\n%s\n--- zh ---\n%s", i, en[i], zh[i])
		}
	}
}

// A2 (contract, unit): docs/en/TOC.md must list the new page under Dataset -> Creation,
// next to Cache Co-locality, mirroring the Chinese TOC, and the link target must exist.
func TestEnglishTOCListsPage(t *testing.T) {
	toc := readDoc(t, "docs/en/TOC.md")
	const link = "](samples/data_toleration.md)"
	idx := strings.Index(toc, link)
	if idx < 0 {
		t.Fatal("docs/en/TOC.md does not link to samples/data_toleration.md")
	}
	// Placement: after Cache Co-locality, before the CSI cross-namespace entry
	// (same relative position as 数据容忍污点调度 in docs/zh/TOC.md).
	co := strings.Index(toc, "](samples/data_co_locality.md)")
	csi := strings.Index(toc, "](samples/dataset_across_namespace_with_csi.md)")
	if co < 0 || csi < 0 {
		t.Fatalf("en TOC missing sibling entries used as landmarks (co_locality=%d, csi=%d)", co, csi)
	}
	if !(co < idx && idx < csi) {
		t.Fatalf("data_toleration entry misplaced: co_locality@%d < toleration@%d < csi@%d expected", co, idx, csi)
	}
	if _, err := os.Stat(filepath.Join(repoRoot(t), "docs/en/samples/data_toleration.md")); err != nil {
		t.Fatalf("TOC link target docs/en/samples/data_toleration.md missing: %v", err)
	}
}

// A3 (contract, unit): the YAML manifests embedded in the English page must parse
// against the real API types they claim to use: the Dataset tolerations field the
// page documents, the AlluxioRuntime, and an apps/v1 StatefulSet whose selector
// matches its pod template labels (the apps/v1 requirement the PR cites).
func TestDocManifestsParseAgainstRealAPITypes(t *testing.T) {
	en := readDoc(t, "docs/en/samples/data_toleration.md")
	manifests := map[string]string{}
	for _, m := range heredocRe.FindAllStringSubmatch(en, -1) {
		manifests[m[1]] = m[2]
	}
	for _, want := range []string{"dataset.yaml", "runtime.yaml", "app.yaml"} {
		if _, ok := manifests[want]; !ok {
			t.Fatalf("English page does not contain a heredoc writing %s", want)
		}
	}

	var dataset datav1alpha1.Dataset
	if err := yaml.Unmarshal([]byte(manifests["dataset.yaml"]), &dataset); err != nil {
		t.Fatalf("dataset.yaml does not unmarshal into datav1alpha1.Dataset: %v", err)
	}
	if len(dataset.Spec.Tolerations) == 0 {
		t.Fatal("dataset.yaml carries no spec.tolerations although the page documents that field")
	}
	found := false
	for _, tol := range dataset.Spec.Tolerations {
		if tol.Key == "hbase" && tol.Operator == "Equal" && tol.Value == "true" {
			found = true
		}
	}
	if !found {
		t.Fatalf("dataset.yaml tolerations do not match the taint used in the demo: %+v", dataset.Spec.Tolerations)
	}

	var runtime datav1alpha1.AlluxioRuntime
	if err := yaml.Unmarshal([]byte(manifests["runtime.yaml"]), &runtime); err != nil {
		t.Fatalf("runtime.yaml does not unmarshal into datav1alpha1.AlluxioRuntime: %v", err)
	}
	if runtime.Name != dataset.Name {
		t.Fatalf("runtime name %q does not match dataset name %q (Fluid pairs them by name)", runtime.Name, dataset.Name)
	}

	var sts appsv1.StatefulSet
	if err := yaml.Unmarshal([]byte(manifests["app.yaml"]), &sts); err != nil {
		t.Fatalf("app.yaml does not unmarshal into appsv1.StatefulSet: %v", err)
	}
	if sts.APIVersion != "apps/v1" {
		t.Fatalf("app.yaml apiVersion = %q, want apps/v1 (apps/v1beta1 was removed in Kubernetes 1.16)", sts.APIVersion)
	}
	if sts.Spec.ServiceName == "" {
		t.Fatal("app.yaml StatefulSet has no spec.serviceName (required by apps/v1)")
	}
	if sts.Spec.Selector == nil || len(sts.Spec.Selector.MatchLabels) == 0 {
		t.Fatal("app.yaml StatefulSet has no spec.selector.matchLabels (required by apps/v1)")
	}
	for k, v := range sts.Spec.Selector.MatchLabels {
		if sts.Spec.Template.Labels[k] != v {
			t.Fatalf("selector label %q=%q not present in pod template labels %v", k, v, sts.Spec.Template.Labels)
		}
	}
}

// A4 (contract, unit): neither toleration page may reference Kubernetes API versions
// removed in v1.16 (the regression the zh-side fix eliminates).
func TestNoRemovedKubernetesAPIs(t *testing.T) {
	removed := []string{"apps/v1beta1", "apps/v1beta2", "extensions/v1beta1"}
	for _, rel := range []string{"docs/en/samples/data_toleration.md", "docs/zh/samples/data_toleration.md"} {
		content := readDoc(t, rel)
		for _, api := range removed {
			if strings.Contains(content, api) {
				t.Fatalf("%s still references removed Kubernetes API %s", rel, api)
			}
		}
	}
}

// A5 (contract, integration): the real docs build tooling (docs/scripts/mergeByTOC.py,
// used by docs/build.sh and docs/Makefile) must merge the English tree without error
// and the merged doc must contain the new page. Runs on a copied docs/ tree so the
// repository is not polluted.
func TestDocsMergeIncludesPage(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skipf("python3 not available; mergeByTOC.py layer cannot run on this machine")
	}
	root := repoRoot(t)
	tmp := t.TempDir()
	for _, dir := range []string{"en", "scripts"} {
		cp := exec.Command("cp", "-r", filepath.Join(root, "docs", dir), filepath.Join(tmp, dir))
		if out, err := cp.CombinedOutput(); err != nil {
			t.Fatalf("copy docs/%s: %v\n%s", dir, err, out)
		}
	}
	cmd := exec.Command("python3", filepath.Join("scripts", "mergeByTOC.py"), "en/")
	cmd.Dir = tmp
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("mergeByTOC.py en/ failed: %v\n%s", err, out)
	}
	merged, err := os.ReadFile(filepath.Join(tmp, "en", "doc.md"))
	if err != nil {
		t.Fatalf("merged en/doc.md not produced: %v", err)
	}
	if !strings.Contains(string(merged), "Dataset Toleration of Node Taints") {
		t.Fatal("merged English doc.md does not contain the new data toleration page")
	}
}
