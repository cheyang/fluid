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

// Verification harness for review of PR #6159 — the postStart sidecar sink that the
// PR does not cover. Reviewer-private: additive test code only.
//
// Both tests execute a real /bin/bash against the code's own output, so a red result
// is an observed side effect rather than a claim about shell semantics.

package poststart

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/fluid-cloudnative/fluid/pkg/utils"
)

func requireLinuxShell(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skipf("harness expects a linux /bin/bash and /proc/self/mountinfo, got %s", runtime.GOOS)
	}
}

// TestVerifyPR6159_F1_PostStartSubPathCannotInjectShellCommands covers the sink the PR
// left untouched: mutator_default.go:337 hands FuseMountInfo.SubPath to
// GetPostStartCommand, which interpolates it into a `bash -c` string with fmt.Sprintf
// (check_fuse_default.go:132). utils.CleanSubPath collapses ".." but preserves every
// shell metacharacter, so it does not make the value safe for this sink.
//
// Dataset spec.mounts[].mountPoint has no character validation (only MinLength=5), and
// there is no validating webhook, so the payload below is expressible by any tenant who
// can create a Dataset.
//
// The test executes the exact argv the mutator would put in the pod's postStart hook.
// /check-mount.sh does not exist here, so the primary command fails — and the injected
// command runs anyway, which is the point.
//
// POLARITY: contract test asserting the subPath cannot introduce a command.
//
//	against PR HEAD: FAILS => injection reachable, sink not covered by the PR
//	after a fix:     PASSES
func TestVerifyPR6159_F1_PostStartSubPathCannotInjectShellCommands(t *testing.T) {
	requireLinuxShell(t)

	marker := filepath.Join(t.TempDir(), "pr6159-injected")

	// Travels as `dataset://<ns>/<name>/x; touch <marker>`.
	rawSubPath := "x; touch " + marker

	// Exactly the sanitizing the PR applies on the way to this sink
	// (runtime_helper.go:110 -> FuseMountInfo.SubPath -> mutator_default.go:337).
	sanitized := utils.CleanSubPath(rawSubPath)
	t.Logf("raw subPath      : %q", rawSubPath)
	t.Logf("after CleanSubPath: %q", sanitized)

	gen := NewDefaultPostStartScriptGenerator()
	handler := gen.GetPostStartCommand("/runtime-mnt/alluxio/default/my-dataset", "alluxio", sanitized)

	argv := handler.Exec.Command
	t.Logf("postStart argv   : %#v", argv)

	cmd := exec.Command(argv[0], argv[1:]...)
	out, runErr := cmd.CombinedOutput()
	t.Logf("bash output      : %s (err=%v)", strings.TrimSpace(string(out)), runErr)

	if _, err := os.Stat(marker); err == nil {
		t.Errorf("INJECTION: the postStart hook executed an attacker-supplied command; %s was created", marker)
	}
}

// TestVerifyPR6159_F2_RenderedScriptEnforcesSubPathExistence runs the *shipped* sidecar
// script (contentPrivilegedSidecar, rendered through `replacer` exactly as
// BuildConfigMap does) and checks that its subPath existence gate cannot be defeated.
//
// The gate is check_fuse_default.go:90:
//
//	while [ ! -e  $ConditionPathIsMountPoint/*/$SubPath ]
//
// unquoted, and containing a literal glob. A subPath with a space makes `[` receive too
// many operands and return 2, so the `while` condition is false, the loop is skipped and
// the script exits 0 — reporting the sub path as present without ever checking it.
//
// Contrast csi/shell/check_mount.sh:48, the CSI-side copy of the same check, which was
// already hardened to `test -e "$ConditionPathIsMountPoint/$SubPath"`. The two copies of
// this check have diverged.
//
// POLARITY: contract test asserting a missing subPath is still reported missing (exit 2).
//
//	against PR HEAD: FAILS => the gate is bypassable, unaddressed by the PR
//	after a fix:     PASSES
func TestVerifyPR6159_F2_RenderedScriptEnforcesSubPathExistence(t *testing.T) {
	requireLinuxShell(t)

	scriptPath := renderShippedSidecarScript(t)
	cond, mountType := liveMountinfoPair(t)

	// Control: an ordinary missing subPath must be reported missing (exit code 2).
	// Costs ~30s: the script polls once a second up to subpath_check_limit.
	t.Run("control_plain_missing_subpath_is_detected", func(t *testing.T) {
		rc, out := runScript(t, scriptPath, cond, mountType, "pr6159-definitely-missing")
		t.Logf("exit=%d out=%s", rc, out)
		if rc != 2 {
			t.Fatalf("harness does not bite: expected exit 2 for a missing subPath, got %d (out=%s)", rc, out)
		}
	})

	// The claim: a subPath containing a space defeats the same gate.
	t.Run("crafted_missing_subpath_with_space", func(t *testing.T) {
		crafted := "pr6159 definitely missing"
		if got := utils.CleanSubPath(crafted); got != crafted {
			t.Fatalf("precondition changed: CleanSubPath(%q) = %q", crafted, got)
		}

		rc, out := runScript(t, scriptPath, cond, mountType, crafted)
		t.Logf("exit=%d out=%s", rc, out)

		if rc == 0 {
			t.Errorf("GATE BYPASSED: subPath %q does not exist, yet the postStart script exited 0 (out=%s)",
				crafted, out)
		}
	})
}

