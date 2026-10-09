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

package engine

// Verification harness for PR #6181 (reviewer: Claude, round 2026-10-10).
// Layer: L1 deterministic unit. HEAD-ONLY: references convertToLegacyTieredStore,
// which does not exist on the merge-base. Do not graft onto the base ref.
//
// Claim mapping:
//   V3-roundtrip [contract] hostPath QuotaList entries are serialized with
//                resource.Quantity.String() and re-parsed downstream by
//                convertToTieredstoreInfo (resource.ParseQuantity). Every
//                quota format a CRD user can write must survive that
//                round-trip: no parse error, and the summed capacity must be
//                exact. Table-driven over BinarySI/DecimalSI/decimal-mantissa
//                formats, including the multi-level and multi-path shapes.

import (
	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/ddc/base"
	"github.com/fluid-cloudnative/fluid/pkg/utils/tieredstore"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/resource"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Verification harness: PR #6181 quota string round-trip", Label("pr-6181-verification"), func() {

	type pathQuota struct {
		path  string
		quota string
	}

	DescribeTable("hostPath quota formats survive the legacy conversion and re-parse exactly",
		func(paths []pathQuota, expectedDisk string) {
			levels := []datav1alpha1.RuntimeTieredStoreLevel{}
			hostPath := &datav1alpha1.HostPathMediumSource{}
			for _, pq := range paths {
				hostPath.Paths = append(hostPath.Paths, pq.path)
				hostPath.Quotas = append(hostPath.Quotas, resource.MustParse(pq.quota))
			}
			levels = append(levels, datav1alpha1.RuntimeTieredStoreLevel{HostPath: hostPath})

			legacy := convertToLegacyTieredStore(datav1alpha1.RuntimeTieredStore{Levels: levels}, logr.Discard())
			Expect(legacy.Levels).To(HaveLen(1))

			// the full production path: WithTieredStore -> convertToTieredstoreInfo
			// -> GetLevelStorageMap. A parse failure would surface as an error
			// from BuildRuntimeInfo and fail the whole reconcile.
			runtimeInfo, err := base.BuildRuntimeInfo("roundtrip", "default", common.CacheRuntime,
				base.WithTieredStore(legacy))
			Expect(err).NotTo(HaveOccurred())

			storage := tieredstore.GetLevelStorageMap(runtimeInfo)
			Expect(storage).To(HaveLen(1))
			disk, ok := storage[common.DiskCacheStore]
			Expect(ok).To(BeTrue())
			Expect(disk.Cmp(resource.MustParse(expectedDisk))).To(Equal(0))
		},
		Entry("binary SI suffixes", []pathQuota{
			{"/mnt/a", "100Gi"}, {"/mnt/b", "50Gi"},
		}, "150Gi"),
		Entry("decimal SI suffixes", []pathQuota{
			{"/mnt/a", "1500M"}, {"/mnt/b", "500M"},
		}, "2000M"),
		Entry("decimal mantissa with binary suffix", []pathQuota{
			{"/mnt/a", "1.5Gi"},
		}, "1.5Gi"),
		Entry("mixed formats across paths", []pathQuota{
			{"/mnt/a", "1.5Gi"}, {"/mnt/b", "1500M"}, {"/mnt/c", "1024Mi"},
		}, "4184354560"),
		Entry("small binary units", []pathQuota{
			{"/mnt/a", "512Ki"}, {"/mnt/b", "512Ki"},
		}, "1Mi"),
		Entry("plain byte count", []pathQuota{
			{"/mnt/a", "1024"},
		}, "1024"),
	)

	It("contract: multiple hostPath levels in one tiered store all count toward disk capacity", func() {
		legacy := convertToLegacyTieredStore(datav1alpha1.RuntimeTieredStore{
			Levels: []datav1alpha1.RuntimeTieredStoreLevel{
				{
					HostPath: &datav1alpha1.HostPathMediumSource{
						Paths:  []string{"/mnt/a"},
						Quotas: []resource.Quantity{resource.MustParse("1Gi")},
					},
				},
				{
					HostPath: &datav1alpha1.HostPathMediumSource{
						Paths:  []string{"/mnt/b", "/mnt/c"},
						Quotas: []resource.Quantity{resource.MustParse("2Gi"), resource.MustParse("3Gi")},
					},
				},
			},
		}, logr.Discard())

		runtimeInfo, err := base.BuildRuntimeInfo("roundtrip2", "default", common.CacheRuntime,
			base.WithTieredStore(legacy))
		Expect(err).NotTo(HaveOccurred())

		storage := tieredstore.GetLevelStorageMap(runtimeInfo)
		Expect(storage[common.DiskCacheStore].Cmp(resource.MustParse("6Gi"))).To(Equal(0))
	})
})
