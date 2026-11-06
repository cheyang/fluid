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

package gomegacontract

import (
	"math"
	"testing"

	. "github.com/onsi/gomega"
)

// assertMatch fails when matcher.Match(actual) disagrees with wantMatch or errors.
func assertMatch(t *testing.T, name string, matcher OmegaMatcher, actual any, wantMatch bool) {
	t.Helper()
	got, err := matcher.Match(actual)
	if err != nil {
		t.Fatalf("%s: matcher returned error: %v", name, err)
	}
	if got != wantMatch {
		t.Fatalf("%s: Match(actual) = %v, want %v", name, got, wantMatch)
	}
}

// TestGomegaBumpContractSemantics pins the matcher behavior that changed in
// gomega v1.44.0 (relative to v1.38.2, the version this PR bumps from).
// Every case asserts the NEW (documented, fixed) behavior:
//
//	#925 BeNumerically compares signed and unsigned integers by value
//	#927 BeNumerically("==", x, threshold) honors the threshold for floats
//	#930/#931 MatchJSON compares large integers exactly
//	#933 MatchYAML compares every document in a multi-document stream
//	#932 MatchXML ignores namespace prefixes, compares namespace URIs
//	#929 HaveKeyWithValue succeeds if any key matched by the key matcher has
//	     a matching value (no map-iteration-order dependence)
//
// Expected to be RED on gomega <= 1.38.2 (except the HaveKeyWithValue case,
// which is order-dependent there) and GREEN on gomega >= 1.44.0.
func TestGomegaBumpContractSemantics(t *testing.T) {
	t.Run("BeNumericallySignedUnsignedEquality", func(t *testing.T) {
		// #925: -1 must no longer equal uint64(math.MaxUint64)
		assertMatch(t, "int64(-1) == uint64(max)", BeNumerically("==", uint64(math.MaxUint64)), int64(-1), false)
		assertMatch(t, "int64(-1) == int64(max)", BeNumerically("==", int64(math.MaxInt64)), int64(math.MaxInt64), true)
	})

	t.Run("BeNumericallySignedUnsignedOrdering", func(t *testing.T) {
		// #925: uint(5) is now greater than -3
		assertMatch(t, "uint(5) > -3", BeNumerically(">", int64(-3)), uint(5), true)
		assertMatch(t, "-3 > uint(5)", BeNumerically(">", uint(5)), int64(-3), false)
	})

	t.Run("BeNumericallyFloatThreshold", func(t *testing.T) {
		// #927: the threshold operand is honored for floats
		assertMatch(t, "1.1 == 1.0 +/- 0.2", BeNumerically("==", 1.0, 0.2), 1.1, true)
		assertMatch(t, "1.5 == 1.0 +/- 0.2", BeNumerically("==", 1.0, 0.2), 1.5, false)
	})

	t.Run("MatchJSONLargeIntegersExact", func(t *testing.T) {
		// #931: integers beyond +-2^53 are compared exactly, so neighboring
		// large integers (e.g. IDs) no longer compare equal
		assertMatch(t, "max-int64 json ids differ",
			MatchJSON(`{"id": 9223372036854775807}`), `{"id": 9223372036854775806}`, false)
		assertMatch(t, "same large json ids",
			MatchJSON(`{"id": 9223372036854775807}`), `{"id": 9223372036854775807}`, true)
	})

	t.Run("MatchYAMLComparesAllDocuments", func(t *testing.T) {
		// #933: every document in a multi-document stream is compared, not
		// only the first
		assertMatch(t, "second yaml document differs",
			MatchYAML("a: 1\n---\nb: 3"), "a: 1\n---\nb: 2", false)
		assertMatch(t, "identical yaml documents",
			MatchYAML("a: 1\n---\nb: 2"), "a: 1\n---\nb: 2", true)
	})

	t.Run("MatchXMLIgnoresNamespacePrefixes", func(t *testing.T) {
		// #932: elements are compared by namespace URI, not prefix
		assertMatch(t, "same uri, different prefixes",
			MatchXML(`<x:a xmlns:x="urn:test"><x:b/></x:a>`),
			`<y:a xmlns:y="urn:test"><y:b/></y:a>`, true)
		assertMatch(t, "different uris",
			MatchXML(`<x:a xmlns:x="urn:test"/>`), `<y:a xmlns:y="urn:other"/>`, false)
	})

	t.Run("HaveKeyWithValueAnyMatchedKey", func(t *testing.T) {
		// #929: if any key accepted by the key matcher has a matching value
		// the matcher succeeds, independent of map iteration order
		m := map[string]string{"key1": "other", "key2": "value"}
		assertMatch(t, "key matcher matches key2 whose value matches",
			HaveKeyWithValue(ContainSubstring("key"), "value"), m, true)
		assertMatch(t, "no matched key has the value",
			HaveKeyWithValue(ContainSubstring("key"), "missing"), m, false)
	})
}
