package operations

import (
	"go/format"
	"os"
	"strings"
	"testing"
)

// TestPR6000CommentAccuracy is a CONTRACT test for PR #6000 (issue #5999):
// a doc comment must sit directly above func TestOperations, and every factual
// claim it makes must map to a statement in the function body.
//
// Polarity: contract. FAILS on the merge base da69206 (comment absent),
// PASSES on the PR head 47fad354.
func TestPR6000CommentAccuracy(t *testing.T) {
	src, err := os.ReadFile("operations_suite_test.go")
	if err != nil {
		t.Fatalf("cannot read operations_suite_test.go: %v", err)
	}
	srcStr := string(src)

	const funcDecl = "func TestOperations(t *testing.T) {"
	idx := strings.Index(srcStr, funcDecl)
	if idx < 0 {
		t.Fatal("func TestOperations not found in operations_suite_test.go")
	}

	lines := strings.Split(srcStr[:idx], "\n")
	// drop the empty piece produced by the final newline
	if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 || !strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "//") {
		t.Fatal("no doc comment directly above func TestOperations; issue #5999 asks for one")
	}
	var comment []string
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(l, "//") {
			break
		}
		comment = append([]string{l}, comment...)
	}
	doc := strings.Join(comment, "\n")
	if len(comment) == 0 {
		t.Fatal("no doc comment above func TestOperations")
	}

	body := srcStr[idx:]

	// Each factual claim the comment makes must correspond to actual code.
	claimToCode := []struct {
		claim string
		code  string
	}{
		{"Gomega fail handler", "RegisterFailHandler(Fail)"},
		{"Operations Suite", "RunSpecs(t, \"Operations Suite\")"},
		{"t (*testing.T)", "t *testing.T"},
	}
	for _, c := range claimToCode {
		if !strings.Contains(doc, c.claim) {
			t.Errorf("doc comment does not mention claim %q", c.claim)
		}
		if !strings.Contains(body, c.code) {
			t.Errorf("function body lacks %q that the doc comment asserts", c.code)
		}
	}
}

// TestPR6000GofmtClean asserts the single file touched by PR #6000 introduces
// no gofmt violation (the CI lint job's "Code fmt check" step).
//
// Polarity: contract. Fails if operations_suite_test.go is not gofmt-clean.
func TestPR6000GofmtClean(t *testing.T) {
	src, err := os.ReadFile("operations_suite_test.go")
	if err != nil {
		t.Fatalf("cannot read operations_suite_test.go: %v", err)
	}
	out, err := format.Source(src)
	if err != nil {
		t.Fatalf("operations_suite_test.go does not parse: %v", err)
	}
	if string(out) != string(src) {
		t.Fatal("operations_suite_test.go is not gofmt-clean; run: gofmt -w pkg/ddc/efc/operations/operations_suite_test.go")
	}
}
