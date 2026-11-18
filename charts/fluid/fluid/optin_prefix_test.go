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

// Reviewer harness (PR #6256, reviewer A — ROUND 2, head da020a3).
//
// The PR was re-scoped between rounds: af73f5d made cluster RBAC names
// release-scoped by default (fullname-based); da020a3 ("fix(chart): make RBAC
// prefix opt-in") reverted the default to the historical fixed names and added
// an opt-in `rbac.clusterScopedNamePrefix`. The round-2 contracts are:
//
//   H-COMPAT: with DEFAULT values the head chart renders byte-identical
//             output to the merge base f432bdc (zero behavior change by
//             default; upgrade of a default install renames nothing).
//   G1-r2   : with the opt-in prefix set, every ClusterRole/ClusterRoleBinding
//             is prefixed, each roleRef resolves within the same release, and
//             two releases with distinct prefixes render disjoint RBAC.
//   F1-r2   (canary, non-gating): even WITH distinct prefixes, a second
//             release still shares cluster-scoped singletons (CSIDriver,
//             MutatingWebhookConfiguration) — the residual #6134 limitation
//             the PR now explicitly declares out of scope. This canary PASSES
//             while the limitation is present; if a future PR shares/skips
//             the singletons it flips to fail and must be inverted.
//
// F1/F2 round-1 contract tests in clusterscoped_collision_test.go stay in
// place: they remain red because the #6134 contract (any two releases
// installable) is still unmet — by design, per the re-scoped PR body.
//
// Test names keep the TestClusterScoped* prefix so the manifest's layer
// command (`go test ./charts/fluid/fluid/ -run 'TestClusterScoped'`) picks
// them all up, including the author's
// TestClusterScopedRBACNamesPreserveDefaultsAndSupportOptInPrefix.

import (
	"bytes"
	"os"
	"os/exec"
	"sort"
	"strings"
	"testing"
)

// mergeBase of PR #6256; used to materialize the base chart for the
// default-render compatibility contract.
const mergeBaseSHA = "f432bdc87ac35941763b74e8e87683c7a84de5d9"

// renderChartValues renders the chart at the test's working directory (the PR
// head chart) with optional --set pairs.
func renderChartValues(t *testing.T, release, namespace string, setPairs ...string) string {
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
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		t.Fatalf("helm template %s failed: %v\n%s", release, err, errOut.String())
	}
	return out.String()
}

// renderMergeBaseChart materializes charts/fluid/fluid at the merge-base
// commit via `git archive` into a temp dir and renders it with the same
// release/namespace. Skips when git or the base commit is unavailable.
func renderMergeBaseChart(t *testing.T, release, namespace string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available; skipping base-render compatibility check")
	}
	if _, err := exec.Command("git", "rev-parse", "--verify", "--quiet", mergeBaseSHA+"^{commit}").Output(); err != nil {
		t.Skip("merge-base commit not available locally; skipping base-render compatibility check")
	}
	tmp, err := os.MkdirTemp("", "fluid-base-chart-")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmp) })
	// `git archive <sha> ':(top)charts/fluid/fluid' | tar -x -C <tmp>` leaves
	// the chart root (Chart.yaml, crds/, templates/, ...) directly in tmp:
	// run from the package dir charts/fluid/fluid, git archive roots the
	// output at the cwd's subtree already, so no --strip-components is used.
	// The :(top) magic pathspec keeps the pathspec cwd-independent.
	archive := exec.Command("bash", "-c",
		"git archive "+mergeBaseSHA+" ':(top)charts/fluid/fluid' | tar -x -C "+tmp)
	archive.Dir = "."
	if out, err := archive.CombinedOutput(); err != nil {
		t.Fatalf("git archive of base chart failed: %v\n%s", err, out)
	}
	if _, err := os.Stat(tmp + "/Chart.yaml"); err != nil {
		t.Fatalf("base chart not materialized correctly at %s: %v", tmp, err)
	}
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm binary not available; skipping render-based check")
	}
	cmd := exec.Command("helm", "template", release, tmp, "--namespace", namespace)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		t.Fatalf("helm template (base chart) failed: %v\n%s", err, errOut.String())
	}
	return out.String()
}

// H-COMPAT: with default values the head chart must render EXACTLY what the
// merge base renders — byte-identical output for the same release/namespace.
// This is the compatibility contract behind "RBAC prefix opt-in": a default
// install (or upgrade) sees zero change, so the default upgrade path renames
// no ClusterRole/ClusterRoleBinding and cannot churn RBAC.
func TestClusterScopedDefaultRenderMatchesMergeBase(t *testing.T) {
	head := renderChartValues(t, "fluid-compat", "fluid-system")
	base := renderMergeBaseChart(t, "fluid-compat", "fluid-system")
	if head != base {
		diffAt := firstDiff(head, base)
		t.Fatalf("default-values render of the PR head differs from the merge base (first difference at byte %d):\n%s\n---\n%s",
			diffAt, excerptAround(head, diffAt), excerptAround(base, diffAt))
	}
}

func firstDiff(a, b string) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return i
		}
	}
	if len(a) != len(b) {
		return min(len(a), len(b))
	}
	return -1
}

func excerptAround(s string, at int) string {
	if at < 0 {
		return "<identical prefix>"
	}
	start := at - 200
	if start < 0 {
		start = 0
	}
	end := at + 200
	if end > len(s) {
		end = len(s)
	}
	return s[start:end]
}

