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
		// F1 regression contract: jindo unit "m" means Mi. Up to previous head
		// 22d2d07c this summed as k8s MILLI-bytes ("4096m", ~4 bytes); the fix in
		// 305e466f converts a trailing "m" to "Mi" inside SumQuotas.
		{"jindo mi units sum in Mi", "2048m,2048m", "4Gi", false},
		{"single jindo mi unit", "512m", "512Mi", false},
		// Documented semantic choice (recorded in the round-2 ledger): a RAW
		// milli-denominated element ("500m" is CRD-legal via [numkMGTPE]) is
		// read as 500Mi, matching how jindo itself reads the same string in the
		// helm values. Up to 22d2d07c it summed as 500 milli-bytes.
		{"raw milli element read as Mi (matches jindo semantics)", "500m", "500Mi", false},
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

// TestVerifySumQuotasNegativeCanary is a BUG-CANARY (polarity: canary): it documents
// that SumQuotas currently accepts and sums negative quota elements
// ("10Gi,-5Gi" -> "5Gi"). The QuotaList CRD validation pattern explicitly allows a
// leading sign, so this input reaches the engine. If a future fix rejects negative
// quotas, this test FLIPS to fail and must be inverted into a contract test.
func TestVerifySumQuotasNegativeCanary(t *testing.T) {
	got, err := SumQuotas("10Gi,-5Gi")
	if err != nil {
		t.Fatalf("canary flipped: SumQuotas now rejects negative quotas (err=%v) - invert this test into a contract test", err)
	}
	if got.String() != "5Gi" {
		t.Fatalf("canary flipped: SumQuotas(\"10Gi,-5Gi\") = %q, want 5Gi (documented current behavior)", got.String())
	}
}
