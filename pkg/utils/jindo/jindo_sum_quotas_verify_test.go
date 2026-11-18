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

package jindo

import (
	"testing"
)

// Reviewer harness for PR #6261 (fluid-cloudnative/fluid), finding-independent
// unit layer for SumQuotas.
//
// Polarity: contract tests. Green = correct behavior. "milli semantics" rows
// document pre-existing behavior of the jindo unit round-trip ("Mi" is turned
// into "m" upstream and never restored, so k8s parses it as milli), which this
// PR neither introduces nor fixes.
func TestSumQuotasContract(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string // expected total.String()
		wantErr bool
	}{
		{"single binary quota", "10Gi", "10Gi", false},
		{"issue 2077 shape", "10Gi,5Gi", "15Gi", false},
		{"CRD-documented format with spaces", "100Gi, 50Gi", "150Gi", false},
		{"decimal SI suffixes", "10G,5G", "15G", false},
		{"mixed binary suffixes canonicalize", "1Gi,512Mi", "1536Mi", false},
		{"milli parses as milli (pre-existing m/Mi ambiguity)", "100m,200m", "300m", false},
		{"negative parses (no sign validation)", "-1Gi,2Gi", "1Gi", false},
		{"jindo lowercase g is not a k8s quantity", "10g,5g", "", true},
		{"invalid token", "10Gi,abc", "", true},
		{"empty string is an error", "", "", true},
		{"empty element is an error", "10Gi,,5Gi", "", true},
	}
	for _, test := range tests {
		got, err := SumQuotas(test.in)
		if test.wantErr {
			if err == nil {
				t.Errorf("%s: SumQuotas(%q) = %q, expected an error", test.name, test.in, got.String())
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: SumQuotas(%q) returned unexpected error: %v", test.name, test.in, err)
			continue
		}
		if got.String() != test.want {
			t.Errorf("%s: SumQuotas(%q).String() = %q, want %q", test.name, test.in, got.String(), test.want)
		}
	}
}