// G1-r2: with the opt-in prefix set, all cluster RBAC is prefixed and
// resolvable, and two releases with distinct prefixes are disjoint. Green on
// the PR head, RED on the merge base (the base chart ignores the unknown
// value and renders fixed names).
func TestClusterScopedOptInPrefixDisjointAcrossReleases(t *testing.T) {
	type releaseSpec struct {
		name, ns, prefix string
	}
	releases := []releaseSpec{
		{"team-a", "team-a-ns", "tenant-a"},
		{"team-b", "team-b-ns", "tenant-b"},
	}
	var rbacSets []map[clusterObject]bool
	for _, r := range releases {
		manifest := renderChartValues(t, r.name, r.ns, "rbac.clusterScopedNamePrefix="+r.prefix)
		rbac := map[clusterObject]bool{}
		roles := map[string]bool{}
		for _, doc := range docSplitRe.Split(manifest, -1) {
			km := kindRe.FindStringSubmatch(doc)
			if km == nil {
				continue
			}
			switch km[1] {
			case "ClusterRole":
				nm := nameRe.FindStringSubmatch(doc)
				if nm == nil {
					t.Fatalf("ClusterRole without metadata name in release %s", r.name)
				}
				if !strings.HasPrefix(nm[1], r.prefix+"-") {
					t.Errorf("release %s (prefix %q): ClusterRole %q is not prefixed", r.name, r.prefix, nm[1])
				}
				rbac[clusterObject{"ClusterRole", nm[1]}] = true
				roles[nm[1]] = true
			case "ClusterRoleBinding":
				nm := nameRe.FindStringSubmatch(doc)
				if nm == nil {
					t.Fatalf("ClusterRoleBinding without metadata name in release %s", r.name)
				}
				if !strings.HasPrefix(nm[1], r.prefix+"-") {
					t.Errorf("release %s (prefix %q): ClusterRoleBinding %q is not prefixed", r.name, r.prefix, nm[1])
				}
				rr := roleRefRe.FindStringSubmatch(doc)
				if rr == nil {
					t.Fatalf("ClusterRoleBinding %q has no roleRef.name in release %s", nm[1], r.name)
				}
				if !strings.HasPrefix(rr[1], r.prefix+"-") {
					t.Errorf("release %s: ClusterRoleBinding %q references unprefixed ClusterRole %q", r.name, nm[1], rr[1])
				}
				if !roles[rr[1]] {
					t.Errorf("release %s: ClusterRoleBinding %q roleRef %q does not match any ClusterRole rendered for this release", r.name, nm[1], rr[1])
				}
				rbac[clusterObject{"ClusterRoleBinding", nm[1]}] = true
			}
		}
		if len(rbac) == 0 {
			t.Fatalf("release %s rendered no cluster-scoped RBAC objects", r.name)
		}
		rbacSets = append(rbacSets, rbac)
	}
	for o := range rbacSets[0] {
		if rbacSets[1][o] {
			t.Errorf("RBAC object %s collides across prefixed releases team-a/team-b", o)
		}
	}
	// The prefixed render must also be disjoint from the DEFAULT (historical)
	// names, so one default release and one prefixed release do not fight over
	// cluster RBAC.
	def := toSet(clusterScopedObjects(t, renderChartValues(t, "team-d", "team-d-ns")))
	for o := range rbacSets[0] {
		if def[o] {
			t.Errorf("prefixed RBAC object %s collides with the default (unprefixed) render", o)
		}
	}
}

// F1-r2 (CANARY, non-gating): with distinct opt-in prefixes the cluster RBAC
// collision is gone, but a second Fluid release still shares fixed-name
// cluster-scoped singletons. While this remains true, the canary PASSES — it
// documents the PR's declared scope boundary ("not complete multi-release
// support"). Helm refuses to install objects owned by another release, so a
// second release still cannot be installed even with the prefix. When this
// flips to FAIL, the singletons are finally shared/skipped — invert the
// assertion then.
func TestClusterScopedSharedSingletonsRemainAfterOptInPrefix(t *testing.T) {
	a := toSet(clusterScopedObjects(t, renderChartValues(t, "team-a", "team-a-ns")))
	b := toSet(clusterScopedObjects(t, renderChartValues(t, "team-b", "team-b-ns", "rbac.clusterScopedNamePrefix=tenant-b")))
	var rbacCollisions, otherCollisions []string
	for o := range a {
		if !b[o] {
			continue
		}
		if o.Kind == "ClusterRole" || o.Kind == "ClusterRoleBinding" {
			rbacCollisions = append(rbacCollisions, o.String())
		} else {
			otherCollisions = append(otherCollisions, o.String())
		}
	}
	sort.Strings(rbacCollisions)
	sort.Strings(otherCollisions)
	if len(rbacCollisions) > 0 {
		// The PR's own claim is broken: the opt-in prefix must remove RBAC
		// from the collision list.
		t.Fatalf("cluster RBAC still collides across a default and a prefixed release (the opt-in prefix does not do its job):\n  %s",
			strings.Join(rbacCollisions, "\n  "))
	}
	knownSingletons := map[string]bool{
		"CSIDriver/fuse.csi.fluid.io":                              true,
		"MutatingWebhookConfiguration/fluid-pod-admission-webhook": true,
	}
	var unknown []string
	for _, c := range otherCollisions {
		if !knownSingletons[c] {
			unknown = append(unknown, c)
		}
	}
	if len(unknown) > 0 {
		t.Errorf("new cluster-scoped collisions beyond the known shared singletons (scope shrank — update this canary):\n  %s",
			strings.Join(unknown, "\n  "))
	}
	if len(otherCollisions) == 0 {
		t.Errorf("no shared singletons left: the multi-release limitation this canary documents is GONE — invert this test into a contract test")
	}
}
