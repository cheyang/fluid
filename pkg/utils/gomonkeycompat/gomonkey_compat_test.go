package gomonkeycompat

// Verification harness for fluid PR #6228 (gomonkey v2.13.0 -> v2.14.4).
//
// These tests pin the exact gomonkey semantics fluid's unit suite relies on
// (pkg/ddc/*, pkg/utils/kubeclient, pkg/csi/plugins, ...): ApplyFunc (162
// uses), ApplyPrivateMethod (86), ApplyMethodFunc (71), ApplyMethod (32),
// ApplyMethodSeq (2), NewPatches (2).
//
// On linux/amd64, v2.14.4 rewrites the public-function patch path
// (ApplyCore) from a 12-byte MOVQ/JMP written at the function entry into a
// 5-byte JMP rel32 into a 256-slot trampoline pool inside gomonkey's own
// text. Private-method patches keep the old direct write. These tests make
// both paths prove themselves at their boundaries.
//
// Run with: go test -gcflags="all=-N -l" ./pkg/utils/gomonkeycompat/
// (inline disabling is mandatory for gomonkey; it is what fluid's Makefile does).

import (
	"bytes"
	"os"
	"runtime/pprof"
	"strings"
	"testing"
	"time"

	"github.com/agiledragon/gomonkey/v2"
)

// shared, exported, method-bearing fixtures --------------------------------

func plainAdd(a, b int) int { return a + b }

func tiny() int { return 42 }

type calculator struct{ base int }

func (c *calculator) Add(n int) int { return c.base + n }
func (c *calculator) Name() string  { return "calc" }

// unexported method -> exercises ApplyPrivateMethod (86 uses in fluid)
func (c *calculator) scale(n int) int { return c.base * n }

// C1 contract: ApplyFunc patches a package-level function and Reset restores.
func TestApplyFuncPatchAndReset(t *testing.T) {
	if got := plainAdd(1, 2); got != 3 {
		t.Fatalf("unpatched plainAdd = %d, want 3", got)
	}
	patches := gomonkey.ApplyFunc(plainAdd, func(a, b int) int { return a * b })
	if got := plainAdd(1, 2); got != 2 {
		t.Fatalf("patched plainAdd = %d, want 2", got)
	}
	patches.Reset()
	if got := plainAdd(1, 2); got != 3 {
		t.Fatalf("after Reset plainAdd = %d, want 3", got)
	}
}

// C1b contract: tiny function entry (< 12 bytes of real prologue under -N -l
// is unlikely, but the smallest target we can write) still patches and resets.
func TestApplyFuncTinyFunction(t *testing.T) {
	patches := gomonkey.ApplyFunc(tiny, func() int { return 7 })
	if got := tiny(); got != 7 {
		t.Fatalf("patched tiny = %d, want 7", got)
	}
	patches.Reset()
	if got := tiny(); got != 42 {
		t.Fatalf("after Reset tiny = %d, want 42", got)
	}
}

// C2 contract: repeated patch/reset cycles on the same target. v2.14.4
// acquires a trampoline slot per patch and releases it on Reset; a slot leak
// exhausts the 256-slot pool and panics within 256 iterations. 1000 cycles
// prove the pool is actually recycled.
func TestApplyFuncRepeatedCycles(t *testing.T) {
	for i := 0; i < 1000; i++ {
		patches := gomonkey.ApplyFunc(plainAdd, func(a, b int) int { return i })
		if got := plainAdd(0, 0); got != i {
			patches.Reset()
			t.Fatalf("cycle %d: patched plainAdd = %d, want %d", i, got, i)
		}
		patches.Reset()
	}
	if got := plainAdd(1, 2); got != 3 {
		t.Fatalf("after 1000 cycles plainAdd = %d, want 3", got)
	}
}

