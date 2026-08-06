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

// Verification harness for https://github.com/fluid-cloudnative/fluid/pull/6152
//
// LAYER: unit (L1) — POLARITY: mechanism (green both before and after the chart fix)
//
// This layer does not discriminate the fix; the fix lives in the Helm chart, not in Go.
// What it proves is the *impact* half of the finding: when the only exported env var is
// the misspelled one the pre-fix chart emitted, the operator-configured threshold is
// silently discarded and FuseRecover falls back to defaultRecoverWarningThreshold.
// The discriminating tests live in the render layer (L2).

package recover

import (
	"testing"

	"github.com/fluid-cloudnative/fluid/pkg/utils"
)

// misspelledEnvName is the env var name the chart emitted before PR #6152.
// It is deliberately hard-coded rather than imported: no Go constant ever held it,
// which is precisely why the chart value was dropped on the floor.
const misspelledEnvName = "REVOCER_WARNING_THRESHOLD"

// resolveThreshold mirrors the threshold resolution in NewFuseRecover verbatim.
// NewFuseRecover itself needs a mount root and a kube client, so the arithmetic is
// re-expressed here rather than driving the whole constructor.
func resolveThreshold() int {
	threshold, found := utils.GetIntValueFromEnv(RecoverWarningThreshold)
	if !found {
		return defaultRecoverWarningThreshold
	}
	return threshold
}

// TestThresholdDroppedWhenOnlyMisspelledEnvIsSet proves the impact of the chart typo:
// an operator who sets csi.recoverWarningThreshold=100 in values.yaml gets 50.
func TestThresholdDroppedWhenOnlyMisspelledEnvIsSet(t *testing.T) {
	// Exactly what the pre-fix chart produced for `--set csi.recoverWarningThreshold=100`.
	t.Setenv(misspelledEnvName, "100")

	got := resolveThreshold()

	if got != defaultRecoverWarningThreshold {
		t.Fatalf("expected the misspelled env var to be ignored and the default %d to be used, got %d",
			defaultRecoverWarningThreshold, got)
	}
	// The operator asked for 100 and silently received the default.
	t.Logf("configured=100 via %s -> effective threshold=%d (operator intent discarded)",
		misspelledEnvName, got)
}

// TestThresholdHonouredWhenCorrectEnvIsSet is the other half: the name the Go code
// actually reads does take effect, so the chart is the only thing that was broken.
func TestThresholdHonouredWhenCorrectEnvIsSet(t *testing.T) {
	t.Setenv(RecoverWarningThreshold, "100")

	got := resolveThreshold()

	if got != 100 {
		t.Fatalf("expected %s=100 to be honoured, got %d", RecoverWarningThreshold, got)
	}
}

// TestNoGoConstantHoldsTheMisspelledName pins the asymmetry that made the bug silent:
// the two names differ, so nothing in Go was ever going to pick up the chart's value.
func TestNoGoConstantHoldsTheMisspelledName(t *testing.T) {
	if RecoverWarningThreshold == misspelledEnvName {
		t.Fatalf("test premise is stale: the Go constant now equals the misspelled chart name %q",
			misspelledEnvName)
	}
	if RecoverWarningThreshold != "RECOVER_WARNING_THRESHOLD" {
		t.Fatalf("the Go-side env var name changed to %q; the chart must be updated to match",
			RecoverWarningThreshold)
	}
}
