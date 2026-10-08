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

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
)

// Reviewer verification harness for https://github.com/fluid-cloudnative/fluid/pull/6200.
//
// POLARITY: contract test (asserts the intended correct behavior).
//   - On BASE d8b37f28 (without the patch): FAILS, printing the observed per-cycle
//     worker Get count (2) — that failure IS the reproduction of the PR's premise
//     ("each status update cycle read the worker AdvancedStatefulSet twice").
//   - On PR HEAD 1d66cffe (with the patch): PASSES with exactly 1 worker Get per cycle.
//
// The file deliberately only uses helpers that exist on both base and head
// (newStatusTestEngineWithClient, newAdvancedStatefulSetComponent, ...) so the same
// source grafts onto either ref. See docs/verification/cache-affinity-claude/README.md.
var _ = Describe("Premise verification: worker read count per status cycle", func() {
	It("should read the worker workload exactly once per status update cycle", func() {
		baseClient := fake.NewFakeClientWithScheme(
			CacheEngineTestScheme,
			newStatusTestRuntime(),
			newAdvancedStatefulSetComponent(testStatusMaster, testStatusNamespace, 1, 1),
			newAdvancedStatefulSetComponent(testStatusWorker, testStatusNamespace, 1, 1),
		)
		countingClient := &premiseGetCountingClient{Client: baseClient}
		testEngine, _ := newStatusTestEngineWithClient(countingClient)

		_, err := testEngine.CheckAndUpdateRuntimeStatus(newStatusTestRuntimeValue(false))
		Expect(err).NotTo(HaveOccurred())
		Expect(countingClient.workerGetCount).To(Equal(1),
			"expected exactly 1 Get of the worker workload per status cycle")

		_, err = testEngine.CheckAndUpdateRuntimeStatus(newStatusTestRuntimeValue(false))
		Expect(err).NotTo(HaveOccurred())
		Expect(countingClient.workerGetCount).To(Equal(2),
			"expected exactly 1 Get of the worker workload per status cycle (2 total after two cycles)")
	})
})

// premiseGetCountingClient counts Get calls whose key names the worker workload.
// Distinct type name so this file compiles whether or not the PR's own
// getCallCountingClient (status_test.go) is present.
type premiseGetCountingClient struct {
	ctrlclient.Client
	workerGetCount int
}

func (c *premiseGetCountingClient) Get(ctx context.Context, key types.NamespacedName, obj ctrlclient.Object, opts ...ctrlclient.GetOption) error {
	if key.Name == testStatusWorker {
		c.workerGetCount++
	}
	return c.Client.Get(ctx, key, obj, opts...)
}
