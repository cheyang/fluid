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

// Additive verification harness for the review of
// https://github.com/fluid-cloudnative/fluid/pull/6061. No production code is
// modified. gomonkey-free so it runs deterministically on darwin/arm64.
// See docs/verification/alluxio-scaledown-e2e/.

package operations

import "testing"

// FINDING F4 — characterization / canary (documents current behavior).
//
// parseActiveWorkerCount counts every non-indented, non-empty line after the
// "Worker Name" header as a worker. If the real `alluxio fsadmin report
// capacity -live` output ever appends a non-indented footer/summary line after
// the worker table, that line is miscounted as an extra live worker. The drain
// completion check in drainScalingDownWorkers is `activeCount <= desired`, so an
// inflated count means the engine never observes the drain as finished and only
// proceeds after defaultWorkerDecommissionDeadline (10m) — ungracefully.
//
// The unit-test sample in decommission_test.go places the worker table last (no
// footer), so this behavior is currently latent. This test pins it: it PASSES
// on the current parser (a footer IS miscounted) and would need revisiting if
// the parser is hardened to ignore trailing summary lines.
func TestVerifyF4ParseActiveWorkerCountTrailingFooter(t *testing.T) {
	reportNoFooter := "" +
		"Worker Name      Last Heartbeat   Storage       MEM\n" +
		"192.168.1.147    0                capacity      2048.00MB\n" +
		"                                 used          443.89MB (21%)\n" +
		"192.168.1.146    0                capacity      2048.00MB\n" +
		"                                 used          0B (0%)\n"

	if n, err := parseActiveWorkerCount(reportNoFooter); err != nil || n != 2 {
		t.Fatalf("baseline: want 2 workers no error, got %d err=%v", n, err)
	}

	// Same report plus a trailing, non-indented summary line.
	reportWithFooter := reportNoFooter + "Total: 2 workers live\n"
	n, err := parseActiveWorkerCount(reportWithFooter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	t.Logf("with a trailing non-indented footer line, parseActiveWorkerCount returns %d (true count 2)", n)
	if n == 3 {
		t.Logf("F4 CONFIRMED (latent): a trailing footer inflates the live-worker count to 3; " +
			"if `report capacity -live` emits any footer, the drain would stall until the deadline and proceed ungracefully")
	}
	// Canary assertion: the parser currently miscounts the footer as a worker.
	// If this ever fails (parser hardened to ignore footers), revisit F4.
	if n != 3 {
		t.Errorf("F4 canary flipped: expected the current parser to miscount the footer (n=3), got n=%d — "+
			"parser may have been hardened; update this finding", n)
	}
}
