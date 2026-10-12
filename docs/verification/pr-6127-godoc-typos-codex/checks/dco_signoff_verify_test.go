// Verification harness for fluid-cloudnative/fluid PR #6127 (Reviewer B / Codex).
// ADDITIVE ONLY: part of the review harness; not proposed for merge.
//
// TestVerifyDCOSignoff_PR6127 (F1, contract): every commit the PR adds must
// carry a valid DCO sign-off in the form "Signed-off-by: Name <email>"
// matching the commit author or committer — exactly what the probot-dco app
// enforces (https://github.com/probot/dco, lib/dco.js:
//   const regex = /^Signed-off-by: (.*) <(.*)>\s*$/gim
// ).
//
// On the reviewed head ffe934cf the commit message ends with
//   "Signed-off-by: saiashok103@gmail.com"
// i.e. a bare email with no "Name <email>" shape, so the regex never matches
// and the DCO check-run on the PR concluded action_required ("The sign-off is
// missing.", check-run 87703619239). This test reproduces that verdict
// locally and goes green once the commit is re-signed with `git commit -s`
// (or `git rebase --signoff`) and force-pushed.
//
// The commit range under test is <base>..HEAD restricted to the production
// files this PR touches, so it validates the PR's commit(s) both on the
// verify branch (where HEAD is the harness commit, which does not touch those
// files) and when re-verify.sh grafts the harness onto an updated PR head.
package checks

import (
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// pr6127BaseSHA is the frozen base from the review round (merge-base of the PR).
const pr6127BaseSHA = "3e44ad250ae658738f6eba536bd6eb938a89bd6c"

// dcoSignoffRE mirrors probot-dco's /^Signed-off-by: (.*) <(.*)>\s*$/gim.
var dcoSignoffRE = regexp.MustCompile(`(?m)^Signed-off-by: (.*) <(.*)>\s*$`)

// gitOutPR6127 runs git at the repository root so pathspecs resolve the same
// regardless of the package directory go test executes from.
func gitOutPR6127(t *testing.T, args ...string) string {
	t.Helper()
	rootOut, err := exec.Command("git", "rev-parse", "--show-toplevel").CombinedOutput()
	if err != nil {
		t.Fatalf("git rev-parse --show-toplevel: %v\n%s", err, rootOut)
	}
	root := strings.TrimSpace(string(rootOut))
	full := append([]string{"-C", root}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(full, " "), err, out)
	}
	return strings.TrimRight(string(out), "\n")
}

func TestVerifyDCOSignoff_PR6127(t *testing.T) {
	commits := gitOutPR6127(t, "log", "--format=%H", pr6127BaseSHA+"..HEAD", "--",
		"pkg/utils/quantity.go", "pkg/utils/errors.go")
	if commits == "" {
		t.Skip("no PR commits found in range touching the PR's files")
	}
	for _, sha := range strings.Split(commits, "\n") {
		authorName := gitOutPR6127(t, "show", "-s", "--format=%an", sha)
		authorEmail := gitOutPR6127(t, "show", "-s", "--format=%ae", sha)
		committerName := gitOutPR6127(t, "show", "-s", "--format=%cn", sha)
		committerEmail := gitOutPR6127(t, "show", "-s", "--format=%ce", sha)
		msg := gitOutPR6127(t, "show", "-s", "--format=%B", sha)

		matches := dcoSignoffRE.FindAllStringSubmatch(msg, -1)
		if len(matches) == 0 {
			t.Errorf("commit %s: no valid DCO sign-off found; probot-dco requires a line "+
				"'Signed-off-by: Name <email>' (author %s <%s>) — a bare email without the "+
				"'Name <email>' shape is treated as a missing sign-off",
				sha, authorName, authorEmail)
			continue
		}
		names := map[string]bool{strings.ToLower(authorName): true, strings.ToLower(committerName): true}
		emails := map[string]bool{strings.ToLower(authorEmail): true, strings.ToLower(committerEmail): true}
		valid := false
		for _, m := range matches {
			if names[strings.ToLower(strings.TrimSpace(m[1]))] && emails[strings.ToLower(strings.TrimSpace(m[2]))] {
				valid = true
				break
			}
		}
		if !valid {
			t.Errorf("commit %s: sign-off(s) present but none match author/committer %s <%s>",
				sha, authorName, authorEmail)
		}
	}
}
