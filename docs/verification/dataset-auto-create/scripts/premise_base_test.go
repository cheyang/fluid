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

package controllers

// P0 premise check for PR #5674 (fluid-cloudnative/fluid), run against the BASE branch.
// The raw annotation string is used (not common.AnnotationDatasetPolicy) so this file
// compiles on base, where the constant does not exist.
//
// Claim under test: given a Runtime annotated fluid.io/dataset-policy=auto-create with
// no Dataset of the same name/namespace, reconciliation auto-creates the Dataset.
// On base this is expected to FAIL (the feature does not exist), which is the
// differential evidence that the premise of the feature request (#4518) is valid:
// today a runtime without a dataset just waits in the
// "No dataset can be bound to the runtime, waiting." loop.

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	base "github.com/fluid-cloudnative/fluid/pkg/ddc/base"
	cruntime "github.com/fluid-cloudnative/fluid/pkg/runtime"
)

var _ = Describe("P0 premise check on base: dataset auto-create policy", func() {
	It("auto-creates the Dataset for an annotated Runtime with no Dataset", func() {
		runtimeObj := newTestAlluxioRuntime(defaultNamespace, demoRuntimeName)
		runtimeObj.UID = types.UID(demoRuntimeName + "-uid")
		runtimeObj.Annotations = map[string]string{
			"fluid.io/dataset-policy": "auto-create",
		}
		impl := &testRuntimeReconcilerImplement{
			getOrCreateEngine: func(cruntime.ReconcileRequestContext) (base.Engine, error) {
				return &testEngine{}, nil
			},
		}
		r := newTestRuntimeReconcilerWithImplement(impl, runtimeObj)

		_, err := r.ReconcileInternal(newTestRequestContext(r.Client, runtimeObj, nil))
		Expect(err).NotTo(HaveOccurred())

		createdDataset := &datav1alpha1.Dataset{}
		err = r.Get(context.Background(), types.NamespacedName{Namespace: defaultNamespace, Name: demoRuntimeName}, createdDataset)
		Expect(err).NotTo(HaveOccurred(), "base branch should not have the auto-create feature; if this passes the premise is refuted")
	})
})
