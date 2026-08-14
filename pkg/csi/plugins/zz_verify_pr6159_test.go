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

// Verification harness for review of PR #6159 ("optim(utils): sanitize and clamp
// subpaths to handle path escaping"). Reviewer-private: additive test code only,
// production code is untouched.
//
// ROUND 2 — updated for head 6432fdfe ("optim(nodeserver): validate fluid_path and
// reject symlink mount paths"), which replaced utils.CleanSubPath in this file with
// filepath.IsLocal, added checkPathUnderMountRoot, and added checkSymlinkFile.
//
// Plain `go test` functions rather than Ginkgo specs so re-verify.sh can resolve a
// verdict per finding from `go test -json`.
//
// Linux only: NodePublishVolume calls utils.IsMounted, which reads /proc/mounts.

package plugins

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/utils"
	csicommon "github.com/kubernetes-csi/drivers/pkg/csi-common"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newVerifyNodeServer() *nodeServer {
	scheme := k8sruntime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = v1alpha1.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	return &nodeServer{
		nodeId:            "verify-node",
		DefaultNodeServer: csicommon.NewDefaultNodeServer(&csicommon.CSIDriver{}),
		client:            c,
		apiReader:         c,
		locks:             utils.NewVolumeLocks(),
	}
}

func requireLinux(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skipf("NodePublishVolume reads /proc/mounts; this harness requires linux, got %s", runtime.GOOS)
	}
}

// shortTempDir returns a temp dir whose every path component satisfies
// validation.IsValidMountRoot (relaxed DNS-1123 per component, 63 char limit).
// t.TempDir() embeds the test name, which is too long and would make GetMountRoot fail
// for reasons unrelated to the claim under test.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "fl6159")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// publishWithSubPath drives the real NodePublishVolume with the symlink publish method,
// which makes the mount source the CSI plugin computed directly observable: the symlink
// created at targetPath points at exactly that path. No FUSE mount or privileged bind
// mount needed.
//
// AnnotationSkipCheckMountReadyTarget=MountPod routes past check_mount.sh (absent outside
// the CSI image) into checkMountPathExists instead.
func publishWithSubPath(t *testing.T, fluidPath, subPath, targetPath string) (string, error) {
	t.Helper()

	req := &csi.NodePublishVolumeRequest{
		VolumeId:   "verify-volume-pr6159",
		TargetPath: targetPath,
		VolumeContext: map[string]string{
			common.VolumeAttrFluidPath:                 fluidPath,
			common.VolumeAttrFluidSubPath:              subPath,
			common.VolumeAttrMountType:                 common.AlluxioMountType,
			common.NodePublishMethod:                   common.NodePublishMethodSymlink,
			common.AnnotationSkipCheckMountReadyTarget: "MountPod",
		},
	}

	if _, err := newVerifyNodeServer().NodePublishVolume(context.Background(), req); err != nil {
		return "", err
	}

	link, readErr := os.Readlink(targetPath)
	if readErr != nil {
		t.Fatalf("expected a symlink at targetPath %s, but could not read it: %v", targetPath, readErr)
	}
	return link, nil
}

// TestVerifyPR6159_P0_SubPathTraversalIsContainedInFuseRoot is the PREMISE check (P0).
//
// POLARITY: contract test. Containment is satisfied either by rejecting the request or by
// publishing a path that stays inside the FUSE root.
//
//	against BASE (05f0665): FAILS  => the escape is real, premise Confirmed
//	against head 70d75f12:  PASSES => lexical clamp contains it
//	against head 6432fdfe:  PASSES => filepath.IsLocal rejects it outright
func TestVerifyPR6159_P0_SubPathTraversalIsContainedInFuseRoot(t *testing.T) {
	requireLinux(t)

	root := shortTempDir(t)
	t.Setenv(utils.MountRoot, root)

	fuseRoot := filepath.Join(root, "alluxio-fuse")
	if err := os.MkdirAll(fuseRoot, 0755); err != nil {
		t.Fatal(err)
	}

	hostOnly := filepath.Join(root, "hostonly")
	if err := os.MkdirAll(hostOnly, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hostOnly, "marker"), []byte("HOST-ONLY-CONTENT"), 0644); err != nil {
		t.Fatal(err)
	}

	// Same-named dir inside the dataset so a clamping implementation also finds an
	// existing path and checkMountPathExists returns immediately.
	insideDataset := filepath.Join(fuseRoot, "hostonly")
	if err := os.MkdirAll(insideDataset, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(insideDataset, "marker"), []byte("DATASET-CONTENT"), 0644); err != nil {
		t.Fatal(err)
	}

	subPath := "../hostonly"
	targetPath := filepath.Join(root, "target")

	link, err := publishWithSubPath(t, fuseRoot, subPath, targetPath)
	if err != nil {
		t.Logf("CONTAINED by rejection: %v", err)
		return
	}

	resolved := filepath.Clean(link)
	t.Logf("published symlink: %s", link)
	t.Logf("resolves to      : %s", resolved)

	got, readErr := os.ReadFile(filepath.Join(targetPath, "marker"))
	if readErr == nil {
		t.Logf("content visible in the pod: %q", string(got))
	}

	if !strings.HasPrefix(resolved, fuseRoot) {
		t.Errorf("ESCAPE: published mount source %q resolves outside the FUSE root %q", resolved, fuseRoot)
	}
	if string(got) == "HOST-ONLY-CONTENT" {
		t.Errorf("ESCAPE: host-only content is reachable through fluid_sub_path %q", subPath)
	}
}

