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

// Review verification harness for PR #5674 (fluid-cloudnative/fluid).
// Finding F4 (contract polarity: RED on the PR head, expected to go GREEN once fixed).
//
// Claim under test: a Runtime that is already terminating (deletionTimestamp set)
// must not auto-create a new Dataset.
//
// Mechanism under test: in ReconcileInternal the "dataset not found" lookup (step 5)
// runs BEFORE the deletion check (step 6). When the runtime carries
// fluid.io/dataset-policy=auto-create and its Dataset is gone (the normal state while
// the runtime is terminating, because the auto-created Dataset is controller-owned by
// the runtime and is deleted by GC first), every deletion reconcile creates a fresh
// phantom Dataset just to unbind and delete it again.

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"

	ctrl "sigs.k8s.io/controller-runtime"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	base "github.com/fluid-cloudnative/fluid/pkg/ddc/base"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	cruntime "github.com/fluid-cloudnative/fluid/pkg/runtime"
)

var _ = Describe("ReconcileInternal auto-create during runtime deletion (PR #5674 verification)", func() {
	It("must not auto-create a Dataset for a Runtime that is being deleted", func() {
		runtimeObj := newDeletingRuntime(defaultNamespace, demoRuntimeName, runtimeProtectionFinalizer)
		runtimeObj.UID = types.UID(demoRuntimeName + "-uid")
		runtimeObj.Annotations = map[string]string{
			common.AnnotationDatasetPolicy: common.DatasetPolicyAutoCreate,
		}
		impl := &testRuntimeReconcilerImplement{
			getOrCreateEngine: func(cruntime.ReconcileRequestContext) (base.Engine, error) {
				return &testEngine{}, nil
			},
			reconcileRuntimeDeletion: func(base.Engine, cruntime.ReconcileRequestContext) (ctrl.Result, error) {
				return ctrl.Result{}, nil
			},
		}
		r := newTestRuntimeReconcilerWithImplement(impl, runtimeObj)

		_, err := r.ReconcileInternal(newTestRequestContext(r.Client, runtimeObj, nil))
		Expect(err).NotTo(HaveOccurred())

		createdDataset := &datav1alpha1.Dataset{}
		err = r.Get(context.Background(), types.NamespacedName{Namespace: defaultNamespace, Name: demoRuntimeName}, createdDataset)
		Expect(err).To(HaveOccurred(), "no Dataset should be auto-created while the runtime is terminating, but one was created")
	})
})
