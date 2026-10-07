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

package thin

// Reviewer verification harness for PR #6187
// (https://github.com/fluid-cloudnative/fluid/pull/6187).
//
// Claim under test (P0/F1, polarity: CONTRACT, layer: unit):
//   When a Dataset mount references a PVC that does not exist, the
//   transformFuseConfig failure inside updateFuseConfigOnChange must be
//   surfaced by ShouldUpdateUFS as an error log ("Failed to update fuse
//   config") carrying the PVC name, and ShouldUpdateUFS must still return a
//   nil *utils.UFSToUpdate (no behavior change beyond observability).
//
// On master (pre-fix) this test FAILS: the error is swallowed
// (return update, nil) and no error log is emitted. With the one-token fix
// (return update, err) it PASSES.

import (
	"strings"
	"testing"

	"github.com/go-logr/logr"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// recordingSink is a minimal logr.LogSink that records Error entries so the
// harness can assert on what ShouldUpdateUFS actually logs. It adds no new
// module dependencies.
type recordingSink struct {
	errorEntries []string // "msg: err"
}

func (s *recordingSink) Init(logr.RuntimeInfo)  {}
func (s *recordingSink) Enabled(level int) bool { return true }
func (s *recordingSink) Info(level int, msg string, keysAndValues ...interface{}) {
}
func (s *recordingSink) Error(err error, msg string, keysAndValues ...interface{}) {
	errStr := "<nil>"
	if err != nil {
		errStr = err.Error()
	}
	s.errorEntries = append(s.errorEntries, msg+": "+errStr)
}
func (s *recordingSink) WithValues(keysAndValues ...interface{}) logr.LogSink { return s }
func (s *recordingSink) WithName(name string) logr.LogSink                    { return s }

// TestVerifyClaude_ShouldUpdateUFSLogsTransformFailure is the contract test
// for PR #6187: the transform failure must reach the operator as an error log.
func TestVerifyClaude_ShouldUpdateUFSLogsTransformFailure(t *testing.T) {
	dataset := &datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: datav1alpha1.DatasetSpec{
			Mounts: []datav1alpha1.Mount{
				{
					MountPoint: "pvc://missing-pvc",
					Name:       "missing-pvc",
				},
			},
		},
	}
	// fuse configmap must exist so updateFuseConfigOnChange reaches
	// transformFuseConfig instead of returning early.
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-fuse-conf",
			Namespace: "default",
		},
		Data: map[string]string{
			"config.json": "{\"mounts\":[],\"targetPath\":\"/thin/default/test/thin-fuse\",\"accessModes\":[\"ReadOnlyMany\"]}",
		},
	}
	thinruntime := &datav1alpha1.ThinRuntime{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
	}

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = datav1alpha1.AddToScheme(scheme)
	client := fake.NewFakeClientWithScheme(scheme, dataset, cm)

	sink := &recordingSink{}
	thinEngine := ThinEngine{
		Client:    client,
		name:      "test",
		namespace: "default",
		runtime:   thinruntime,
		Log:       logr.New(sink),
	}

	ufsToUpdate := thinEngine.ShouldUpdateUFS()

	// No behavior change: ufsToUpdate stays nil on this failure path.
	if ufsToUpdate != nil {
		t.Errorf("expected nil ufsToUpdate on transform failure, got %+v", ufsToUpdate)
	}

	// Observability fix: the failure must be logged with the PVC name.
	found := false
	for _, e := range sink.errorEntries {
		if strings.Contains(e, "Failed to update fuse config") && strings.Contains(e, "missing-pvc") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an error log 'Failed to update fuse config' mentioning missing-pvc, got error entries: %+v", sink.errorEntries)
	}
}
