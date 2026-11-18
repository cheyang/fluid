/*
Verification harness for PR #6266 commit "fix(paths): use POSIX paths for runtime mounts"
(Reviewer B / Codex). Additive test file: production code untouched.

Polarity: CONTRACT, and deliberately a CHARACTERIZATION test: it pins the exact Linux behavior
of NormalizeSubPath and UFSPathBuilder. It must PASS identically on the base branch and on the
PR head. If it passes on both, the filepath->path switch changes nothing on Linux (the only OS
fluid controllers/CSI run on), i.e. the commit is a no-op for production behavior and only makes
dev/test behavior on non-Linux hosts deterministic.
*/
package utils

import (
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
)

func TestVerifyNormalizeSubPathCharacterization(t *testing.T) {
	cases := []struct {
		in        string
		want      string
		expectErr bool
	}{
		{"", "", false},
		{"/", "", false},
		{"a/b", "a/b", false},
		{"/a//b/", "a/b", false},
		{"a/./b", "a/b", false},
		{"a/../b", "b", false}, // traversal that does not escape is cleaned, per doc comment
		{"..", "", true},
		{"../escape", "", true},
		{"a/../../b", "", true},
	}
	for _, c := range cases {
		got, err := NormalizeSubPath(c.in)
		if (err != nil) != c.expectErr {
			t.Errorf("NormalizeSubPath(%q) err=%v, expectErr=%v", c.in, err, c.expectErr)
			continue
		}
		if err == nil && got != c.want {
			t.Errorf("NormalizeSubPath(%q)=%q, want %q", c.in, got, c.want)
		}
	}
}

func TestVerifyUFSPathBuilderCharacterization(t *testing.T) {
	u := UFSPathBuilder{}
	if got := u.GenUFSPathInUnifiedNamespace(datav1alpha1.Mount{Path: "/custom/path", Name: "n"}); got != "/custom/path" {
		t.Errorf("abs path: got %q", got)
	}
	if got := u.GenUFSPathInUnifiedNamespace(datav1alpha1.Mount{Path: "rel", Name: "mybucket"}); got != "/mybucket" {
		t.Errorf("rel path: got %q", got)
	}
	if got := u.GenLocalStoragePath(datav1alpha1.Mount{Path: "/data"}); got != "/underFSStorage/data" {
		t.Errorf("local abs: got %q", got)
	}
	if got := u.GenLocalStoragePath(datav1alpha1.Mount{Name: "mybucket"}); got != "/underFSStorage/mybucket" {
		t.Errorf("local name: got %q", got)
	}
}
