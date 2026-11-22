/*
Verification harness for fluid-cloudnative/fluid PR #6283 (reviewer: codex).
ADDITIVE ONLY - no production code is modified by this file.

Claim under test (issue #6282): JuiceFSEngine.isCommandChanged must detect a
changed mount option (e.g. cache-size) even when the dataset name (embedded in
the mount path) or an option name contains the substring "-o".

Polarity: every test here is a CONTRACT test - it asserts the intended correct
behavior. On the base branch (253f6bb) the *_ChangeDetected tests FAIL (that
failure is the premise reproduction); on the PR head they PASS.
*/

package juicefs

import (
	"fmt"
	"testing"

	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
)

// verifyCmd builds the exact single-line mount command shape produced by
// genFuseMount/genWorkerMount: exec <bin> <source> <mountPath> -o <options>
func verifyCmd(bin, source, datasetName, options string) string {
	return fmt.Sprintf("exec %s %s /runtime-mnt/juicefs/default/%s/juicefs-fuse -o %s",
		bin, source, datasetName, options)
}

// C1: dataset name contains "-o" (mount path contains it), only cache-size changes.
// Contract: changed must be true. Fails on base (bug reproduces), passes on head.
func TestVerifyPR6283_DatasetDashO_ChangeDetected(t *testing.T) {
	j := JuiceFSEngine{Log: fake.NullLogger()}
	crt := verifyCmd("/bin/mount.juicefs", "my-vol", "demo-output", "cache-dir=/dev/shm,cache-size=1024,metrics=0.0.0.0:9567")
	latest := verifyCmd("/bin/mount.juicefs", "my-vol", "demo-output", "cache-dir=/dev/shm,cache-size=2048,metrics=0.0.0.0:9567")
	changed, _ := j.isCommandChanged(crt, latest)
	if !changed {
		t.Errorf("dataset name contains -o: cache-size 1024->2048 must be detected as changed, got changed=false (crt=%q latest=%q)", crt, latest)
	}
}

// C2: an option name contains "-o" (cache-partial-only), only cache-size changes.
// Contract: changed must be true. Fails on base, passes on head.
func TestVerifyPR6283_OptionNameDashO_ChangeDetected(t *testing.T) {
	j := JuiceFSEngine{Log: fake.NullLogger()}
	crt := verifyCmd("/bin/mount.juicefs", "my-vol", "demo", "cache-partial-only,cache-size=1024")
	latest := verifyCmd("/bin/mount.juicefs", "my-vol", "demo", "cache-partial-only,cache-size=2048")
	changed, _ := j.isCommandChanged(crt, latest)
	if !changed {
		t.Errorf("option name contains -o: cache-size 1024->2048 must be detected as changed, got changed=false (crt=%q latest=%q)", crt, latest)
	}
}

// Guard: dataset name contains "-o", identical options in a different order.
// Contract: changed must be false (no over-reporting). Passes on base and head.
func TestVerifyPR6283_DatasetDashO_NoChangeDifferentOrder(t *testing.T) {
	j := JuiceFSEngine{Log: fake.NullLogger()}
	crt := verifyCmd("/bin/mount.juicefs", "my-vol", "demo-output", "cache-dir=/dev/shm,cache-size=1024")
	latest := verifyCmd("/bin/mount.juicefs", "my-vol", "demo-output", "cache-size=1024,cache-dir=/dev/shm")
	changed, _ := j.isCommandChanged(crt, latest)
	if changed {
		t.Errorf("same options in different order must NOT be reported as changed (crt=%q latest=%q)", crt, latest)
	}
}

// Guard: dataset name contains "-o", only the metrics option differs (hostNetwork case).
// Contract: changed must be false - metrics is intentionally ignored. Passes on base and head.
func TestVerifyPR6283_MetricsOptionIgnored(t *testing.T) {
	j := JuiceFSEngine{Log: fake.NullLogger()}
	crt := verifyCmd("/bin/mount.juicefs", "my-vol", "demo-output", "cache-size=1024,metrics=0.0.0.0:9567")
	latest := verifyCmd("/bin/mount.juicefs", "my-vol", "demo-output", "cache-size=1024,metrics=127.0.0.1:9567")
	changed, _ := j.isCommandChanged(crt, latest)
	if changed {
		t.Errorf("a metrics-only difference must NOT be reported as changed (crt=%q latest=%q)", crt, latest)
	}
}

