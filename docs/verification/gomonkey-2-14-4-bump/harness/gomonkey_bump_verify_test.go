// Review-verification harness for fluid PR #6228 (gomonkey/v2 2.13.0 -> 2.14.4).
//
// Polarity:
//   - "contract" tests assert intended behavior; they must PASS on the PR head
//     and would FAIL if the bump broke basic patching.
//   - "canary" tests pin newly introduced upstream behavior; they PASS on the
//     PR head and FAIL on the 2.13.0 base (differential evidence).
//
// Run with inlining disabled, the same way fluid's unit suite runs:
//
//	go test ./docs/verification/gomonkey-2-14-4-bump/harness/ -gcflags="all=-N -l" -count=1 -v
package harness

import (
	"reflect"
	"runtime"
	"strings"
	"testing"
	"unsafe"

	"github.com/agiledragon/gomonkey/v2"
)

//go:noinline
func greetTarget() string { return "original" }

//go:noinline
func addTarget(a, b int) int { return a + b }

//go:noinline
func shortTarget() {}

//go:noinline
func adjacentTarget() int { return 42 }

type calc struct{ base int }

//go:noinline
func (c *calc) Add(x int) int { return c.base + x }

//go:noinline
func (c *calc) hidden() int { return -1 }

// --- contract: basic ApplyFunc round-trip still works on 2.14.4 -------------

func TestBasicPatchRoundTrip(t *testing.T) {
	p := gomonkey.ApplyFunc(greetTarget, func() string { return "patched" })
	if got := greetTarget(); got != "patched" {
		t.Fatalf("patched call = %q, want %q", got, "patched")
	}
	p.Reset()
	if got := greetTarget(); got != "original" {
		t.Fatalf("after Reset, call = %q, want %q", got, "original")
	}
}

// --- contract: the API variants fluid uses still round-trip -----------------

func TestMethodAndReturnVariants(t *testing.T) {
	c := &calc{base: 10}

	p1 := gomonkey.ApplyMethod(c, "Add", func(_ *calc, x int) int { return x * 100 })
	if got := c.Add(2); got != 200 {
		t.Fatalf("ApplyMethod: got %d, want 200", got)
	}
	p1.Reset()
	if got := c.Add(2); got != 12 {
		t.Fatalf("after Reset: got %d, want 12", got)
	}

	p2 := gomonkey.ApplyFuncReturn(addTarget, 777)
	if got := addTarget(1, 2); got != 777 {
		t.Fatalf("ApplyFuncReturn: got %d, want 777", got)
	}
	p2.Reset()
	if got := addTarget(1, 2); got != 3 {
		t.Fatalf("after Reset: got %d, want 3", got)
	}

	p3 := gomonkey.ApplyPrivateMethod(c, "hidden", func(_ *calc) int { return 99 })
	if got := c.hidden(); got != 99 {
		t.Fatalf("ApplyPrivateMethod: got %d, want 99", got)
	}
	p3.Reset()
	if got := c.hidden(); got != -1 {
		t.Fatalf("after Reset: got %d, want -1", got)
	}
}

// --- contract: Origin() restores originals for fn and re-applies after ------

func TestOriginRestoresThenReapplies(t *testing.T) {
	p := gomonkey.ApplyFunc(greetTarget, func() string { return "patched" })
	defer p.Reset()

	var inside string
	p.Origin(func() { inside = greetTarget() })
	if inside != "original" {
		t.Fatalf("inside Origin fn: got %q, want %q", inside, "original")
	}
	if got := greetTarget(); got != "patched" {
		t.Fatalf("after Origin: got %q, want %q", got, "patched")
	}
}

// --- contract: Reset releases amd64 trampoline slots (no leak over 1024) ----
// On 2.14.4 each live function patch holds one of 256 trampoline slots; if
// Reset failed to release a slot, iteration 257 would panic.

func TestTrampolineSlotsReleasedOnReset(t *testing.T) {
	for i := 0; i < 1024; i++ {
		p := gomonkey.ApplyFunc(greetTarget, func() string { return "patched" })
		if got := greetTarget(); got != "patched" {
			t.Fatalf("iter %d: patched call = %q", i, got)
		}
		p.Reset()
	}
	if got := greetTarget(); got != "original" {
		t.Fatalf("after loop: got %q, want %q", got, "original")
	}
}

