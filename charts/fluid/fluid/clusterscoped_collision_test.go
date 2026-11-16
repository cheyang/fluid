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

// Reviewer harness (PR #6256, reviewer A). These tests render the chart with a
// real `helm template` for two releases and check that no CLUSTER-SCOPED object
// (kind, name) collides across the two releases. Helm refuses to install a
// resource that exists and is owned by another release ("exists and cannot be
// imported into the current release: invalid ownership metadata"), so any
// collision reproduced here is exactly the failure reported in issue #6134.
//
// Polarity: all tests below are CONTRACT tests — they assert the intended
// behavior (two releases can coexist) and are RED while the bug is present.
// They skip when no `helm` binary is available.
//
// Test map (see docs/verification/multi-release-rbac/):
//   F1: TestClusterScopedObjectsDisjointAcrossReleases
//       (distinct release names, default values — CSIDriver/MWC still collide on PR head)
//   F2: TestClusterScopedObjectsDisjointForSameReleaseNameInTwoNamespaces
//       (same release name in two namespaces — RBAC names still collide on PR head)
//   G1: TestClusterScopedRBACReleaseScopedAcrossReleases
//       (control/guard: the RBAC renaming itself works; green on PR head, red on base)

import (
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var clusterScopedKinds = map[string]bool{
	"ClusterRole":                  true,
	"ClusterRoleBinding":           true,
	"CustomResourceDefinition":     true,
	"CSIDriver":                    true,
	"MutatingWebhookConfiguration": true,
	"ValidatingWebhookConfiguration": true,
	"PriorityClass":                true,
	"StorageClass":                 true,
	"APIService":                   true,
	"IngressClass":                 true,
	"RuntimeClass":                 true,
	"Namespace":                    true,
}

type clusterObject struct {
	Kind string
	Name string
}

func (c clusterObject) String() string { return c.Kind + "/" + c.Name }

var (
	docSplitRe = regexp.MustCompile(`(?m)^---\s*$`)
	kindRe     = regexp.MustCompile(`(?m)^kind:\s*(\S+)\s*$`)
	nameRe     = regexp.MustCompile(`(?m)^  name:\s*(\S+)\s*$`)
	roleRefRe  = regexp.MustCompile(`(?m)^roleRef:\n(?:  .*\n)*  name:\s*(\S+)\s*$`)
)

// renderChart runs `helm template <release> . --namespace <ns>` in the chart
// directory (the test package's working dir) and returns the rendered manifest.
func renderChart(t *testing.T, release, namespace string) string {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm binary not available; skipping render-based check")
	}
	cmd := exec.Command("helm", "template", release, ".", "--namespace", namespace)
	cmd.Dir = "." // package dir == charts/fluid/fluid == the chart root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helm template %s failed: %v\n%s", release, err, out)
	}
	return string(out)
}

// clusterScopedObjects extracts all cluster-scoped (kind, name) pairs from a
// rendered manifest. Helm's crds/ directory is not rendered by `helm template`
// and is not release-managed on install, so it cannot collide.
func clusterScopedObjects(t *testing.T, manifest string) []clusterObject {
	t.Helper()
	var objs []clusterObject
	for _, doc := range docSplitRe.Split(manifest, -1) {
		km := kindRe.FindStringSubmatch(doc)
		if km == nil || !clusterScopedKinds[km[1]] {
			continue
		}
		nm := nameRe.FindStringSubmatch(doc)
		if nm == nil {
			t.Fatalf("cluster-scoped %s document has no 2-space-indented metadata name:\n%s", km[1], doc)
		}
		objs = append(objs, clusterObject{Kind: km[1], Name: nm[1]})
	}
	if len(objs) == 0 {
		t.Fatalf("no cluster-scoped objects rendered; the render is suspicious:\n%.500s", manifest)
	}
	return objs
}

func toSet(objs []clusterObject) map[clusterObject]bool {
	s := make(map[clusterObject]bool, len(objs))
	for _, o := range objs {
		s[o] = true
	}
	return s
}

