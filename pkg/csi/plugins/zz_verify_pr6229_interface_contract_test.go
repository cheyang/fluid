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

// Review harness for PR #6229 (chore(deps): bump
// github.com/container-storage-interface/spec from 1.8.0 to 1.13.0).
//
// Polarity: CONTRACT test. On the PR head (csi spec v1.13.0) the whole
// package fails to compile because csi_grpc.pb.go (regenerated with a newer
// protoc-gen-go-grpc, since spec v1.10.0) requires every server
// implementation to embed the UnimplementedXxxServer structs, and neither
// the vendored github.com/kubernetes-csi/drivers/pkg/csi-common defaults
// (v1.0.2, repo archived/EOL) nor fluid's own servers do so.
// On the merge-base (csi spec v1.8.0) this file compiles and passes.
//
// The assertions below mirror exactly how pkg/csi/plugins/driver.go uses
// these types: NewDefaultIdentityServer / newControllerServer /
// newNodeServer are passed to csicommon's NonBlockingGRPCServer.Start as
// csi.IdentityServer / csi.ControllerServer / csi.NodeServer.

package plugins

import (
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	csicommon "github.com/kubernetes-csi/drivers/pkg/csi-common"
)

// Compile-time interface satisfaction contracts.
var (
	// csi-common's standalone identity default is passed as csi.IdentityServer
	// in csi-common/utils.go (RunNodePublishServer etc.) and in driver.go's
	// s.Start(csicommon.NewDefaultIdentityServer(d.csiDriver), ...).
	_ csi.IdentityServer = (*csicommon.DefaultIdentityServer)(nil)

	// fluid's controllerServer embeds *csicommon.DefaultControllerServer and
	// is passed as csi.ControllerServer in driver.go's s.Start(...).
	_ csi.ControllerServer = (*controllerServer)(nil)

	// fluid's nodeServer embeds *csicommon.DefaultNodeServer and is passed as
	// csi.NodeServer in driver.go's s.Start(...).
	_ csi.NodeServer = (*nodeServer)(nil)
)

func TestVerifyPR6229CSIInterfaceContracts(t *testing.T) {
	// Reaching this point means the package (and therefore the CSI plugin
	// chain cmd/csi -> pkg/csi -> pkg/csi/plugins -> csi-common) compiled
	// against the vendored container-storage-interface/spec.
	t.Log("csi-common defaults and fluid csi plugin servers satisfy the vendored csi server interfaces")
}
