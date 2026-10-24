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

package engine

// Reviewer verification harness for PR #6186, round 3: F6 observability.
//
// F6 (round 2): the only signal for a deliberately un-completable partial version
// was logIncompleteVersion at V(1), which the shipped controller never emits
// (chart pins --development=false, zap defaults to InfoLevel, and the binary
// registers no verbosity flag); the docs' "-v=1" hint did not even parse. Round 3
// (8430488e + a1bf08bb) adds a RuntimeVersionIncomplete Warning event on the
// CacheRuntime and logs at the default verbosity.
//
// These specs assert the round-3 behavior through paths that exist on BOTH the
// round-2 head (382d926a) and the round-3 head (902625af) — syncRuntimeSpec with
// the same signature — using string literals rather than the new
// common.RuntimeVersionIncomplete constant, so the file compiles against both:
//   - against 382d926a: both contracts are RED (no event, no default-verbosity log).
//   - against 902625af: both contracts are GREEN.
// The "no event when the template completes the version" spec is a control that
// stays green everywhere.

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/go-logr/logr/funcr"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
)

var _ = Describe("PR-6186 verification: partial runtime version observability", Label("pkg.ddc.cache.engine.version_completion_observability_verify_test.go"), func() {
	const digestTemplate = "verify/cache@sha256:067614b70d25b496e3edc3480747d558ee8a364ef47a67f669f5d96ca5098552"

	Context("F6 signal for an un-completable partial version", func() {
		It("contract: an un-completable partial version emits a RuntimeVersionIncomplete Warning event [red on round-2 head]", func() {
			engine, _, runtimeClass, ctx, client := newPartialVersionFixture(digestTemplate, "verify/cache:v1")
			recorder := record.NewFakeRecorder(10)
			engine.Recorder = recorder
			before := workerImageOf(client, ctx)

			syncWorkerVersion(engine, client, ctx, runtimeClass, datav1alpha1.VersionSpec{ImageTag: "v2"})

			// the drop itself is unchanged: the workload keeps its image
			Expect(workerImageOf(client, ctx)).To(Equal(before))
			// ... but it is now reported as a Warning event on the CacheRuntime
			Expect(recorder.Events).To(Receive(And(
				HavePrefix(corev1.EventTypeWarning+" RuntimeVersionIncomplete "),
				ContainSubstring(`imageTag "v2"`))))
			Expect(recorder.Events).To(BeEmpty())
		})

		It("contract: the incomplete-version log line is emitted at the default verbosity [red on round-2 head]", func() {
			var lines []string
			engine, _, runtimeClass, ctx, client := newPartialVersionFixture(digestTemplate, "verify/cache:v1")
			// funcr with default options: V(0) Info is written, V(1) is not — this is
			// what the shipped controller's zap config effectively enables.
			engine.Log = funcr.New(func(prefix, args string) {
				lines = append(lines, args)
			}, funcr.Options{})

			syncWorkerVersion(engine, client, ctx, runtimeClass, datav1alpha1.VersionSpec{ImageTag: "v2"})

			Expect(lines).To(ContainElement(ContainSubstring("runtimeVersion cannot be completed")))
		})

		It("control: a partial version the template completes emits no event [green everywhere]", func() {
			engine, _, runtimeClass, ctx, client := newPartialVersionFixture("verify/cache:v1", "verify/cache:v1")
			recorder := record.NewFakeRecorder(10)
			engine.Recorder = recorder

			syncWorkerVersion(engine, client, ctx, runtimeClass, datav1alpha1.VersionSpec{ImageTag: "v2"})

			Expect(workerImageOf(client, ctx)).To(Equal("verify/cache:v2"))
			Expect(recorder.Events).To(BeEmpty())
		})
	})

	Context("N2 event wording is true on both the creation and the update path", func() {
		// Round 4 (12d9b900) rewords the message. The spec asserts through the same
		// syncRuntimeSpec chain as the F6 specs; reportIncompleteVersion is the single
		// emission point shared by both paths (sync.go on update, transform_worker.go /
		// transform_master.go / transform_client.go at creation), so a message that
		// claims the image "is left unchanged" — true only on update — misdescribes the
		// creation path, where the component is created from the template image.
		// Red on the round-3 head 902625af, green on 12d9b900.
		It("contract: the RuntimeVersionIncomplete message makes no update-only claim [red on round-3 head]", func() {
			engine, _, runtimeClass, ctx, client := newPartialVersionFixture(digestTemplate, "verify/cache:v1")
			recorder := record.NewFakeRecorder(10)
			engine.Recorder = recorder
			before := workerImageOf(client, ctx)

			syncWorkerVersion(engine, client, ctx, runtimeClass, datav1alpha1.VersionSpec{ImageTag: "v2"})

			// the drop itself is unchanged: the workload keeps its image
			Expect(workerImageOf(client, ctx)).To(Equal(before))
			var msg string
			Expect(recorder.Events).To(Receive(&msg))
			Expect(msg).To(HavePrefix(corev1.EventTypeWarning + " RuntimeVersionIncomplete "))
			Expect(msg).NotTo(ContainSubstring("left unchanged"),
				"the message is also emitted on the creation path, where the component is created from the template image")
			Expect(msg).To(ContainSubstring("is ignored"))
			Expect(recorder.Events).To(BeEmpty())
		})
	})
})
