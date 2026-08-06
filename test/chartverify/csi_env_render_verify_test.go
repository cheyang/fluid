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

// Verification harness for https://github.com/fluid-cloudnative/fluid/pull/6152
//
// LAYER: integration / render (L2) — needs the real `helm` binary and the real chart.
// POLARITY: contract — RED on the pre-fix chart, GREEN on the PR head.
//
// These are the tests that discriminate the fix, because the defect is in the chart
// template rather than in Go. They render charts/fluid/fluid with helm and inspect the
// env block of the csi-plugin container.
//
// Rendering is hermetic: the chart is copied to a temp dir, templates that call helm's
// `lookup` (which would contact a live API server) are dropped, and KUBECONFIG is pinned
// to /dev/null. Without this the render result depends on the reviewer's kubeconfig.

package chartverify

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// goSideEnvName is the name pkg/csi/recover/recover.go reads. Duplicated as a literal on
// purpose: the point of these tests is to catch the chart and the Go code drifting apart,
// so importing the constant would hide half of the drift.
const goSideEnvName = "RECOVER_WARNING_THRESHOLD"

// misspelledEnvName is what the chart emitted before PR #6152.
const misspelledEnvName = "REVOCER_WARNING_THRESHOLD"

const chartRelPath = "charts/fluid/fluid"

// repoRoot walks up from the test's working directory until it finds go.mod.
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
			t.Fatalf("could not locate go.mod above %s", dir)
		}
		dir = parent
	}
}

func requireHelm(t *testing.T) string {
	t.Helper()
	helm, err := exec.LookPath("helm")
	if err != nil {
		t.Skip("helm not on PATH; the render layer needs the real helm binary")
	}
	return helm
}

// hermeticChartCopy copies the chart under review into a temp dir and removes templates
// that call `lookup`, so rendering never depends on a reachable cluster.
func hermeticChartCopy(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	dst := filepath.Join(t.TempDir(), "fluid")

	if out, err := exec.Command("cp", "-R", filepath.Join(root, chartRelPath), dst).CombinedOutput(); err != nil {
		t.Fatalf("copy chart: %v\n%s", err, out)
	}

	tmplDir := filepath.Join(dst, "templates")
	err := filepath.Walk(tmplDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		b, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(b), "lookup ") {
			t.Logf("hermetic render: dropping %s (calls helm lookup)",
				strings.TrimPrefix(path, dst+string(os.PathSeparator)))
			return os.Remove(path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("prune lookup templates: %v", err)
	}
	return dst
}

// renderChart runs `helm template` against a chart dir with no cluster access.
func renderChart(t *testing.T, chartDir string, sets ...string) string {
	t.Helper()
	helm := requireHelm(t)

	args := []string{"template", "fluid-verify", chartDir}
	for _, s := range sets {
		args = append(args, "--set", s)
	}
	cmd := exec.Command(helm, args...)
	cmd.Env = append(os.Environ(), "KUBECONFIG=/dev/null")

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helm template failed: %v\n%s", err, out)
	}
	return string(out)
}

// csiPluginEnv scrapes the env name/value pairs of the csi-plugin container ("plugins")
// out of rendered chart YAML. A dependency-free scraper is used rather than a YAML
// library so the harness stays additive and adds no module requirements.
func csiPluginEnv(t *testing.T, rendered string) map[string]string {
	t.Helper()
	env := map[string]string{}

	inPlugins := false
	inEnv := false
	var name string

	for _, raw := range strings.Split(rendered, "\n") {
		trimmed := strings.TrimSpace(strings.TrimRight(raw, " \t\r"))

		if trimmed == "- name: plugins" {
			inPlugins, inEnv = true, false
			continue
		}
		if !inPlugins {
			continue
		}
		if trimmed == "env:" {
			inEnv = true
			continue
		}
		if !inEnv {
			// Another container starts before we reached this one's env block.
			if strings.HasPrefix(trimmed, "- name: ") {
				inPlugins = false
			}
			continue
		}
		// The env list ends at the container's next sibling key (e.g. volumeMounts:).
		if strings.HasSuffix(trimmed, ":") && !strings.HasPrefix(trimmed, "- ") &&
			!strings.HasPrefix(trimmed, "valueFrom") && !strings.HasPrefix(trimmed, "fieldRef") {
			inEnv, inPlugins = false, false
			continue
		}
		switch {
		case strings.HasPrefix(trimmed, "- name: "):
			name = strings.TrimSpace(strings.TrimPrefix(trimmed, "- name: "))
			env[name] = "" // valueFrom entries keep an empty value
		case strings.HasPrefix(trimmed, "value: ") && name != "":
			env[name] = strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "value: ")), `"`)
		}
	}
	return env
}

// TestScraperSanity guards the scraper itself, so a failure elsewhere in this file means
// the chart changed rather than the parser silently returning nothing.
func TestScraperSanity(t *testing.T) {
	env := csiPluginEnv(t, renderChart(t, hermeticChartCopy(t), "csi.recoverWarningThreshold=100"))

	for _, must := range []string{"NODE_ID", "NODE_IP", "KUBELET_ROOTDIR", "CSI_ENDPOINT", "NODEPUBLISH_METHOD"} {
		if _, ok := env[must]; !ok {
			b, _ := json.Marshal(env)
			t.Fatalf("scraper did not find baseline env var %s; scraped: %s", must, b)
		}
	}
	t.Logf("scraped %d env vars from the csi-plugin container", len(env))
}

