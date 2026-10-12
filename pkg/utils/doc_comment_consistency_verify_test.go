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

// Verification harness for fluid-cloudnative/fluid PR #6127 (Reviewer B / Codex).
// ADDITIVE ONLY: this file is part of the review harness on branch
// verify/pr-6127-godoc-typos-codex; it is not proposed for merge into the PR.
//
// All tests below are CONTRACT tests: they assert the behavior the PR itself
// advertises (godoc convention: a doc comment begins with the identifier it
// documents; every exported identifier is documented). They FAIL on the
// reviewed head ffe934cf and must PASS before the PR is considered complete.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// exportedFuncDocsPR6127 returns map[funcName]docText for every exported
// package-level function in the given file of this package.
func exportedFuncDocsPR6127(t *testing.T, file string) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	out := map[string]string{}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || !fn.Name.IsExported() {
			continue
		}
		doc := ""
		if fn.Doc != nil {
			doc = strings.TrimSpace(fn.Doc.Text())
		}
		out[fn.Name.Name] = doc
	}
	return out
}

var pr6127TouchedFiles = []string{"quantity.go", "errors.go"}

// TestVerifyDocCommentsMatchIdentifiers_PR6127 (F2, contract):
// In the files this PR touches, every documented exported function's doc
// comment must begin with the exact identifier name — the convention the PR
// body itself cites (https://go.dev/blog/godoc). The PR's own commit message
// claims to fix "'TranformQuantityToUnits' -> 'TransformQuantityToUnits'" but
// the diff does not touch it, so on head the doc comment
// "TransformQuantityToUnits ..." still sits on func TranformQuantityToUnits
// and this test is RED (that red IS the reproduction of F2).
func TestVerifyDocCommentsMatchIdentifiers_PR6127(t *testing.T) {
	for _, file := range pr6127TouchedFiles {
		for name, doc := range exportedFuncDocsPR6127(t, file) {
			if doc == "" {
				continue // undocumented funcs are F4's concern
			}
			first := strings.Fields(doc)[0]
			// godoc allows "A ..."/"An ..." leading article only for types; for
			// funcs the comment must start with the function name itself.
			if first != name {
				t.Errorf("%s: doc comment of exported func %q starts with %q — comment/identifier mismatch (PR #6127 commit message claims this was fixed)",
					file, name, first)
			}
		}
	}
}

// TestVerifyExportedFuncsDocumented_PR6127 (F4, contract):
// The PR's stated goal is that "every exported identifier is documented" in
// these files. On head, TransformEFCUnitToQuantity in quantity.go still has no
// doc comment, so this test is RED (that red IS the reproduction of F4).
func TestVerifyExportedFuncsDocumented_PR6127(t *testing.T) {
	for _, file := range pr6127TouchedFiles {
		for name, doc := range exportedFuncDocsPR6127(t, file) {
			if doc == "" {
				t.Errorf("%s: exported func %q has no GoDoc comment", file, name)
			}
		}
	}
}

// TestVerifyIgnoreNoKindMatchErrorDocComplete_PR6127 (F3, contract):
// IgnoreNoKindMatchError is implemented with apimeta.IsNoMatchError, which
// (vendor/k8s.io/apimachinery/pkg/api/meta/errors.go) matches BOTH
// *NoKindMatchError AND *NoResourceMatchError. The GoDoc this PR adds says it
// "ignores NoKindMatch errors" only. The doc must describe the full behavior;
// on head it does not mention NoResourceMatch, so this test is RED.
// If the author rewords the doc with equivalent language that covers
// NoResourceMatchError semantics, update the assertion accordingly.
func TestVerifyIgnoreNoKindMatchErrorDocComplete_PR6127(t *testing.T) {
	docs := exportedFuncDocsPR6127(t, "errors.go")
	doc, ok := docs["IgnoreNoKindMatchError"]
	if !ok {
		t.Fatal("IgnoreNoKindMatchError not found in errors.go")
	}
	if !strings.Contains(doc, "NoKindMatch") {
		t.Errorf("doc comment lost its NoKindMatch mention:\n%s", doc)
	}
	if !strings.Contains(doc, "NoResourceMatch") {
		t.Errorf("doc comment understates behavior: IgnoreNoKindMatchError also suppresses *NoResourceMatchError (apimeta.IsNoMatchError), but the doc mentions only NoKindMatch:\n%s", doc)
	}
}
