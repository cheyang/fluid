/*
Verification harness for PR #6266 commit "fix(paths): use POSIX paths for runtime mounts"
(Reviewer B / Codex). Additive test file: production code untouched.

Polarity: CONTRACT / CHARACTERIZATION. Pins the Linux behavior of IsValidMountRoot; must PASS
identically on base and head, proving the filepath->path switch is Linux-neutral.
*/
package validation

import "testing"

func TestVerifyIsValidMountRootCharacterization(t *testing.T) {
	valid := []string{"/runtime-mnt", "/a", "/A_B/c-d", "/a//b", "/a/../b"}
	for _, in := range valid {
		if err := IsValidMountRoot(in); err != nil {
			t.Errorf("IsValidMountRoot(%q)=%v, want nil", in, err)
		}
	}
	invalid := []string{"", "rel/path", "/", "/a b"}
	for _, in := range invalid {
		if err := IsValidMountRoot(in); err == nil {
			t.Errorf("IsValidMountRoot(%q)=nil, want error", in)
		}
	}
}
