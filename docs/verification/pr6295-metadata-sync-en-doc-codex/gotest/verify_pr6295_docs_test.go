// Verification harness for PR https://github.com/fluid-cloudnative/fluid/pull/6295
// "docs: add the English metadata sync policy sample".
//
// This PR is docs-only: it adds docs/en/samples/metadata_sync.md (English
// translation of docs/zh/samples/metadata_sync.md) and registers it in
// docs/en/TOC.md. There is no production code to unit-test, so these tests
// encode the PR's own acceptance claims as CONTRACT tests (they must PASS on
// the PR head; a future revision that breaks them goes RED):
//
//	L1 (deterministic):
//	- TestPR6295EnglishPageRegisteredInTOC        (A1) TOC wiring + zh/en parity
//	- TestPR6295CodeBlocksMatchChineseSource      (A2) code blocks identical to zh source
//	- TestPR6295MetadataSyncFieldPathMatchesAPI   (A3) spec.management.metadataSyncPolicy.autoSync is real
//	- TestPR6295RelativeLinksResolve              (A4) relative links resolve to files
//	L2 (integration, runs the repo's real docs build script):
//	- TestPR6295DocsBuildIncludesNewPage          (A5) docs/scripts/mergeByTOC.py picks the page up
//
// The harness is additive only; it never modifies production code.
package pr6295docs

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// repoRoot returns the repository root, derived from this file's location:
// <root>/docs/verification/pr6295-metadata-sync-en-doc-codex/gotest/.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", ".."))
}

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

// --- TOC parsing helpers (mirror docs/scripts/mergeByTOC.py semantics) -------

type tocEntry struct {
	indent int    // number of leading spaces
	bullet string // "+" or "-"
	text   string // link text or section name
	link   string // "" for plain section headings
}

var tocLineRe = regexp.MustCompile(`^(\s*)([-+])\s+(?:\[(.*?)\]\((.*?)\)|(.*))$`)

func parseTOC(t *testing.T, rel string) []tocEntry {
	t.Helper()
	content := readRepoFile(t, rel)
	var entries []tocEntry
	inTOC := false
	sc := bufio.NewScanner(strings.NewReader(content))
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "## ") {
			if inTOC {
				break
			}
			inTOC = true
			continue
		}
		if !inTOC || strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}
		m := tocLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		e := tocEntry{indent: len(m[1]), bullet: m[2], link: m[4]}
		if m[3] != "" {
			e.text = m[3]
		} else {
			e.text = strings.TrimSpace(m[5])
		}
		entries = append(entries, e)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan %s: %v", rel, err)
	}
	return entries
}

// subsectionsOf returns the level-2 (2-space-indent) subsection names of the
// named level-1 (0-space-indent) section, in order, and the link targets found
// at level 3 within each subsection.
func subsectionsOf(entries []tocEntry, section string) (subs []string, links map[string][]string) {
	links = map[string][]string{}
	inSection := false
	curSub := ""
	for _, e := range entries {
		switch {
		case e.indent == 0:
			inSection = e.text == section
			curSub = ""
		case e.indent == 2 && inSection:
			curSub = e.text
			subs = append(subs, e.text)
		case e.indent == 4 && inSection && e.link != "":
			links[curSub] = append(links[curSub], e.link)
		}
	}
	return subs, links
}

// ------------------------------- A1: TOC wiring ------------------------------

