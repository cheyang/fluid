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

package utils

// Reviewer verification harness (PR #6266, commit "fix(paths): use POSIX paths
// for runtime mounts"). Contract test, polarity: contract.
// C2: the four re-based helpers must keep their exact Linux behavior after the
// filepath -> path switch (on Linux path.Clean/Join/IsAbs are equivalent to
// the filepath versions), and must treat these container-consumed paths with
// POSIX semantics: a Windows drive-letter path is NOT absolute, a backslash is
// NOT a separator. This test FAILS if the switch accidentally changed any
// Linux-visible behavior.

import (
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
)

func TestVerifyNormalizeSubPathPOSIXSemantics(t *testing.T) {
	cases := []struct {
		in        string
		want      string
		wantError bool
	}{
		{"", "", false},
		{"/", "", false},
		{"//", "", false},
		{"a/b", "a/b", false},
		{"/a/b", "a/b", false},
		{"a//b/./c", "a/b/c", false},
		{"a/..", "", false},
		{"..", "", true},          // escapes -> error, not clamped
		{"../x", "", true},        // escapes -> error
		{"a/../../x", "", true},   // escapes after clean -> error
		{`a/..\b`, `a/..\b`, false}, // backslash is a normal char, not a separator
	}
	for _, c := range cases {
		got, err := NormalizeSubPath(c.in)
		if c.wantError {
			if err == nil {
				t.Errorf("NormalizeSubPath(%q) = %q, nil; want error", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("NormalizeSubPath(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("NormalizeSubPath(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}

func TestVerifyUFSPathBuilderPOSIXSemantics(t *testing.T) {
	u := UFSPathBuilder{}

	if got := u.GenUFSPathInUnifiedNamespace(datav1alpha1.Mount{Path: "/data", Name: "ignored"}); got != "/data" {
		t.Errorf("GenUFSPathInUnifiedNamespace(abs path) = %q; want /data", got)
	}
	if got := u.GenUFSPathInUnifiedNamespace(datav1alpha1.Mount{Path: "data", Name: "spark"}); got != "/spark" {
		t.Errorf("GenUFSPathInUnifiedNamespace(rel path) = %q; want /spark", got)
	}
	// A Windows drive-letter path is not absolute in POSIX semantics: fall back
	// to the mount name. (On Linux the pre-PR filepath.IsAbs already returned
	// false here, so this also pins unchanged Linux behavior.)
	if got := u.GenUFSPathInUnifiedNamespace(datav1alpha1.Mount{Path: `C:\data`, Name: "spark"}); got != "/spark" {
		t.Errorf("GenUFSPathInUnifiedNamespace(windows path) = %q; want /spark", got)
	}

	if got := u.GenLocalStoragePath(datav1alpha1.Mount{Path: "/mnt"}); got != "/underFSStorage/mnt" {
		t.Errorf("GenLocalStoragePath(abs path) = %q; want /underFSStorage/mnt", got)
	}
	if got := u.GenLocalStoragePath(datav1alpha1.Mount{Name: "spark"}); got != "/underFSStorage/spark" {
		t.Errorf("GenLocalStoragePath(name) = %q; want /underFSStorage/spark", got)
	}
	if got := u.GenLocalStoragePath(datav1alpha1.Mount{Path: "/a/../b"}); got != "/underFSStorage/b" {
		t.Errorf("GenLocalStoragePath(cleanable path) = %q; want /underFSStorage/b", got)
	}
}
