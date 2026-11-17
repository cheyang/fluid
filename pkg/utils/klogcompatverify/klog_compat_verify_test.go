/*
Verification harness for fluid PR #6259 (dependabot: k8s.io/klog/v2 2.130.1 -> 2.140.0).
Additive test-only package; no production code is touched.

These tests pin the behavior of the *vendored* klog that fluid's binaries link
(kubernetes libraries such as client-go log through klog; fluid's own
controllers log through zap, but cmd/csi/app/csi.go still manipulates the
"logtostderr" flag namespace and klog is the fallback logger before
ctrl.SetLogger runs).

Polarity notes (see docs/verification/klog-v2.140-bump-codex/verify-manifest.json):
  - "compat" contracts must pass on BOTH base (klog 2.130.1) and head (2.140.0).
  - "new-feature"/"behavior-change" contracts pass on head only; on base they
    fail by design and thereby document the delivered delta.
*/

package klogcompatverify

import (
	"errors"
	"flag"
	"io"
	"os"
	"strings"
	"testing"

	"k8s.io/klog/v2"
)

// captureStderr swaps os.Stderr for a pipe while fn runs. klog writes to
// os.Stderr synchronously when logtostderr=true (and for the stderr copy when
// alsologtostderr=true), so no flush waiting is needed.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stderr = w
	defer func() { os.Stderr = old }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read captured stderr: %v", err)
	}
	os.Stderr = old
	return string(out)
}

// setKlogFlags applies flag settings to the global klog logging state.
func setKlogFlags(t *testing.T, kvs map[string]string) {
	t.Helper()
	fs := flag.NewFlagSet("klog-verify", flag.ContinueOnError)
	klog.InitFlags(fs)
	for k, v := range kvs {
		if err := fs.Set(k, v); err != nil {
			t.Fatalf("setting -%s=%s: %v", k, v, err)
		}
	}
}

// TestLogToStderrDefaultUnfiltered — COMPAT contract (must pass on base AND head).
// With logtostderr=true (the klog default, also forced by cmd/csi/app/csi.go),
// the legacy default must keep writing INFO to stderr even when
// stderrthreshold=ERROR. klog 2.140.0 keeps this via the new
// -legacy_stderr_threshold_behavior flag DEFAULTING to true; this test
// deliberately relies on the default instead of setting the flag, so a
// default flip on either side would fail here. (The flag does not exist on
// base, which is exactly why it must not be set here.)
func TestLogToStderrDefaultUnfiltered(t *testing.T) {
	setKlogFlags(t, map[string]string{
		"logtostderr":     "true",
		"stderrthreshold": "ERROR",
	})
	out := captureStderr(t, func() {
		klog.InfoS("verify-pr6259-info-visible", "case", "legacy-default")
	})
	if !strings.Contains(out, "verify-pr6259-info-visible") {
		t.Errorf("INFO log missing from stderr with legacy default (stderrthreshold=ERROR, logtostderr=true); got: %q", out)
	}
}

// TestLegacyStderrThresholdOptIn — NEW-FEATURE contract (passes on head only;
// the flag does not exist in klog 2.130.1). With
// legacy_stderr_threshold_behavior=false, stderrthreshold=ERROR must filter
// INFO out of stderr while ERROR still passes. This is the upstream fix for
// kubernetes/klog#212, delivered opt-in.
func TestLegacyStderrThresholdOptIn(t *testing.T) {
	setKlogFlags(t, map[string]string{
		"logtostderr":                      "true",
		"stderrthreshold":                  "ERROR",
		"legacy_stderr_threshold_behavior": "false",
	})
	out := captureStderr(t, func() {
		klog.InfoS("verify-pr6259-info-filtered", "case", "opt-in")
		klog.ErrorS(errors.New("verify-pr6259-err"), "verify-pr6259-error-visible")
	})
	if strings.Contains(out, "verify-pr6259-info-filtered") {
		t.Errorf("INFO log should be filtered from stderr when legacy_stderr_threshold_behavior=false and stderrthreshold=ERROR; got: %q", out)
	}
	if !strings.Contains(out, "verify-pr6259-error-visible") {
		t.Errorf("ERROR log missing from stderr when legacy_stderr_threshold_behavior=false; got: %q", out)
	}
	// restore legacy default for subsequent tests
	setKlogFlags(t, map[string]string{"legacy_stderr_threshold_behavior": "true", "stderrthreshold": "ERROR"})
}