func sortedKeys(s map[clusterObject]bool) []string {
	var out []string
	for o := range s {
		out = append(out, o.String())
	}
	sort.Strings(out)
	return out
}

// F1: with DEFAULT values and two DIFFERENT release names (the issue #6134
// scenario), every cluster-scoped object of release A must be disjoint from
// release B's. Any shared (kind, name) makes `helm install` of the second
// release fail with the same ownership error the issue reports.
func TestClusterScopedObjectsDisjointAcrossReleases(t *testing.T) {
	a := clusterScopedObjects(t, renderChart(t, "team-a", "team-a-ns"))
	b := clusterScopedObjects(t, renderChart(t, "team-b", "team-b-ns"))
	sa, sb := toSet(a), toSet(b)
	var collisions []string
	for o := range sa {
		if sb[o] {
			collisions = append(collisions, o.String())
		}
	}
	sort.Strings(collisions)
	if len(collisions) > 0 {
		t.Fatalf("cluster-scoped objects collide across two Fluid releases (issue #6134 symptom persists):\n  %s\n"+
			"helm install of the second release will fail with 'exists and cannot be imported into the current release' on these objects",
			strings.Join(collisions, "\n  "))
	}
}

// F2: the SAME release name in two different namespaces (Helm 3 permits this;
// `helm install fluid -n team-a` + `helm install fluid -n team-b` is the most
// natural multi-team pattern for a chart literally named `fluid`) must also
// render disjoint cluster-scoped objects.
func TestClusterScopedObjectsDisjointForSameReleaseNameInTwoNamespaces(t *testing.T) {
	a := clusterScopedObjects(t, renderChart(t, "fluid", "ns-a"))
	b := clusterScopedObjects(t, renderChart(t, "fluid", "ns-b"))
	sa, sb := toSet(a), toSet(b)
	var collisions []string
	for o := range sa {
		if sb[o] {
			collisions = append(collisions, o.String())
		}
	}
	sort.Strings(collisions)
	if len(collisions) > 0 {
		t.Fatalf("cluster-scoped objects collide when the same release name is installed in two namespaces:\n  %s\n"+
			"fluid.fullname does not include the namespace, so release-scoped names are identical",
			strings.Join(collisions, "\n  "))
	}
}

// G1 (control/guard): for two different release names, every ClusterRole and
// ClusterRoleBinding name must be release-scoped, each ClusterRoleBinding's
// roleRef must resolve to a ClusterRole of the same release, and the RBAC
// objects must be disjoint across the releases. Green on the PR head, RED on
// the merge base — this is the differential proof that the RBAC part of the
// PR does fix what it claims.
func TestClusterScopedRBACReleaseScopedAcrossReleases(t *testing.T) {
	releases := []struct {
		name, ns string
	}{
		{"team-a", "team-a-ns"},
		{"team-b", "team-b-ns"},
	}
	var rbacSets []map[clusterObject]bool
	for _, r := range releases {
		manifest := renderChart(t, r.name, r.ns)
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
				if !strings.Contains(nm[1], r.name) {
					t.Errorf("release %s: ClusterRole %q is not release-scoped", r.name, nm[1])
				}
				rbac[clusterObject{"ClusterRole", nm[1]}] = true
				roles[nm[1]] = true
			case "ClusterRoleBinding":
				nm := nameRe.FindStringSubmatch(doc)
				if nm == nil {
					t.Fatalf("ClusterRoleBinding without metadata name in release %s", r.name)
				}
				if !strings.Contains(nm[1], r.name) {
					t.Errorf("release %s: ClusterRoleBinding %q is not release-scoped", r.name, nm[1])
				}
				rr := roleRefRe.FindStringSubmatch(doc)
				if rr == nil {
					t.Fatalf("ClusterRoleBinding %q has no roleRef.name in release %s", nm[1], r.name)
				}
				if !strings.Contains(rr[1], r.name) {
					t.Errorf("release %s: ClusterRoleBinding %q references non-release-scoped ClusterRole %q", r.name, nm[1], rr[1])
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
			t.Errorf("RBAC object %s collides across releases team-a and team-b", o)
		}
	}
}
