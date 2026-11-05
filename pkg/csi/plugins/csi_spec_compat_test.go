/*
Copyright 2026 The Fluid Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package plugins

// Reviewer harness for PR #6229 (bump github.com/container-storage-interface/spec
// 1.8.0 -> 1.13.0). Additive test file only; production code is untouched.
//
// Contract under test (finding F1): after the CSI spec bump, the module must
// still compile and fluid's CSI driver server types must still satisfy the
// csi.*Server gRPC interfaces.
//
// Mechanism of the break: spec >= 1.10.0 ships code generated with
// protoc-gen-go-grpc v1.3.0, whose server interfaces add
// mustEmbedUnimplemented*Server() and new RPC methods (ControllerModifyVolume,
// GetSnapshot, ControllerListVolumeHealth, ControllerGetVolumeHealth,
// NodeGetVolumeHealth, NodeGetStorageHealth). The vendored
// github.com/kubernetes-csi/drivers v1.0.2 csi-common Default*Server types
// predate that pattern, so they — and fluid's controllerServer / nodeServer,
// which embed them — no longer satisfy the interfaces and the module fails to
// build ("missing method mustEmbedUnimplementedIdentityServer").
//
// Polarity: CONTRACT test. On the current PR head it fails (the package does
// not compile — that is the reproduction). It passes once the csi-common
// compatibility is restored (verified locally: embedding
// csi.Unimplemented*Server into the three csi-common Default*Server structs
// makes the build and all pkg/csi tests pass).

import (
	"context"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	csicommon "github.com/kubernetes-csi/drivers/pkg/csi-common"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Compile-time assertions: the vendored csi-common defaults must satisfy the
// spec 1.13.0 server interfaces. These are the sharp root-cause check — they
// fail to compile with a precise "missing method" diagnostic as soon as the
// csi spec outgrows whatever the vendored csi-common provides.
var _ csi.IdentityServer = (*csicommon.DefaultIdentityServer)(nil)
var _ csi.ControllerServer = (*csicommon.DefaultControllerServer)(nil)
var _ csi.NodeServer = (*csicommon.DefaultNodeServer)(nil)

// fluid's own servers (they embed the csi-common defaults).
var _ csi.ControllerServer = (*controllerServer)(nil)
var _ csi.NodeServer = (*nodeServer)(nil)

// TestCSISpecCompatHarness is the runtime marker keyed on by
// docs/verification/csi-spec-bump-claude/scripts/re-verify.sh (unit layer).
// It verifies that a spec >= 1.9 RPC fluid does not implement degrades to
// codes.Unimplemented instead of breaking the driver at compile time.
func TestCSISpecCompatHarness(t *testing.T) {
	driver := csicommon.NewCSIDriver("fuse.csi.fluid.io", "1.0.0", "test-node")
	cs := &controllerServer{
		DefaultControllerServer: csicommon.NewDefaultControllerServer(driver),
	}

	resp, err := cs.ControllerModifyVolume(context.Background(), &csi.ControllerModifyVolumeRequest{})
	if err == nil {
		t.Fatalf("expected an error from the unimplemented ControllerModifyVolume RPC, got resp=%v", resp)
	}
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("expected codes.Unimplemented from ControllerModifyVolume, got %v", err)
	}
}
