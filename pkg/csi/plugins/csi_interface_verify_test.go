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

// Verification harness for the review of
// https://github.com/fluid-cloudnative/fluid/pull/5392
// (dependabot: bump k8s.io/kubernetes 1.29.15 -> 1.32.10).
//
// Additive test file only — no production code is modified by the harness.
//
// Covered claim:
//   F2 (blocker, contract): fluid's controllerServer must satisfy the CSI
//       spec's ControllerServer interface. The PR bumps
//       github.com/container-storage-interface/spec v1.8.0 -> v1.9.0, whose
//       generated ControllerServer gained ControllerModifyVolume.
//       controllerServer only embeds csicommon.DefaultControllerServer (built
//       for the v1.8 interface), so on the PR head this package fails to
//       build with:
//         pkg/csi/plugins/driver.go:107:3: cannot use d.newControllerServer()
//           ... missing method ControllerModifyVolume
//       (masked at the un-fixed head by the F1 apimachinery build failure;
//       it surfaces as soon as F1 is fixed).

import (
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
)

// Compile-time contract: controllerServer implements the full CSI
// ControllerServer interface of the vendored spec version.
var _ csi.ControllerServer = (*controllerServer)(nil)

func TestVerifyControllerServerSatisfiesCSIInterface(t *testing.T) {
	var cs csi.ControllerServer = &controllerServer{}
	if cs == nil {
		t.Fatal("controllerServer must satisfy csi.ControllerServer")
	}
	t.Log("controllerServer satisfies csi.ControllerServer: F2 CSI-interface breakage absent")
}
