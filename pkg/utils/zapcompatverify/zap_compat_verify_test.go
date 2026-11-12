// Verification harness for PR #6241 (bump go.uber.org/zap 1.27.0 -> 1.28.0).
// Additive-only: no production code changed. All tests are CONTRACT tests
// (they assert the intended correct behavior on the PR head).

package zapcompatverify

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// C2: level text parsing must remain backward compatible with zap 1.27.
// Operators/controllers pass level strings through zapcore.Level.UnmarshalText
// (flag.Value Set). zap 1.28 removed the explicit uppercase cases from the
// switch; this pins that the ToLower fallback keeps them working, and that
// the newly-added "warning" alias parses too.
func TestLevelUnmarshalCompatibility(t *testing.T) {
	cases := map[string]zapcore.Level{
		"debug":   zapcore.DebugLevel,
		"DEBUG":   zapcore.DebugLevel,
		"info":    zapcore.InfoLevel,
		"INFO":    zapcore.InfoLevel,
		"":        zapcore.InfoLevel,
		"warn":    zapcore.WarnLevel,
		"WARN":    zapcore.WarnLevel,
		"warning": zapcore.WarnLevel, // new alias in 1.28
		"error":   zapcore.ErrorLevel,
		"ERROR":   zapcore.ErrorLevel,
		"dpanic":  zapcore.DPanicLevel,
		"panic":   zapcore.PanicLevel,
		"fatal":   zapcore.FatalLevel,
	}
	for in, want := range cases {
		var l zapcore.Level
		if err := l.UnmarshalText([]byte(in)); err != nil {
			t.Fatalf("UnmarshalText(%q) unexpected error: %v", in, err)
		}
		if l != want {
			t.Fatalf("UnmarshalText(%q) = %v, want %v", in, l, want)
		}
	}
	var l zapcore.Level
	if err := l.UnmarshalText([]byte("bogus")); err == nil {
		t.Fatalf("UnmarshalText(bogus) should error")
	}
}

// C3: fluid's production logging path (cmd/fluidapp/app/fluidapp.go):
// NewProductionEncoderConfig + CapitalLevelEncoder + ISO8601TimeEncoder +
// console encoder + AddCaller must still build and emit parseable output.
func TestFluidProductionLoggerShape(t *testing.T) {
	buf := &bytes.Buffer{}
	encCfg := zap.NewProductionEncoderConfig()
	encCfg.EncodeLevel = zapcore.CapitalLevelEncoder
	encCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	core := zapcore.NewCore(zapcore.NewConsoleEncoder(encCfg), zapcore.AddSync(buf), zapcore.InfoLevel)
	logger := zap.New(core, zap.AddCaller())
	logger.Info("verify-message", zap.String("k", "v"))
	out := buf.String()
	if !strings.Contains(out, "INFO") {
		t.Fatalf("expected capital INFO level in output, got %q", out)
	}
	if !strings.Contains(out, "verify-message") {
		t.Fatalf("expected message in output, got %q", out)
	}
	if !strings.Contains(out, "zapcompatverify") {
		t.Fatalf("expected caller (AddCaller) in output, got %q", out)
	}
	if err := logger.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}
}

// C4: zap 1.28 fix (#1501): zap.Object with a nil marshaler must not panic.
// On 1.27 this panics with a nil pointer dereference when the entry is
// encoded. Contract on head: no panic, no error.
func TestObjectNilMarshalerDoesNotPanic(t *testing.T) {
	buf := &bytes.Buffer{}
	core := zapcore.NewCore(zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()), zapcore.AddSync(buf), zapcore.DebugLevel)
	logger := zap.New(core)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("zap.Object(nil) panicked: %v", r)
		}
	}()
	logger.Info("nil-object", zap.Object("obj", nil))
}

// C5: zap 1.27.1/1.28 fix (#1511): NewLazyWith had a data race on the
// embedded Core field. Exercise concurrent With/Check/Write/Sync under
// -race; contract: no race, all writes delivered after init.
func TestLazyWithConcurrentUse(t *testing.T) {
	buf := &lockedBuffer{}
	core := zapcore.NewCore(zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()), zapcore.AddSync(buf), zapcore.InfoLevel)
	lazy := zapcore.NewLazyWith(core, []zapcore.Field{zap.String("lazy", "ctx")})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			l := zap.New(lazy).With(zap.Int("i", i))
			l.Info("concurrent-write")
			_ = lazy.Sync()
		}(i)
	}
	wg.Wait()
	if got := strings.Count(buf.String(), "concurrent-write"); got != 8 {
		t.Fatalf("expected 8 lazy writes, got %d", got)
	}
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