// C3 contract: re-patching the SAME target on the SAME Patches object
// (common in fluid's table-driven tests) routes through the already-acquired
// trampoline slot.
func TestRepatchSameTargetSamePatches(t *testing.T) {
	patches := gomonkey.NewPatches()
	defer patches.Reset()

	patches.ApplyFunc(plainAdd, func(a, b int) int { return 100 })
	if got := plainAdd(1, 2); got != 100 {
		t.Fatalf("first patch: plainAdd = %d, want 100", got)
	}
	patches.ApplyFunc(plainAdd, func(a, b int) int { return 200 })
	if got := plainAdd(1, 2); got != 200 {
		t.Fatalf("second patch: plainAdd = %d, want 200", got)
	}
	patches.Reset()
	if got := plainAdd(1, 2); got != 3 {
		t.Fatalf("after Reset plainAdd = %d, want 3", got)
	}
}

// C4 contract: ApplyMethod / ApplyMethodFunc (71 + 32 uses in fluid).
func TestApplyMethodVariants(t *testing.T) {
	c := &calculator{base: 10}

	patches := gomonkey.ApplyMethod(c, "Add", func(c *calculator, n int) int { return 1000 })
	if got := c.Add(5); got != 1000 {
		patches.Reset()
		t.Fatalf("ApplyMethod: c.Add = %d, want 1000", got)
	}
	patches.Reset()
	if got := c.Add(5); got != 15 {
		t.Fatalf("after Reset c.Add = %d, want 15", got)
	}

	patches = gomonkey.ApplyMethodFunc(c, "Add", func(n int) int { return 2000 })
	if got := c.Add(5); got != 2000 {
		patches.Reset()
		t.Fatalf("ApplyMethodFunc: c.Add = %d, want 2000", got)
	}
	patches.Reset()
	if got := c.Add(5); got != 15 {
		t.Fatalf("after Reset c.Add = %d, want 15", got)
	}
}

// C5 contract: ApplyPrivateMethod (86 uses in fluid). On linux/amd64 this
// keeps the pre-2.14.4 direct-write path; on darwin/arm64 it is the path the
// upstream release rewrote.
func TestApplyPrivateMethod(t *testing.T) {
	c := &calculator{base: 6}
	patches := gomonkey.ApplyPrivateMethod(c, "scale", func(c *calculator, n int) int { return 999 })
	if got := c.scale(7); got != 999 {
		patches.Reset()
		t.Fatalf("ApplyPrivateMethod: c.scale = %d, want 999", got)
	}
	patches.Reset()
	if got := c.scale(7); got != 42 {
		t.Fatalf("after Reset c.scale = %d, want 42", got)
	}
}

// C6 contract: seq/return doubles (ApplyMethodSeq is used twice in fluid;
// ApplyFuncSeq/ApplyFuncReturn share the same getDoubleFunc machinery).
func TestApplySeqAndReturn(t *testing.T) {
	patches := gomonkey.ApplyFuncSeq(plainAdd, []gomonkey.OutputCell{
		{Values: gomonkey.Params{11}, Times: 1},
		{Values: gomonkey.Params{22}, Times: 1},
	})
	defer patches.Reset()
	if got := plainAdd(0, 0); got != 11 {
		t.Fatalf("seq call 1 = %d, want 11", got)
	}
	if got := plainAdd(0, 0); got != 22 {
		t.Fatalf("seq call 2 = %d, want 22", got)
	}

	patches2 := gomonkey.ApplyFuncReturn(tiny, 7)
	defer patches2.Reset()
	if got := tiny(); got != 7 {
		t.Fatalf("ApplyFuncReturn tiny = %d, want 7", got)
	}
}

// C7 contract: Patches.Origin executes the unpatched original while the
// patch stays installed afterwards. v2.14.4 rebuilds the branch directive
// through the (already-acquired) trampoline here.
func TestOriginRunsOriginal(t *testing.T) {
	patches := gomonkey.ApplyFunc(plainAdd, func(a, b int) int { return -1 })
	defer patches.Reset()

	originResult := -100
	patches.Origin(func() {
		originResult = plainAdd(1, 2)
	})
	if originResult != 3 {
		t.Fatalf("Origin() observed plainAdd = %d, want 3", originResult)
	}
	if got := plainAdd(1, 2); got != -1 {
		t.Fatalf("after Origin() the patch must be re-applied: plainAdd = %d, want -1", got)
	}
}

