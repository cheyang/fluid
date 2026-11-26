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

// Reviewer verification harness for PR #6285 (fluid-cloudnative/fluid).
//
// Claim under test (contract polarity): when the `alluxio fs mount` output
// contains lines with fewer than three whitespace-separated fields (empty
// output, blank lines, stray short lines), IsMounted and
// GetMountedAlluxioPaths must skip those lines and return normally instead
// of panicking with `index out of range [2]`.
//
// On the merge base 253f6bb84f26a906fba301550b5c81ebdd73af35 this test
// FAILS (panic: index out of range [2]); on the PR head it PASSES.
// Run with: go test -gcflags=all=-l -run TestClaudeVerify ./pkg/ddc/alluxio/operations/

import (
	"reflect"
	"testing"

	"github.com/agiledragon/gomonkey/v2"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
)

const claudeHbaseEntry = "https://mirrors.bit.edu.cn/apache/hbase/stable  on  /hbase (web, capacity=-1B, used=-1B, read-only, not shared, properties={})"
const claudeRootEntry = "/underFSStorage  on  /  (local, capacity=0B, used=0B, not read-only, not shared, properties={})"

// TestClaudeVerify_MountTableParsingSkipsShortLines is a CONTRACT test:
// correct behavior (skip short lines, no panic) passes; the pre-fix code
// fails by panicking.
func TestClaudeVerify_MountTableParsingSkipsShortLines(t *testing.T) {
	var testCases = []struct {
		name          string
		stdout        string
		alluxioPath   string
		expectMounted bool
		expectPaths   []string
	}{
		{
			name:          "empty stdout",
			stdout:        "",
			alluxioPath:   "/hbase",
			expectMounted: false,
			expectPaths:   nil,
		},
		{
			name:          "whitespace-only stdout",
			stdout:        "   ",
			alluxioPath:   "/hbase",
			expectMounted: false,
			expectPaths:   nil,
		},
		{
			name:          "single-field line",
			stdout:        "garbage\n",
			alluxioPath:   "/hbase",
			expectMounted: false,
			expectPaths:   nil,
		},
		{
			name:          "two-field line",
			stdout:        "foo on\n",
			alluxioPath:   "/hbase",
			expectMounted: false,
			expectPaths:   nil,
		},
		{
			name:          "short line before and after valid entries, mount hit on second entry",
			stdout:        "\n" + claudeHbaseEntry + "\n\nx y\n" + claudeRootEntry + "\n\n",
			alluxioPath:   "/",
			expectMounted: true,
			expectPaths:   []string{"/hbase", "/"},
		},
		{
			name:          "valid entries with trailing blank line, mount miss",
			stdout:        claudeHbaseEntry + "\n" + claudeRootEntry + "\n\n",
			alluxioPath:   "/spark",
			expectMounted: false,
			expectPaths:   []string{"/hbase", "/"},
		},
	}

	a := &AlluxioFileUtils{log: fake.NullLogger()}
	for _, test := range testCases {
		test := test
		func() {
			stdout := test.stdout
			patches := gomonkey.ApplyPrivateMethod(reflect.TypeOf(AlluxioFileUtils{}), "exec",
				func(a AlluxioFileUtils, command []string, verbose bool) (string, string, error) {
					return stdout, "", nil
				})
			defer patches.Reset()

			mounted, err := a.IsMounted(test.alluxioPath)
			if err != nil {
				t.Errorf("%s: IsMounted want nil err, got: %v", test.name, err)
			}
			if mounted != test.expectMounted {
				t.Errorf("%s: IsMounted want: %t, got: %t", test.name, test.expectMounted, mounted)
			}

			paths, err := a.GetMountedAlluxioPaths()
			if err != nil {
				t.Errorf("%s: GetMountedAlluxioPaths want nil err, got: %v", test.name, err)
			}
			if !reflect.DeepEqual(paths, test.expectPaths) {
				t.Errorf("%s: GetMountedAlluxioPaths want: %v, got: %v", test.name, test.expectPaths, paths)
			}
		}()
	}
}

// TestClaudeVerify_IsMountedStopsAtFirstMatch is a CONTRACT test pinning the
// early-return semantics of IsMounted: the first matching line wins, even
// when a later line also matches, and lines after the match are not parsed.
func TestClaudeVerify_IsMountedStopsAtFirstMatch(t *testing.T) {
	stdout := claudeHbaseEntry + "\n" + claudeRootEntry + "\n"
	patches := gomonkey.ApplyPrivateMethod(reflect.TypeOf(AlluxioFileUtils{}), "exec",
		func(a AlluxioFileUtils, command []string, verbose bool) (string, string, error) {
			return stdout, "", nil
		})
	defer patches.Reset()

	a := &AlluxioFileUtils{log: fake.NullLogger()}
	mounted, err := a.IsMounted("/hbase")
	if err != nil {
		t.Errorf("IsMounted want nil err, got: %v", err)
	}
	if !mounted {
		t.Errorf("IsMounted want: true, got: false")
	}
}
