// Round-2 addition: regression guard for F3 (toolchain directive contract).
//
// F3 history: the first head (af817d9) deleted the explicit
// "toolchain go1.25.12" line from go.mod while .circleci/config.yml narrated
// that pin in prose. On the remediated head (a70ab0a, rebased onto the Go 1.26
// migration #6227) go.mod again carries "toolchain go1.26.10" and the
// .circleci comment was rewritten to match. This contract test keeps the
// invariant: go.mod carries an explicit toolchain directive, it is >= the go
// directive, and the CircleCI config that relies on it names the same Go
// minor version.
package pr6230verify

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestGoModToolchainDirectivePresent(t *testing.T) {
	root := repoRoot(t)
	gomod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	goM := regexp.MustCompile(`(?m)^go (\d+\.\d+(\.\d+)?)$`).FindSubmatch(gomod)
	if goM == nil {
		t.Fatal("no go directive in go.mod")
	}
	goDirective := parseMinor(t, string(goM[1]))

	tcM := regexp.MustCompile(`(?m)^toolchain go(\d+\.\d+)(\.\d+)?$`).FindSubmatch(gomod)
	if tcM == nil {
		t.Errorf("F3: go.mod carries no explicit toolchain directive; the documented " +
			"pin contract (.circleci/config.yml narrates it) is gone")
		return
	}
	tc := parseMinor(t, string(tcM[1]))
	t.Logf("go directive %d.%d, toolchain directive %d.%d", goDirective.major, goDirective.minor, tc.major, tc.minor)
	if !tc.ge(goDirective) {
		t.Errorf("F3: toolchain go%d.%d is older than the go directive %d.%d",
			tc.major, tc.minor, goDirective.major, goDirective.minor)
	}

	cc, err := os.ReadFile(filepath.Join(root, ".circleci", "config.yml"))
	if err != nil {
		t.Skip("no .circleci/config.yml")
	}
	imgM := regexp.MustCompile(`cimg/go:(\d+\.\d+)`).FindSubmatch(cc)
	if imgM == nil {
		return
	}
	img := parseMinor(t, string(imgM[1]))
	if img.major != tc.major || img.minor != tc.minor {
		t.Errorf("F3: .circleci/config.yml uses cimg/go:%d.%d but go.mod pins toolchain go%d.%d — stale coupling",
			img.major, img.minor, tc.major, tc.minor)
	}
}
