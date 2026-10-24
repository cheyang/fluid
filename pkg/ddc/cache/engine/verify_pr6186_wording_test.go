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

// Verification harness for PR #6186 review finding N2 (reviewer: codex).
// Additive test file only - production code is NOT modified.
//
// N2: reportIncompleteVersion is shared by the creation path (transformMaster/
// transformWorker/transformClient) and the update path (syncRuntimeSpec). At
// 902625af its log said "leaving the image unchanged" and its event ended "so
// the image is left unchanged" - which only holds on update: on creation there
// is no prior image, the component is created from the template image. The
// signal must be worded neutrally so it reads correctly on both paths. Commit
// 12d9b900 reworded both to say the version "is ignored".
//
// Polarity: contract. RED on 902625af (message claims "unchanged"), GREEN on
// 12d9b900 ("ignored", no "unchanged" claim).
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

var _ = Describe("PR6186 N2 incomplete-version signal wording", Label("verify-pr6186"), func() {
	const digestTemplate = "fluid/cache@sha256:067614b70d25b496e3edc3480747d558ee8a364ef47a67f669f5d96ca5098552"
	partialVersion := datav1alpha1.VersionSpec{ImageTag: "v9"}

	It("N2 [contract]: the discarded-edit log does not claim the image is left unchanged", func() {
		var buf bytes.Buffer
		e := &CacheEngine{Log: ctrlzap.New(ctrlzap.UseDevMode(false), ctrlzap.WriteTo(&buf))}

		e.reportIncompleteVersion(nil, common.ComponentTypeWorker, partialVersion, digestTemplate)

		// Creation path has no prior image, so "unchanged" is wrong there; the
		// neutral wording ("ignored") reads correctly on both paths.
		Expect(buf.String()).NotTo(BeEmpty(), "the signal must still be logged")
		Expect(buf.String()).NotTo(ContainSubstring("unchanged"))
		Expect(buf.String()).To(ContainSubstring("ignoring"))
	})

	It("N2 [contract]: the discarded-edit event does not claim the image is left unchanged", func() {
		recorder := record.NewFakeRecorder(10)
		e := &CacheEngine{Log: logr.Discard(), Recorder: recorder}
		runtime := &datav1alpha1.CacheRuntime{ObjectMeta: metav1.ObjectMeta{Name: "bugtest", Namespace: "bugtest"}}

		e.reportIncompleteVersion(runtime, common.ComponentTypeWorker, partialVersion, digestTemplate)

		Expect(recorder.Events).To(Receive(And(
			HavePrefix(corev1.EventTypeWarning+" "+common.RuntimeVersionIncomplete+" "),
			Not(ContainSubstring("unchanged")),
			ContainSubstring("ignored"))))
		Expect(recorder.Events).To(BeEmpty())
	})
})