// --- contract: re-patching the same target reuses its slot ------------------

func TestRePatchSameTargetReusesSlot(t *testing.T) {
	p := gomonkey.NewPatches()
	p.ApplyFunc(greetTarget, func() string { return "first" })
	p.ApplyFunc(greetTarget, func() string { return "second" })
	if got := greetTarget(); got != "second" {
		t.Fatalf("re-patched call = %q, want %q", got, "second")
	}
	p.Reset()
	if got := greetTarget(); got != "original" {
		t.Fatalf("after Reset: got %q, want %q", got, "original")
	}
}

// --- contract: patching a short function does not corrupt its neighbor ------

func TestPatchShortFunctionAdjacentIntact(t *testing.T) {
	p := gomonkey.ApplyFunc(shortTarget, func() {})
	shortTarget()
	p.Reset()
	if got := adjacentTarget(); got != 42 {
		t.Fatalf("adjacent function after patch+reset: got %d, want 42", got)
	}
}

// --- canary (differential): 2.14.4 caps concurrent amd64 patches at 256 -----
// On the 2.13.0 base, holding 300 concurrent function patches succeeds and this
// test FAILS (no panic). On the 2.14.4 head, patch #257 panics with
// "no reachable amd64 patch trampoline available" and this test PASSES.
// It pins the new upstream behavior so a future bump changing the cap is caught.

func TestConcurrentPatchCap(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("trampoline pool is amd64-specific")
	}
	targets := capTargets() // 300 distinct targets
	patches := make([]*gomonkey.Patches, 0, len(targets))
	defer func() {
		for _, p := range patches {
			p.Reset()
		}
	}()

	panicMsg := func() (msg string) {
		defer func() {
			if r := recover(); r != nil {
				if s, ok := r.(string); ok {
					msg = s
				} else if e, ok := r.(error); ok {
					msg = e.Error()
				}
			}
		}()
		for _, tgt := range targets {
			patches = append(patches, gomonkey.ApplyFunc(tgt, func() int { return -1 }))
		}
		return ""
	}()

	if panicMsg == "" {
		t.Fatalf("300 concurrent function patches succeeded; expected the 2.14.4 " +
			"amd64 trampoline pool (256 slots) to panic on exhaustion")
	}
	if !strings.Contains(panicMsg, "no reachable amd64 patch trampoline available") {
		t.Fatalf("panic = %q, want it to contain %q", panicMsg,
			"no reachable amd64 patch trampoline available")
	}
	t.Logf("observed expected pool-exhaustion panic: %s", panicMsg)
}

// --- canary (differential): 2.14.4 patches entries with a 5-byte JMP rel32 --
// Release-note mechanism behind the #191/#192 unwind fix: the patched entry is
// "a 5-byte JMP rel32 into a NOSPLIT/NOFRAME trampoline" instead of the 2.13.0
// 12-byte MOVABS+JMP sequence (0x48 0xBA ... 0xFF 0x22) whose instruction
// boundary at entry+10 mismatched the original pcsp table. On the 2.13.0 base
// this test FAILS (entry[0] == 0x48); on the 2.14.4 head it PASSES.

//go:noinline
func footprintTarget() int { return 1 }

func TestEntryFootprintIsFiveByteBranch(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("entry-footprint mechanism is amd64-specific")
	}
	p := gomonkey.ApplyFunc(footprintTarget, func() int { return 2 })
	defer p.Reset()

	entry := reflect.ValueOf(footprintTarget).Pointer()
	raw := unsafe.Slice((*byte)(unsafe.Pointer(entry)), 8)
	if raw[0] != 0xE9 {
		t.Fatalf("patched entry opcode = %#x, want 0xE9 (JMP rel32); raw entry bytes: % x", raw[0], raw)
	}
	t.Logf("patched entry bytes: % x (5-byte JMP rel32 into trampoline)", raw)
}
