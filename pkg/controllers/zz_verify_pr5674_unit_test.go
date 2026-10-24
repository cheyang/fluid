/*
Verification harness for PR #5674 (feat(runtime): support dataset auto-create policy).
ADDITIVE ONLY: this file is part of the review harness on branch
verify/runtime-dataset-autocreate-codex; it must never land in the PR itself.

Polarity legend:
  [contract]  asserts the intended-correct behavior. Fails on buggy code, passes once fixed.
  [canary]    asserts the current (suspect) behavior. Passes while the bug exists, flips when fixed.
*/

package controllers

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/ddc/base"
	cruntime "github.com/fluid-cloudnative/fluid/pkg/runtime"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("PR5674 verification: dataset auto-create policy", func() {

	// P0 / backward-compat [contract]: a runtime WITHOUT the annotation and without a
	// Dataset must keep the pre-PR behavior: wait (requeue) and create nothing.
	// Expected to PASS on both base and head.
	It("[contract] runtime without the annotation still waits and creates no dataset", func() {
		runtimeObj := newTestAlluxioRuntime(defaultNamespace, demoRuntimeName)
		impl := &testRuntimeReconcilerImplement{}
		r := newTestRuntimeReconcilerWithImplement(impl, runtimeObj)

		result, err := r.ReconcileInternal(newTestRequestContext(r.Client, runtimeObj, nil))

		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(5 * time.Second))
		ds := &datav1alpha1.Dataset{}
		getErr := r.Get(context.Background(), types.NamespacedName{Namespace: defaultNamespace, Name: demoRuntimeName}, ds)
		Expect(apierrors.IsNotFound(getErr)).To(BeTrue(), "no dataset may be created without the auto-create annotation")
	})

	// Feature [contract]: an annotated runtime with no Dataset gets a stub Dataset with the
	// policy annotation and a controller reference. PASSes on head, FAILs on base (feature).
	It("[contract] annotated runtime auto-creates a stub dataset bound to itself", func() {
		runtimeObj := newTestAlluxioRuntime(defaultNamespace, demoRuntimeName)
		runtimeObj.UID = types.UID(demoRuntimeName + "-uid")
		runtimeObj.Annotations = map[string]string{common.AnnotationDatasetPolicy: common.DatasetPolicyAutoCreate}
		impl := &testRuntimeReconcilerImplement{}
		r := newTestRuntimeReconcilerWithImplement(impl, runtimeObj)

		result, err := r.ReconcileInternal(newTestRequestContext(r.Client, runtimeObj, nil))

		Expect(err).NotTo(HaveOccurred())
		Expect(result.Requeue).To(BeTrue())
		ds := &datav1alpha1.Dataset{}
		Expect(r.Get(context.Background(), types.NamespacedName{Namespace: defaultNamespace, Name: demoRuntimeName}, ds)).To(Succeed())
		Expect(ds.Annotations[common.AnnotationDatasetPolicy]).To(Equal(common.DatasetPolicyAutoCreate))
		Expect(ds.Spec.Mounts).To(BeEmpty())
		Expect(ds.OwnerReferences).To(HaveLen(1))
		Expect(ds.OwnerReferences[0].Name).To(Equal(demoRuntimeName))
	})

	// F1 [contract]: a TERMINATING runtime (deletionTimestamp set) must not trigger
	// dataset auto-creation. ReconcileInternal runs the auto-create step (5) BEFORE the
	// deletion branch (6), so on the PR head a runtime that is being deleted with its
	// dataset missing spawns a brand-new Dataset owned by a terminating object.
	// Expected: FAIL on head (bug reproduced), PASS once the auto-create branch is
	// guarded by deletionTimestamp.IsZero().
	It("[contract] terminating runtime with the annotation must not auto-create a dataset", func() {
		runtimeObj := newDeletingRuntime(defaultNamespace, demoRuntimeName, runtimeProtectionFinalizer)
		runtimeObj.UID = types.UID(demoRuntimeName + "-uid")
		runtimeObj.Annotations = map[string]string{common.AnnotationDatasetPolicy: common.DatasetPolicyAutoCreate}
		var deletionReconciled bool
		impl := &testRuntimeReconcilerImplement{
			reconcileRuntimeDeletion: func(engine base.Engine, ctx cruntime.ReconcileRequestContext) (ctrl.Result, error) {
				deletionReconciled = true
				return ctrl.Result{}, nil
			},
		}
		r := newTestRuntimeReconcilerWithImplement(impl, runtimeObj)

		_, err := r.ReconcileInternal(newTestRequestContext(r.Client, runtimeObj, nil))

		Expect(err).NotTo(HaveOccurred())
		Expect(deletionReconciled).To(BeTrue())
		ds := &datav1alpha1.Dataset{}
		getErr := r.Get(context.Background(), types.NamespacedName{Namespace: defaultNamespace, Name: demoRuntimeName}, ds)
		Expect(apierrors.IsNotFound(getErr)).To(BeTrue(),
			"no dataset may be created for a runtime that is already being deleted")
	})

	// F2 [characterization]: the auto-created Dataset carries a controller owner
	// reference with blockOwnerDeletion=true. On clusters running the
	// OwnerReferencesPermissionEnforcement admission plugin (default-off upstream,
	// enabled on hardened distros), creating this object requires 'update' on the
	// OWNER's <kind>/finalizers subresource — which no fluid runtime-controller
	// ClusterRole grants. This test documents the triggering property; it PASSes
	// everywhere (it is evidence, not a red test).
	It("[characterization] auto-created dataset sets controller=true blockOwnerDeletion=true", func() {
		runtimeObj := newTestAlluxioRuntime(defaultNamespace, demoRuntimeName)
		runtimeObj.UID = types.UID(demoRuntimeName + "-uid")
		runtimeObj.Annotations = map[string]string{common.AnnotationDatasetPolicy: common.DatasetPolicyAutoCreate}
		r := newTestRuntimeReconcilerWithImplement(&testRuntimeReconcilerImplement{}, runtimeObj)

		_, err := r.ReconcileInternal(newTestRequestContext(r.Client, runtimeObj, nil))
		Expect(err).NotTo(HaveOccurred())

		ds := &datav1alpha1.Dataset{}
		Expect(r.Get(context.Background(), types.NamespacedName{Namespace: defaultNamespace, Name: demoRuntimeName}, ds)).To(Succeed())
		Expect(ds.OwnerReferences).To(HaveLen(1))
		ref := ds.OwnerReferences[0]
		Expect(ref.Controller).NotTo(BeNil())
		Expect(*ref.Controller).To(BeTrue())
		Expect(ref.BlockOwnerDeletion).NotTo(BeNil())
		Expect(*ref.BlockOwnerDeletion).To(BeTrue(),
			"blockOwnerDeletion=true is what triggers the OwnerReferencesPermissionEnforcement RBAC check")
	})

	// F3 [characterization]: the AlreadyExists adoption guard in ensureDatasetForRuntime
	// ("belongs to someone else and must not be silently taken over") only fires in a
	// stale-cache race window. On the very next reconcile the same-named foreign Dataset
	// is bound anyway through the normal path (GetDataset succeeds -> AddOwnerAndRequeue
	// makes the runtime owned by that Dataset). This test documents that steady-state
	// behavior so the guard's limited value is explicit.
	It("[characterization] same-named foreign dataset is adopted via the normal bind path", func() {
		runtimeObj := newTestAlluxioRuntime(defaultNamespace, demoRuntimeName)
		runtimeObj.UID = types.UID(demoRuntimeName + "-uid")
		runtimeObj.Annotations = map[string]string{common.AnnotationDatasetPolicy: common.DatasetPolicyAutoCreate}
		foreign := newTestDataset(defaultNamespace, demoRuntimeName) // no policy annotation, no owner
		r := newTestRuntimeReconcilerWithImplement(&testRuntimeReconcilerImplement{}, runtimeObj, foreign)

		result, err := r.ReconcileInternal(newTestRequestContext(r.Client, runtimeObj, nil))

		Expect(err).NotTo(HaveOccurred())
		Expect(result.Requeue).To(BeTrue()) // AddOwnerAndRequeue
		updated := newTestAlluxioRuntime(defaultNamespace, demoRuntimeName)
		Expect(r.Get(context.Background(), types.NamespacedName{Namespace: defaultNamespace, Name: demoRuntimeName}, updated)).To(Succeed())
		Expect(updated.OwnerReferences).To(HaveLen(1))
		Expect(updated.OwnerReferences[0].Kind).To(Equal("Dataset"))
		Expect(updated.OwnerReferences[0].Name).To(Equal(demoRuntimeName))
	})

	// F2 [contract]: every engine controller ClusterRole that can create datasets must
	// also hold update on its own <engine>runtimes/finalizers subresource. The
	// auto-created Dataset carries blockOwnerDeletion=true; on clusters running the
	// OwnerReferencesPermissionEnforcement admission plugin the CREATE is rejected
	// without that permission. FAILs on head (permission missing); passes once the
	// RBAC is added (or blockOwnerDeletion is dropped).
	It("[contract] engine ClusterRoles with dataset access grant owner finalizers update", func() {
		roleDir := filepath.Join("..", "..", "charts", "fluid", "fluid", "templates", "role")
		entries, err := os.ReadDir(roleDir)
		Expect(err).NotTo(HaveOccurred())
		missing := []string{}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			switch name {
			case "csi", "fluidapp", "webhook", "cache", "dataset":
				continue // not runtime engine controllers
			}
			raw, err := os.ReadFile(filepath.Join(roleDir, name, "rbac.yaml"))
			Expect(err).NotTo(HaveOccurred())
			content := string(raw)
			if strings.Contains(content, "- datasets") && !strings.Contains(content, "finalizers") {
				missing = append(missing, name)
			}
		}
		Expect(missing).To(BeEmpty(),
			"engine ClusterRoles missing <engine>runtimes/finalizers update: %v", missing)
	})
})
