/*
Package harness pins the behavior of github.com/onsi/gomega matchers that
fluid-cloudnative/fluid's test suite actually uses, across the bump from
v1.38.2 (base) to v1.44.0 (PR #6231 head).

Polarity notes (see review-finding-verifier skill):
  - TestBeNumericallyFluidPatterns / TestHaveKeyWithValueLiteralPatterns are
    CONTRACT tests for behavior fluid relies on: they must PASS on BOTH
    gomega v1.38.2 and v1.44.0. A failure on either version means the bump
    changed semantics fluid's suite depends on.
  - TestBeNumericallySignedUnsignedDelta pins the KNOWN upstream behavior
    change (onsi/gomega#925): v1.44.0 compares signed/unsigned integers by
    value, so -1 != uint64(math.MaxUint64). It must PASS on v1.44.0 (head)
    and FAIL on v1.38.2 (base). The base-side failure is the "harness bites"
    proof: the harness is sensitive to the gomega version under test.
*/
package harness

import (
	"math"
	"testing"
	"time"

	. "github.com/onsi/gomega"
)

// TestBeNumericallyFluidPatterns replicates every BeNumerically call shape
// found in fluid's test suite (35 call sites; all are ">"/">="/"<="/"<="
// against int-compatible literals or same-type variables — no "~" and no
// "==" with threshold, no mixed signedness).
func TestBeNumericallyFluidPatterns(t *testing.T) {
	g := NewWithT(t)

	// shape: Expect(len(x)).To(BeNumerically(">", 0))   (9 uses)
	g.Expect(len([]string{"a"})).To(BeNumerically(">", 0))
	// shape: Expect(len(s)).To(BeNumerically("<=", 63)) (DNS1035LabelMaxLength)
	g.Expect(len("fluid")).To(BeNumerically("<=", 63))
	// shape: port ranges, e.g. BeNumerically(">=", 14000) / ("<=", 16000)
	g.Expect(20050).To(BeNumerically(">=", 14000))
	g.Expect(15000).To(BeNumerically("<=", 16000))
	// shape: time.Duration comparisons (manager_test.go, runtime controllers)
	delay := 5 * time.Second
	g.Expect(delay).To(BeNumerically("<=", 10*time.Second))
	g.Expect(delay).To(BeNumerically(">", time.Duration(0)))
	// shape: same-type variable-vs-variable
	first, second := int64(3), int64(5)
	g.Expect(second).To(BeNumerically(">=", first))
}

// TestHaveKeyWithValueLiteralPatterns replicates fluid's HaveKey/HaveKeyWithValue
// usage: 216 HaveKeyWithValue + 89 HaveKey call sites, ALL with literal string
// keys/values (no matcher arguments). Upstream changes #926/#929 only affect
// matcher-valued keys; literal-key behavior must be unchanged.
func TestHaveKeyWithValueLiteralPatterns(t *testing.T) {
	g := NewWithT(t)
	annotations := map[string]string{
		"data.fluid.io/inject": "true",
		"new-label":            "new-value",
	}
	g.Expect(annotations).To(HaveKeyWithValue("data.fluid.io/inject", "true"))
	g.Expect(annotations).To(HaveKeyWithValue("new-label", "new-value"))
	g.Expect(annotations).To(HaveKey("data.fluid.io/inject"))
	g.Expect(annotations).ToNot(HaveKeyWithValue("new-label", "other"))
}

// TestBeNumericallySignedUnsignedDelta pins the upstream fix onsi/gomega#925
// shipped in v1.44.0: signed and unsigned integers are compared BY VALUE.
// On v1.38.2, -1 was considered equal to uint64(math.MaxUint64).
//
// CONTRACT (against head / v1.44.0): must PASS.
// On base (v1.38.2) this MUST FAIL — that failure is the sensitivity proof.
func TestBeNumericallySignedUnsignedDelta(t *testing.T) {
	g := NewWithT(t)
	// v1.44.0 semantics: these are NOT equal.
	g.Expect(-1).ToNot(BeNumerically("==", uint64(math.MaxUint64)))
	// and ordering across signedness is now by value: uint(5) > -3
	g.Expect(uint(5)).To(BeNumerically(">", -3))
}
