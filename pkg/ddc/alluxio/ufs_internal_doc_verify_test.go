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

package alluxio

// Verification harness for PR #6082 (https://github.com/fluid-cloudnative/fluid/pull/6082).
//
// PR #6082 adds a doc comment to processUpdatingUFS claiming:
//
//	"It first determines the update method based on whether mount config map
//	 is enabled, then if the update succeeds, it resets the dataset's UfsTotal
//	 status to trigger a metadata sync, and finally updates the mount
//	 timestamp when new mount paths are added."
//
// These tests pin down the ACTUAL control flow so the comment's accuracy is
// checkable. Polarity is important:
//
//   - F1 (canary): "if the update succeeds" is wrong — the gate is `updateReady`,
//     not err == nil. In the configmap path, updateUFSWithMountConfigMapScript
//     can return (false, nil): the update ran without error but Alluxio's
//     mounted paths have not caught up with the configmap change yet
//     (documented propagation delay, see the "only ready when alluxio mounted
//     paths is the same as dataset mount paths" block), so processUpdatingUFS
//     retries on the next reconcile and does NOT reset UfsTotal or run
//     SyncMetadata. This canary PASSES while the code has this behavior and
//     FLIPS TO RED if the code is ever changed to match the comment's wording
//     (reset/sync whenever err == nil).
//   - F2 (canary): "finally updates the mount timestamp when new mount paths
//     are added" omits that updateMountTime is skipped when SyncMetadata fails
//     (processUpdatingUFS returns early with (true, nil)). This canary PASSES
//     while that early-return behavior exists.
//
// Harness-bites check (skill Step 4) was performed by temporarily editing the
// production gate in processUpdatingUFS from `if updateReady {` to `if err == nil {`
// — F1 flips to FAIL, proving the test exercises the path. The edit was then
// reverted; production code is untouched on this branch.

import (
	"context"
	"os"

	"github.com/agiledragon/gomonkey/v2"
	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/utils"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	"k8s.io/apimachinery/pkg/types"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("processUpdatingUFS doc-comment verification (PR #6082)", Label("pkg.ddc.alluxio.ufs_internal_doc_verify_test.go"), func() {
	var (
		dataset        *datav1alpha1.Dataset
		alluxioruntime *datav1alpha1.AlluxioRuntime
		engine         *AlluxioEngine
	)

	BeforeEach(func() {
		dataset, alluxioruntime = mockFluidObjectsForTests(types.NamespacedName{Namespace: mockNamespace, Name: mockDatasetName})
		engine = mockAlluxioEngineForTests(dataset, alluxioruntime)
	})

	// F1 — canary. Gate is updateReady, not "the update succeeds".
	It("F1: update succeeds (nil error) but updateReady=false: no UfsTotal reset, no SyncMetadata", func() {
		os.Setenv(MountConfigStorage, ConfigmapStorageName)
		defer os.Unsetenv(MountConfigStorage)

		// A concrete, already-computed UfsTotal; a reset would overwrite it
		// with metadataSyncNotDoneMsg ("[Calculating]").
		dataset.Status.UfsTotal = "100.00GiB"

		fakeClient := fake.NewFakeClientWithScheme(datav1alpha1.UnitTestScheme, dataset, alluxioruntime)
		engine.Client = fakeClient

		// Configmap path: update ran fine (nil error), but Alluxio's mounted
		// paths have not caught up with the configmap change -> not ready yet.
		patchConfigMap := gomonkey.ApplyPrivateMethod(engine, "updateUFSWithMountConfigMapScript", func(dataset *datav1alpha1.Dataset) (bool, error) {
			return false, nil
		})
		defer patchConfigMap.Reset()

		syncMetadataCalled := false
		patchSyncMetadata := gomonkey.ApplyMethodFunc(engine, "SyncMetadata", func() error {
			syncMetadataCalled = true
			return nil
		})
		defer patchSyncMetadata.Reset()

		ufsToUpdate := utils.NewUFSToUpdate(dataset)
		ready, err := engine.processUpdatingUFS(ufsToUpdate)

		Expect(err).To(BeNil(), "the update itself succeeded — no error")
		Expect(ready).To(BeFalse(), "but it is not ready yet: mounted paths not caught up")
		Expect(syncMetadataCalled).To(BeFalse(), "SyncMetadata must NOT run when updateReady is false")

		got := &datav1alpha1.Dataset{}
		err = fakeClient.Get(context.TODO(), types.NamespacedName{Namespace: mockNamespace, Name: mockDatasetName}, got)
		Expect(err).To(BeNil())
		Expect(got.Status.UfsTotal).To(Equal("100.00GiB"),
			"UfsTotal must NOT be reset to [Calculating] when updateReady is false, even though the update succeeded")
	})

	// F2 — canary. updateMountTime is skipped when SyncMetadata fails.
	It("F2: updateReady=true with paths to add but SyncMetadata failing: mount time NOT updated, returns (true, nil)", func() {
		os.Setenv(MountConfigStorage, "secret")
		defer os.Unsetenv(MountConfigStorage)

		ufsToUpdate := utils.NewUFSToUpdate(dataset)
		ufsToUpdate.AddMountPaths([]string{mockAlluxioPath})

		fakeClient := fake.NewFakeClientWithScheme(datav1alpha1.UnitTestScheme, dataset, alluxioruntime)
		engine.Client = fakeClient

		patchMountCmd := gomonkey.ApplyPrivateMethod(engine, "updatingUFSWithMountCommand", func(dataset *datav1alpha1.Dataset, ufsToUpdate *utils.UFSToUpdate) (bool, error) {
			return true, nil
		})
		defer patchMountCmd.Reset()

		// SyncMetadata is documented as non-critical: its error is reported and swallowed.
		patchSyncMetadata := gomonkey.ApplyMethodFunc(engine, "SyncMetadata", func() error {
			return errMock
		})
		defer patchSyncMetadata.Reset()

		ready, err := engine.processUpdatingUFS(ufsToUpdate)

		Expect(err).To(BeNil(), "SyncMetadata errors are deliberately swallowed")
		Expect(ready).To(BeTrue(), "the UFS update itself is ready")

		got := &datav1alpha1.AlluxioRuntime{}
		err = fakeClient.Get(context.TODO(), types.NamespacedName{Namespace: mockNamespace, Name: mockDatasetName}, got)
		Expect(err).To(BeNil())
		Expect(got.Status.MountTime).To(BeNil(),
			"updateMountTime must NOT run when SyncMetadata failed (early return), despite new mount paths being added")
	})
})
