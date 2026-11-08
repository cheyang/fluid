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

// Verification harness for PR #6233 (reviewer: codex).
// ADDITIVE ONLY — this file must compile against BOTH the base commit
// (a01c038b, pre-PR) and the PR head (952e37ed), so it must not reference
// any symbol introduced by the PR (e.g. ErrPVCNotBound). It exercises only
// public/observable behavior of ThinEngine.ShouldUpdateUFS.
//
// Polarity legend (see docs/verification/pr6233-pvcnotbound-log/):
//   P0a, P0b : premise contract tests — FAIL on base (symptom reproduces),
//              PASS on head (fixed).
//   GUARD    : behavior-preservation contract — PASSes on base AND head.
//   CANARY   : bug-canary for finding F2 — PASSes on head documenting the
//              current (disputed) behavior; must FLIP to red once ClaimLost
//              is kept at Error level.

package thin

import (
	"context"
	"strings"
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
)

// verifyPR6233LogSink records every log record WITH its verbosity level so
// tests can distinguish Error / V(0) Info / V(1)+ Info. (The PR's own
// testCaptureLogSink discards the level — see finding F3.)
type verifyPR6233LogSink struct {
	infoLevels []int
	infoMsgs   []string
	errorMsgs  []string
	errors     []error
}

func (s *verifyPR6233LogSink) Init(info logr.RuntimeInfo) {}
func (s *verifyPR6233LogSink) Enabled(level int) bool     { return true }
func (s *verifyPR6233LogSink) Info(level int, msg string, keysAndValues ...interface{}) {
	s.infoLevels = append(s.infoLevels, level)
	s.infoMsgs = append(s.infoMsgs, msg)
}
func (s *verifyPR6233LogSink) Error(err error, msg string, keysAndValues ...interface{}) {
	s.errorMsgs = append(s.errorMsgs, msg)
	s.errors = append(s.errors, err)
}
func (s *verifyPR6233LogSink) WithValues(keysAndValues ...interface{}) logr.LogSink { return s }
func (s *verifyPR6233LogSink) WithName(name string) logr.LogSink                    { return s }

// newVerifyPR6233Engine builds a ThinEngine whose dataset has a single
// pvc:// mount, with the fuse-conf ConfigMap present (so
// updateFuseConfigOnChange actually reaches transformFuseConfig instead of
// short-circuiting on a missing ConfigMap), plus any extra objects.
func newVerifyPR6233Engine(t *testing.T, name string, extraObjs ...k8sruntime.Object) (*ThinEngine, *verifyPR6233LogSink) {
	t.Helper()
	t.Setenv(EnvFuseConfigStorage, "configmap")

	rt := &datav1alpha1.ThinRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "fluid"},
	}
	ds := &datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "fluid"},
		Spec: datav1alpha1.DatasetSpec{
			Mounts: []datav1alpha1.Mount{{Name: "my-pvc", MountPoint: "pvc://my-pvc"}},
		},
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: name + "-fuse-conf", Namespace: "fluid"},
		Data:       map[string]string{"config.json": "{}"},
	}

	objs := append([]k8sruntime.Object{rt, ds, cm}, extraObjs...)
	sink := &verifyPR6233LogSink{}
	engine := &ThinEngine{
		name:      name,
		namespace: "fluid",
		runtime:   rt,
		Client:    fake.NewFakeClientWithScheme(testScheme, objs...),
		Log:       logr.New(sink),
	}
	return engine, sink
}

func verifyPR6233PVC(name string, phase corev1.PersistentVolumeClaimPhase, volumeName string) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "fluid"},
		Spec:       corev1.PersistentVolumeClaimSpec{VolumeName: volumeName},
		Status:     corev1.PersistentVolumeClaimStatus{Phase: phase},
	}
}

// P0a (contract, premise): a pending PVC must not produce any Error-level log
// from ShouldUpdateUFS. FAILS on base (Error log = the issue #6203 symptom);
// PASSES on head.
func TestVerifyPR6233_Premise_PendingPVCNoErrorLog(t *testing.T) {
	engine, sink := newVerifyPR6233Engine(t, "verify-p0a",
		verifyPR6233PVC("my-pvc", corev1.ClaimPending, ""))

	if got := engine.ShouldUpdateUFS(); got != nil {
		t.Fatalf("ShouldUpdateUFS() = %v, want nil", got)
	}
	if len(sink.errors) != 0 {
		t.Fatalf("pending PVC produced %d Error log records (%v); want 0 — issue #6203 symptom present", len(sink.errors), sink.errorMsgs)
	}
}

