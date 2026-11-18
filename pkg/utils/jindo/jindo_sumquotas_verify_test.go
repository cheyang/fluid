/*
Verification harness for PR #6261 (fluid-cloudnative/fluid).
Reviewer: Codex (reviewer B). Additive only - does not modify production code.

Unit-level contracts for jindoutils.SumQuotas. NOTE: this file only compiles on the
PR head (SumQuotas does not exist on base); the premise check P0 lives in the
engine-level tests which compile against both base and head.
*/

package jindo

import "testing"

func TestVerifySumQuotasContracts(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string // "" means: expect error
		wantErr bool
	}{
		{"gi list", "10Gi,5Gi", "15Gi", false},
		{"gi list with space (api doc example)", "10Gi, 5Gi", "15Gi", false},
		{"single gi", "10Gi", "10Gi", false},
		{"invalid element", "10Gi,abc", "", true},
		{"empty string", "", "", true},
		{"trailing comma", "10Gi,", "", true},
		// root-cause demonstrator for F2: jindo unit "m" is parsed by k8s
		// quantity semantics as MILLI (1/1000), not Mi. Summing "2048m,2048m"
		// therefore yields 4096m (~4 bytes), not 4096Mi.
		{"jindo mi units are milli in k8s semantics", "2048m,2048m", "4096m", false},
	}
	for _, tt := range tests {
		got, err := SumQuotas(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("%s: expected error, got %v", tt.name, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: unexpected error: %v", tt.name, err)
			continue
		}
		if got.String() != tt.want {
			t.Errorf("%s: SumQuotas(%q) = %q, want %q", tt.name, tt.in, got.String(), tt.want)
		}
	}
}