// Guard: option removal must be detected even with a dash-o dataset name.
// Contract: changed must be true. Fails on base, passes on head.
func TestVerifyPR6283_DatasetDashO_OptionRemoved(t *testing.T) {
	j := JuiceFSEngine{Log: fake.NullLogger()}
	crt := verifyCmd("/bin/mount.juicefs", "my-vol", "demo-output", "cache-dir=/dev/shm,cache-size=1024")
	latest := verifyCmd("/bin/mount.juicefs", "my-vol", "demo-output", "cache-size=1024")
	changed, _ := j.isCommandChanged(crt, latest)
	if !changed {
		t.Errorf("removing the cache-dir option must be detected as changed (crt=%q latest=%q)", crt, latest)
	}
}

// L2-ish integration: drive the REAL command generators (genFuseMount /
// genWorkerMount) for a dataset whose name contains "-o", then feed the
// generated commands into isCommandChanged - exactly what syncWorkerSpec and
// updateFuseCmdConfigmapOnChanged do at runtime. This guards the fix against
// command-format drift, which hardcoded-string tests cannot see.
//
// The "changed" subcases FAIL on base (bug reproduces through the real
// generator) and PASS on head. The "unchanged" subcase passes on both and
// stresses order-insensitivity: genArgs iterates a Go map, so the option
// order in the generated command is random across calls.
func TestVerifyPR6283_GeneratorRoundTrip(t *testing.T) {
	const datasetName = "demo-output"
	mountPath := fmt.Sprintf("/runtime-mnt/juicefs/default/%s/juicefs-fuse", datasetName)

	newEngine := func() *JuiceFSEngine {
		return &JuiceFSEngine{
			name:      datasetName,
			namespace: "default",
			Log:       fake.NullLogger(),
			// runtimeInfo intentionally nil: genFuseMount then never touches Client
		}
	}

	newValue := func(edition string) *JuiceFS {
		return &JuiceFS{
			FullnameOverride: datasetName,
			Edition:          edition,
			Source:           "my-vol",
			Fuse:             Fuse{MountPath: mountPath},
			Worker:           Worker{MountPath: mountPath},
		}
	}

	editions := []struct {
		name    string
		edition string
	}{
		{"community", CommunityEdition},
		{"enterprise", EnterpriseEdition},
	}

	for _, ed := range editions {
		t.Run(ed.name+" fuse cache-size change detected", func(t *testing.T) {
			vOld := newValue(ed.edition)
			if err := newEngine().genFuseMount(vOld, map[string]string{"cache-size": "1024", "cache-dir": "/dev/shm"}); err != nil {
				t.Fatalf("genFuseMount: %v", err)
			}
			vNew := newValue(ed.edition)
			if err := newEngine().genFuseMount(vNew, map[string]string{"cache-size": "2048", "cache-dir": "/dev/shm"}); err != nil {
				t.Fatalf("genFuseMount: %v", err)
			}
			changed, _ := newEngine().isCommandChanged(vOld.Fuse.Command, vNew.Fuse.Command)
			if !changed {
				t.Errorf("real genFuseMount (%s): cache-size 1024->2048 must be detected.\n crt: %s\n new: %s", ed.name, vOld.Fuse.Command, vNew.Fuse.Command)
			}
		})

		t.Run(ed.name+" worker cache-size change detected", func(t *testing.T) {
			vOld := newValue(ed.edition)
			newEngine().genWorkerMount(vOld, map[string]string{"cache-size": "1024", "cache-dir": "/dev/shm"})
			vNew := newValue(ed.edition)
			newEngine().genWorkerMount(vNew, map[string]string{"cache-size": "2048", "cache-dir": "/dev/shm"})
			changed, _ := newEngine().isCommandChanged(vOld.Worker.Command, vNew.Worker.Command)
			if !changed {
				t.Errorf("real genWorkerMount (%s): cache-size 1024->2048 must be detected.\n crt: %s\n new: %s", ed.name, vOld.Worker.Command, vNew.Worker.Command)
			}
		})

		t.Run(ed.name+" identical options never reported changed", func(t *testing.T) {
			// map iteration order is random; generate 20 independent pairs.
			for i := 0; i < 20; i++ {
				opts := func() map[string]string {
					return map[string]string{"cache-size": "1024", "cache-dir": "/dev/shm", "free-space-ratio": "0.1", "cache-partial-only": ""}
				}
				vA := newValue(ed.edition)
				if err := newEngine().genFuseMount(vA, opts()); err != nil {
					t.Fatalf("genFuseMount: %v", err)
				}
				vB := newValue(ed.edition)
				if err := newEngine().genFuseMount(vB, opts()); err != nil {
					t.Fatalf("genFuseMount: %v", err)
				}
				changed, _ := newEngine().isCommandChanged(vA.Fuse.Command, vB.Fuse.Command)
				if changed {
					t.Fatalf("identical options must NOT be reported changed (iteration %d).\n a: %s\n b: %s", i, vA.Fuse.Command, vB.Fuse.Command)
				}
			}
		})
	}
}
