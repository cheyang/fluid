// Verification harness addition for round 2 of
// https://github.com/fluid-cloudnative/fluid/pull/6206 (reviewer finding F2).
//
// Additive reviewer harness only — no production code is modified.
//
// Round-1 finding F2: the widened recovery trigger (len(gvk.Group) == 0) also ran the
// fluidScheme lookup for fully-typed core-group objects (e.g. Pod with apiVersion "v1"),
// which always fails and logged a misleading error ("the generated ownerReference stays
// incomplete") for an object whose reference is in fact complete. Head 0daf2ba0 gates the
// log on the emitted reference actually being incomplete (kind or version still empty).
//
// Polarity legend:
//
//	CONTRACT  — asserts the intended (fixed) behavior. The log noise did not exist on
//	            base e4a10bf (narrow trigger), was introduced by the previous head
//	            0fa32294 (widened trigger, unconditional log), so this test PASSES on
//	            base, FAILS on 0fa32294 (reproducing F2), PASSES on head 0daf2ba0.
//	GUARD     — asserts behavior that must stay identical across base/prev/head.
//
// Layer: L1 (deterministic unit).
package transformer

import (
	"strings"
	"sync"
	"testing"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// errorCaptureSink is a minimal logr.LogSink that records every Error call.
// ctrl.Log is a delegating logger; ctrl.SetLogger fulfills it exactly once, so all
// tests in this file share a single sink installed via sharedErrorSink and reset it
// before use. (These tests never run in parallel.)
type errorCaptureSink struct {
	mu     sync.Mutex
	errors []string
}

func (s *errorCaptureSink) Init(info logr.RuntimeInfo)                               {}
func (s *errorCaptureSink) Enabled(level int) bool                                   { return true }
func (s *errorCaptureSink) Info(level int, msg string, keysAndValues ...interface{}) {}
func (s *errorCaptureSink) WithValues(keysAndValues ...interface{}) logr.LogSink     { return s }
func (s *errorCaptureSink) WithName(name string) logr.LogSink                        { return s }
func (s *errorCaptureSink) Error(err error, msg string, keysAndValues ...interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.errors = append(s.errors, msg)
}

func (s *errorCaptureSink) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.errors = nil
}

func (s *errorCaptureSink) sawIncompleteOwnerRefLog() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.errors {
		if strings.Contains(m, "ownerReference stays incomplete") {
			return true
		}
	}
	return false
}

var (
	sharedSink     = &errorCaptureSink{}
	sharedSinkOnce sync.Once
)

func sharedErrorSink() *errorCaptureSink {
	sharedSinkOnce.Do(func() { ctrl.SetLogger(logr.New(sharedSink)) })
	sharedSink.reset()
	return sharedSink
}

// TestVerifyPR6206CoreTypeCompleteTypeMetaNoErrorLog is CONTRACT polarity for the F2 fix:
// a core type with a complete (legitimately group-less) TypeMeta must not emit the
// "ownerReference stays incomplete" error log, because its emitted reference is complete.
// The noise was introduced at 0fa32294 (base e4a10bf never logged here), so the
// differential is: base PASS / 0fa32294 FAIL (F2 reproduced) / 0daf2ba0 PASS (fixed).
func TestVerifyPR6206CoreTypeCompleteTypeMetaNoErrorLog(t *testing.T) {
	sink := sharedErrorSink()

	pod := &corev1.Pod{
		TypeMeta:   metav1.TypeMeta{Kind: "Pod", APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default", UID: "uid-p"},
	}
	ref := GenerateOwnerReferenceFromObject(pod)
	t.Logf("generated ownerReference: apiVersion=%q kind=%q", ref.APIVersion, ref.Kind)

	// Output must stay the pre-change shape (guard, unchanged by the fix).
	if ref.APIVersion != "v1" || ref.Kind != "Pod" {
		t.Errorf("reference shape changed: got apiVersion=%q kind=%q, want v1/Pod", ref.APIVersion, ref.Kind)
	}
	if sink.sawIncompleteOwnerRefLog() {
		t.Errorf("misleading error log emitted for a complete core-type TypeMeta: %v", sink.errors)
	}
}

// TestVerifyPR6206KindlessUnregisteredStillLogs is GUARD polarity against over-suppression:
// when the lookup fails AND the emitted reference is actually incomplete (kind empty), the
// error log must still fire. Expected to PASS on base, previous head, and head.
func TestVerifyPR6206KindlessUnregisteredStillLogs(t *testing.T) {
	sink := sharedErrorSink()

	// corev1.Pod is not registered in fluidScheme, so the recovery lookup fails;
	// with Kind empty the emitted reference stays genuinely incomplete.
	pod := &corev1.Pod{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{Name: "kindless", Namespace: "default", UID: "uid-k"},
	}
	ref := GenerateOwnerReferenceFromObject(pod)
	t.Logf("generated ownerReference: apiVersion=%q kind=%q", ref.APIVersion, ref.Kind)

	if ref.Kind != "" {
		t.Errorf("expected kind to stay empty for an unregistered type, got %q", ref.Kind)
	}
	if !sink.sawIncompleteOwnerRefLog() {
		t.Errorf("expected the incomplete-reference error log to still fire when kind is unrecovered; captured: %v", sink.errors)
	}
}