// Contract: docs/en/samples/metadata_sync.md is registered in docs/en/TOC.md
// under Dataset -> Advanced Configuration, mirroring the zh TOC (数据集使用 ->
// 高级配置), and the target file exists.
func TestPR6295EnglishPageRegisteredInTOC(t *testing.T) {
	root := repoRoot(t)

	en := parseTOC(t, "docs/en/TOC.md")
	enSubs, enLinks := subsectionsOf(en, "Dataset")
	zh := parseTOC(t, "docs/zh/TOC.md")
	zhSubs, zhLinks := subsectionsOf(zh, "数据集使用")

	wantEnOrder := []string{"Creation", "Operation", "Security", "Advanced Configuration"}
	if strings.Join(enSubs, ",") != strings.Join(wantEnOrder, ",") {
		t.Errorf("en Dataset subsection order = %v, want %v", enSubs, wantEnOrder)
	}
	// zh/en structural parity: the subsection holding the page must be at the
	// same ordinal position (4th) in both languages.
	if len(enSubs) != len(zhSubs) {
		t.Errorf("zh/en Dataset subsection count differs: zh=%v en=%v", zhSubs, enSubs)
	} else if enSubs[len(enSubs)-1] != "Advanced Configuration" || zhSubs[len(zhSubs)-1] != "高级配置" {
		t.Errorf("metadata-sync subsection is not the last Dataset subsection in both TOCs: zh=%q en=%q",
			zhSubs[len(zhSubs)-1], enSubs[len(enSubs)-1])
	}

	enTargets := enLinks["Advanced Configuration"]
	found := false
	for _, l := range enTargets {
		if l == "samples/metadata_sync.md" {
			found = true
		}
	}
	if !found {
		t.Errorf("docs/en/TOC.md Dataset>Advanced Configuration links = %v, want samples/metadata_sync.md among them", enTargets)
	}
	zhTargets := zhLinks["高级配置"]
	foundZh := false
	for _, l := range zhTargets {
		if l == "samples/metadata_sync.md" {
			foundZh = true
		}
	}
	if !foundZh {
		t.Errorf("docs/zh/TOC.md 数据集使用>高级配置 links = %v, want samples/metadata_sync.md among them", zhTargets)
	}
	if _, err := os.Stat(filepath.Join(root, "docs/en/samples/metadata_sync.md")); err != nil {
		t.Errorf("TOC target docs/en/samples/metadata_sync.md missing: %v", err)
	}
}

// ------------------------- A2: code-block fidelity ---------------------------

func extractFencedBlocks(t *testing.T, rel string) []string {
	t.Helper()
	content := readRepoFile(t, rel)
	var blocks []string
	var cur []string
	inBlock := false
	sc := bufio.NewScanner(strings.NewReader(content))
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			if inBlock {
				blocks = append(blocks, strings.Join(cur, "\n"))
				cur = nil
			}
			inBlock = !inBlock
			continue
		}
		if inBlock {
			cur = append(cur, line)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan %s: %v", rel, err)
	}
	return blocks
}

func rstripLines(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	return strings.Join(lines, "\n")
}

// Contract: the English page's fenced code blocks match the Chinese source
// page exactly, modulo trailing whitespace (the zh page carries one trailing
// space in the kubectl patch block; dropping it is the only tolerated delta).
func TestPR6295CodeBlocksMatchChineseSource(t *testing.T) {
	zh := extractFencedBlocks(t, "docs/zh/samples/metadata_sync.md")
	en := extractFencedBlocks(t, "docs/en/samples/metadata_sync.md")
	if len(zh) != len(en) {
		t.Fatalf("fenced code block count: zh=%d en=%d", len(zh), len(en))
	}
	for i := range zh {
		if zh[i] == en[i] {
			continue
		}
		if rstripLines(zh[i]) == rstripLines(en[i]) {
			t.Logf("block %d differs only in trailing whitespace (tolerated)", i)
			continue
		}
		t.Errorf("block %d content diverged from zh source\n--- zh ---\n%s\n--- en ---\n%s", i, zh[i], en[i])
	}
}

// -------------------- A3: documented API field path is real ------------------

// Contract: spec.management.metadataSyncPolicy.autoSync exists in the Go API
// with exactly these JSON tags, matching the doc's manifest and kubectl patch
// path /spec/management/metadataSyncPolicy/autoSync; default (unset) = sync on.
func TestPR6295MetadataSyncFieldPathMatchesAPI(t *testing.T) {
	runtimeTypes := readRepoFile(t, "api/v1alpha1/alluxioruntime_types.go")
	common := readRepoFile(t, "api/v1alpha1/common.go")

	if !regexp.MustCompile("RuntimeManagement RuntimeManagement `json:\"management,omitempty\"`").MatchString(runtimeTypes) {
		t.Error("AlluxioRuntimeSpec no longer exposes RuntimeManagement as json `management`")
	}
	if !regexp.MustCompile("MetadataSyncPolicy MetadataSyncPolicy `json:\"metadataSyncPolicy,omitempty\"`").MatchString(common) {
		t.Error("RuntimeManagement no longer exposes MetadataSyncPolicy as json `metadataSyncPolicy`")
	}
	if !regexp.MustCompile("AutoSync \\*bool `json:\"autoSync,omitempty\"`").MatchString(common) {
		t.Error("MetadataSyncPolicy no longer exposes AutoSync as json `autoSync`")
	}
	// The doc tells users to turn autoSync OFF; that only means something if the
	// default is ON. AutoSyncEnabled() must keep returning true when unset.
	if !strings.Contains(common, "func (msb *MetadataSyncPolicy) AutoSyncEnabled() bool {") {
		t.Error("MetadataSyncPolicy.AutoSyncEnabled() missing; default-sync-on semantics unverifiable")
	}

	// Mechanically check the doc's kubectl patch path against the json tag chain.
	doc := readRepoFile(t, "docs/en/samples/metadata_sync.md")
	if !strings.Contains(doc, `"path": "/spec/management/metadataSyncPolicy/autoSync"`) {
		t.Error("doc no longer contains the kubectl patch path /spec/management/metadataSyncPolicy/autoSync")
	}
	for _, seg := range []string{`json:"management,omitempty"`, `json:"metadataSyncPolicy,omitempty"`, `json:"autoSync,omitempty"`} {
		if !strings.Contains(runtimeTypes+common, seg) {
			t.Errorf("json tag %s backing the doc's patch path not found in API sources", seg)
		}
	}
	if !strings.Contains(doc, "autoSync: false") || !strings.Contains(doc, "autoSync=false") {
		t.Error("doc no longer demonstrates turning autoSync off")
	}
}

