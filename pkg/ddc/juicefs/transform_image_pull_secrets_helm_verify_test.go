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

// Integration-layer verification for PR #6245: drives the helm-render and
// CRD-schema scripts so re-verify.sh can consume them as a gotest layer.
// Additive only; skips (harness-not-runnable) when helm/python3 are missing.

package juicefs

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func pr6245RepoRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("git rev-parse --show-toplevel: %v", err)
	}
	return string(out[:len(out)-1])
}

// Contract: rendered worker/fuse pod specs carry the right imagePullSecrets in
// all value scenarios, and legacy values render identically on the base chart.
func TestVerifyPr6245HelmRenderScenarios(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm not installed; integration layer not runnable here")
	}
	root := pr6245RepoRoot(t)
	script := filepath.Join(root, "docs/verification/juicefs-imagepullsecrets-codex/scripts/helm_render_check.sh")
	cmd := exec.Command("bash", script)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("helm_render_check.sh failed: %v\n%s", err, out)
	}
}

// Contract: the regenerated CRD exposes the new fields and validates both new
// and legacy JuiceFSRuntime CRs.
func TestVerifyPr6245CRDSchemaCheck(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not installed; integration layer not runnable here")
	}
	root := pr6245RepoRoot(t)
	script := filepath.Join(root, "docs/verification/juicefs-imagepullsecrets-codex/scripts/crd_schema_check.py")
	cmd := exec.Command("python3", script, root)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("crd_schema_check.py failed: %v\n%s", err, out)
	}
}

// Contract: the three new JuiceFS imagePullSecrets rows in docs/{en,zh}/dev/
// api_doc.md sit inside the field tables of their own type sections
// (JuiceFSCompTemplateSpec, JuiceFSFuseSpec, JuiceFSRuntimeSpec) and not under
// the root JuiceFSRuntime type or Vineyard's MasterSpec. Guards finding
// F-apidoc-rows-misplaced (revision 6711374b..3271f28d added them shifted one
// section down; the pinned crd-ref-docs v0.3.0 places them correctly).
func TestVerifyPr6245APIDocPlacement(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not installed; integration layer not runnable here")
	}
	root := pr6245RepoRoot(t)
	script := filepath.Join(root, "docs/verification/juicefs-imagepullsecrets-codex/scripts/apidoc_placement_check.py")
	cmd := exec.Command("python3", script, root)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("apidoc_placement_check.py failed: %v\n%s", err, out)
	}
}
