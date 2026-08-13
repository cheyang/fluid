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
// These are deliberately plain `go test` functions rather than Ginkgo specs, so that
// re-verify.sh can resolve a verdict per finding from `go test -json` output.
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
	corev1 "k8s.io/api/core/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// newVerifyNodeServer mirrors the nodeServer construction in nodeserver_test.go.
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

// publishWithSubPath drives the real NodePublishVolume with the symlink publish
// method, which makes the mount source the CSI plugin computed directly observable:
// the symlink created at targetPath points at exactly that path. This avoids needing
// a real FUSE mount or a privileged bind mount to observe the escape.
//
// AnnotationSkipCheckMountReadyTarget=MountPod routes past the check_mount.sh shell
// helper (absent outside the CSI image) and into checkMountPathExists instead.
func publishWithSubPath(t *testing.T, ns *nodeServer, fluidPath, subPath, targetPath string) (string, error) {
	t.Helper()

	req := &csi.NodePublishVolumeRequest{
		VolumeId:   "verify-volume-pr6159",
		TargetPath: targetPath,
		VolumeContext: map[string]string{
			common.VolumeAttrFluidPath:                fluidPath,
			common.VolumeAttrFluidSubPath:             subPath,
			common.VolumeAttrMountType:                common.AlluxioMountType,
			common.NodePublishMethod:                  common.NodePublishMethodSymlink,
			common.AnnotationSkipCheckMountReadyTarget: "MountPod",
		},
	}

	_, err := ns.NodePublishVolume(context.Background(), req)
	if err != nil {
		return "", err
	}

	link, readErr := os.Readlink(targetPath)
	if readErr != nil {
		t.Fatalf("expected a symlink at targetPath %s, but could not read it: %v", targetPath, readErr)
	}
	return link, nil
}

func requireLinux(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skipf("NodePublishVolume reads /proc/mounts; this harness requires linux, got %s", runtime.GOOS)
	}
}

// TestVerifyPR6159_P0_SubPathTraversalIsContainedInFuseRoot is the PREMISE check (claim P0).
//
// POLARITY: contract test. It asserts the behavior the PR is trying to establish —
// that an untrusted fluid_sub_path cannot make the CSI plugin publish a path outside
// the FUSE mount root.
//
//	against BASE (05f0665): FAILS  => the escape is real, PR premise Confirmed
//	against PR HEAD:        PASSES => the lexical clamp closes this path
//
// The assertion is on real content read through the published path, not just on the
// string, so a red result is a demonstration of host-file exposure rather than an
// inference about it.
func TestVerifyPR6159_P0_SubPathTraversalIsContainedInFuseRoot(t *testing.T) {
	requireLinux(t)

	root := t.TempDir()

	// A plausible Fluid FUSE mount root, five levels below `root`.
	fuseRoot := filepath.Join(root, "runtime-mnt", "alluxio", "default", "my-dataset", "alluxio-fuse")
	if err := os.MkdirAll(fuseRoot, 0755); err != nil {
		t.Fatal(err)
	}

	// Host-side content that the tenant must never be able to reach through the volume.
	hostOnly := filepath.Join(root, "hostonly")
	if err := os.MkdirAll(hostOnly, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hostOnly, "marker"), []byte("HOST-ONLY-CONTENT"), 0644); err != nil {
		t.Fatal(err)
	}

	// A same-named directory *inside* the dataset, so that the clamped path also
	// exists and checkMountPathExists returns immediately on patched code.
	insideDataset := filepath.Join(fuseRoot, "hostonly")
	if err := os.MkdirAll(insideDataset, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(insideDataset, "marker"), []byte("DATASET-CONTENT"), 0644); err != nil {
		t.Fatal(err)
	}

	// What a tenant can express today via `dataset://<ns>/<name>/<subpath>`:
	// there is no character or traversal validation on Dataset spec.mounts[].mountPoint.
	subPath := "../../../../../hostonly"
	targetPath := filepath.Join(root, "kubelet", "pods", "verify-pod", "volumes", "fluid-vol")

	link, err := publishWithSubPath(t, newVerifyNodeServer(), fuseRoot, subPath, targetPath)
	if err != nil {
		t.Fatalf("NodePublishVolume returned an unexpected error: %v", err)
	}

	resolvedLink := filepath.Clean(link)
	t.Logf("fuse root        : %s", fuseRoot)
	t.Logf("published symlink: %s", link)
	t.Logf("resolves to      : %s", resolvedLink)

	got, err := os.ReadFile(filepath.Join(targetPath, "marker"))
	if err != nil {
		t.Fatalf("could not read through the published volume: %v", err)
	}
	t.Logf("content visible in the pod: %q", string(got))

	if !strings.HasPrefix(resolvedLink, fuseRoot) {
		t.Errorf("ESCAPE: published mount source %q resolves outside the FUSE root %q", resolvedLink, fuseRoot)
	}
	if string(got) != "DATASET-CONTENT" {
		t.Errorf("ESCAPE: the pod sees %q; host-only content is reachable through fluid_sub_path %q",
			string(got), subPath)
	}
}

// TestVerifyPR6159_F3_AbsoluteSubPathStaysBackwardCompatible checks the upgrade path
// for PVs that were already provisioned with an ABSOLUTE fluid_sub_path.
//
// Such a value is reachable on the base branch: GetPhysicalDatasetSubPath uses
// strings.SplitAfterN(path, "/", 3), so `dataset://ns/ds//sub-c` (note the double
// slash) yields subPath "/sub-c", which is written verbatim into the PV attribute by
// referencedataset/volume.go. On base, `fluidPath + "/" + "/sub-c"` collapses in POSIX
// and the volume mounts fine. PR 6159 makes NodePublishVolume reject it outright.
//
// The PV attribute is set only at PV creation time (`if !found`), so upgrading the CSI
// plugin does not rewrite already-persisted values.
//
// POLARITY: contract test asserting backward compatibility.
//
//	against BASE:    PASSES => the value mounts today
//	against PR HEAD: FAILS  => regression confirmed (InvalidArgument on an existing PV)
func TestVerifyPR6159_F3_AbsoluteSubPathStaysBackwardCompatible(t *testing.T) {
	requireLinux(t)

	root := t.TempDir()
	fuseRoot := filepath.Join(root, "runtime-mnt", "alluxio", "default", "my-dataset", "alluxio-fuse")
	sub := filepath.Join(fuseRoot, "sub-c")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "marker"), []byte("DATASET-CONTENT"), 0644); err != nil {
		t.Fatal(err)
	}

	// Exactly what a pre-upgrade PV can hold.
	subPath := "/sub-c"
	targetPath := filepath.Join(root, "kubelet", "pods", "verify-pod", "volumes", "fluid-vol")

	link, err := publishWithSubPath(t, newVerifyNodeServer(), fuseRoot, subPath, targetPath)
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
