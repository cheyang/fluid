package pr6292

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// diagnoseScripts maps each diagnose script to the runtime CRD kind it must describe.
var diagnoseScripts = []struct {
	name        string
	script      string
	runtimeKind string
}{
	{"alluxio", "diagnose-fluid-alluxio.sh", "alluxioruntime"},
	{"curvine", "diagnose-fluid-curvine.sh", "cacheruntime"},
	{"jindo", "diagnose-fluid-jindo.sh", "jindoruntime"},
	{"juicefs", "diagnose-fluid-juicefs.sh", "juicefsruntime"},
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "tools")); err != nil {
		t.Fatalf("repo root %q has no tools/ dir: %v", root, err)
	}
	return root
}

func writeStub(t *testing.T, binDir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir stub bin: %v", err)
	}
	p := filepath.Join(binDir, name)
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatalf("write stub %s: %v", name, err)
	}
}

// TestDescribeRuntimeUsesRequestedName_Stub is a CONTRACT test (unit layer).
//
// Claim (P0 + F-scope): `kubectl_resource` in each diagnose script must pass the
// runtime name given with `-r` to `kubectl describe <runtime-kind>`. On the base
// branch the scripts reference `${name}`, a variable that is never set, so kubectl
// receives no name and describes EVERY runtime of that kind in the namespace into
// an archive file named for a single runtime.
//
// Polarity: contract — passes on fixed code, fails on the buggy base. The failure
// on base doubles as the P0 premise reproduction.
func TestDescribeRuntimeUsesRequestedName_Stub(t *testing.T) {
	for _, tc := range diagnoseScripts {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			bin := filepath.Join(tmp, "bin")
			kubectlLog := filepath.Join(tmp, "kubectl.args")
			writeStub(t, bin, "kubectl", "#!/usr/bin/env bash\necho \"$@\" >> \""+kubectlLog+"\"\nexit 0\n")
			writeStub(t, bin, "helm", "#!/usr/bin/env bash\nexit 0\n")

			cmd := exec.Command("bash", filepath.Join(repoRoot(t), "tools", tc.script),
				"collect", "-r", "demo", "-n", "ns1",
				"--collect-path", filepath.Join(tmp, "out.tar.gz"))
			cmd.Dir = tmp
			cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("script %s failed: %v\n%s", tc.script, err, out)
			}

			logBytes, err := os.ReadFile(kubectlLog)
			if err != nil {
				t.Fatalf("kubectl stub was never called: %v", err)
			}
			found := false
			for _, line := range strings.Split(string(logBytes), "\n") {
				fields := strings.Fields(line)
				if len(fields) >= 2 && fields[0] == "describe" && fields[1] == tc.runtimeKind {
					found = true
					last := fields[len(fields)-1]
					if last != "demo" {
						t.Errorf("CONTRACT VIOLATION: %s ran `kubectl %s` — the requested runtime name is missing (last arg %q, want \"demo\"); kubectl therefore describes every %s in the namespace into %sruntime-demo.yaml",
							tc.script, line, last, tc.runtimeKind, tc.runtimeKind)
					}
				}
			}
			if !found {
				t.Fatalf("no `kubectl describe %s` invocation captured in %s", tc.runtimeKind, kubectlLog)
			}
		})
	}
}
