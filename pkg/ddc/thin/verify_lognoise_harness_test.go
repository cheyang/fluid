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

// Verification harness for PR #6233 (issue #6203).
//
// POLARITY: contract tests. They assert the INTENDED behavior:
//   - a pending (not-yet-bound) pvc:// mount must NOT produce Error-level
//     logs from ShouldUpdateUFS;
//   - an unexpected failure (e.g. PVC not found) MUST still produce an
//     Error-level log.
// On the PR base (a01c038, pre-fix) the first contract FAILS -- that red run
// is the P0 premise reproduction. On the PR head it PASSES.
//
// This file deliberately avoids referencing ErrPVCNotBound so that it also
// compiles on the base branch (needed for the P0 differential run).
package thin

import (
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
)

// verifyLogSink captures Error/Info calls so tests can assert log levels.
// (Separate type name from the PR's testCaptureLogSink to avoid collision.)
type verifyLogSink struct {
	infoMsgs  []string
	infoLvls  []int
	errorMsgs []string
	errors    []error
}

func (s *verifyLogSink) Init(info logr.RuntimeInfo) {}
func (s *verifyLogSink) Enabled(level int) bool     { return true }
func (s *verifyLogSink) Info(level int, msg string, keysAndValues ...interface{}) {
	s.infoMsgs = append(s.infoMsgs, msg)
	s.infoLvls = append(s.infoLvls, level)
}
func (s *verifyLogSink) Error(err error, msg string, keysAndValues ...interface{}) {
	s.errorMsgs = append(s.errorMsgs, msg)
	s.errors = append(s.errors, err)
}
func (s *verifyLogSink) WithValues(keysAndValues ...interface{}) logr.LogSink { return s }
func (s *verifyLogSink) WithName(name string) logr.LogSink                    { return s }

func verifyNewPendingPVCEngine(t *testing.T, pvcExists bool) (*ThinEngine, *verifyLogSink) {
	t.Setenv(EnvFuseConfigStorage, "configmap")

	runtime := &datav1alpha1.ThinRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: "verify-pending", Namespace: "fluid"},
	}
	dataset := &datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: "verify-pending", Namespace: "fluid"},
		Spec: datav1alpha1.DatasetSpec{
			Mounts: []datav1alpha1.Mount{
				{Name: "my-pvc", MountPoint: "pvc://my-pvc"},
			},
		},
	}
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "verify-pending-fuse-conf", Namespace: "fluid"},
		Data:       map[string]string{"config.json": "{}"},
	}
	pendingPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "my-pvc", Namespace: "fluid"},
		Status: corev1.PersistentVolumeClaimStatus{
			Phase: corev1.ClaimPending,
		},
	}

	objs := []k8sruntime.Object{runtime, dataset, configMap}
	if pvcExists {
		objs = append(objs, pendingPVC)
	}

	sink := &verifyLogSink{}
	engine := &ThinEngine{
		name:      "verify-pending",
		namespace: "fluid",
		runtime:   runtime,
		Client:    fake.NewFakeClientWithScheme(testScheme, objs...),
		Log:       logr.New(sink),
	}
	return engine, sink
}

// P0 premise + fix contract: pending (not-yet-bound) PVC must not log at Error level.
// RED on base (that red is the issue #6203 reproduction), GREEN on PR head.
func TestHarnessP0_PendingPVCNoErrorLog(t *testing.T) {
	engine, sink := verifyNewPendingPVCEngine(t, true)

	ufsToUpdate := engine.ShouldUpdateUFS()
	if ufsToUpdate != nil {
		t.Errorf("expected nil ufsToUpdate while PVC pending, got %+v", ufsToUpdate)
	}
	if len(sink.errorMsgs) != 0 {
		t.Errorf("P0: expected NO Error-level log for a not-yet-bound PVC, got %d: %v (errors: %v)",
			len(sink.errorMsgs), sink.errorMsgs, sink.errors)
	}
}

// Contract: the demoted message must be emitted at V(1) Info so operators can
// still see the transient state when debug logging is on.
// RED on base (nothing but the Error log), GREEN on PR head.
func TestHarnessP0_PendingPVCLogsV1Info(t *testing.T) {
	engine, sink := verifyNewPendingPVCEngine(t, true)

	engine.ShouldUpdateUFS()
	for i, msg := range sink.infoMsgs {
		if sink.infoLvls[i] >= 1 && msg == "PVC is not bounded yet, will update fuse config once bounded" {
			return
		}
	}
	t.Errorf("expected a V(>=1) Info log \"PVC is not bounded yet, will update fuse config once bounded\", got infoMsgs=%v infoLvls=%v",
		sink.infoMsgs, sink.infoLvls)
}

// Contract (both base and head): unexpected errors -- e.g. the PVC does not
// exist at all -- must still be logged at Error level.
func TestHarness_UnexpectedErrorStillErrorLog(t *testing.T) {
	engine, sink := verifyNewPendingPVCEngine(t, false)

	ufsToUpdate := engine.ShouldUpdateUFS()
	if ufsToUpdate != nil {
		t.Errorf("expected nil ufsToUpdate for nonexistent PVC, got %+v", ufsToUpdate)
	}
	found := false
	for _, msg := range sink.errorMsgs {
		if msg == "Failed to update fuse config" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an Error-level \"Failed to update fuse config\" log for a nonexistent PVC, got errorMsgs=%v", sink.errorMsgs)
	}
}
