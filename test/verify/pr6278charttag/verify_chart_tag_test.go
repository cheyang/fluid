// Review-verification harness for fluid PR #6278 ("fix(chart): point the image
// tag at the published v1.1.0-dcb76167"). Additive only: no production code.
//
// Layers:
//
//	unit        - deterministic: helm template render + file parsing, no network.
//	integration - real dependency: Docker Hub registry API (hub.docker.com).
//
// Polarity:
//
//	contract tests FAIL on the broken (pre-PR) chart and PASS on the fixed chart.
//	canary   tests PASS on current behavior and must FLIP to fail once the
//	         maintainers harden the release tooling (see F1).
package pr6278charttag

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

const (
	// repoRoot is relative to this package dir (test/verify/pr6278charttag).
	repoRoot      = "../../.."
	chartRelPath  = "charts/fluid/fluid"
	valuesRelPath = chartRelPath + "/values.yaml"
	chartYamlPath = chartRelPath + "/Chart.yaml"
	makefilePath  = "Makefile"

	// commitUnderTest is the master commit the v1.1.0-* images were built from.
	// Full sha: dcb76167b0143a239addc6bed83ed50b45267631.
	commitUnderTest = "dcb76167"

	hubAPI = "https://hub.docker.com/v2/repositories/%s/tags/%s"
)

var (
	defaultVersionRe = regexp.MustCompile(`^version:\s*&defaultVersion\s+(\S+)\s*$`)
	appVersionRe     = regexp.MustCompile(`^appVersion:\s*(\S+)\s*$`)
	// imageRefRe finds image references anywhere in rendered manifests
	// (covers both `image:` fields and env `value:` entries).
	imageRefRe = regexp.MustCompile(`[a-z0-9.\-/:]*fluidcloudnative/[a-z0-9.\-]+:[a-zA-Z0-9][a-zA-Z0-9.\-_]*`)
)

func helmBinary(t *testing.T) string {
	t.Helper()
	bin, err := exec.LookPath("helm")
	if err != nil {
		t.Skip("helm not found on PATH; install helm v3 to run this harness")
	}
	return bin
}

// readDefaultTag extracts the `version: &defaultVersion <tag>` anchor from values.yaml.
func readDefaultTag(t *testing.T) string {
	t.Helper()
	f, err := os.Open(filepath.Join(repoRoot, valuesRelPath))
	if err != nil {
		t.Fatalf("open values.yaml: %v", err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if m := defaultVersionRe.FindStringSubmatch(sc.Text()); m != nil {
			return m[1]
		}
	}
	t.Fatalf("no `version: &defaultVersion` anchor found in %s", valuesRelPath)
	return ""
}

func readAppVersion(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot, chartYamlPath))
	if err != nil {
		t.Fatalf("open Chart.yaml: %v", err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		if m := appVersionRe.FindStringSubmatch(line); m != nil {
			return m[1]
		}
	}
	t.Fatalf("no appVersion found in %s", chartYamlPath)
	return ""
}

// renderChart runs `helm template` with default values and returns the output.
func renderChart(t *testing.T) string {
	t.Helper()
	helm := helmBinary(t)
	chart := filepath.Join(repoRoot, chartRelPath)
	out, err := exec.Command(helm, "template", "fluid", chart, "--namespace", "fluid-system").CombinedOutput()
	if err != nil {
		t.Fatalf("helm template failed: %v\n%s", err, out)
	}
	return string(out)
}

// defaultTaggedRefs returns the unique fluidcloudnative/* image references in the
// rendered chart whose tag equals the chart default tag.
func defaultTaggedRefs(t *testing.T, rendered, defaultTag string) []string {
	t.Helper()
	seen := map[string]bool{}
	var refs []string
	for _, ref := range imageRefRe.FindAllString(rendered, -1) {
		parts := strings.Split(ref, ":")
		if len(parts) != 2 || parts[1] != defaultTag {
			continue
		}
		if !seen[ref] {
			seen[ref] = true
			refs = append(refs, ref)
		}
	}
	if len(refs) == 0 {
		t.Fatalf("no fluidcloudnative images with default tag %q found in rendered chart", defaultTag)
	}
	return refs
}

// TestChartRendersWithDefaults is a durable invariant: the chart must render with
// default values and reference at least one default-tagged fluidcloudnative image.
// Polarity: contract.
func TestChartRendersWithDefaults(t *testing.T) {
	tag := readDefaultTag(t)
	rendered := renderChart(t)
	refs := defaultTaggedRefs(t, rendered, tag)
	t.Logf("default tag %q used by %d unique image refs", tag, len(refs))
	for _, r := range refs {
		t.Logf("  %s", r)
	}
}

