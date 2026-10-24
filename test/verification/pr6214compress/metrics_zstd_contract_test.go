// Package pr6214compress is the review-verification harness for PR #6214
// (chore(deps): bump github.com/klauspost/compress from 1.17.11 to 1.18.7).
//
// In fluid's compiled dependency graph the only importer of
// github.com/klauspost/compress is
// github.com/prometheus/client_golang/prometheus/promhttp (reached via
// sigs.k8s.io/controller-runtime/pkg/metrics/server), which offers zstd
// response compression by default: defaultCompressionFormats = {Identity,
// Gzip, Zstd}. promhttp negotiates with
//
//	zstd.NewWriter(rw, zstd.WithEncoderLevel(zstd.SpeedFastest))
//
// followed by an immediate Reset(rw) (see negotiateEncodingWriter).
//
// POLARITY: every test here is a CONTRACT test. Each asserts behavior fluid
// relies on and must PASS on both the pre-bump (v1.17.11) and post-bump
// (v1.18.7) vendored code. A failure on the PR head is a regression
// introduced by the bump; these tests double as the regression suite for
// future re-verification rounds.
package pr6214compress

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const verifyMetricName = "fluid_verify_pr6214_requests_total"

func newVerifyRegistry(t *testing.T, withMetric bool) *prometheus.Registry {
	t.Helper()
	reg := prometheus.NewRegistry()
	if withMetric {
		c := prometheus.NewCounter(prometheus.CounterOpts{
			Name: verifyMetricName,
			Help: "contract test counter for PR 6214 verification",
		})
		reg.MustRegister(c)
		c.Add(42)
	}
	return reg
}

func scrape(t *testing.T, handler http.Handler, acceptEncoding string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Accept-Encoding", acceptEncoding)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("scrape with Accept-Encoding=%q: got HTTP %d, want 200; body: %s",
			acceptEncoding, rec.Code, rec.Body.String())
	}
	return rec
}

// TestPromhttpZstdRoundTrip is the primary consumer-path contract: a scrape
// that negotiates zstd must return a zstd Content-Encoding and a body that
// the vendored zstd decoder reads back to the exact exposition payload.
func TestPromhttpZstdRoundTrip(t *testing.T) {
	handler := promhttp.HandlerFor(newVerifyRegistry(t, true), promhttp.HandlerOpts{})
	rec := scrape(t, handler, "zstd")

	if enc := rec.Header().Get("Content-Encoding"); enc != "zstd" {
		t.Fatalf("Content-Encoding = %q, want %q (zstd must be negotiated by default)", enc, "zstd")
	}
	zr, err := zstd.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("zstd.NewReader over response body failed: %v", err)
	}
	defer zr.Close()
	body, err := io.ReadAll(zr.IOReadCloser())
	if err != nil {
		t.Fatalf("decoding zstd metrics body failed: %v", err)
	}
	if !strings.Contains(string(body), verifyMetricName) {
		t.Fatalf("decoded metrics body does not contain %q; got %d bytes: %q",
			verifyMetricName, len(body), string(body))
	}
}

// TestPromhttpZstdEmptyRegistry pins the boundary affected by the upstream
// v1.18.5 change "zstd: Default to full zero frames": encoding a (near-)
// empty payload. An empty registry produces an empty exposition, so the
// response body is a zstd frame over zero bytes. Whatever frame shape the
// encoder emits, the vendored decoder must read it back without error.
func TestPromhttpZstdEmptyRegistry(t *testing.T) {
	handler := promhttp.HandlerFor(newVerifyRegistry(t, false), promhttp.HandlerOpts{})
	rec := scrape(t, handler, "zstd")

	if enc := rec.Header().Get("Content-Encoding"); enc != "zstd" {
		t.Fatalf("Content-Encoding = %q, want %q", enc, "zstd")
	}
	zr, err := zstd.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("zstd.NewReader over empty-payload response failed: %v", err)
	}
	defer zr.Close()
	if _, err := io.ReadAll(zr.IOReadCloser()); err != nil {
		t.Fatalf("decoding empty zstd metrics body failed: %v", err)
	}
}

// TestZstdSpeedFastestResetReuse exercises the encoder exactly the way
// promhttp does (NewWriter with SpeedFastest, then Reset before writing),
// across payload sizes including the 0-byte boundary, and asserts exact
// round-trip fidelity with the vendored decoder.
func TestZstdSpeedFastestResetReuse(t *testing.T) {
	payloads := []struct {
		name string
		data []byte
	}{
		{"empty", []byte{}},
		{"one-byte", []byte{0x42}},
		{"small-text", []byte("fluid_verify_pr6214 payload\n")},
		{"1MiB-pattern", bytes.Repeat([]byte("fluid-pr6214-"), 1<<20/13)},
	}

	var buf bytes.Buffer
	enc, err := zstd.NewWriter(&buf, zstd.WithEncoderLevel(zstd.SpeedFastest))
	if err != nil {
		t.Fatalf("zstd.NewWriter failed: %v", err)
	}
	for _, p := range payloads {
		buf.Reset()
		enc.Reset(&buf) // promhttp calls Reset immediately after NewWriter
		if _, err := enc.Write(p.data); err != nil {
			t.Fatalf("%s: Write failed: %v", p.name, err)
		}
		if err := enc.Close(); err != nil {
			t.Fatalf("%s: Close failed: %v", p.name, err)
		}
		dec, err := zstd.NewReader(bytes.NewReader(buf.Bytes()))
		if err != nil {
			t.Fatalf("%s: zstd.NewReader failed: %v", p.name, err)
		}
		got, err := io.ReadAll(dec.IOReadCloser())
		dec.Close()
		if err != nil {
			t.Fatalf("%s: decode failed: %v", p.name, err)
		}
		if !bytes.Equal(got, p.data) {
			t.Fatalf("%s: round-trip mismatch: wrote %d bytes, read %d bytes", p.name, len(p.data), len(got))
		}
	}
}

// TestPromhttpGzipControl is the control: the gzip negotiation path is
// untouched by this bump and must keep working alongside zstd.
func TestPromhttpGzipControl(t *testing.T) {
	handler := promhttp.HandlerFor(newVerifyRegistry(t, true), promhttp.HandlerOpts{})
	rec := scrape(t, handler, "gzip")

	if enc := rec.Header().Get("Content-Encoding"); enc != "gzip" {
		t.Fatalf("Content-Encoding = %q, want %q", enc, "gzip")
	}
	gr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("gzip.NewReader over response body failed: %v", err)
	}
	defer gr.Close()
	body, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("decoding gzip metrics body failed: %v", err)
	}
	if !strings.Contains(string(body), verifyMetricName) {
		t.Fatalf("decoded gzip body does not contain %q", verifyMetricName)
	}
}
