// Verification harness for fluid PR #6255 (reviewer: Codex).
// Contract tests: they PASS on the fixed (PR head) tree and FAIL on the base
// (go1.26.10) tree, proving the harness bites. Additive only; no production
// code is touched.
//
// Claims under test (GO-2026-6859 / CVE-2026-97054):
//   vulnerable toolchain ranges: [0, 1.26.11) and [1.27.0, 1.27.4)
//   (per https://vuln.go.dev/ID/GO-2026-6859.json, DB updated 2026-11-11)
//
//	C1: go.mod pins a `toolchain` version outside the vulnerable ranges.
//	C2: every Dockerfile that builds Go code pins golang:<fixed>-bookworm
//	    by digest, and all pins use one identical digest (consistency).
//	C3: no stale references to the old toolchain (1.26.10) or the old image
//	    digest remain in CI config, go.mod, or the Dockerfiles.
package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// repoRoot resolves the repository root relative to this file:
// docs/verification/<topic>/harness -> 4 levels up.
func repoRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join("..", "..", "..", "..")
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("cannot locate repo root from harness dir: %v", err)
	}
	return root
}

// semver is a parsed go toolchain version (major.minor.patch).
type semver struct{ major, minor, patch int }

func parseGoVersion(v string) (semver, error) {
	v = strings.TrimPrefix(v, "go")
	var s semver
	_, err := fmt.Sscanf(v, "%d.%d.%d", &s.major, &s.minor, &s.patch)
	return s, err
}

// vulnerableToGO20266859 reports whether a toolchain version is inside the
// affected ranges published for GO-2026-6859:
//
//	introduced 0        -> fixed 1.26.11
//	introduced 1.27.0-0 -> fixed 1.27.4
func vulnerableToGO20266859(s semver) bool {
	if s.major != 1 {
		return false // out of scope of this advisory's published ranges
	}
	switch s.minor {
	case 26:
		return s.patch < 11
	case 27:
		return s.patch < 4
	default:
		// 1.28+ branched after both fixes; earlier minors were fixed long ago
		// for their own lines, but treat anything < 1.26 as vulnerable per
		// "introduced 0" when it precedes its line's fix.
		return s.minor < 26
	}
}

// C1: go.mod pins a toolchain version that carries the GO-2026-6859 fix.
func TestToolchainVersionNotVulnerable(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	re := regexp.MustCompile(`(?m)^toolchain go([0-9]+\.[0-9]+\.[0-9]+)$`)
	m := re.FindSubmatch(data)
	if m == nil {
		t.Fatalf("no `toolchain goX.Y.Z` line found in go.mod")
	}
	sv, err := parseGoVersion("go" + string(m[1]))
	if err != nil {
		t.Fatalf("parse toolchain version %q: %v", m[1], err)
	}
	t.Logf("go.mod toolchain = go%s", m[1])
	if vulnerableToGO20266859(sv) {
		t.Errorf("toolchain go%s is inside GO-2026-6859 vulnerable range "+
			"(want >= go1.26.11, or >= go1.27.4 on the 1.27 line)", m[1])
	}
}

// dockerfilesBuildingGo lists every repo Dockerfile whose builder stage uses
// a golang base image (source of truth: scan for `FROM golang:` lines).
func golangFromLines(t *testing.T, root string) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	dirs := []string{"docker", "test", "addons", "samples", "charts"}
	for _, d := range dirs {
		_ = filepath.Walk(filepath.Join(root, d), func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.Contains(info.Name(), "Dockerfile") {
				return nil
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return nil
			}
			for _, line := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(strings.TrimSpace(line), "FROM golang:") {
					rel, _ := filepath.Rel(root, p)
					out[rel] = append(out[rel], strings.TrimSpace(line))
				}
			}
			return nil
		})
	}
	if len(out) == 0 {
		t.Fatalf("no `FROM golang:` lines found under %v", dirs)
	}
	return out
}

// C2: every golang builder image is pinned tag@sha256 with a fixed version,
// and all pins share one identical digest (the multi-arch index).
func TestDockerfileGolangPinsConsistentAndFixed(t *testing.T) {
	root := repoRoot(t)
	re := regexp.MustCompile(`^FROM golang:([0-9]+\.[0-9]+\.[0-9]+)-bookworm@sha256:([0-9a-f]{64})\b`)
	digests := map[string]int{}
	for file, lines := range golangFromLines(t, root) {
		for _, line := range lines {
			m := re.FindStringSubmatch(line)
			if m == nil {
				t.Errorf("%s: unpinned or malformed golang FROM line: %q", file, line)
				continue
			}
			sv, err := parseGoVersion("go" + m[1])
			if err != nil {
				t.Errorf("%s: unparsable golang tag %q", file, m[1])
				continue
			}
			if vulnerableToGO20266859(sv) {
				t.Errorf("%s: golang:%s is inside GO-2026-6859 vulnerable range", file, m[1])
			}
			digests[m[2]]++
		}
	}
	t.Logf("digests in use: %v", digests)
	if len(digests) != 1 {
		t.Errorf("expected exactly one shared golang image digest across all Dockerfiles, got %d: %v",
			len(digests), digests)
	}
}

// C3: no stale references to the previous toolchain (1.26.10) or the previous
// pinned golang digest anywhere outside vendor/.
func TestNoStaleToolchainReferences(t *testing.T) {
	root := repoRoot(t)
	const oldDigest = "23ce71337b726608afb1bc00986e630a05034a87a87ea1fc2cd1ae5cf33387eb"
	targets := []string{
		"go.mod", ".travis.yml",
		filepath.Join(".circleci", "config.yml"),
		filepath.Join(".github", "workflows", "project-check.yml"),
		filepath.Join(".github", "workflows", "kind-e2e.yml"),
		filepath.Join(".github", "workflows", "backward-compatibility-e2e.yml"),
	}
	// add every Dockerfile that builds Go code
	for f := range golangFromLines(t, root) {
		targets = append(targets, f)
	}
	for _, rel := range targets {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Errorf("read %s: %v", rel, err)
			continue
		}
		if strings.Contains(string(data), "1.26.10") {
			t.Errorf("%s: stale reference to go 1.26.10", rel)
		}
		if strings.Contains(string(data), oldDigest) {
			t.Errorf("%s: stale reference to previous golang image digest", rel)
		}
	}
}