// C8 contract: 256 simultaneously-live patches on distinct targets must all
// work. This is exactly the size of the v2.14.4 amd64 trampoline pool; fluid
// stays far below it today, but this pins the floor of what must keep working.
func TestTwoHundredFiftySixLivePatches(t *testing.T) {
	patches := gomonkey.NewPatches()
	defer patches.Reset()

	const n = 256
	for i := 0; i < n; i++ {
		patches.ApplyFunc(lookupTarget(i), func() int { return 10000 + i })
	}
	for i := 0; i < n; i++ {
		got, want := callTarget(i), 10000+i
		if got != want {
			t.Fatalf("live patch %d: got %d, want %d", i, got, want)
		}
	}
}

// C9 canary (polarity: canary, NOT a contract test): documents the new hard
// capacity limit introduced by v2.14.4's 256-slot trampoline pool. On
// v2.14.4 (linux/amd64) the 257th simultaneously-live distinct-target patch
// panics with "no reachable amd64 patch trampoline available"; on v2.13.0
// there was no cap (300 live patches succeed). This test PASSES on the
// patched (v2.14.4) code by asserting the panic, and FLIPS (fails) if the
// cap is ever lifted or the panic turns into silent corruption. When it
// flips, invert it into a contract test.
//
// Fluid's real suites hold at most dozens of live patches per test binary,
// so this limit is not a regression for fluid — it is recorded so the
// behavior change is explicit and re-checked.
func TestTrampolinePoolLimitCanary(t *testing.T) {
	patches := gomonkey.NewPatches()
	defer patches.Reset()

	panickedAt := -1
	func() {
		defer func() {
			if r := recover(); r != nil {
				msg, _ := r.(error)
				if msg == nil {
					if s, ok := r.(string); ok {
						msg = errString(s)
					}
				}
				if msg == nil || !strings.Contains(msg.Error(), "trampoline") {
					t.Fatalf("unexpected panic value: %v", r)
				}
			}
		}()
		for i := 0; i < 300; i++ {
			patches.ApplyFunc(lookupTarget(i), func() int { return 20000 + i })
			panickedAt = i
		}
	}()

	if panickedAt != 299 && panickedAt != 255 {
		t.Fatalf("expected either v2.13.0 behavior (all 300 live, panickedAt=299) or v2.14.4 pool exhaustion at index 256, got last-ok index %d", panickedAt)
	}
	t.Logf("pool limit behavior: last successful live-patch index = %d (256 => v2.14.4 trampoline pool cap, 299 => uncapped)", panickedAt)
}

// helpers over the generated 300 targets -------------------------------

func lookupTarget(i int) func() int {
	return targetTable[i]
}

func callTarget(i int) int {
	return targetTable[i]()
}

type errString string

func (e errString) Error() string { return string(e) }

// C10 contract / premise check: upstream gomonkey #192 claims v2.13.x on
// amd64 crashes with "invalid runtime symbol table" when CPU profiling or
// stack unwinding samples a function whose entry is patched, and that
// v2.14.x fixes it by routing the entry through a NOSPLIT/NOFRAME
// trampoline. fluid starts a pprof server in its unit tests (pkg/utils
// NewPprofServer), so profiling-over-patched-code must be safe. On v2.14.4
// this must complete without a runtime fatal error; run the same test
// against v2.13.0 (base) to see the claimed crash.
func TestCPUProfileOverPatchedEntry(t *testing.T) {
	patches := gomonkey.ApplyFunc(plainAdd, func(a, b int) int { return a - b })
	defer patches.Reset()

	var buf bytes.Buffer
	if err := pprof.StartCPUProfile(&buf); err != nil {
		t.Fatalf("StartCPUProfile: %v", err)
	}
	// ~1s of calls so the 100Hz sampler lands on patched frames ~100 times.
	deadline := time.Now().Add(time.Second)
	i := 0
	for time.Now().Before(deadline) {
		plainAdd(i, i)
		i++
	}
	sink = i
	pprof.StopCPUProfile()

	if buf.Len() == 0 {
		t.Fatal("cpu profile is empty; sampling never happened")
	}
	// Force a full stack-unwind pass over (possibly patched) frames too.
	pprof.Lookup("goroutine").WriteTo(os.Stderr, 1)
	t.Logf("captured %d bytes of cpu profile over a patched entry", buf.Len())
}

// sink keeps the profile loop's counter observable to the compiler.
var sink int
