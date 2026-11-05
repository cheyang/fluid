// Verification harness for PR #6230 (chore(deps): bump prometheus/client_golang
// 1.20.5 -> 1.25.0). Harness-only file; no production code is modified.
//
// Finding F1 (contract polarity: FAILS on the PR head as submitted, PASSES once
// the toolchain story is made consistent in EITHER direction):
//
//	client_golang v1.25.0 raises its minimum required Go version to 1.26
//	(client_golang release notes: "[CHANGE] Minimum required Go version is now
//	1.26"). The PR therefore rewrites go.mod to "go 1.26.0" and drops the
//	"toolchain go1.25.12" line. But the project pins Go 1.25.12 in every place
//	that builds it:
//	  - .github/workflows/project-check.yml   GO_VERSION: 1.25.12
//	  - .github/workflows/backward-compatibility-e2e.yml  GO_VERSION: 1.25.12
//	  - .github/workflows/kind-e2e.yml        GO_VERSION: 1.25.12
//	  - .circleci/config.yml                  cimg/go:1.25
//	  - docker/Dockerfile.* (11 files)        golang:1.25.12-bookworm
//	CI runs with GOTOOLCHAIN=local and the official golang docker images bake
//	ENV GOTOOLCHAIN=local, so nothing auto-downloads Go 1.26: every build,
//	lint, unit-test, e2e and image-build job fails with
//	  "go: go.mod requires go >= 1.26.0 (running go 1.25.12; GOTOOLCHAIN=local)"
//
//	This test asserts the invariant the PR violated: the module's go directive
//	must not exceed any pinned toolchain that builds the repo.
package pr6230verify

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

type semver struct{ major, minor int }

func parseMinor(t *testing.T, s string) semver {
	t.Helper()
	re := regexp.MustCompile(`^(\d+)\.(\d+)`)
	m := re.FindStringSubmatch(s)
	if m == nil {
		t.Fatalf("cannot parse version %q", s)
	}
	maj, _ := strconv.Atoi(m[1])
	min, _ := strconv.Atoi(m[2])
	return semver{maj, min}
}

func (v semver) ge(o semver) bool {
	return v.major > o.major || (v.major == o.major && v.minor >= o.minor)
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		up := filepath.Dir(dir)
		if up == dir {
			t.Fatal("go.mod not found walking up from cwd")
		}
		dir = up
	}
}

// TestToolchainConsistencyWithPinnedToolchains is the F1 contract test.
// Expected: FAIL on the PR head af817d9 (go 1.26.0 directive vs 1.25.12 pins);
// PASS on base 30d488b and on any fixed head (either client_golang pinned back
// to a Go-1.25-compatible release, or all pins raised to >= 1.26 together).
func TestToolchainConsistencyWithPinnedToolchains(t *testing.T) {
	root := repoRoot(t)

	gomod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^go (\d+\.\d+(\.\d+)?)$`).FindSubmatch(gomod)
	if m == nil {
		t.Fatal("no go directive in go.mod")
	}
	goDirective := parseMinor(t, string(m[1]))
	t.Logf("go.mod go directive: %d.%d", goDirective.major, goDirective.minor)

	type pin struct{ where, ver string }
	var pins []pin

	// GitHub workflows: GO_VERSION: x.y.z
	wfGlob, _ := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.yml"))
	wfRe := regexp.MustCompile(`(?m)^\s*GO_VERSION:\s*"?([0-9]+\.[0-9]+(\.[0-9]+)?)"?\s*$`)
	for _, f := range re2Files(t, wfGlob) {
		for _, mm := range wfRe.FindAllStringSubmatch(f.content, -1) {
			pins = append(pins, pin{".github/workflows/" + filepath.Base(f.path), mm[1]})
		}
	}

	// Dockerfiles: FROM golang:x.y.z-...
	dfRe := regexp.MustCompile(`(?m)^FROM golang:([0-9]+\.[0-9]+(\.[0-9]+)?)`)
	dfGlob, _ := filepath.Glob(filepath.Join(root, "docker", "Dockerfile.*"))
	for _, f := range re2Files(t, dfGlob) {
		for _, mm := range dfRe.FindAllStringSubmatch(f.content, -1) {
			pins = append(pins, pin{"docker/" + filepath.Base(f.path), mm[1]})
		}
	}

	// CircleCI: cimg/go:x.y
	cc, err := os.ReadFile(filepath.Join(root, ".circleci", "config.yml"))
	if err == nil {
		ccRe := regexp.MustCompile(`cimg/go:([0-9]+\.[0-9]+)`)
		for _, mm := range ccRe.FindAllStringSubmatch(string(cc), -1) {
			pins = append(pins, pin{".circleci/config.yml", mm[1]})
		}
	}

	if len(pins) == 0 {
		t.Fatal("no pinned toolchains discovered; harness broken")
	}
	for _, p := range pins {
		pv := parseMinor(t, p.ver)
		if !pv.ge(goDirective) {
			t.Errorf("F1: %s pins Go %s but go.mod requires go %d.%d: "+
				"this build entrypoint fails with 'go.mod requires go >= %d.%d (GOTOOLCHAIN=local)'",
				p.where, p.ver, goDirective.major, goDirective.minor,
				goDirective.major, goDirective.minor)
		}
	}
}

type fileContent struct {
	path, content string
}

func re2Files(t *testing.T, paths []string) []fileContent {
	t.Helper()
	var out []fileContent
	for _, path := range paths {
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		out = append(out, fileContent{path, string(b)})
	}
	return out
}
