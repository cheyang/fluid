// Verification harness for PR #6253 (reviewer: codex). ADDITIVE ONLY — this file
// is part of the review harness, not the PR. It statically checks the claims the
// new sample docs (docs/{en,zh}/samples/cacheruntime/vllm_with_curvine.md) make:
// embedded manifests parse, en/zh parity, heredoc expansion safety, relative link
// resolution, code-level claims (labels / phases / helm values), cross-references
// into the Curvine sample doc, and the #6252 merge-order dependency (F1).
package harness

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found walking up from test package")
		}
		dir = parent
	}
}

func readDoc(t *testing.T, root, lang, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "docs", lang, "samples", "cacheruntime", name))
	if err != nil {
		t.Fatalf("read %s doc: %v", lang, err)
	}
	return string(b)
}

var heredocRe = regexp.MustCompile("(?s)cat<<('?)EOF'?\\s*>(\\S+\\.yaml)\\s*\\n(.*?)\\nEOF")

type heredoc struct {
	quoted bool
	fname  string
	body   string
}

func extractHeredocs(doc string) []heredoc {
	var out []heredoc
	for _, m := range heredocRe.FindAllStringSubmatch(doc, -1) {
		out = append(out, heredoc{quoted: m[1] == "'", fname: m[2], body: m[3]})
	}
	return out
}

var newDocName = "vllm_with_curvine.md"
var refDocName = "curvine_cache_runtime.md"

// C1: every embedded manifest in the new docs is parseable YAML with apiVersion+kind.
func TestDocManifestsParseAsYAML(t *testing.T) {
	root := repoRoot(t)
	wantKinds := map[string]bool{"Dataset": false, "CacheRuntime": false, "DataLoad": false, "Deployment": false, "Service": false}
	for _, lang := range []string{"en", "zh"} {
		docs := extractHeredocs(readDoc(t, root, lang, newDocName))
		if len(docs) != 4 {
			t.Fatalf("%s: expected 4 yaml heredocs in new doc, got %d", lang, len(docs))
		}
		for _, h := range docs {
			// a heredoc file may hold several YAML documents (vllm.yaml: Deployment + Service)
			for di, docText := range regexp.MustCompile(`(?m)^---$`).Split(h.body, -1) {
				if strings.TrimSpace(docText) == "" {
					continue
				}
				var obj map[string]interface{}
				if err := yaml.Unmarshal([]byte(docText), &obj); err != nil {
					t.Fatalf("%s %s doc#%d: YAML parse error: %v", lang, h.fname, di, err)
				}
				kind, _ := obj["kind"].(string)
				api, _ := obj["apiVersion"].(string)
				if kind == "" || api == "" {
					t.Fatalf("%s %s doc#%d: missing kind/apiVersion", lang, h.fname, di)
				}
				if _, ok := wantKinds[kind]; ok {
					wantKinds[kind] = true
				}
				t.Logf("%s %s doc#%d -> %s/%s", lang, h.fname, di, api, kind)
			}
		}
	}
	for k, seen := range wantKinds {
		if !seen {
			t.Fatalf("kind %s not found among embedded manifests", k)
		}
	}
}

// C2: the en and zh docs embed byte-identical manifests.
func TestEnZhManifestParity(t *testing.T) {
	root := repoRoot(t)
	en := extractHeredocs(readDoc(t, root, "en", newDocName))
	zh := extractHeredocs(readDoc(t, root, "zh", newDocName))
	if len(en) != len(zh) {
		t.Fatalf("en has %d yaml blocks, zh has %d", len(en), len(zh))
	}
	for i := range en {
		if en[i].fname != zh[i].fname || en[i].body != zh[i].body {
			t.Fatalf("block %d differs: en=%s zh=%s", i, en[i].fname, zh[i].fname)
		}
	}
}

// C3: every UNQUOTED heredoc in the new docs has no shell-expandable content
// (the bug class fixed by #6252 in the referenced doc).
func TestHeredocsExpansionSafe(t *testing.T) {
	root := repoRoot(t)
	for _, lang := range []string{"en", "zh"} {
		for _, h := range extractHeredocs(readDoc(t, root, lang, newDocName)) {
			if h.quoted {
				continue
			}
			if strings.Contains(h.body, "$") || strings.Contains(h.body, "`") {
				t.Fatalf("%s %s: unquoted heredoc contains $ or backtick; local shell will expand it", lang, h.fname)
			}
		}
	}
}