// P0b (contract, premise + F3 evidence): the pending-PVC notice must be
// emitted at verbosity level >= 1 (i.e. via Log.V(1).Info, not plain
// Log.Info), so it stays hidden at default verbosity. FAILS on base (no such
// info log exists); PASSES on head. This is the level-aware assertion the
// PR's own capture sink cannot make (F3).
func TestVerifyPR6233_Premise_PendingPVCLogsAtVerboseLevel(t *testing.T) {
	engine, sink := newVerifyPR6233Engine(t, "verify-p0b",
		verifyPR6233PVC("my-pvc", corev1.ClaimPending, ""))

	engine.ShouldUpdateUFS()

	for i, msg := range sink.infoMsgs {
		if strings.Contains(msg, "not bounded yet") {
			if sink.infoLevels[i] < 1 {
				t.Fatalf("pending-PVC notice %q logged at V(%d); want V(1) or higher so default verbosity stays quiet", msg, sink.infoLevels[i])
			}
			return
		}
	}
	t.Fatalf("no 'not bounded yet' info log recorded; head should log it at V(1) (infoMsgs=%v)", sink.infoMsgs)
}

// GUARD (contract, both base and head must PASS): an unexpected error (here:
// the mounted PVC does not exist at all) must still be logged at Error level.
// Guards that the sentinel demotion does not swallow real failures.
func TestVerifyPR6233_Guard_UnexpectedErrorStillErrorLevel(t *testing.T) {
	engine, sink := newVerifyPR6233Engine(t, "verify-guard") // no PVC created

	if got := engine.ShouldUpdateUFS(); got != nil {
		t.Fatalf("ShouldUpdateUFS() = %v, want nil", got)
	}
	if len(sink.errors) == 0 {
		t.Fatalf("missing-PVC failure produced no Error log; want Error-level visibility preserved (infoMsgs=%v)", sink.infoMsgs)
	}
	for _, msg := range sink.errorMsgs {
		if msg != "Failed to update fuse config" {
			t.Fatalf("unexpected Error message %q", msg)
		}
	}
}

// CANARY for finding F2 (asserts CURRENT head behavior; flip expected):
// a PVC in ClaimLost (PV lost after binding — a storage failure, not a
// transient provisioning state) currently takes the ErrPVCNotBound branch and
// is demoted to V(1), i.e. invisible at default verbosity. PASSES on head
// today, FAILS on base (base logs Error). If maintainers agree ClaimLost
// should stay Error-level, this test must flip red and then be inverted.
func TestVerifyPR6233_Canary_LostPVCDemotedToVerboseLog(t *testing.T) {
	engine, sink := newVerifyPR6233Engine(t, "verify-canary",
		verifyPR6233PVC("my-pvc", corev1.ClaimLost, "my-pv"))

	if got := engine.ShouldUpdateUFS(); got != nil {
		t.Fatalf("ShouldUpdateUFS() = %v, want nil", got)
	}
	if len(sink.errors) != 0 {
		t.Fatalf("CANARY FLIPPED: ClaimLost PVC now produces Error logs (%v) — if intentional, invert this canary", sink.errorMsgs)
	}
	foundV1 := false
	for i, msg := range sink.infoMsgs {
		if strings.Contains(msg, "not bounded yet") && sink.infoLevels[i] >= 1 {
			foundV1 = true
		}
	}
	if !foundV1 {
		t.Fatalf("ClaimLost PVC: expected the demoted V(1) notice, got infoMsgs=%v levels=%v", sink.infoMsgs, sink.infoLevels)
	}
	t.Log("CANARY holds: ClaimLost is currently demoted to V(1) together with ClaimPending (see finding F2)")
}

// Compile-time guard: the sink must satisfy logr.LogSink.
var _ logr.LogSink = &verifyPR6233LogSink{}

// keep context import used even if scenarios above change
var _ = context.TODO
