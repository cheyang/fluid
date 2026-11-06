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

package alluxio

// Verification harness for fluid PR #6074 review finding F1 (nit).
// This file is ADDITIVE review tooling; it touches no production code.
//
// F1 claim: the doc comment added for TestGetAPIGatewayStatus is separated
// from the function declaration by a blank line, which severs Go doc-comment
// association and is inconsistent with the sibling comment on
// TestQueryAPIGatewayEndpoint in the same file (no blank line there).
//
// Polarity: CONTRACT — asserts the intended (fixed) shape. FAILS on the
// reviewed head (2a0c023); PASSES once the blank line is removed.

import (
	"os"
	"regexp"
	"testing"
)

func TestPR6074CommentAttached(t *testing.T) {
	src, err := os.ReadFile("api_gateway_test.go")
	if err != nil {
		t.Fatalf("harness: cannot read api_gateway_test.go: %v", err)
	}
	// The doc comment for TestGetAPIGatewayStatus must be immediately
	// adjacent to the func declaration: comment line, then "func" on the
	// very next line. A blank line in between (i.e. "\n\nfunc ...") means
	// the comment is detached (not a Go doc comment).
	detached := regexp.MustCompile(`(?m)^// [^\n]*\n\nfunc TestGetAPIGatewayStatus\(t \*testing\.T\)`)
	if detached.Find(src) != nil {
		t.Fatalf("F1: doc comment for TestGetAPIGatewayStatus is detached from the func declaration by a blank line; remove the blank line so it reads as a Go doc comment (cf. TestQueryAPIGatewayEndpoint in the same file)")
	}
	// Positive control: an attached comment must exist.
	attached := regexp.MustCompile(`(?m)^// TestGetAPIGatewayStatus [^\n]*(\n// [^\n]*)*\nfunc TestGetAPIGatewayStatus\(t \*testing\.T\)`)
	if attached.Find(src) == nil {
		t.Fatalf("F1: no attached doc comment found for TestGetAPIGatewayStatus")
	}
}