// C4: relative markdown links in the new docs resolve, and both TOCs list the doc.
func TestRelativeLinksResolve(t *testing.T) {
	root := repoRoot(t)
	linkRe := regexp.MustCompile(`\]\(([^)\s]+)\)`)
	for _, lang := range []string{"en", "zh"} {
		docPath := filepath.Join(root, "docs", lang, "samples", "cacheruntime", newDocName)
		b, err := os.ReadFile(docPath)
		if err != nil {
			t.Fatalf("read doc: %v", err)
		}
		for _, m := range linkRe.FindAllStringSubmatch(string(b), -1) {
			target := m[1]
			if strings.HasPrefix(target, "http://") || strings.Contains(target, "://") || strings.HasPrefix(target, "#") {
				continue
			}
			target = strings.SplitN(target, "#", 2)[0]
			resolved := filepath.Clean(filepath.Join(filepath.Dir(docPath), target))
			if _, err := os.Stat(resolved); err != nil {
				t.Fatalf("%s: link %s does not resolve (%v)", lang, m[1], err)
			}
			t.Logf("%s: link %s -> OK", lang, m[1])
		}
		tocPath := filepath.Join(root, "docs", lang, "TOC.md")
		toc, err := os.ReadFile(tocPath)
		if err != nil {
			t.Fatalf("read TOC: %v", err)
		}
		entry := "samples/cacheruntime/" + newDocName
		if !strings.Contains(string(toc), "("+entry+")") {
			t.Fatalf("%s TOC.md missing entry %s", lang, entry)
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(tocPath), entry)); err != nil {
			t.Fatalf("%s TOC entry %s does not resolve", lang, entry)
		}
	}
}

// C5: code-level claims in the doc match the source of truth in the repo.
func TestDocClaimsMatchSource(t *testing.T) {
	root := repoRoot(t)
	assertContains := func(rel, needle, why string) {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if !strings.Contains(string(b), needle) {
			t.Fatalf("%s: expected to find %q (%s)", rel, needle, why)
		}
	}
	// pod label selector used by `kubectl get pod -l cacheruntime.fluid.io/name=curvine-demo`
	assertContains("pkg/common/label.go", `LabelAnnotationPrefix = "fluid.io/"`, "label prefix")
	assertContains("pkg/common/label.go", `CacheRuntimeLabelAnnotationPrefix = "cacheruntime." + LabelAnnotationPrefix`, "cacheruntime label prefix")
	assertContains("pkg/common/label.go", `LabelCacheRuntimeName = CacheRuntimeLabelAnnotationPrefix + "name"`, "doc selector cacheruntime.fluid.io/name")
	assertContains("pkg/ddc/cache/component/component_manager.go", "common.LabelCacheRuntimeName: runtimeName", "pods carry the label")
	// DataLoad phase value used by `kubectl wait --for=jsonpath='{.status.phase}'=Complete`
	assertContains("pkg/common/types.go", `PhaseComplete  Phase = "Complete"`, "dataload Complete phase")
	assertContains("api/v1alpha1/dataload_types.go", "`json:\"target,omitempty\"`", "dataload spec.target")
	assertContains("api/v1alpha1/dataload_types.go", "Path string `json:\"path\"`", "dataload target.path")
	assertContains("api/v1alpha1/dataload_types.go", "JSONPath=`.status.phase`", "dataload status.phase jsonpath")
	// helm toggle + controller deployment name from Prerequisites
	assertContains("charts/fluid/fluid/values.yaml", "cacheruntime:", "runtime.cacheruntime values key")
	assertContains("charts/fluid/fluid/templates/controller/cacheruntime_controller.yaml", "name: cacheruntime-controller", "controller deployment name")
	assertContains("charts/fluid/fluid/templates/controller/cacheruntime_controller.yaml", ".Values.runtime.cacheruntime.enabled", "helm gate used by install docs")
	// CacheRuntime CRD fields used by cacheruntime.yaml
	assertContains("api/v1alpha1/cacheruntime_types.go", "RuntimeClassName string `json:\"runtimeClassName\"`", "spec.runtimeClassName")
	assertContains("api/v1alpha1/cacheruntime_types.go", "Levels []RuntimeTieredStoreLevel `json:\"levels,omitempty\"`", "tieredStore.levels")
	assertContains("api/v1alpha1/cacheruntime_types.go", "EmptyDir *EmptyDirMediumSource `json:\"emptyDir,omitempty\"`", "levels[].emptyDir")
	assertContains("api/v1alpha1/cacheruntime_types.go", "Quota resource.Quantity `json:\"quota\"`", "emptyDir.quota")
}

