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
// F3: the only signal for a partial runtimeVersion the template cannot complete is
// e.Log.V(1) in logIncompleteVersion (pkg/ddc/cache/engine/transform_common.go). The
// shipped chart pins --development=false
// (charts/fluid/fluid/templates/controller/cacheruntime_controller.yaml), which selects
// zap.InfoLevel via the vendored controller-runtime zap defaults, so V(1) records are
// dropped. The binary registers no zap.BindFlags/klog flags, so an operator cannot raise
// verbosity at runtime either. The discarded edit is therefore still silent in any
// default deployment, and the docs' "logged when the controller runs with -v=1" is not
// actionable.
package engine

import (
	"bytes"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	ctrlzap "sigs.k8s.io/controller-runtime/pkg/log/zap"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
)

var _ = Describe("PR6186 F3 discarded-edit signal visibility", Label("verify-pr6186"), func() {
	// A tag-only version against a digest-pinned template cannot be completed, so
	// logIncompleteVersion is the only signal that the edit is discarded.
	const digestTemplate = "fluid/cache@sha256:067614b70d25b496e3edc3480747d558ee8a364ef47a67f669f5d96ca5098552"
	partialVersion := datav1alpha1.VersionSpec{ImageTag: "v9"}

	logsUnder := func(devMode bool) string {
		var buf bytes.Buffer
		e := &CacheEngine{Log: ctrlzap.New(ctrlzap.UseDevMode(devMode), ctrlzap.WriteTo(&buf))}
		e.logIncompleteVersion(common.ComponentTypeWorker, partialVersion, digestTemplate)
		return buf.String()
	}

	It("F3 [contract]: the shipped production config (--development=false) emits the discarded-edit signal", func() {
		// charts pin --development=false => zap.InfoLevel => V(1) dropped.
		Expect(logsUnder(false)).To(ContainSubstring("cannot be completed"))
	})

	It("F3 [control]: development mode emits the same record, so the gap is the level, not the call path", func() {
		Expect(logsUnder(true)).To(ContainSubstring("cannot be completed"))
	})
})
