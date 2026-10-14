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

package portallocator

// Verification harness for the review of
// https://github.com/fluid-cloudnative/fluid/pull/5392
// (dependabot: bump k8s.io/kubernetes 1.29.15 -> 1.32.10).
//
// Additive test file only — no production code is modified by the harness.
//
// Covered claims:
//   F1 (blocker, contract): the module set chosen by the PR must compile.
//       This package transitively imports k8s.io/apimachinery, so on the PR
//       head this file fails to build with:
//         vendor/k8s.io/apimachinery/pkg/util/managedfields/internal/structuredmerge.go:56:4:
//           unknown field IgnoredFields in struct literal of type merge.Updater
//       (sigs.k8s.io/structured-merge-diff/v4 v4.4.2 renamed the field to
//       IgnoreFilter, but k8s.io/apimachinery stays pinned at v0.29.15).
//   F3 (risk guard, contract): fluid's bitmap allocator intentionally DISCARDS
//       the offset argument that vendored portallocator.New computes
//       (newBitMapAllocator ignores `offset` and calls NewAllocationMap, whose
//       strategy is a random scan from offset 0). Upstream 1.32 removed the
//       ServiceNodePortStaticSubrange feature gate and now always computes a
//       static-subrange offset (calculateRangeOffset: min 16 ports for ranges
//       >16). Because fluid's factory drops that argument, dynamic allocation
//       must still be able to serve the WHOLE range, including the first 16
//       ports of a 256-port range. If a future fix "honors the offset",
//       AllocateNext can never return ports below the subrange and a full-range
//       batch allocation becomes impossible — this test pins current behavior.

import (
	"sort"
	"testing"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/util/net"
)

// TestVerifyRepoCompilesPortallocator is a canary for F1: it has no logic at
// all. Its presence in `go test` output proves the package — and therefore
// k8s.io/apimachinery + sigs.k8s.io/structured-merge-diff — compiled.
func TestVerifyRepoCompilesPortallocator(t *testing.T) {
	t.Log("package pkg/ddc/base/portallocator compiled: F1 module-skew breakage absent")
}

// TestVerifyAllocateBatchCoversFullRange pins F3: every port in the range,
// including the would-be static subrange, must be reachable by dynamic
// allocation (AllocateBatch -> AllocateNext), exactly as with
// k8s.io/kubernetes v1.29.15 (feature gate disabled) and with the v1.32.10
// vendored code (offset computed but discarded by the fluid factory).
//
// Note: on regressed (offset-honoring) code AllocateBatch cannot satisfy the
// full-range request and fluid's AllocateBatch calls os.Exit(1); `go test`
// then reports the package as FAIL (exit status 1) — still a red signal.
func TestVerifyAllocateBatchCoversFullRange(t *testing.T) {
	const base, size = 20000, 256 // offset would be 16 if it were honored
	pr := net.ParsePortRangeOrDie("20000-20255")
	if pr.Size != size {
		t.Fatalf("test setup: range size = %d, want %d", pr.Size, size)
	}
	alloc, err := newBitMapAllocator(pr, logr.Discard())
	if err != nil {
		t.Fatalf("newBitMapAllocator failed: %v", err)
	}
	ports, err := alloc.AllocateBatch(size)
	if err != nil {
		t.Fatalf("AllocateBatch(%d) failed: %v", size, err)
	}
	sort.Ints(ports)
	for i := 0; i < size; i++ {
		if ports[i] != base+i {
			t.Fatalf("dynamic allocation did not cover the full range: sorted ports[%d]=%d, want %d (upstream 1.32 static-subrange offset must stay discarded by the fluid factory)", i, ports[i], base+i)
		}
	}
	_ = base
}