// C6: the steps this doc delegates to the Curvine sample doc actually exist there
// (steps 1/2/4), with the resource and file names the new doc references.
func TestCrossReferencedStepsExist(t *testing.T) {
	root := repoRoot(t)
	for _, lang := range []string{"en", "zh"} {
		ref := readDoc(t, root, lang, refDocName)
		for _, needle := range []string{
			"name: curvine-secret",               // credential secret created with MinIO
			"http://minio:9000/test",             // bucket `test` creation
			"name: curvine-demo",                 // CacheRuntimeClass name
			"cat<<EOF >minio.yaml",               // cleanup deletes minio.yaml
			"cat<<EOF >minio_create_bucket.yaml", // cleanup deletes minio_create_bucket.yaml
			"app: minio",                         // selector for kubectl wait -l app=minio
		} {
			if !strings.Contains(ref, needle) {
				t.Fatalf("%s referenced doc missing %q", lang, needle)
			}
		}
	}
	enRef := readDoc(t, root, "en", refDocName)
	for _, h := range []string{"## Step 1: Deploy MinIO", "## Step 2: Create a MinIO Bucket", "## Step 4: Define the CacheRuntimeClass"} {
		if !strings.Contains(enRef, h) {
			t.Fatalf("en referenced doc missing heading %q", h)
		}
	}
}

// C7: the doc claims "only the quota changes" vs the basic sample's CacheRuntime
// (1Gi -> 4Gi); pin that so future drift in either doc is caught.
func TestRuntimeSampleDeltaIsOnlyQuota(t *testing.T) {
	root := repoRoot(t)
	for _, lang := range []string{"en", "zh"} {
		newDocs := extractHeredocs(readDoc(t, root, lang, newDocName))
		refDocs := extractHeredocs(readDoc(t, root, lang, refDocName))
		byName := func(hs []heredoc, n string) *heredoc {
			for i := range hs {
				if hs[i].fname == n {
					return &hs[i]
				}
			}
			return nil
		}
		newCR, refCR := byName(newDocs, "cacheruntime.yaml"), byName(refDocs, "cacheruntime.yaml")
		if newCR == nil || refCR == nil {
			t.Fatalf("%s: cacheruntime.yaml heredoc missing (new=%v ref=%v)", lang, newCR != nil, refCR != nil)
		}
		nl, rl := strings.Split(strings.TrimSpace(newCR.body), "\n"), strings.Split(strings.TrimSpace(refCR.body), "\n")
		if len(nl) != len(rl) {
			t.Fatalf("%s: cacheruntime.yaml line count differs from referenced doc (%d vs %d)", lang, len(nl), len(rl))
		}
		diff := 0
		for i := range nl {
			if nl[i] != rl[i] {
				diff++
				if !(strings.Contains(rl[i], "quota: 1Gi") && strings.Contains(nl[i], "quota: 4Gi")) {
					t.Fatalf("%s: unexpected diff at line %d: ref=%q new=%q", lang, i+1, rl[i], nl[i])
				}
			}
		}
		if diff != 1 {
			t.Fatalf("%s: expected exactly 1 diff line (quota), got %d", lang, diff)
		}
		newDS, refDS := byName(newDocs, "dataset.yaml"), byName(refDocs, "dataset.yaml")
		if newDS == nil || refDS == nil || newDS.body != refDS.body {
			t.Fatalf("%s: dataset.yaml differs between the two docs", lang)
		}
	}
}

