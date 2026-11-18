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

package fluid

// Reviewer harness (PR #6256, reviewer A — ROUND 3, head 64d570d).
//
// F9 (round-2 convergent nit, truncation facet): fluid.clusterRBACName used
// to apply `trunc 253` to the combined name, so an overlong
// rbac.clusterScopedNamePrefix silently collapsed distinct ClusterRoles onto
// one 253-character name (observed at da020a3: a 250-char prefix rendered
// 12 identically-named ClusterRoles). The head commit 64d570d ("fix(chart):
// reject overlong cluster RBAC prefixes") replaces the silent truncation
// with a render-time fail. These tests pin that contract:
//
//   F9-trunc  (contract, gating): an overlong prefix must FAIL the render
//              with an error naming rbac.clusterScopedNamePrefix and the
//              253-character DNS subdomain limit. RED on da020a3 (render
//              succeeded, names collided); GREEN on 64d570d.
//   F9-bndry  (contract, gating): a prefix sized so the longest combined
//              name is EXACTLY 253 renders fine; one character more must
//              fail. Guards against an off-by-one in the length guard.
//   F9-null   (CANARY, non-gating): rbac=null still breaks rendering with
//              the round-2 nil-pointer error (loud failure on pathological
//              input — the residual F9 facet the fix does not address).
//              PASSES while the ugly-but-loud error remains; if a future
//              change makes rbac=null render or fail with a clean
//              validation message, tighten or invert this canary.

import (
	"os/exec"
	"strings"
	"testing"
)

// renderExpectingFailure renders the chart and returns combined output plus
// the command error (nil error = render succeeded).
func renderExpectingFailure(t *testing.T, release, namespace string, setPairs ...string) (string, error) {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm binary not available; skipping render-based check")
	}
	args := []string{"template", release, ".", "--namespace", namespace}
	for _, p := range setPairs {
		args = append(args, "--set", p)
	}
	cmd := exec.Command("helm", args...)
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestClusterScopedRBACOverlongPrefixRejectedAtRender(t *testing.T) {
	prefix := strings.Repeat("p", 250)
	out, err := renderExpectingFailure(t, "fluid-ovl", "fluid-system", "rbac.clusterScopedNamePrefix="+prefix)
	if err == nil {
		t.Fatalf("helm template accepted an overlong prefix that used to silently truncate into name collisions (F9 regression):\n%s", out)
	}
	for _, want := range []string{"rbac.clusterScopedNamePrefix is too long", "253"} {
		if !strings.Contains(out, want) {
			t.Fatalf("overlong-prefix render failed without the expected message %q:\n%s", want, out)
		}
	}
}

func TestClusterScopedRBACPrefixBoundaryAtDNSLimit(t *testing.T) {
	// Discover the longest cluster-RBAC name the chart renders for a short
	// prefix, then size the prefix so the longest combined name is exactly 253.
	const probe = "zz"
	manifest := renderChartValues(t, "fluid-bnd", "fluid-system", "rbac.clusterScopedNamePrefix="+probe)
	maxLen := 0
	for _, doc := range docSplitRe.Split(manifest, -1) {
		km := kindRe.FindStringSubmatch(doc)
		if km == nil || (km[1] != "ClusterRole" && km[1] != "ClusterRoleBinding") {
			continue
		}
		nm := nameRe.FindStringSubmatch(doc)
		if nm == nil {
			continue
		}
		if len(nm[1]) > maxLen {
			maxLen = len(nm[1])
		}
	}
	if maxLen == 0 {
		t.Fatalf("no cluster RBAC rendered; cannot compute the boundary prefix")
	}
	suffixLen := maxLen - len(probe) - 1 // minus "zz-"
	boundary := 253 - 1 - suffixLen      // longest combined name == exactly 253
	// At exactly 253 the render must succeed.
	renderChartValues(t, "fluid-bnd", "fluid-system", "rbac.clusterScopedNamePrefix="+strings.Repeat("b", boundary))
	// One character more must fail.
	_, err := renderExpectingFailure(t, "fluid-bnd", "fluid-system", "rbac.clusterScopedNamePrefix="+strings.Repeat("b", boundary+1))
	if err == nil {
		t.Fatalf("render accepted a combined RBAC name of 254 characters (off-by-one in the length guard)")
	}
}

func TestClusterScopedRBACNullValueStillFailsLoudly(t *testing.T) {
	out, err := renderExpectingFailure(t, "fluid-null", "fluid-system", "rbac=null")
	if err == nil {
		t.Errorf("rbac=null now renders successfully — the round-2 nil-pointer facet is GONE; invert this canary into a contract test for a clean validation error:\n%s", out)
	}
}
