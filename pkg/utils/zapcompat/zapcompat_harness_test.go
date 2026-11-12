package zapcompat

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	zapOpt "go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	ctrlzap "sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// ---------------------------------------------------------------------------
// A1 (contract, unit): fluid's actual logger construction path works on zap
// v1.28.0. This mirrors the option chain every fluid cmd/* app uses
// (cmd/alluxio/app/alluxio.go and its 10 siblings):
//
//	ctrl.SetLogger(zap.New(func(o *zap.Options) { o.Development = development },
//	  func(o *zap.Options) { o.ZapOpts = append(o.ZapOpts, zapOpt.AddCaller()) },
//	  func(o *zap.Options) {
//	      encCfg := zapOpt.NewProductionEncoderConfig()
//	      encCfg.EncodeLevel = zapcore.CapitalLevelEncoder
//	      encCfg.EncodeTime = zapcore.ISO8601TimeEncoder
//	      o.Encoder = zapcore.NewConsoleEncoder(encCfg)
//	  }))
//
// The only deviation: DestWriter is a buffer so output is observable.
// Expected: entries at info/error are emitted with capitalized level names,
// ISO8601 timestamps and a caller, and the logger Syncs cleanly.
// ---------------------------------------------------------------------------
func TestHarnessFluidLoggerPathWorks(t *testing.T) {
	// fluid's cmd apps construct the logger once per development flag value;
	// cover both branches exactly as cmd/webhook/app/webhook.go does.
	for _, development := range []bool{true, false} {
		var buf bytes.Buffer
		logger := ctrlzap.New(func(o *ctrlzap.Options) {
			o.Development = development
			o.DestWriter = &buf
		}, func(o *ctrlzap.Options) {
			o.ZapOpts = append(o.ZapOpts, zapOpt.AddCaller())
		}, func(o *ctrlzap.Options) {
			if !development {
				encCfg := zapOpt.NewProductionEncoderConfig()
				encCfg.EncodeLevel = zapcore.CapitalLevelEncoder
				encCfg.EncodeTime = zapcore.ISO8601TimeEncoder
				o.Encoder = zapcore.NewConsoleEncoder(encCfg)
			}
		})

		logger.Info("harness info message", "key", "value")
		logger.Error(nil, "harness error message")

		out := buf.String()
		// console encoder renders structured fields as a trailing JSON object
		for _, want := range []string{"INFO", "ERROR", "harness info message", `"key": "value"`, "zapcompat_harness_test.go"} {
			if !strings.Contains(out, want) {
				t.Errorf("development=%v: logger output missing %q\noutput:\n%s", development, want, out)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// A2 (contract, unit): every log-level spelling accepted by zap v1.27.0's
// zapcore.Level.UnmarshalText is still accepted after the bump (polarity:
// regression guard — must pass on both base and head).
// ---------------------------------------------------------------------------
func TestHarnessLevelStringsStillAccepted(t *testing.T) {
	// all spellings accepted by zap v1.27.0 unmarshalText (+ case variants
	// via the ToLower fallback that v1.27.0 and v1.28.0 both implement)
	for _, s := range []string{
		"debug", "DEBUG", "Debug",
		"info", "INFO", "",
		"warn", "WARN", "Warn",
		"error", "ERROR",
		"dpanic", "DPANIC",
		"panic", "PANIC",
		"fatal", "FATAL",
	} {
		var l zapcore.Level
		if err := l.UnmarshalText([]byte(s)); err != nil {
			t.Errorf("level %q rejected by zapcore.Level.UnmarshalText after upgrade: %v", s, err)
		}
	}
}

// ---------------------------------------------------------------------------
// A3 (contract, unit, discriminating): zap v1.28.0 newly accepts "warning"
// as an alias for WarnLevel. Fails on base (v1.27.0) and passes on head —
// this is what proves the vendored tree really moved to >= v1.27.1.
// ---------------------------------------------------------------------------
func TestHarnessWarningLevelNewlyAccepted(t *testing.T) {
	for _, s := range []string{"warning", "WARNING"} {
		var l zapcore.Level
		if err := l.UnmarshalText([]byte(s)); err != nil {
			t.Errorf("level %q should be accepted as warn level in zap v1.28.0: %v", s, err)
		} else if l != zapcore.WarnLevel {
			t.Errorf("level %q parsed to %v, want warn", s, l)
		}
	}
}

// ---------------------------------------------------------------------------
// A4 (contract, unit, discriminating): zap v1.27.1 fix — zap.Object with a
// nil ObjectMarshaler no longer panics; it marshals as an explicit nil field.
// Panics on base (v1.27.0) — again proving the vendored tree moved.
// ---------------------------------------------------------------------------
func TestHarnessNilObjectNoPanic(t *testing.T) {
	var buf bytes.Buffer
	logger := zapOpt.New(zapcore.NewCore(
		zapcore.NewJSONEncoder(zapOpt.NewProductionEncoderConfig()),
		zapcore.AddSync(&buf),
		zapcore.DebugLevel,
	))

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("zap.Object(key, nil) panicked: %v", r)
		}
	}()

	f := zapOpt.Object("maybeNil", nil)
	logger.Info("nil object field", f)

	if out := buf.String(); !strings.Contains(out, `"maybeNil":null`) {
		t.Errorf("expected maybeNil to marshal as explicit null, got:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// A5 (contract, unit): vendored zap version is consistent between go.mod and
// vendor/modules.txt (the failure mode a hand-edited vendor bump would hit;
// `go build` also enforces this, the test makes it explicit and attributed).
// ---------------------------------------------------------------------------
func TestHarnessVendoredZapVersionConsistent(t *testing.T) {
	modVersion := requireVersion(t, "go.mod", "go.uber.org/zap")
	vendorVersion := requireVersion(t, "vendor/modules.txt", "go.uber.org/zap")
	if modVersion == "" || vendorVersion == "" {
		t.Fatalf("could not determine zap version (go.mod=%q vendor/modules.txt=%q)", modVersion, vendorVersion)
	}
	if modVersion != vendorVersion {
		t.Errorf("go.mod requires go.uber.org/zap %s but vendor/modules.txt says %s", modVersion, vendorVersion)
	}
	t.Logf("go.uber.org/zap pinned at %s in both go.mod and vendor/modules.txt", modVersion)
}

func requireVersion(t *testing.T, path, module string) string {
	t.Helper()
	data, err := os.ReadFile(repoRelative(path))
	if err != nil {
		t.Logf("skipping %s: %v", path, err)
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# "+module+" ") { // vendor/modules.txt form
			return strings.TrimPrefix(line, "# "+module+" ")
		}
		if strings.HasPrefix(line, module+" ") { // go.mod require line form
			return strings.Fields(line)[1]
		}
	}
	return ""
}

// repoRelative resolves a repo-root-relative path from this package dir
// (pkg/utils/zapcompat -> repo root is three levels up).
func repoRelative(path string) string {
	return filepath.Join("..", "..", "..", path)
}
