package pr6214compress

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

// TestVendoredCompressVersion guards the bump itself: the vendored
// github.com/klauspost/compress module must be at least v1.18.7 (the version
// this PR moves to). It reads vendor/modules.txt, the same source of truth the
// Go toolchain enforces against go.mod in vendor mode.
//
// POLARITY: contract for the post-bump state. On the pre-bump base (v1.17.11)
// this test FAILS by design — that expected failure is the evidence that the
// guard bites; on the PR head and any later head it must PASS.
func TestVendoredCompressVersion(t *testing.T) {
	modulesTxt := filepath.Join("..", "..", "..", "vendor", "modules.txt")
	data, err := os.ReadFile(modulesTxt)
	if err != nil {
		t.Fatalf("cannot read %s: %v", modulesTxt, err)
	}
	m := regexp.MustCompile(`(?m)^# github\.com/klauspost/compress v(\d+)\.(\d+)\.(\d+)$`).FindSubmatch(data)
	if m == nil {
		t.Fatalf("klauspost/compress version not found in %s", modulesTxt)
	}
	major, _ := strconv.Atoi(string(m[1]))
	minor, _ := strconv.Atoi(string(m[2]))
	patch, _ := strconv.Atoi(string(m[3]))
	if major < 1 || (major == 1 && minor < 18) || (major == 1 && minor == 18 && patch < 7) {
		t.Fatalf("vendored klauspost/compress is v%d.%d.%d, want >= v1.18.7", major, minor, patch)
	}
	t.Logf("vendored klauspost/compress = v%d.%d.%d (>= v1.18.7)", major, minor, patch)
}
