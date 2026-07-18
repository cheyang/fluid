/*
Copyright 2024 The Fluid Authors.

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

// This file is an ADDITIVE verification harness for the review of
// https://github.com/fluid-cloudnative/fluid/pull/6064
// It does not modify any production code. See docs/verification/cache-engine-selector/.
//
// It proves two findings from that review:
//
//   FINDING 1 (contract, expect PASS on PR head):
//     getWorkerSelectors() produces a label-selector string that actually MATCHES
//     the labels applied to CacheRuntime worker pods. The worker-pod side of that
//     scheme is pinned independently in the sibling component package test
//     (component/selector_verify_test.go: TestVerifyWorkerCommonLabelScheme), so a
//     drift on either side turns one of these tests red.
//
//   FINDING 2 (canary, expect PASS on PR head, must FLIP when the gap is closed):
//     Status.Selector is populated by the PR, but nothing consumes it for
//     CacheRuntime: the generated CacheRuntime CRD has no `scale` subresource with
//     selectorpath=.status.selector, unlike alluxio/juicefs/vineyard. This test
//     PASSES while the gap exists and FLIPS to fail once a scale subresource is
//     added (at which point it should be inverted / promoted to a contract test).

package engine

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/fluid-cloudnative/fluid/pkg/common"
	"k8s.io/apimachinery/pkg/labels"
)

// repoRoot walks up from this test file (pkg/ddc/cache/engine) to the module root,
// so CRD-file reads work regardless of the test's working directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve caller path")
	}
	// pkg/ddc/cache/engine/<file> -> up 4 dirs to repo root
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", "..", ".."))
}

// FINDING 1 — contract. The selector string emitted by getWorkerSelectors() must
// match the exact label set that CacheRuntime worker pods carry.
func TestVerifyWorkerSelectorMatchesWorkerLabels(t *testing.T) {
	const name = "test-runtime"
	e := &CacheEngine{name: name, namespace: "default"}

	selStr := e.getWorkerSelectors()
	sel, err := labels.Parse(selStr)
	if err != nil {
		t.Fatalf("getWorkerSelectors() returned an unparseable selector %q: %v", selStr, err)
	}

	// The labels a worker pod/STS actually carries — same scheme as
	// component.getCommonLabelsFromComponent for the worker component, where
	// Owner.Name == runtime name and component.Name == GetCacheComponentName(name, worker).
	workerLabels := labels.Set{
		common.LabelCacheRuntimeName:          name,
		common.LabelCacheRuntimeComponentName: common.GetCacheComponentName(name, common.ComponentTypeWorker),
	}
	if !sel.Matches(workerLabels) {
		t.Fatalf("selector %q does NOT match worker pod labels %v — HPA/tooling would find no worker pods", selStr, workerLabels)
	}

	// Specificity: it must NOT match a master pod (different component-name), otherwise
	// the selector would be too broad and pull in the wrong component's pods.
	masterLabels := labels.Set{
		common.LabelCacheRuntimeName:          name,
		common.LabelCacheRuntimeComponentName: common.GetCacheComponentName(name, common.ComponentTypeMaster),
	}
	if sel.Matches(masterLabels) {
		t.Fatalf("selector %q incorrectly matches MASTER pod labels %v — should be worker-specific", selStr, masterLabels)
	}

	// It must also not match pods of a different runtime.
	otherRuntime := labels.Set{
		common.LabelCacheRuntimeName:          "other-runtime",
		common.LabelCacheRuntimeComponentName: common.GetCacheComponentName("other-runtime", common.ComponentTypeWorker),
	}
	if sel.Matches(otherRuntime) {
		t.Fatalf("selector %q matches a DIFFERENT runtime's worker pods %v", selStr, otherRuntime)
	}
}

// FINDING 2 — canary. The CacheRuntime CRD currently has NO scale subresource, so the
// freshly-populated Status.Selector is inert for HPA. Passes while the gap exists;
// FLIPS to fail (and should then be inverted) once a scale subresource is wired in.
func TestVerifyCacheRuntimeHasNoScaleSubresource(t *testing.T) {
	root := repoRoot(t)
	crdDir := filepath.Join(root, "config", "crd", "bases")

	cacheCRD := filepath.Join(crdDir, "data.fluid.io_cacheruntimes.yaml")
	alluxioCRD := filepath.Join(crdDir, "data.fluid.io_alluxioruntimes.yaml")

	cacheBytes, err := os.ReadFile(cacheCRD)
	if err != nil {
		t.Fatalf("cannot read CacheRuntime CRD %s: %v", cacheCRD, err)
	}
	alluxioBytes, err := os.ReadFile(alluxioCRD)
	if err != nil {
		t.Fatalf("cannot read AlluxioRuntime CRD %s: %v", alluxioCRD, err)
	}

	// Sanity anchor: a peer runtime DOES wire scale->selector, proving this check would
	// detect the marker if CacheRuntime had it. If this anchor ever fails, the check is
	// broken (not the finding), so fail loudly.
	if !strings.Contains(string(alluxioBytes), "scale:") {
		t.Fatalf("anchor broken: AlluxioRuntime CRD unexpectedly has no `scale:` subresource — the check can no longer distinguish presence from absence")
	}

	// The finding: CacheRuntime has no scale subresource yet.
	if strings.Contains(string(cacheBytes), "scale:") {
		t.Fatalf("FINDING 2 FLIPPED: CacheRuntime CRD now HAS a `scale:` subresource. " +
			"Status.Selector is no longer inert — invert this canary (or promote it to a contract test " +
			"asserting selectorpath=.status.selector).")
	}
}
