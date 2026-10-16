// Package securitymodeldocs is a review-verification harness for PR #6211
// (docs: define Fluid security model and resource permissions). It is ADDITIVE
// review tooling: it changes no production code and asserts that the factual
// claims made in security/security-model{,_zh}.md hold against the code under
// review. See docs/verification/security-model-codex/README.md.
package securitymodeldocs

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	yaml "sigs.k8s.io/yaml"
)

// repoRoot resolves the repository root from this test file's location
// (docs/verification/security-model-codex -> 3 levels up).
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve test file path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

func readDoc(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "security", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

var yamlBlockRe = regexp.MustCompile("(?s)```yaml\n(.*?)```")

// splitYAMLDocs splits a fenced yaml block into documents on '---' lines.
func splitYAMLDocs(block string) []string {
	var docs []string
	var cur []string
	for _, line := range strings.Split(block, "\n") {
		if strings.TrimSpace(line) == "---" {
			docs = append(docs, strings.Join(cur, "\n"))
			cur = nil
			continue
		}
		cur = append(cur, line)
	}
	docs = append(docs, strings.Join(cur, "\n"))
	var out []string
	for _, d := range docs {
		if strings.TrimSpace(d) != "" {
			out = append(out, d)
		}
	}
	return out
}

// TestL1_YAMLBlocksParseAndIdenticalAcrossLanguages: every fenced yaml example
// in the security model must parse, and the EN/ZH documents must carry the
// same examples byte-for-byte (the PR's own test plan claims this).
func TestL1_YAMLBlocksParseAndIdenticalAcrossLanguages(t *testing.T) {
	en := readDoc(t, "security-model.md")
	zh := readDoc(t, "security-model_zh.md")
	enBlocks := yamlBlockRe.FindAllStringSubmatch(en, -1)
	zhBlocks := yamlBlockRe.FindAllStringSubmatch(zh, -1)
	if len(enBlocks) == 0 {
		t.Fatal("no yaml blocks found in EN doc")
	}
	if len(enBlocks) != len(zhBlocks) {
		t.Fatalf("yaml block count differs: en=%d zh=%d", len(enBlocks), len(zhBlocks))
	}
	for i := range enBlocks {
		// blocks may hold multiple documents separated by '---'
		for j, doc := range splitYAMLDocs(enBlocks[i][1]) {
			var v interface{}
			if err := yaml.Unmarshal([]byte(doc), &v); err != nil {
				t.Errorf("EN yaml block %d doc %d does not parse: %v", i, j, err)
			}
		}
		if enBlocks[i][1] != zhBlocks[i][1] {
			t.Errorf("yaml block %d differs between EN and ZH docs", i)
		}
	}
	t.Logf("%d yaml blocks parse and are identical across languages", len(enBlocks))
}

var linkRe = regexp.MustCompile(`\]\(([^)]+)\)`)

// TestL1_RelativeLinksResolve: every relative markdown link target must exist.
func TestL1_RelativeLinksResolve(t *testing.T) {
	for _, doc := range []string{"security-model.md", "security-model_zh.md"} {
		body := readDoc(t, doc)
		for _, m := range linkRe.FindAllStringSubmatch(body, -1) {
			target := m[1]
			if strings.Contains(target, "://") || strings.HasPrefix(target, "#") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			target = strings.SplitN(target, "#", 2)[0]
			if target == "" {
				continue
			}
			p := filepath.Join(repoRoot(t), "security", filepath.FromSlash(target))
			if _, err := os.Stat(p); err != nil {
				t.Errorf("%s: link target %q does not resolve (%v)", doc, m[1], err)
			}
		}
	}
}

// TestL1_HeadingStructureParity: EN and ZH documents must have the same number
// of headings with the same section numbering.
func TestL1_HeadingStructureParity(t *testing.T) {
	numRe := regexp.MustCompile(`^#+\s*([0-9S][0-9.]*)`)
	extract := func(body string) []string {
		var out []string
		for _, line := range strings.Split(body, "\n") {
			if strings.HasPrefix(line, "#") {
				num := ""
				if m := numRe.FindStringSubmatch(line); m != nil {
					num = m[1]
				}
				out = append(out, num)
			}
		}
		return out
	}
	en := extract(readDoc(t, "security-model.md"))
	zh := extract(readDoc(t, "security-model_zh.md"))
	if len(en) != len(zh) {
		t.Fatalf("heading count differs: en=%d zh=%d", len(en), len(zh))
	}
	for i := range en {
		if en[i] != zh[i] {
			t.Errorf("heading %d numbering differs: en=%q zh=%q", i, en[i], zh[i])
		}
	}
	t.Logf("%d headings aligned across languages", len(en))
}

var codeRefRe = regexp.MustCompile("`((?:pkg|api|charts|config)/[^`]+)`")

// TestL1_CodePathReferencesResolve: the doc's preamble declares "A reference
// that no longer resolves is a documentation bug." Every backticked repo path
// cited in the doc must exist in the tree under review.
func TestL1_CodePathReferencesResolve(t *testing.T) {
	for _, doc := range []string{"security-model.md", "security-model_zh.md"} {
		body := readDoc(t, doc)
		seen := map[string]bool{}
		for _, m := range codeRefRe.FindAllStringSubmatch(body, -1) {
			ref := m[1]
			if seen[ref] {
				continue
			}
			seen[ref] = true
			p := filepath.Join(repoRoot(t), filepath.FromSlash(ref))
			if _, err := os.Stat(p); err != nil {
				t.Errorf("%s: code reference %q does not resolve", doc, ref)
			}
		}
		t.Logf("%s: %d distinct code references checked", doc, len(seen))
	}
}

// pathFuncRe pairs a backticked file path with the next backticked token when
// that token looks like a Go function/method name (doc convention:
// `pkg/x/y.go`, `someFunc`).
var pathFuncRe = regexp.MustCompile("`((?:pkg|api|charts|config)/[^`]+\\.go)`[;,]?\\s*`([A-Za-z_][A-Za-z0-9_]*)`")

// TestL1_CitedFunctionsExist: function names cited next to a file path must
// appear in that file.
func TestL1_CitedFunctionsExist(t *testing.T) {
	for _, doc := range []string{"security-model.md", "security-model_zh.md"} {
		body := readDoc(t, doc)
		checked := 0
		for _, m := range pathFuncRe.FindAllStringSubmatch(body, -1) {
			file, fn := m[1], m[2]
			b, err := os.ReadFile(filepath.Join(repoRoot(t), filepath.FromSlash(file)))
			if err != nil {
				t.Errorf("%s: cited file %q unreadable: %v", doc, file, err)
				continue
			}
			if !strings.Contains(string(b), fn) {
				t.Errorf("%s: function %q not found in %s", doc, fn, file)
			}
			checked++
		}
		t.Logf("%s: %d path->function citations checked", doc, checked)
	}
}

// TestL1_CRDCountMatchesDoc: the doc (§4) claims 14 user-facing data.fluid.io
// CRDs and one internal workload.fluid.io CRD.
func TestL1_CRDCountMatchesDoc(t *testing.T) {
	root := repoRoot(t)
	matches, err := filepath.Glob(filepath.Join(root, "config", "crd", "bases", "data.fluid.io_*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 14 {
		t.Errorf("doc claims 14 user-facing data.fluid.io CRDs; found %d in config/crd/bases", len(matches))
	}
	wl, err := filepath.Glob(filepath.Join(root, "charts", "fluid", "fluid", "crds", "workload.fluid.io_*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(wl) != 1 {
		t.Errorf("doc claims one internal workload.fluid.io CRD; found %d", len(wl))
	}
}

// TestL1_ConfigRBACScaffoldRolesUnreferenced: §4's note claims the
// kubebuilder editor/viewer role files under config/rbac are referenced by
// neither config/rbac/kustomization.yaml nor the Helm chart. (The scaffolded
// files are actually named *_editor_role.yaml / *_viewer_role.yaml.)
func TestL1_ConfigRBACScaffoldRolesUnreferenced(t *testing.T) {
	root := repoRoot(t)
	roles, err := filepath.Glob(filepath.Join(root, "config", "rbac", "*_editor_role.yaml"))
	if err != nil || len(roles) == 0 {
		t.Fatalf("no editor roles found (glob err=%v) - claim vacuous", err)
	}
	kust, err := os.ReadFile(filepath.Join(root, "config", "rbac", "kustomization.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(kust), "editor-role") || strings.Contains(string(kust), "viewer-role") {
		t.Error("config/rbac/kustomization.yaml references editor/viewer roles; doc claim stale")
	}
	chartDir := filepath.Join(root, "charts", "fluid", "fluid", "templates")
	err = filepath.Walk(chartDir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		if strings.Contains(string(b), "editor-role") || strings.Contains(string(b), "viewer-role") {
			t.Errorf("helm template %s references editor/viewer roles; doc claim stale", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestL1_DefaultHelmDriverIsConfigmap: §5 claims the default Helm setting is
// helmDriver: configmap.
func TestL1_DefaultHelmDriverIsConfigmap(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "charts", "fluid", "fluid", "values.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`(?m)^helmDriver:\s*(\w+)`)
	m := re.FindSubmatch(b)
	if m == nil || string(m[1]) != "configmap" {
		t.Errorf("default helmDriver is not configmap: %q", m)
	}
}

// TestL1_ThinPublishSecretCopyClaims: §5 claims (a) the copied secret is named
// <runtime>-thin-publish-secret, (b) an empty source namespace resolves to
// "default", (c) the copy is gated behind an explicit non-default policy whose
// default is MountNodePublishSecretIfExists.
func TestL1_ThinPublishSecretCopyClaims(t *testing.T) {
	root := repoRoot(t)
	src, err := os.ReadFile(filepath.Join(root, "pkg", "ddc", "thin", "transform_pvc_mounts.go"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	if !strings.Contains(s, `"%s-%s-publish-secret"`) {
		t.Error("copy naming pattern <name>-<type>-publish-secret not found in transform_pvc_mounts.go")
	}
	if !strings.Contains(s, "secretNamespace = corev1.NamespaceDefault") {
		t.Error("empty source secret namespace no longer resolves to 'default'")
	}
	types, err := os.ReadFile(filepath.Join(root, "api", "v1alpha1", "thinruntimeprofile_types.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(types), "+kubebuilder:default=MountNodePublishSecretIfExists") {
		t.Error("default nodePublishSecretPolicy is not MountNodePublishSecretIfExists")
	}
}

// TestL1_ReferenceDatasetSubpathGuard: §5 claims a dataset:// reference
// subpath cannot escape the physical Dataset root (NormalizeSubPath rejects
// non-local paths).
func TestL1_ReferenceDatasetSubpathGuard(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "pkg", "utils", "mount.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "filepath.IsLocal(cleaned)") {
		t.Error("NormalizeSubPath no longer rejects escaping subpaths via filepath.IsLocal")
	}
}

// TestL1_CrossNamespaceTargetRejection: §5 claims DataLoad and DataProcess
// reject a target Dataset in another namespace.
func TestL1_CrossNamespaceTargetRejection(t *testing.T) {
	root := repoRoot(t)
	for _, rel := range []string{
		"pkg/controllers/v1alpha1/dataload/implement.go",
		"pkg/controllers/v1alpha1/dataprocess/implement.go",
	} {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), "TargetDatasetNamespaceNotSame") {
			t.Errorf("%s: cross-namespace target rejection (TargetDatasetNamespaceNotSame) not found", rel)
		}
	}
}

// TestL1_UnprivilegedMutatorClaims: §5 claims the unprivileged sidecar mutator
// sets privileged:false, removes SYS_ADMIN from Capabilities.Add (and only
// there), and that the mutation path still converts the Dataset PVC volume to
// hostPath.
func TestL1_UnprivilegedMutatorClaims(t *testing.T) {
	root := repoRoot(t)
	u, err := os.ReadFile(filepath.Join(root, "pkg", "application", "inject", "fuse", "mutator", "mutator_unprivileged.go"))
	if err != nil {
		t.Fatal(err)
	}
	us := string(u)
	if !strings.Contains(us, "SecurityContext.Privileged = &privilegedContainer") {
		t.Error("unprivileged mutator no longer sets privileged:false")
	}
	if !strings.Contains(us, `TrimCapabilities(template.FuseContainer.SecurityContext.Capabilities.Add, []string{"SYS_ADMIN"})`) {
		t.Error("unprivileged mutator no longer trims SYS_ADMIN from Capabilities.Add")
	}
	if strings.Contains(us, "Capabilities.Drop") {
		t.Error("unprivileged mutator now touches Capabilities.Drop; doc says it does not")
	}
	if !strings.Contains(us, "mutateDatasetVolumesFn = defaultMutateDatasetVolumes") {
		t.Error("unprivileged mutator no longer uses defaultMutateDatasetVolumes; hostPath claim stale")
	}
	d, err := os.ReadFile(filepath.Join(root, "pkg", "application", "inject", "fuse", "mutator", "mutator_default.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(d), "HostPath: &corev1.HostPathVolumeSource") {
		t.Error("defaultMutateDatasetVolumes no longer converts the Dataset PVC volume to hostPath")
	}
}

// TestL1_FuseCommandClaimScoped (F1, contract): the doc (§3) says that "for
// Runtime APIs with a FUSE spec, writers choose the ... entrypoint
// (spec.fuse.command, spec.fuse.args)". In the code under review,
// spec.fuse.command exists ONLY on ThinRuntime (verified: among the CRDs only
// thinruntimes carries a fuse-level command field). This test fails while the
// doc sentence presents spec.fuse.command as a generic field without scoping
// it to ThinRuntime, and passes once the sentence names ThinRuntime.
func TestL1_FuseCommandClaimScoped(t *testing.T) {
	root := repoRoot(t)
	// Ground truth from the CRDs: which runtime kinds carry a fuse-level command field?
	withCommand := []string{}
	for _, kind := range []string{"alluxioruntimes", "juicefsruntimes", "jindoruntimes", "thinruntimes", "efcruntimes", "vineyardruntimes", "cacheruntimes"} {
		b, err := os.ReadFile(filepath.Join(root, "config", "crd", "bases", "data.fluid.io_"+kind+".yaml"))
		if err != nil {
			t.Fatal(err)
		}
		var v map[string]interface{}
		if err := yaml.Unmarshal(b, &v); err != nil {
			t.Fatalf("parse CRD %s: %v", kind, err)
		}
		if hasFuseCommand(v) {
			withCommand = append(withCommand, kind)
		}
	}
	t.Logf("runtime kinds whose fuse spec carries `command`: %v", withCommand)
	if len(withCommand) == 0 {
		t.Fatal("no runtime kind carries spec.fuse.command; harness assumption broken")
	}

	en := readDoc(t, "security-model.md")
	zh := readDoc(t, "security-model_zh.md")
	// F1 targets §3 (the basic rule), where the fields are presented as
	// references. §7's out-of-scope list is explicitly example-based
	// ("and equivalents") and is not part of the finding.
	section3 := func(body string) string {
		start := strings.Index(body, "## 3.")
		end := strings.Index(body, "## 4.")
		if start < 0 || end < 0 || end <= start {
			t.Fatal("cannot locate section 3 in doc")
		}
		return body[start:end]
	}
	for name, body := range map[string]string{"en": section3(en), "zh": section3(zh)} {
		for _, line := range strings.Split(body, "\n") {
			if !strings.Contains(line, "spec.fuse.command") {
				continue
			}
			scoped := false
			for _, kind := range withCommand {
				// accept either the Kind or the plural resource name in the same line
				if strings.Contains(strings.ToLower(line), strings.TrimSuffix(kind, "s")) {
					scoped = true
				}
			}
			if !scoped {
				t.Errorf("%s doc: line mentions spec.fuse.command without scoping it to %v: %s", name, withCommand, strings.TrimSpace(line))
			}
		}
	}
}

// hasFuseCommand walks a parsed CRD to
// spec.versions[*].schema.openAPIV3Schema.properties.spec.properties.fuse
// .properties and reports whether a `command` field exists. sigs.k8s.io/yaml
// decodes via JSON, so nested maps are map[string]interface{}.
func hasFuseCommand(crd map[string]interface{}) bool {
	get := func(m map[string]interface{}, key string) map[string]interface{} {
		if m == nil {
			return nil
		}
		child, _ := m[key].(map[string]interface{})
		return child
	}
	spec := get(crd, "spec")
	versions, _ := spec["versions"].([]interface{})
	for _, ver := range versions {
		vm, _ := ver.(map[string]interface{})
		props := get(get(get(get(get(vm, "schema"), "openAPIV3Schema"), "properties"), "spec"), "properties")
		fuse := get(props, "fuse")
		if fuseProps := get(fuse, "properties"); fuseProps != nil {
			if _, ok := fuseProps["command"]; ok {
				return true
			}
		}
	}
	return false
}

// TestL1_ConfigRBACGlobPatternMatches (F2, contract): the doc (§4 note) cites
// the scaffolded roles with backticked glob patterns. The actual files use
// underscores (dataload_editor_role.yaml, ...); the doc's literal patterns
// must match at least one real file each. Fails until the doc patterns match
// the real filenames.
func TestL1_ConfigRBACGlobPatternMatches(t *testing.T) {
	root := repoRoot(t)
	globRe := regexp.MustCompile("`(\\*[^`]*role[^`]*)`")
	for _, doc := range []string{"security-model.md", "security-model_zh.md"} {
		body := readDoc(t, doc)
		// restrict to the config/rbac note section
		idx := strings.Index(body, "config/rbac")
		if idx < 0 {
			t.Fatalf("%s: no config/rbac mention found", doc)
		}
		section := body[idx:]
		if end := strings.Index(section, "\n## "); end > 0 {
			section = section[:end]
		}
		patterns := globRe.FindAllStringSubmatch(section, -1)
		if len(patterns) == 0 {
			t.Fatalf("%s: no role glob patterns found near config/rbac note", doc)
		}
		for _, m := range patterns {
			matches, err := filepath.Glob(filepath.Join(root, "config", "rbac", m[1]))
			if err != nil {
				t.Fatalf("%s: bad glob %q: %v", doc, m[1], err)
			}
			if len(matches) == 0 {
				t.Errorf("%s: doc pattern %q matches no file in config/rbac (real files use underscores, e.g. dataload_editor_role.yaml)", doc, m[1])
			}
		}
	}
}