// TestVerifyPR6159_N1_SymlinkCheckCoversIntermediateComponents targets the NEW guard
// added at head 6432fdfe:
//
//	if isSymlinkFile, err := checkSymlinkFile(mountPath); ... // nodeserver.go
//
// checkSymlinkFile does os.Lstat(mountPath), which only inspects the FINAL component.
// When the sub path has more than one component and an EARLIER component is a symlink,
// Lstat follows that symlink and stats the real target, reports "not a symlink", and the
// bind mount / target symlink is then created against a path outside the FUSE root.
//
// The new test the author added (nodeserver_test.go, "should reject a mount path that is
// a symlink") only exercises subPath "evil" — the single-component case that works.
//
// POLARITY: contract test.
//
//	against head 6432fdfe: FAILS => the symlink guard is incomplete
//	after a fix (resolve the whole path beneath the root): PASSES
func TestVerifyPR6159_N1_SymlinkCheckCoversIntermediateComponents(t *testing.T) {
	requireLinux(t)

	root := shortTempDir(t)
	outside := shortTempDir(t) // a separate tree: a genuine host location
	t.Setenv(utils.MountRoot, root)

	fuseRoot := filepath.Join(root, "alluxio-fuse")
	if err := os.MkdirAll(fuseRoot, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(outside, "inner"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "inner", "marker"), []byte("HOST-ONLY-CONTENT"), 0644); err != nil {
		t.Fatal(err)
	}

	// Ordinary dataset content: a symlink inside the mounted filesystem. Fluid does not
	// create this, the UFS does.
	if err := os.Symlink(outside, filepath.Join(fuseRoot, "evil")); err != nil {
		t.Fatal(err)
	}

	// Two components, the FIRST of which is the symlink.
	subPath := "evil/inner"
	if !filepath.IsLocal(subPath) {
		t.Fatalf("precondition: %q should pass filepath.IsLocal", subPath)
	}

	targetPath := filepath.Join(root, "target")

	link, err := publishWithSubPath(t, fuseRoot, subPath, targetPath)
	if err != nil {
		t.Logf("CONTAINED by rejection: %v", err)
		return
	}

	t.Logf("published symlink: %s", link)

	resolved, evalErr := filepath.EvalSymlinks(link)
	if evalErr != nil {
		t.Fatalf("could not resolve %s: %v", link, evalErr)
	}
	t.Logf("resolves to      : %s", resolved)

	got, readErr := os.ReadFile(filepath.Join(targetPath, "marker"))
	if readErr == nil {
		t.Logf("content visible in the pod: %q", string(got))
	}

	realFuseRoot, err := filepath.EvalSymlinks(fuseRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(resolved, realFuseRoot) {
		t.Errorf("ESCAPE: sub path %q published a mount source resolving to %q, outside the FUSE root %q",
			subPath, resolved, realFuseRoot)
	}
	if string(got) == "HOST-ONLY-CONTENT" {
		t.Errorf("ESCAPE: content outside the FUSE root is reachable through sub path %q", subPath)
	}
}

// TestVerifyPR6159_N1guard_SingleComponentSymlinkIsRejected is the bites guard for N1:
// the single-component case the author's new check does handle. It must stay green, and
// its contrast with N1 is what shows the guard is incomplete rather than absent.
func TestVerifyPR6159_N1guard_SingleComponentSymlinkIsRejected(t *testing.T) {
	requireLinux(t)

	root := shortTempDir(t)
	outside := shortTempDir(t)
	t.Setenv(utils.MountRoot, root)

	fuseRoot := filepath.Join(root, "alluxio-fuse")
	if err := os.MkdirAll(fuseRoot, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(fuseRoot, "evil")); err != nil {
		t.Fatal(err)
	}

	_, err := publishWithSubPath(t, fuseRoot, "evil", filepath.Join(root, "target"))
	if err == nil {
		t.Errorf("expected the single-component symlink sub path to be rejected, but publish succeeded")
		return
	}
	t.Logf("rejected as expected: code=%s msg=%v", status.Code(err), err)
}

// TestVerifyPR6159_F3_AbsoluteSubPathStaysBackwardCompatible checks the upgrade path for
// PVs already provisioned with an ABSOLUTE fluid_sub_path.
//
// Reachable on base: GetPhysicalDatasetSubPath uses strings.SplitAfterN(path, "/", 3), so
// `dataset://ns/ds//sub-c` yields "/sub-c", written verbatim into the PV attribute by
// referencedataset/volume.go:95. On base, `fluidPath + "/" + "/sub-c"` collapses in POSIX
// and mounts fine. The attribute is only set at PV creation (`if !found`), so upgrading
// does not rewrite already-persisted values.
//
// At head 70d75f12 filepath.IsAbs rejected it; at head 6432fdfe filepath.IsLocal still
// rejects it. The regression survived the round-2 rework.
//
// POLARITY: contract test asserting backward compatibility.
//
//	against BASE:          PASSES => the value mounts today
//	against head 6432fdfe: FAILS  => regression confirmed
func TestVerifyPR6159_F3_AbsoluteSubPathStaysBackwardCompatible(t *testing.T) {
	requireLinux(t)

	root := shortTempDir(t)
	t.Setenv(utils.MountRoot, root)

	fuseRoot := filepath.Join(root, "alluxio-fuse")
	sub := filepath.Join(fuseRoot, "sub-c")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "marker"), []byte("DATASET-CONTENT"), 0644); err != nil {
		t.Fatal(err)
	}

	subPath := "/sub-c" // exactly what a pre-upgrade PV can hold
	targetPath := filepath.Join(root, "target")

	link, err := publishWithSubPath(t, fuseRoot, subPath, targetPath)
	if err != nil {
		t.Fatalf("REGRESSION: an already-persisted absolute fluid_sub_path %q no longer publishes: %v",
			subPath, err)
	}

	t.Logf("published symlink: %s", link)

	got, err := os.ReadFile(filepath.Join(targetPath, "marker"))
	if err != nil {
		t.Fatalf("REGRESSION: could not read through the published volume: %v", err)
	}
	if string(got) != "DATASET-CONTENT" {
		t.Errorf("expected the dataset content to stay reachable, got %q", string(got))
	}
}

