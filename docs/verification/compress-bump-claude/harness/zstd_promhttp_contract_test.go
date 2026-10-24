package harness

// Contract tests for the klauspost/compress v1.17.11 -> v1.18.7 vendored bump (PR #6214).
//
// The only consumer of github.com/klauspost/compress inside Fluid's build is
// prometheus/client_golang's promhttp package (zstd), reached at runtime through
// controller-runtime's metrics server (promhttp.HandlerFor with default compression,
// offering Identity, Gzip and Zstd). These tests pin that user-visible contract:
// a scraper sending "Accept-Encoding: zstd" must receive a valid zstd frame that
// decodes to exactly the identity exposition.
//
// Polarity: CONTRACT tests. They assert intended-correct behavior and must PASS
// on both the pre-bump (v1.17.11) and post-bump (v1.18.7) vendored trees. A red
// result here is the reproduction of a regression introduced by the bump.
//
// Notable v1.18.x behavior changes exercised:
//   - zstd: Default to full zero frames (upstream #1134) -> all-zero inputs boundary
//   - zstd: pool hash tables in encode paths (upstream #1143) -> concurrent scrape test
//   - s2 fixes are out of scope: the s2 package is not vendored in this module.

import (
	"bytes"
	"crypto/rand"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// zstdFrameMagic is the mandatory prefix of every zstd frame (spec: 0xFD2FB528 LE).
var zstdFrameMagic = []byte{0x28, 0xB5, 0x2F, 0xFD}

func encodeAll(t *testing.T, payload []byte) []byte {
	t.Helper()
	// Mirror promhttp's encoder configuration exactly:
	// vendored promhttp/http.go:449 uses zstd.NewWriter(rw, zstd.WithEncoderLevel(zstd.SpeedFastest)).
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest))
	if err != nil {
		t.Fatalf("zstd.NewWriter: %v", err)
	}
	defer enc.Close()
	out := enc.EncodeAll(payload, nil)
	if len(out) < 4 || !bytes.Equal(out[:4], zstdFrameMagic) {
		t.Fatalf("encoded output is not a zstd frame: first bytes %v (len=%d)", out[:min(4, len(out))], len(out))
	}
	return out
}

func decodeAll(t *testing.T, frame []byte) []byte {
	t.Helper()
	dec, err := zstd.NewReader(bytes.NewReader(frame))
	if err != nil {
		t.Fatalf("zstd.NewReader: %v", err)
	}
	defer dec.Close()
	got, err := io.ReadAll(dec)
	if err != nil {
		t.Fatalf("zstd decode: %v", err)
	}
	return got
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func realisticExposition() []byte {
	// A Prometheus text exposition payload of the shape/size Fluid's controllers serve.
	var b strings.Builder
	for i := 0; i < 3000; i++ {
		b.WriteString("fluid_runtime_audit_operations_total{runtime=\"alluxio\",namespace=\"default\",pod=\"alluxio-master-0\",type=\"ACCESS_FILE\",succeeded=\"true\"} 42\n")
	}
	return []byte(b.String())
}

// TestZstdRoundTripContract — P1: every payload (including the upstream #1134
// "full zero frames" boundary of all-zero and empty inputs) must round-trip
// byte-identically through the vendored encoder/decoder, and the encoded output
// must be a well-formed zstd frame.
func TestZstdRoundTripContract(t *testing.T) {
	randBlob := make([]byte, 256*1024)
	if _, err := rand.Read(randBlob); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		payload []byte
	}{
		{"empty", []byte{}},
		{"single-zero-byte", []byte{0}},
		{"all-zero-64KiB", make([]byte, 64*1024)},
		{"all-zero-1MiB", make([]byte, 1024*1024)}, // upstream #1134 boundary
		{"metrics-text", realisticExposition()},
		{"random-256KiB", randBlob},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			frame := encodeAll(t, c.payload)
			got := decodeAll(t, frame)
			if !bytes.Equal(got, c.payload) {
				t.Fatalf("round-trip mismatch: got %d bytes, want %d", len(got), len(c.payload))
			}
		})
	}
}

// newMetricsHandler mirrors how controller-runtime's metrics server builds the
// handler (sigs.k8s.io/controller-runtime/pkg/metrics/server/server.go:218:
// promhttp.HandlerFor(registry, promhttp.HandlerOpts{ErrorHandling: HTTPErrorOnError})).
// With no DisableCompression set, promhttp offers Identity, Gzip and Zstd.
func newMetricsHandler() http.Handler {
	reg := prometheus.NewRegistry()
	gauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "fluid_harness_zstd_check_bytes",
		Help: "synthetic gauge for the compress-bump verification harness",
	})
	gauge.Set(1234.5)
	reg.MustRegister(gauge)
	return promhttp.HandlerFor(reg, promhttp.HandlerOpts{ErrorHandling: promhttp.HTTPErrorOnError})
}

