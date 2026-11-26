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

package operations

// Reviewer-B (Codex) verification harness for fluid-cloudnative/fluid PR #6285.
// Polarity: CONTRACT tests — they assert the intended behavior (no panic,
// short/blank lines skipped, well-formed lines parsed as before).
// On the merge-base (253f6bb8) they FAIL with
// "panic: runtime error: index out of range [2]"; on the PR head they PASS.
// Additive only: this file must never touch production code.

import (
	"reflect"
	"testing"

	"github.com/agiledragon/gomonkey/v2"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
)

const (
	verifyHbaseEntry = "https://mirrors.bit.edu.cn/apache/hbase/stable  on  /hbase (web, capacity=-1B, used=-1B, read-only, not shared, properties={})"
	verifyRootEntry  = "/underFSStorage  on  /  (local, capacity=0B, used=0B, not read-only, not shared, properties={})"
)

var verifyPR6285Cases = []struct {
	name          string
	stdout        string
	queryPath     string
	expectMounted bool
	expectPaths   []string
}{
	{
		name:          "empty output",
		stdout:        "",
		queryPath:     "/hbase",
		expectMounted: false,
		expectPaths:   nil,
	},
	{
		name:          "whitespace-only output",
		stdout:        "   \n\t \n  ",
		queryPath:     "/",
		expectMounted: false,
		expectPaths:   nil,
	},
	{
		name:          "single two-field line",
		stdout:        "unexpected line",
		queryPath:     "/",
		expectMounted: false,
		expectPaths:   nil,
	},
	{
		name:          "single one-token line",
		stdout:        "garbage",
		queryPath:     "/",
		expectMounted: false,
		expectPaths:   nil,
	},
	{
		name:          "blank line between entries",
		stdout:        verifyHbaseEntry + "\n\n" + verifyRootEntry,
		queryPath:     "/",
		expectMounted: true,
		expectPaths:   []string{"/hbase", "/"},
	},
	{
		name:          "short line before matching entry",
		stdout:        "not-a-mount-entry\n" + verifyHbaseEntry,
		queryPath:     "/hbase",
		expectMounted: true,
		expectPaths:   []string{"/hbase"},
	},
	{
		name:          "well-formed table unchanged",
		stdout:        verifyHbaseEntry + "\n" + verifyRootEntry,
		queryPath:     "/hbase",
		expectMounted: true,
		expectPaths:   []string{"/hbase", "/"},
	},
	{
		name:          "no match in well-formed table",
		stdout:        verifyRootEntry,
		queryPath:     "/does-not-exist",
		expectMounted: false,
		expectPaths:   []string{"/"},
	},
}

func patchExecReturning(stdout string) *gomonkey.Patches {
	return gomonkey.ApplyPrivateMethod(reflect.TypeOf(AlluxioFileUtils{}), "exec",
		func(a AlluxioFileUtils, command []string, verbose bool) (string, string, error) {
			return stdout, "", nil
		})
}

func TestVerifyPR6285_IsMountedShortLines(t *testing.T) {
	a := &AlluxioFileUtils{log: fake.NullLogger()}
	for _, tc := range verifyPR6285Cases {
		patches := patchExecReturning(tc.stdout)
		mounted, err := a.IsMounted(tc.queryPath)
		patches.Reset()
		if err != nil {
			t.Errorf("%s: IsMounted returned err: %v", tc.name, err)
		}
		if mounted != tc.expectMounted {
			t.Errorf("%s: IsMounted(%q) = %t, want %t", tc.name, tc.queryPath, mounted, tc.expectMounted)
		}
	}
}

func TestVerifyPR6285_GetMountedAlluxioPathsShortLines(t *testing.T) {
	a := &AlluxioFileUtils{log: fake.NullLogger()}
	for _, tc := range verifyPR6285Cases {
		patches := patchExecReturning(tc.stdout)
		paths, err := a.GetMountedAlluxioPaths()
		patches.Reset()
		if err != nil {
			t.Errorf("%s: GetMountedAlluxioPaths returned err: %v", tc.name, err)
		}
		if !reflect.DeepEqual(paths, tc.expectPaths) {
			t.Errorf("%s: GetMountedAlluxioPaths = %v, want %v", tc.name, paths, tc.expectPaths)
		}
	}
}
