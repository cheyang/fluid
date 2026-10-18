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

// Verification harness for PR #6186 review finding F3 (reviewer: codex).
// Additive test file only - production code is NOT modified.
//
// F3 (round 3 update): the round-2 harness probed logIncompleteVersion, which logged at
// V(1) and was therefore invisible under the shipped --development=false config with no
// verbosity flag. Commit 8430488e replaced it with reportIncompleteVersion: the log moved
// to V(0) and a RuntimeVersionIncomplete Warning event is emitted on the CacheRuntime
// (extended to the creation path by a1bf08bb). The harness is updated additively to the
// new symbol; the contracts are unchanged in substance:
//   - the discarded-edit signal must be visible under the shipped production logging config
//   - issue #6178's fallback ask ("at minimum a Warning event") must be met
//
// The create-path and update-path event emission is additionally pinned by the PR's own
// specs (transform_worker_test.go, sync_test.go), which run in the same suite.
package engine

import (
	"bytes"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	ctrlzap "sigs.k8s.io/controller-runtime/pkg/log/zap"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
)

var _ = Describe("PR6186 F3 discarded-edit signal visibility", Label("verify-pr6186"), func() {
	// A tag-only version against a digest-pinned template cannot be completed, so
	// reportIncompleteVersion is the only signal that the edit is discarded.
	const digestTemplate = "fluid/cache@sha256:067614b70d25b496e3edc3480747d558ee8a364ef47a67f669f5d96ca5098552"
	partialVersion := datav1alpha1.VersionSpec{ImageTag: "v9"}

	logsUnder := func(devMode bool, version datav1alpha1.VersionSpec, templateImage string) string {
		var buf bytes.Buffer
		e := &CacheEngine{Log: ctrlzap.New(ctrlzap.UseDevMode(devMode), ctrlzap.WriteTo(&buf))}
		e.reportIncompleteVersion(nil, common.ComponentTypeWorker, version, templateImage)
		return buf.String()
	}

	It("F3 [contract]: the shipped production config (--development=false) emits the discarded-edit signal in the log", func() {
		// charts pin --development=false => zap.InfoLevel; the record must be written at V(0).
		Expect(logsUnder(false, partialVersion, digestTemplate)).To(ContainSubstring("cannot be completed"))
	})

	It("F3 [contract]: a Warning event reports the discarded edit on the CacheRuntime", func() {
		recorder := record.NewFakeRecorder(10)
		e := &CacheEngine{Log: logr.Discard(), Recorder: recorder}
		runtime := &datav1alpha1.CacheRuntime{ObjectMeta: metav1.ObjectMeta{Name: "bugtest", Namespace: "bugtest"}}

		e.reportIncompleteVersion(runtime, common.ComponentTypeWorker, partialVersion, digestTemplate)

		Expect(recorder.Events).To(Receive(And(
			HavePrefix(corev1.EventTypeWarning+" "+common.RuntimeVersionIncomplete+" "),
			ContainSubstring(`imageTag "v9"`))))
		Expect(recorder.Events).To(BeEmpty())
	})

	It("F3 [control]: a partial version the template completes emits neither signal", func() {
		Expect(logsUnder(false, partialVersion, "fluid/cache:v1")).To(BeEmpty())

		recorder := record.NewFakeRecorder(10)
		e := &CacheEngine{Log: logr.Discard(), Recorder: recorder}
		e.reportIncompleteVersion(&datav1alpha1.CacheRuntime{}, common.ComponentTypeWorker, partialVersion, "fluid/cache:v1")
		Expect(recorder.Events).To(BeEmpty())
	})
})