// TestAlsoLogToStderrThresholdDefaultCompat — COMPAT contract (base AND head).
// When logging to files with alsologtostderr=true, the new
// -alsologtostderrthreshold flag defaults to INFO, which must preserve the old
// behavior of copying every severity to stderr.
func TestAlsoLogToStderrThresholdDefaultCompat(t *testing.T) {
	dir := t.TempDir()
	setKlogFlags(t, map[string]string{
		"logtostderr":     "false",
		"alsologtostderr": "true",
		"log_dir":         dir,
	})
	out := captureStderr(t, func() {
		klog.InfoS("verify-pr6259-alsolog-info", "case", "alsolog-compat")
	})
	if !strings.Contains(out, "verify-pr6259-alsolog-info") {
		t.Errorf("INFO log not copied to stderr with alsologtostderr=true (default threshold); got: %q", out)
	}
	setKlogFlags(t, map[string]string{"logtostderr": "true", "alsologtostderr": "false"})
}

// TestDuplicateKeysDedupLastWins — BEHAVIOR-CHANGE contract (passes on head;
// FAILS on base, documenting the delta delivered by kubernetes/klog#415).
// Duplicate keys within one structured call must be de-duplicated with
// "last one wins". klog 2.130.1 printed both occurrences.
func TestDuplicateKeysDedupLastWins(t *testing.T) {
	setKlogFlags(t, map[string]string{"logtostderr": "true"})
	out := captureStderr(t, func() {
		klog.InfoS("verify-pr6259-dedup", "dup", "first", "dup", "second", "unique", 1)
	})
	if !strings.Contains(out, `dup="second"`) {
		t.Errorf("expected last-wins dup=\"second\" in output; got: %q", out)
	}
	if strings.Contains(out, `dup="first"`) {
		t.Errorf("obsolete dup=\"first\" should have been de-duplicated; got: %q", out)
	}
	if strings.Count(out, "dup=") != 1 {
		t.Errorf("expected exactly one occurrence of dup= key, got %d in: %q", strings.Count(out, "dup="), out)
	}
}

// TestCallSiteOverridesWithValues — COMPAT contract (base AND head).
// Through the logr sink (the path k8s.io/* libraries use), a call-site key
// must override the same key from WithValues. Both klog 2.130.1 (MergeKVs)
// and 2.140.0 (WithValues + format-time dedup) guarantee this.
func TestCallSiteOverridesWithValues(t *testing.T) {
	setKlogFlags(t, map[string]string{"logtostderr": "true"})
	logger := klog.NewKlogr().WithValues("origin", "withvalues")
	out := captureStderr(t, func() {
		logger.Info("verify-pr6259-logr-override", "origin", "callsite")
	})
	if !strings.Contains(out, `origin="callsite"`) {
		t.Errorf("call-site value must win over WithValues; got: %q", out)
	}
	if strings.Contains(out, `origin="withvalues"`) {
		t.Errorf("WithValues value should have been overridden; got: %q", out)
	}
}

// TestOddKeyValueCountMissing — COMPAT contract (base AND head).
// A dangling key must still render as "(MISSING)" rather than panic.
func TestOddKeyValueCountMissing(t *testing.T) {
	setKlogFlags(t, map[string]string{"logtostderr": "true"})
	out := captureStderr(t, func() {
		klog.InfoS("verify-pr6259-odd", "dangling")
	})
	if !strings.Contains(out, `dangling="(MISSING)"`) {
		t.Errorf("expected dangling=\"(MISSING)\" in output; got: %q", out)
	}
}