// TestPromhttpZstdNegotiationContract — P2: the metrics endpoint must negotiate
// zstd for "Accept-Encoding: zstd" and the zstd response body must decode to
// exactly the identity response body. Gzip and identity must keep working.
func TestPromhttpZstdNegotiationContract(t *testing.T) {
	srv := httptest.NewServer(newMetricsHandler())
	defer srv.Close()

	fetch := func(acceptEncoding string) (string, string, []byte) {
		req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		if acceptEncoding != "" {
			req.Header.Set("Accept-Encoding", acceptEncoding)
		}
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatalf("GET (Accept-Encoding: %q): %v", acceptEncoding, err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read body (Accept-Encoding: %q): %v", acceptEncoding, err)
		}
		return resp.Header.Get("Content-Encoding"), resp.Header.Get("Content-Type"), body
	}

	identityCE, _, identityBody := fetch("")
	if identityCE != "" && identityCE != "identity" {
		t.Fatalf("plain request negotiated unexpected encoding %q", identityCE)
	}
	if !bytes.Contains(identityBody, []byte("fluid_harness_zstd_check_bytes")) {
		t.Fatalf("identity body does not contain the registered metric: %q", identityBody[:min(120, len(identityBody))])
	}

	zstdCE, _, zstdBody := fetch("zstd")
	if zstdCE != "zstd" {
		t.Fatalf("Accept-Encoding: zstd negotiated %q, want %q", zstdCE, "zstd")
	}
	if len(zstdBody) < 4 || !bytes.Equal(zstdBody[:4], zstdFrameMagic) {
		t.Fatalf("zstd response does not start with the zstd frame magic: %v", zstdBody[:min(4, len(zstdBody))])
	}
	decoded := decodeAll(t, zstdBody)
	if !bytes.Equal(decoded, identityBody) {
		t.Fatalf("zstd-decoded body differs from identity body: %d vs %d bytes", len(decoded), len(identityBody))
	}

	gzipCE, _, _ := fetch("gzip")
	if gzipCE != "gzip" {
		t.Fatalf("Accept-Encoding: gzip negotiated %q, want %q", gzipCE, "gzip")
	}
}

// TestPromhttpZstdConcurrentScrapes — P3: concurrent scrapes all negotiate zstd
// and every response decodes to the same exposition. promhttp creates one
// zstd.NewWriter per request, but upstream #1143 pooled hash tables inside the
// encoder's encode path, so this is the concurrency contract over that change.
// Run with -race to make it a race detector too.
func TestPromhttpZstdConcurrentScrapes(t *testing.T) {
	srv := httptest.NewServer(newMetricsHandler())
	defer srv.Close()

	const workers, iterations = 16, 20
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var want []byte
			for i := 0; i < iterations; i++ {
				req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
				if err != nil {
					errs <- err
					return
				}
				req.Header.Set("Accept-Encoding", "zstd")
				resp, err := srv.Client().Do(req)
				if err != nil {
					errs <- err
					return
				}
				if resp.Header.Get("Content-Encoding") != "zstd" {
					resp.Body.Close()
					errs <- io.ErrUnexpectedEOF
					return
				}
				body, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				if err != nil {
					errs <- err
					return
				}
				decoded := decodeAll(t, body)
				if want == nil {
					want = decoded
				}
				if !bytes.Equal(decoded, want) {
					errs <- io.ErrUnexpectedEOF
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent scrape failed: %v", err)
	}
}

// TestHarnessBitesZstdCorruption — self-control for the harness itself (the
// verifier skill's "prove the harness bites" step, encoded as a permanent test):
// a single flipped byte in the middle of a frame must be detected by the decode
// path (error or byte mismatch). If this ever passes silently, the round-trip
// assertions above would be worthless.
func TestHarnessBitesZstdCorruption(t *testing.T) {
	payload := realisticExposition()
	frame := encodeAll(t, payload)
	corrupted := make([]byte, len(frame))
	copy(corrupted, frame)
	corrupted[len(corrupted)/2] ^= 0xFF

	dec, err := zstd.NewReader(bytes.NewReader(corrupted))
	if err != nil {
		return // decoder rejected the corrupted frame: harness bites
	}
	defer dec.Close()
	got, readErr := io.ReadAll(dec)
	if readErr == nil && bytes.Equal(got, payload) {
		t.Fatal("corrupted frame decoded without error AND matched the original payload — the round-trip assertions cannot detect corruption")
	}
}
