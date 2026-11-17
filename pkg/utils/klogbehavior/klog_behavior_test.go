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

// Package klogbehavior holds the reviewer verification harness for the
// k8s.io/klog/v2 dependency bump (2.130.1 -> 2.140.0) in PR
// https://github.com/fluid-cloudnative/fluid/pull/6259.
//
// Fluid does not import klog directly (except hack/sdk/main.go); klog is
// exercised transitively by k8s.io/client-go, k8s.io/component-base and
// controller-runtime. Therefore the contract under test is: "the vendored
// klog upgrade must not change the log-routing behavior fluid's components
// and their k8s dependencies relied on, except for the documented
// intentional change (key/value de-duplication)".
//
// Polarity: all tests below are CONTRACT tests (fail = regression).
// They pass on klog v2.130.1 (base) as well as v2.140.0 (head), except
// TestStructuredKVDeDuplication which encodes the *new intended* v2.140.0
// behavior; it fails on v2.130.1 by design and is used as the
// harness-bites proof.
package klogbehavior

import (
	"flag"
	"os"
	"strings"
	"testing"

	klog "k8s.io/klog/v2"
)

// setKlogFlags parses klog flags through the public klog.InitFlags API.
// No fluid binary registers klog flags on its command line, so in
// production all of these remain at their defaults; the tests below
// deviate from defaults only to prove the routing contract.
func setKlogFlags(t *testing.T, args ...string) {
	t.Helper()
	fs := flag.NewFlagSet("klog-harness", flag.ContinueOnError)
	klog.InitFlags(fs)
	if err := fs.Parse(args); err != nil {
		t.Fatalf("parsing klog flags %v: %v", args, err)
	}
}

// captureStderr swaps the os.Stderr package variable (which klog resolves
// at write time when logging to stderr) with a temp file.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "stderr-*")
	if err != nil {
		t.Fatalf("creating stderr capture file: %v", err)
	}
	old := os.Stderr
	os.Stderr = f
	defer func() {
		os.Stderr = old
		f.Close()
	}()
	fn()
	klog.Flush()
	if err := f.Sync(); err != nil {
		t.Fatalf("syncing stderr capture file: %v", err)
	}
	data, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatalf("reading stderr capture file: %v", err)
	}
	return string(data)
}

// K1 (contract): with the default logtostderr=true, logs of every severity
// reach stderr, and -stderrthreshold does not filter them (legacy behavior
// is preserved by the new legacy_stderr_threshold_behavior=true default in
// klog v2.140.0). This is the routing every fluid binary relies on, since
// none of them configure klog flags.
func TestDefaultLogToStderrRoutesAllSeverities(t *testing.T) {
	setKlogFlags(t, "-logtostderr=true", "-stderrthreshold=FATAL")
	out := captureStderr(t, func() {
		klog.Info("K1 info message")
		klog.Error("K1 error message")
	})
	if !strings.Contains(out, "K1 info message") {
		t.Errorf("INFO log line missing from stderr (legacy logtostderr routing broken):\n%s", out)
	}
	if !strings.Contains(out, "K1 error message") {
		t.Errorf("ERROR log line missing from stderr:\n%s", out)
	}
}

// K2 (contract): with logtostderr=false and alsologtostderr=true, INFO-level
// messages still reach stderr. klog v2.140.0 rewrote this condition from
// `alsoToStderr || l.alsoToStderr || s >= stderrThreshold` to
// `alsoToStderr || (l.alsoToStderr && s >= alsologtostderrthreshold) || ...`;
// with the v2.140.0 default alsologtostderrthreshold=INFO the observable
// behavior must be identical.
func TestAlsoLogToStderrKeepsInfoOnStderr(t *testing.T) {
	setKlogFlags(t, "-logtostderr=false", "-alsologtostderr=true", "-alsologtostderrthreshold=INFO", "-log_dir="+t.TempDir())
	out := captureStderr(t, func() {
		klog.Info("K2 info message")
	})
	if !strings.Contains(out, "K2 info message") {
		t.Errorf("INFO log line missing from stderr with -alsologtostderr=true (default alsologtostderrthreshold regression):\n%s", out)
	}
}

// K3 (contract): with logtostderr=false and alsologtostderr=false, only
// messages at or above the default stderrthreshold (ERROR) reach stderr.
// INFO must NOT appear. Same expectation on v2.130.1 and v2.140.0.
func TestFileModeStderrThresholdStillFilters(t *testing.T) {
	setKlogFlags(t, "-logtostderr=false", "-alsologtostderr=false", "-stderrthreshold=ERROR", "-log_dir="+t.TempDir())
	out := captureStderr(t, func() {
		klog.Info("K3 info message")
		klog.Error("K3 error message")
	})
	if strings.Contains(out, "K3 info message") {
		t.Errorf("INFO log line reached stderr although stderrthreshold=ERROR:\n%s", out)
	}
	if !strings.Contains(out, "K3 error message") {
		t.Errorf("ERROR log line missing from stderr although stderrthreshold=ERROR:\n%s", out)
	}
}

// K4 (contract, encodes the intended v2.140.0 change): duplicate keys in
// structured logging are de-duplicated, last one wins. klog v2.130.1
// emitted both pairs; v2.140.0 must emit exactly one. This test FAILS on
// the base (v2.130.1) by design - that asymmetry is the proof the harness
// actually exercises the changed code path (harness-bites check).
func TestStructuredKVDeDuplication(t *testing.T) {
	setKlogFlags(t, "-logtostderr=true")
	out := captureStderr(t, func() {
		klog.InfoS("K4 dedup message", "key", 1, "key", 2)
	})
	if got := strings.Count(out, "key="); got != 1 {
		t.Errorf("expected exactly 1 key= pair after de-duplication, got %d:\n%s", got, out)
	}
	if !strings.Contains(out, "key=2") {
		t.Errorf("last value should win after de-duplication, want key=2:\n%s", out)
	}
}

// K5 (contract): error key/value pairs emitted for klog.ErrorS(err, ...) are
// still rendered (format change from KVListFormat to FormatKVs must not
// drop the "err" pair).
func TestErrorSPrintsErrKeyValue(t *testing.T) {
	setKlogFlags(t, "-logtostderr=true")
	out := captureStderr(t, func() {
		klog.ErrorS(nil, "K5 errorS message", "foo", "bar")
	})
	if !strings.Contains(out, "K5 errorS message") {
		t.Errorf("ErrorS message missing from stderr:\n%s", out)
	}
	if !strings.Contains(out, `foo="bar"`) {
		t.Errorf(`foo="bar" key/value pair missing from ErrorS output (values are quoted by klog's serializer):\n%s`, out)
	}
}
