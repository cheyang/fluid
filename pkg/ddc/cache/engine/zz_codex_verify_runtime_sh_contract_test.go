// Verification harness for PR #6286 (reviewer: codex). ADDITIVE ONLY — this
// file is not part of the PR; it lives on the verify/mooncake-clientless-e2e-codex
// branch and pins the contract between the merged runtime.sh generator and the
// Mooncake client-less e2e scripts that this PR rewrites to consume it:
//
//   - test/gha-e2e/mooncake/image/custom-entrypoint.sh (worker branch) sources
//     runtime.sh and reads MASTER_SERVICE_NAME, WORKER_SERVICE_NAME and
//     WORKER_TIEREDSTORE_LEVEL_0_QUOTA_0;
//   - test/gha-e2e/mooncake/rw_job.yaml sources the injected runtime.sh and
//     reads MASTER_NAME and MASTER_SERVICE_NAME to reach
//     "${MASTER_NAME}-0.${MASTER_SERVICE_NAME}" (previously the hardcoded
//     "mooncake-demo-master-0.svc-mooncake-demo-master").
//
// Polarity: CONTRACT. On code that breaks the contract this test FAILS.
package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fluid-cloudnative/fluid/pkg/common"
)

// mirror of test/gha-e2e/mooncake/{dataset,cacheruntime}.yaml: dataset
// "mooncake-demo", master+worker with headless services, one emptyDir tier
// with quota 1Gi.
func mooncakeE2EConfig() *common.CacheRuntimeConfig {
	dataset := "mooncake-demo"
	return &common.CacheRuntimeConfig{
		Master: &common.CacheRuntimeComponentConfig{
			Enabled:  true,
			Name:     common.GetCacheComponentName(dataset, common.ComponentTypeMaster),
			Replicas: 1,
			Service:  common.CacheRuntimeComponentServiceConfig{Name: GetComponentServiceName(dataset, common.ComponentTypeMaster)},
		},
		Worker: &common.CacheRuntimeComponentConfig{
			Enabled:  true,
			Name:     common.GetCacheComponentName(dataset, common.ComponentTypeWorker),
			Replicas: 1,
			Service:  common.CacheRuntimeComponentServiceConfig{Name: GetComponentServiceName(dataset, common.ComponentTypeWorker)},
			TieredStoreLevels: []common.TieredStoreLevelConfig{
				{MediumType: common.Memory, MountPaths: []string{GetEmptyDirTieredStoreMountPath(0)}, Quotas: []string{"1Gi"}, High: "0.8", Low: "0.5"},
			},
		},
	}
}

func TestRuntimeShContractForMooncakeE2E(t *testing.T) {
	e := &CacheEngine{}
	sh, err := e.generateRuntimeSh(mooncakeE2EConfig())
	if err != nil {
		t.Fatalf("generateRuntimeSh failed: %v", err)
	}

	// The exact exports the e2e scripts read, with the exact values the
	// mooncake-demo topology produces.
	wantExports := []string{
		"export MASTER_NAME='mooncake-demo-master'",
		"export MASTER_SERVICE_NAME='svc-mooncake-demo-master'",
		"export WORKER_NAME='mooncake-demo-worker'",
		"export WORKER_SERVICE_NAME='svc-mooncake-demo-worker'",
		"export WORKER_TIEREDSTORE_LEVEL_0_QUOTA_0='1Gi'",
	}
	for _, want := range wantExports {
		if !strings.Contains(sh, want+"\n") {
			t.Errorf("runtime.sh no longer exports %q; the mooncake e2e sources runtime.sh and reads this variable", want)
		}
	}

	// rw_job.yaml builds the master address as ${MASTER_NAME}-0.${MASTER_SERVICE_NAME};
	// it must keep resolving to the address the e2e used to hardcode.
	if !strings.Contains(sh, "export MASTER_NAME='mooncake-demo-master'") ||
		!strings.Contains(sh, "export MASTER_SERVICE_NAME='svc-mooncake-demo-master'") {
		t.Fatalf("master discovery via runtime.sh would no longer yield mooncake-demo-master-0.svc-mooncake-demo-master")
	}

	// Emit the generated runtime.sh as a fixture for the shell layer of the
	// harness, which sources it while running the real custom-entrypoint.sh.
	fixture := filepath.Join("..", "..", "..", "..", "docs", "verification", "mooncake-clientless-e2e", "results", "runtime.sh.generated")
	if err := os.MkdirAll(filepath.Dir(fixture), 0o755); err == nil {
		if werr := os.WriteFile(fixture, []byte(sh), 0o644); werr != nil {
			t.Logf("could not write fixture %s: %v (test result unaffected)", fixture, werr)
		}
	}
}