// TestVerifyPR6159_N2_MountRootIsANewHardDependency documents that NodePublishVolume did
// not consult MOUNT_ROOT before head 6432fdfe and now fails closed without it.
// GetMountRoot returns an error for an empty MOUNT_ROOT (validation.IsValidMountRoot),
// so checkPathUnderMountRoot turns a missing env var into InvalidArgument for every
// volume.
//
// The shipped Helm chart defaults runtime.mountRoot to /runtime-mnt, so a default install
// is unaffected; config/fluid/bases/csi/daemonset.yaml does not set it, but no Makefile
// target deploys from there. Recorded as an observation, not a blocker.
//
// POLARITY: informational — asserts the dependency exists, so it stays green either way.
func TestVerifyPR6159_N2_MountRootIsANewHardDependency(t *testing.T) {
	requireLinux(t)

	root := shortTempDir(t)
	t.Setenv(utils.MountRoot, "") // simulate the env var being absent

	fuseRoot := filepath.Join(root, "alluxio-fuse")
	if err := os.MkdirAll(fuseRoot, 0755); err != nil {
		t.Fatal(err)
	}

	_, err := publishWithSubPath(t, fuseRoot, "", filepath.Join(root, "target"))
	if err == nil {
		t.Logf("MOUNT_ROOT is not required for publishing (pre-6432fdfe behavior)")
		return
	}
	t.Logf("with MOUNT_ROOT unset, publishing fails closed: code=%s msg=%v", status.Code(err), err)
}