// renderShippedSidecarScript writes the shipped script to disk exactly as
// BuildConfigMap renders it, with one documented exception: the standalone
// `redirect_output_with_retry` invocation is neutralized. That function execs stdout
// onto /proc/1/fd/1, which only exists meaningfully inside a pod, and under `set -e` its
// failure would abort the script before the subPath logic under test is reached. Every
// line of the mount and subPath checks is left byte-identical.
func renderShippedSidecarScript(t *testing.T) string {
	t.Helper()

	rendered := replacer.Replace(contentPrivilegedSidecar)

	lines := strings.Split(rendered, "\n")
	neutralized := 0
	for i, line := range lines {
		if strings.TrimSpace(line) == "redirect_output_with_retry" {
			lines[i] = ": # neutralized by verification harness (pod-only stdout redirect)"
			neutralized++
		}
	}
	if neutralized != 1 {
		t.Fatalf("expected exactly 1 redirect_output_with_retry invocation to neutralize, found %d", neutralized)
	}

	// Guard: the line under test must still be the unquoted, globbed original.
	if !strings.Contains(rendered, "while [ ! -e  $ConditionPathIsMountPoint/*/$SubPath ]") {
		t.Fatalf("the subPath check in contentPrivilegedSidecar changed shape; update this harness")
	}

	path := filepath.Join(t.TempDir(), "check-mount.sh")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

// liveMountinfoPair returns a (mount point, fstype) pair taken from the same
// /proc/self/mountinfo line, so the script's first gate — which greps mountinfo for both
// — passes immediately and execution reaches the subPath check.
func liveMountinfoPair(t *testing.T) (string, string) {
	t.Helper()

	raw, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		t.Skipf("cannot read /proc/self/mountinfo: %v", err)
	}

	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}
		mountPoint := fields[4]
		sep := -1
		for i, f := range fields {
			if f == "-" {
				sep = i
				break
			}
		}
		if sep < 0 || sep+1 >= len(fields) {
			continue
		}
		fsType := fields[sep+1]
		// Keep it simple and glob-free so only the subPath varies between subtests.
		if mountPoint == "/" || strings.ContainsAny(mountPoint+fsType, "*?[ ") {
			continue
		}
		t.Logf("using mountinfo pair: mountPoint=%s fsType=%s", mountPoint, fsType)
		return mountPoint, fsType
	}
	t.Skip("no usable /proc/self/mountinfo line found")
	return "", ""
}

func runScript(t *testing.T, scriptPath, cond, mountType, subPath string) (int, string) {
	t.Helper()

	cmd := exec.Command("/bin/bash", scriptPath, cond, mountType, subPath)
	out, err := cmd.CombinedOutput()

	rc := 0
	if err != nil {
		var ee *exec.ExitError
		if ok := asExitError(err, &ee); ok {
			rc = ee.ExitCode()
		} else {
			t.Fatalf("failed to run the rendered script: %v", err)
		}
	}
	return rc, strings.TrimSpace(string(out))
}

func asExitError(err error, target **exec.ExitError) bool {
	if ee, ok := err.(*exec.ExitError); ok {
		*target = ee
		return true
	}
	return false
}