// F1 (contract): the new doc delegates its Step 1 to the Curvine sample doc. For the
// sample to be followable end-to-end, that referenced doc must carry the #6252 fixes:
// a quoted heredoc for the CacheRuntimeClass (its body uses $VARS, and an unquoted
// heredoc provably mangles them) and an existing image tag (curvine/curvine:vX, not
// curvine/curvine:X). FAILS until #6252 is merged; the failure IS the F1 repro.
func TestReferencedCurvineDocCarriesDependencyFixes(t *testing.T) {
	root := repoRoot(t)
	for _, lang := range []string{"en", "zh"} {
		ref := readDoc(t, root, lang, refDocName)
		var class *heredoc
		docs := extractHeredocs(ref)
		for i := range docs {
			if docs[i].fname == "cacheruntimeclass.yaml" {
				class = &docs[i]
			}
		}
		if class == nil {
			t.Fatalf("%s: referenced doc has no cacheruntimeclass.yaml heredoc", lang)
		}
		if !class.quoted {
			t.Fatalf("F1 %s: CacheRuntimeClass heredoc is unquoted cat<<EOF; body contains $VARS that the local shell mangles (needs #6252)", lang)
		}
		if strings.Contains(class.body, "curvine/curvine:0.3.6-alpha") {
			t.Fatalf("F1: %s referenced doc uses image tag curvine/curvine:0.3.6-alpha which does not exist on Docker Hub (needs #6252)", lang)
		}
		if !strings.Contains(class.body, "curvine/curvine:v") {
			t.Fatalf("F1: %s referenced doc does not pin a v-prefixed curvine image tag", lang)
		}
	}
}

// F3 (contract; fix landed in 46cd547): Step 2's port-forward lifecycle must not
// rely on shell job control (`kill %1` kills whatever happens to be job #1), and
// the readiness wait must be bounded so a failed port-forward cannot spin forever.
// Fails on 33c1eef (kill %1 + unbounded `until` loop); passes once the PID is
// captured and reused and the wait is wrapped in `timeout`.
func TestPortForwardLifecycleHardened(t *testing.T) {
	root := repoRoot(t)
	for _, lang := range []string{"en", "zh"} {
		doc := readDoc(t, root, lang, newDocName)
		if strings.Contains(doc, "kill %1") {
			t.Fatalf("F3 %s: doc still stops the port-forward with `kill %%1` (job-control; kills the wrong job if anything else was backgrounded)", lang)
		}
		iPF := strings.Index(doc, "kubectl port-forward svc/minio 19000:9000 &")
		iCap := strings.Index(doc, "PF_PID=$!")
		iKill := strings.Index(doc, `kill "$PF_PID"`)
		if iPF < 0 || iCap < 0 || iKill < 0 {
			t.Fatalf("F3 %s: expected port-forward start, PF_PID capture and `kill \"$PF_PID\"` (found start=%v capture=%v kill=%v)", lang, iPF >= 0, iCap >= 0, iKill >= 0)
		}
		if !(iPF < iCap && iCap < iKill) {
			t.Fatalf("F3 %s: PF_PID capture/kill ordering wrong (start=%d capture=%d kill=%d)", lang, iPF, iCap, iKill)
		}
		// bounded readiness wait: the `until curl ...` loop must be wrapped so it
		// terminates (timeout ...) and the loop must not be the bare unbounded form
		if strings.Contains(doc, "$ until curl -s -o /dev/null http://127.0.0.1:19000/minio/health/live; do sleep 1; done") {
			t.Fatalf("F3 %s: readiness wait is the unbounded `until curl ...` loop with no timeout", lang)
		}
		if !strings.Contains(doc, "timeout 60 sh -c 'until curl -s -o /dev/null http://127.0.0.1:19000/minio/health/live") {
			t.Fatalf("F3 %s: readiness wait is not bounded by `timeout` around the until-loop", lang)
		}
	}
}

// F4 (contract; fix landed in 46cd547): `kubectl wait --for=jsonpath=...` (used in
// Step 4) requires kubectl >= 1.23, so Prerequisites must say so. Fails on 33c1eef
// (prereq listed kubectl with no version); passes once 1.23 is named with the reason.
func TestPrerequisitesStateKubectlVersion(t *testing.T) {
	root := repoRoot(t)
	for _, lang := range []string{"en", "zh"} {
		doc := readDoc(t, root, lang, newDocName)
		if !strings.Contains(doc, "kubectl wait --for=jsonpath") {
			t.Fatalf("F4 %s: prereq should name --for=jsonpath as the reason for the kubectl floor", lang)
		}
		if !strings.Contains(doc, "1.23") {
			t.Fatalf("F4 %s: Prerequisites do not state the kubectl >= 1.23 floor implied by --for=jsonpath", lang)
		}
	}
}