// TestChartEmitsRecoverWarningThresholdUnderTheNameGoReads is the primary contract test
// for PR #6152. On the pre-fix chart it fails, because the chart emits the misspelled name.
func TestChartEmitsRecoverWarningThresholdUnderTheNameGoReads(t *testing.T) {
	env := csiPluginEnv(t, renderChart(t, hermeticChartCopy(t), "csi.recoverWarningThreshold=100"))

	got, ok := env[goSideEnvName]
	if !ok {
		names := make([]string, 0, len(env))
		for n := range env {
			names = append(names, n)
		}
		t.Fatalf("csi-plugin container has no env var named %s, so a configured "+
			"csi.recoverWarningThreshold is silently ignored. rendered env names: %v",
			goSideEnvName, names)
	}
	if got != "100" {
		t.Fatalf("expected %s=100, got %q", goSideEnvName, got)
	}
}

// TestChartDoesNotEmitMisspelledEnvName is the direct negative of the fix.
func TestChartDoesNotEmitMisspelledEnvName(t *testing.T) {
	env := csiPluginEnv(t, renderChart(t, hermeticChartCopy(t), "csi.recoverWarningThreshold=100"))

	if _, present := env[misspelledEnvName]; present {
		t.Fatalf("chart still emits %s, which no Go constant reads", misspelledEnvName)
	}
}

// TestCsiPluginEnvNamesAreAllConsumed is the regression guard for the coverage gap that
// let the typo ship: every env name the chart sets on the csi-plugin container must be
// read somewhere in the tree. Pre-fix this fails on REVOCER_WARNING_THRESHOLD.
func TestCsiPluginEnvNamesAreAllConsumed(t *testing.T) {
	root := repoRoot(t)
	env := csiPluginEnv(t, renderChart(t, hermeticChartCopy(t), "csi.recoverWarningThreshold=100"))
	if len(env) == 0 {
		t.Fatal("scraped no env vars from the csi-plugin container; the harness scraper is stale")
	}

	// Declared and dereferenced via $(VAR) inside the same template's args, so no
	// application code needs to mention them.
	substitutedInArgs := map[string]bool{"NODE_ID": true, "CSI_ENDPOINT": true}

	var unconsumed []string
	for name := range env {
		if substitutedInArgs[name] || consumedInTree(t, root, name) {
			continue
		}
		unconsumed = append(unconsumed, name)
	}

	if len(unconsumed) > 0 {
		t.Fatalf("the chart sets env vars on the csi-plugin container that nothing reads: %v\n"+
			"each of these is a silently-ignored setting", unconsumed)
	}
	t.Logf("all %d rendered csi-plugin env names are consumed", len(env))
}

// consumedInTree reports whether name appears in Go sources or shell scripts that could
// actually read it, excluding vendor/ and this harness.
func consumedInTree(t *testing.T, root, name string) bool {
	t.Helper()
	out, err := exec.Command("grep", "-rl",
		"--include=*.go", "--include=*.sh",
		"--exclude=*_verify_test.go",
		name,
		filepath.Join(root, "pkg"),
		filepath.Join(root, "cmd"),
		filepath.Join(root, "csi"),
	).Output()
	if err != nil {
		// grep exits 1 with no match; that is a legitimate "not consumed".
		return false
	}
	for _, f := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if f == "" || strings.Contains(f, string(os.PathSeparator)+"vendor"+string(os.PathSeparator)) {
			continue
		}
		return true
	}
	return false
}

// TestChartToolchainIsSilentAboutUnreadEnvNames documents why CI never caught this.
// It reintroduces the misspelling into the hermetic copy and shows that rendering still
// succeeds and happily emits a name nothing reads — no error, no warning. The project-check
// workflow only runs `helm lint`, which is strictly weaker than a successful render, so it
// could not have flagged this either.
// POLARITY: evidence — green regardless of the fix; it is a fact about the chart toolchain.
func TestChartToolchainIsSilentAboutUnreadEnvNames(t *testing.T) {
	chartDir := hermeticChartCopy(t)
	dsPath := filepath.Join(chartDir, "templates", "csi", "daemonset.yaml")

	b, err := os.ReadFile(dsPath)
	if err != nil {
		t.Fatalf("read daemonset template: %v", err)
	}
	broken := strings.Replace(string(b), goSideEnvName, misspelledEnvName, 1)
	if broken == string(b) {
		t.Skipf("chart does not contain %s at this ref; nothing to un-fix", goSideEnvName)
	}
	if err := os.WriteFile(dsPath, []byte(broken), 0o644); err != nil {
		t.Fatalf("write un-fixed template: %v", err)
	}

	// renderChart fails the test on a non-zero exit, so reaching the assertion below
	// already proves the toolchain raised no error.
	env := csiPluginEnv(t, renderChart(t, chartDir, "csi.recoverWarningThreshold=100"))

	if _, ok := env[misspelledEnvName]; !ok {
		t.Fatalf("expected the un-fixed chart to emit %s; scraper or template layout changed",
			misspelledEnvName)
	}
	if _, ok := env[goSideEnvName]; ok {
		t.Fatalf("un-fixed chart unexpectedly still emits %s", goSideEnvName)
	}
	t.Logf("helm rendered the misspelled chart with no error or warning: the toolchain "+
		"cannot detect that %s is read by nobody", misspelledEnvName)
}