// TestAppVersionMatchesDefaultTag pins the repo convention:
// Chart.yaml appVersion == default image tag without the leading "v".
// Polarity: contract (invariant, passes on base and head).
func TestAppVersionMatchesDefaultTag(t *testing.T) {
	tag := readDefaultTag(t)
	app := readAppVersion(t)
	if want := strings.TrimPrefix(tag, "v"); app != want {
		t.Errorf("appVersion %q does not match default tag %q (want %q)", app, tag, want)
	}
}

// TestDefaultTaggedImagesExistInRegistry is THE incident detector (F2):
// every image a fresh `helm install` would pull must exist in the registry.
// On the base chart (tag v1.1.0-dcb76167b, never pushed) this FAILS with 404s;
// on the PR head (v1.1.0-dcb76167) it PASSES.
// Polarity: contract. Layer: integration (real registry).
func TestDefaultTaggedImagesExistInRegistry(t *testing.T) {
	tag := readDefaultTag(t)
	rendered := renderChart(t)
	refs := defaultTaggedRefs(t, rendered, tag)

	client := &http.Client{Timeout: 15 * time.Second}
	// Distinguish "registry unreachable" (skip; harness cannot decide) from
	// "tag missing" (404; the bug this PR fixes).
	probe, err := client.Get("https://hub.docker.com/v2/repositories/fluidcloudnative")
	if err != nil {
		t.Skipf("hub.docker.com unreachable (%v); skipping registry contract", err)
	}
	probe.Body.Close()

	for _, ref := range refs {
		repo := strings.Split(ref, ":")[0] // e.g. fluidcloudnative/dataset-controller
		url := fmt.Sprintf(hubAPI, repo, tag)
		resp, err := client.Get(url)
		if err != nil {
			t.Errorf("GET %s: transport error: %v", url, err)
			continue
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("image %s NOT in registry: GET %s -> %d (fresh helm install would ImagePullBackOff)", ref, url, resp.StatusCode)
		} else {
			t.Logf("OK %s (200)", ref)
		}
	}
}

// TestMakefileUsesAutoAbbrevLength is the F1 CANARY. It documents the current
// (risky) behavior: the Makefile computes image tags with `git rev-parse --short`
// at AUTO length, which scales with repository object count and differs between
// build environments, while chart bumps record whatever a maintainer's local
// clone prints. When the maintainers pin an explicit length (or otherwise harden
// the flow) this test FLIPS to fail and must be inverted/promoted.
// Polarity: canary.
func TestMakefileUsesAutoAbbrevLength(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRoot, makefilePath))
	if err != nil {
		t.Fatalf("open Makefile: %v", err)
	}
	autoRe := regexp.MustCompile(`git rev-parse --short(\s|\)|$)`)
	pinnedRe := regexp.MustCompile(`git rev-parse --short=[0-9]+`)
	body := string(b)
	if pinnedRe.MatchString(body) {
		t.Fatalf("hardening detected: Makefile pins an explicit --short=<n> length; flip this canary (F1 fixed)")
	}
	if !autoRe.MatchString(body) {
		t.Skip("Makefile no longer uses `git rev-parse --short` for GIT_SHA; reassess F1 manually")
	}
	t.Log("canary: Makefile still uses auto-length `git rev-parse --short` (F1 risk present)")
}

// TestGitShortShaLengthIsNine records the premise mechanism (P0): on a full
// clone of this repository, `git rev-parse --short dcb76167` prints 9
// characters even though the 8-char prefix disambiguates uniquely, because
// git's auto abbreviation length has scaled to 9 with the repo object count.
// Informational evidence; skips on shallow/partial clones missing the object.
func TestGitShortShaLengthIsNine(t *testing.T) {
	out, err := exec.Command("git", "-C", repoRoot, "rev-parse", "--short", commitUnderTest).CombinedOutput()
	if err != nil {
		t.Skipf("commit prefix %s unavailable in this clone: %v (%s)", commitUnderTest, err, out)
	}
	got := strings.TrimSpace(string(out))
	t.Logf("git rev-parse --short %s = %s (len=%d)", commitUnderTest, got, len(got))
	if len(got) != 9 {
		t.Errorf("expected 9-char abbreviation, got %d (%s)", len(got), got)
	}
	// The 8-char prefix itself is unique; the 9th char comes from the
	// repo-wide auto-abbrev default, not a per-commit collision.
	dis, err := exec.Command("git", "-C", repoRoot, "rev-parse", "--disambiguate="+commitUnderTest).CombinedOutput()
	if err != nil {
		t.Fatalf("disambiguate failed: %v (%s)", err, dis)
	}
	if n := len(strings.Fields(strings.TrimSpace(string(dis)))); n != 1 {
		t.Errorf("expected 8-char prefix to be unique, found %d objects", n)
	}
}
