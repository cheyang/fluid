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

// Verification harness for review of PR #6159 — the containment guarantee stated in
// CleanSubPath's doc comment. Reviewer-private: additive test code only.

package utils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestVerifyPR6159_F4_CleanSubPathContainmentHoldsForSymlinkComponent tests the
// guarantee CleanSubPath's own doc comment makes:
//
//	"Anchoring the path at the root before cleaning makes any leading ".." elements
//	 collapse, so the result can never escape the directory it is later joined to."
//
// filepath.Join and filepath.Clean are purely lexical and documented as such: Clean
// "does not consider symbolic links". So when a component of the sub path is a symlink,
// the joined path still escapes once the kernel resolves it — which is what the two
// changed callers then do, since `mount --bind` and os.Symlink both act on the resolved
// path rather than the lexical one.
//
// POLARITY: contract test asserting the doc comment's guarantee.
//
//	against PR HEAD: FAILS => the guarantee as worded does not hold
//	after a fix (symlink-aware resolution, or reworded doc + rejection at the sinks): PASSES
func TestVerifyPR6159_F4_CleanSubPathContainmentHoldsForSymlinkComponent(t *testing.T) {
	root := t.TempDir()

	// Stand-in for the FUSE mount root that the sub path is joined to.
	fuseRoot := filepath.Join(root, "alluxio-fuse")
	if err := os.MkdirAll(fuseRoot, 0755); err != nil {
		t.Fatal(err)
	}

	// Host content outside the dataset.
	outside := filepath.Join(root, "hostonly")
	if err := os.MkdirAll(outside, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "marker"), []byte("HOST-ONLY-CONTENT"), 0644); err != nil {
		t.Fatal(err)
	}

	// A symlink inside the mounted filesystem. For a dataset this is ordinary content:
	// it comes from the UFS, not from Fluid.
	if err := os.Symlink(outside, filepath.Join(fuseRoot, "link")); err != nil {
		t.Fatal(err)
	}

	subPath := "link"
	cleaned := CleanSubPath(subPath)
	joined := filepath.Join(fuseRoot, cleaned)

	resolved, err := filepath.EvalSymlinks(joined)
	if err != nil {
		t.Fatalf("could not resolve %s: %v", joined, err)
	}

	t.Logf("fuse root : %s", fuseRoot)
	t.Logf("subPath   : %q -> CleanSubPath -> %q", subPath, cleaned)
	t.Logf("joined    : %s", joined)
	t.Logf("resolves to: %s", resolved)

	// Demonstrate that the escape is not hypothetical: read through the joined path.
	got, err := os.ReadFile(filepath.Join(joined, "marker"))
	if err != nil {
		t.Fatalf("could not read through the joined path: %v", err)
	}
	t.Logf("content reachable via the sub path: %q", string(got))

	realFuseRoot, err := filepath.EvalSymlinks(fuseRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(resolved, realFuseRoot+string(filepath.Separator)) && resolved != realFuseRoot {
		t.Errorf("CONTAINMENT CLAIM FALSE: CleanSubPath(%q) joined to the mount root resolves to %q, outside %q",
			subPath, resolved, realFuseRoot)
	}
	if string(got) == "HOST-ONLY-CONTENT" {
		t.Errorf("CONTAINMENT CLAIM FALSE: content outside the mount root is reachable through sub path %q", subPath)
	}
}
