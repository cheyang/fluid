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

/*
Round-2 harness for PR #6245 (delta 6711374..3271f28, "docs(juicefs): document
runtime level imagePullSecrets").

docs/{en,zh}/dev/api_doc.md is a generated artifact (Makefile target
`update-api-doc`: pinned elastic/crd-ref-docs v0.3.0, then `cp` zh -> en). The
round-2 commit hand-edited it instead of running the generator, and the three
imagePullSecrets rows landed in the wrong places:

  - the JuiceFSCompTemplateSpec row is orphaned right after the `#### JuiceFSRuntime`
    heading (the JuiceFSCompTemplateSpec table itself has no row);
  - the JuiceFSFuseSpec row was pasted into the JuiceFSRuntimeSpec table, giving
    the runtime-level field a "pull the JuiceFS fuse image" description;
  - the JuiceFSRuntimeSpec row is orphaned right after the Vineyard `#### MasterSpec`
    heading.

This contract test asserts the placement the pinned generator produces
(verified against a local crd-ref-docs v0.3.0 run):

  - JuiceFSCompTemplateSpec table: imagePullSecrets row, description containing
    "Only the worker uses it for now";
  - JuiceFSFuseSpec table: imagePullSecrets row, description containing
    "JuiceFS fuse image";
  - JuiceFSRuntimeSpec table: imagePullSecrets row, description containing
    "all JuiceFS components" (NOT the fuse wording);
  - no JuiceFS imagePullSecrets row anywhere else (no strays under
    `#### JuiceFSRuntime` or the Vineyard `#### MasterSpec`).

Polarity: contract test. FAILS on the hand-edited docs; PASSES after
`make update-api-doc` (which also picks up unrelated pre-existing master drift
around MediumSource — expected, not this PR's doing).

(Note for the zh file: the Makefile copies zh over en, so both files are checked
with the same expectations.)
*/

package juicefs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	goruntime "runtime"
)

// parseApiDocSections splits a crd-ref-docs markdown file into sections keyed by
// their `#### <TypeName>` heading; the value is the section's lines (heading
// excluded).
func parseApiDocSections(content string) map[string][]string {
	sections := map[string][]string{}
	current := ""
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "#### ") {
			current = strings.TrimSpace(strings.TrimPrefix(line, "#### "))
			sections[current] = []string{}
			continue
		}
		if current != "" {
			sections[current] = append(sections[current], line)
		}
	}
	return sections
}

// apiDocTableRows returns the table rows of a section: lines starting with "|"
// that come after the "| Field | Description ..." header. Rows appearing before
// that header are "orphaned" (that is where the hand-edit left two of them).
func apiDocTableRows(sectionLines []string) []string {
	rows := []string{}
	inTable := false
	for _, line := range sectionLines {
		if strings.HasPrefix(line, "| Field |") {
			inTable = true
			continue
		}
		if inTable && strings.HasPrefix(line, "|") {
			rows = append(rows, line)
		}
	}
	return rows
}

func TestJuicefsApiDocImagePullSecretsPlacement(t *testing.T) {
	_, thisFile, _, _ := goruntime.Caller(0)
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(thisFile))))

	// marker -> the section whose table must contain a row with that marker.
	// The markers are unique to the JuiceFS imagePullSecrets rows (verified by
	// grep at head: each appears exactly once per file).
	required := []struct {
		section string
		marker  string
	}{
		{"JuiceFSCompTemplateSpec", "Only the worker uses it for now"},
		{"JuiceFSFuseSpec", "JuiceFS fuse image"},
		{"JuiceFSRuntimeSpec", "all JuiceFS components"},
	}
	allowedSections := map[string]bool{}
	for _, r := range required {
		allowedSections[r.section] = true
	}

	for _, lang := range []string{"en", "zh"} {
		path := filepath.Join(repoRoot, "docs", lang, "dev", "api_doc.md")
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("cannot read %s: %v", path, err)
		}
		sections := parseApiDocSections(string(content))

		// 1. each required row sits INSIDE its section's table.
		for _, r := range required {
			lines, ok := sections[r.section]
			if !ok {
				t.Errorf("%s: section %q missing entirely", path, r.section)
				continue
			}
			found := false
			for _, row := range apiDocTableRows(lines) {
				if strings.Contains(row, "`imagePullSecrets`") && strings.Contains(row, r.marker) {
					found = true
				}
			}
			if !found {
				t.Errorf("%s: section %q table has no imagePullSecrets row containing %q.\nRows in table:\n%s",
					path, r.section, r.marker, strings.Join(apiDocTableRows(lines), "\n"))
			}
		}

		// 2. the JuiceFSRuntimeSpec row must not carry the fuse wording
		// (guards the copy-paste mixup where the fuse description was pasted
		// into the runtime-spec table).
		for _, row := range apiDocTableRows(sections["JuiceFSRuntimeSpec"]) {
			if strings.Contains(row, "`imagePullSecrets`") && strings.Contains(row, "JuiceFS fuse image") {
				t.Errorf("%s: JuiceFSRuntimeSpec table describes imagePullSecrets with the FUSE wording:\n%s", path, row)
			}
		}

		// 3. no JuiceFS imagePullSecrets row may appear in any other section
		// (catches rows orphaned between a heading and its description, e.g.
		// under `#### JuiceFSRuntime` or the Vineyard `#### MasterSpec`).
		for heading, lines := range sections {
			if allowedSections[heading] {
				continue
			}
			for _, line := range lines {
				for _, r := range required {
					if strings.Contains(line, r.marker) {
						t.Errorf("%s: JuiceFS imagePullSecrets row (marker %q) in wrong section %q:\n%s",
							path, r.marker, heading, line)
					}
				}
			}
		}
	}
}