// --------------------------- A4: relative links ------------------------------

var mdLinkRe = regexp.MustCompile(`\[[^\]]*\]\(([^)\s]+)\)`)

// Contract: every relative link in the new page resolves to an existing file.
func TestPR6295RelativeLinksResolve(t *testing.T) {
	root := repoRoot(t)
	docRel := "docs/en/samples/metadata_sync.md"
	doc := readRepoFile(t, docRel)
	dir := filepath.Join(root, filepath.Dir(docRel))
	checked := 0
	for _, m := range mdLinkRe.FindAllStringSubmatch(doc, -1) {
		link := m[1]
		if strings.HasPrefix(link, "http://") || strings.HasPrefix(link, "https://") ||
			strings.HasPrefix(link, "#") || strings.HasPrefix(link, "mailto:") {
			continue
		}
		target := strings.SplitN(link, "#", 2)[0]
		if target == "" {
			continue
		}
		checked++
		if _, err := os.Stat(filepath.Clean(filepath.Join(dir, target))); err != nil {
			t.Errorf("relative link %q in %s does not resolve: %v", link, docRel, err)
		}
	}
	if checked == 0 {
		t.Error("no relative links found in the new page; expected at least the Installation Guide link")
	}
}

// --------------------- A5 (L2): real docs build picks it up ------------------

func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("readdir %s: %v", src, err)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dst, err)
	}
	for _, e := range entries {
		s := filepath.Join(src, e.Name())
		d := filepath.Join(dst, e.Name())
		if e.IsDir() {
			copyDir(t, s, d)
			continue
		}
		b, err := os.ReadFile(s)
		if err != nil {
			t.Fatalf("read %s: %v", s, err)
		}
		if err := os.WriteFile(d, b, 0o644); err != nil {
			t.Fatalf("write %s: %v", d, err)
		}
	}
}

// Contract (integration layer): the repository's own docs build script,
// docs/scripts/mergeByTOC.py, includes the new page when generating the
// all-in-one English doc (en/doc.md). Runs in a temp copy so the repo tree is
// never polluted.
func TestPR6295DocsBuildIncludesNewPage(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal("python3 not found; the docs build toolchain requires it")
	}
	root := repoRoot(t)
	tmp := t.TempDir()
	copyDir(t, filepath.Join(root, "docs", "en"), filepath.Join(tmp, "en"))
	copyDir(t, filepath.Join(root, "docs", "scripts"), filepath.Join(tmp, "scripts"))

	cmd := exec.Command(python, "scripts/mergeByTOC.py", "en/")
	cmd.Dir = tmp
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("mergeByTOC.py en/ failed: %v\n%s", err, out)
	}
	merged, err := os.ReadFile(filepath.Join(tmp, "en", "doc.md"))
	if err != nil {
		t.Fatalf("en/doc.md not generated: %v", err)
	}
	body := string(merged)
	// The page sits at TOC level 3 (Dataset -> Advanced Configuration -> page),
	// so its H1 is re-leveled to ### by mergeByTOC.py.
	if !strings.Contains(body, "### DEMO - Configure the Metadata Sync Policy") {
		t.Error("merged en/doc.md does not contain the new page heading at the expected level (###)")
	}
	if !strings.Contains(body, "kubectl patch alluxioruntime hbase") {
		t.Error("merged en/doc.md does not contain the new page body")
	}
}
