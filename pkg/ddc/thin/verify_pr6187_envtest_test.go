/*
L2 integration harness for the review of PR #6187 (additive; production code untouched).

Same contract as the L1 caller test, but against a real API server (envtest) so the
ConfigMap lookup and the missing-PVC NotFound go through the actual client/server
round trip instead of the fake client. Skipped unless KUBEBUILDER_ASSETS is set.
*/
package thin

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func TestVerifyPR6187ShouldUpdateUFSRealAPIServer(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS not set; skipping envtest integration layer")
	}
	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
	}
	cfg, err := testEnv.Start()
	if err != nil {
		t.Fatalf("start envtest: %v", err)
	}
	defer func() {
		if err := testEnv.Stop(); err != nil {
			t.Errorf("stop envtest: %v", err)
		}
	}()

	scheme := verifyPR6187Scheme(t)
	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	dataset, cm, _ := verifyPR6187Objects()
	ctx := context.Background()
	if err := c.Create(ctx, dataset); err != nil {
		t.Fatalf("create dataset: %v", err)
	}
	if err := c.Create(ctx, cm); err != nil {
		t.Fatalf("create fuse configmap: %v", err)
	}

	sink := &recordingSink{}
	engine := verifyPR6187Engine(c, sink)
	engine.Log = logr.New(sink)

	ufsToUpdate := engine.ShouldUpdateUFS()
	if ufsToUpdate != nil {
		t.Errorf("ShouldUpdateUFS() = %v, want nil (transform failure must not schedule an update)", ufsToUpdate)
	}
	if !sink.loggedErrorContaining("failed to extract volume info") {
		t.Errorf("transform failure was not surfaced: expected an Error log containing %q, got errs=%v msgs=%v",
			"failed to extract volume info", sink.errs, sink.errorMsgs)
	}
}
