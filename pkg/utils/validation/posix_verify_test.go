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

package validation

// Reviewer verification harness (PR #6266, commit "fix(paths): use POSIX paths
// for runtime mounts"). Contract test, polarity: contract.
// C2: IsValidMountRoot must keep its exact Linux behavior after the
// filepath -> path switch: POSIX-absolute required, components cleaned and
// checked against relaxed DNS-1123. A Windows drive-letter path stays
// non-absolute (rejected). FAILS if the switch changed Linux-visible behavior.

import "testing"

func TestVerifyIsValidMountRootPOSIXSemantics(t *testing.T) {
	valid := []string{
		"/runtime-mnt/alluxio/default/hbase",
		"/runtime-mnt/alluxio/default/hbase/",  // trailing slash cleans away
		"/runtime-mnt//alluxio/default/hbase",  // double slash cleans away
		"/underFSStorage",
		"/runtime_mnt/fluid", // underscore allowed via relaxed DNS-1123
	}
	for _, in := range valid {
		if err := IsValidMountRoot(in); err != nil {
			t.Errorf("IsValidMountRoot(%q) = %v; want nil", in, err)
		}
	}

	invalid := []string{
		"",                // empty
		"runtime-mnt/x",   // not absolute
		"C:\\fluid",       // Windows drive path is not POSIX-absolute
		"\\fluid",         // backslash is not a separator -> not absolute
	}
	for _, in := range invalid {
		if err := IsValidMountRoot(in); err == nil {
			t.Errorf("IsValidMountRoot(%q) = nil; want error", in)
		}
	}
}
