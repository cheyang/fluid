/*
Integration-layer (envtest) verification for PR #5674.
ADDITIVE ONLY: review harness on branch verify/runtime-dataset-autocreate-codex.

Runs ONLY when VERIFY_PR5674_ENVTEST=1 is set and envtest binaries are available
(KUBEBUILDER_ASSETS or the default kubebuilder-envtest lookup). Otherwise it skips,
so a plain `go test ./pkg/controllers` is unaffected.

  I1 [contract]  a stub Dataset with empty spec.mounts passes CRD validation on a real
                 API server (the auto-create payload is API-valid).
  I2 [canary]    F3 — with the production wiring (cache-backed manager client), the
                 FIRST reconcile after auto-create fails: ensureDatasetForRuntime returns
                 the object passed to client.Create, whose TypeMeta is stripped by the
                 typed client, so AddOwnerAndRequeue writes an ownerReference with empty
                 apiVersion/kind onto the runtime and the API server rejects the update.
                 The SECOND reconcile heals via the cached read (TypeMeta populated by
                 the informer). On current head this canary PASSES by observing
                 r1Err != nil && r2 clean; once the GVK is set on the created object the
                 r1Err assertion flips to FAIL — invert it then.
  Note: envtest does not enable the OwnerReferencesPermissionEnforcement admission
  plugin, so the F2 RBAC concern (hardened clusters) is out of scope for this layer.
*/

package controllers

import (
	"context"
	"os"
	"path/filepath"
	"time"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	cruntime "github.com/fluid-cloudnative/fluid/pkg/runtime"
	fakeutil "github.com/fluid-cloudnative/fluid/pkg/utils/fake"
)

var _ = Describe("PR5674 envtest: auto-create against a real API server", Ordered, func() {
	var testEnv *envtest.Environment
	var directClient client.Client

	BeforeAll(func() {
		if os.Getenv("VERIFY_PR5674_ENVTEST") != "1" {
			Skip("set VERIFY_PR5674_ENVTEST=1 to run the envtest layer")
		}
		testEnv = &envtest.Environment{
			CRDDirectoryPaths: []string{filepath.Join("..", "..", "config", "crd", "bases")},
		}
		cfg, err := testEnv.Start()
		Expect(err).NotTo(HaveOccurred())
		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(datav1alpha1.AddToScheme(scheme)).To(Succeed())
		directClient, err = client.New(cfg, client.Options{Scheme: scheme})
		Expect(err).NotTo(HaveOccurred())
	})

	AfterAll(func() {
		if testEnv != nil {
			Expect(testEnv.Stop()).To(Succeed())
		}
	})

	It("I1: an empty-mounts stub Dataset is accepted by the real API server", func() {
		ds := &datav1alpha1.Dataset{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "pr5674-stub",
				Namespace:   "default",
				Annotations: map[string]string{common.AnnotationDatasetPolicy: common.DatasetPolicyAutoCreate},
			},
		}
		Expect(directClient.Create(context.Background(), ds)).To(Succeed())
		Expect(directClient.Delete(context.Background(), ds)).To(Succeed())
	})

	It("I2 [canary]: first auto-create reconcile errors on empty-GVK ownerRef, second heals", func() {
		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(datav1alpha1.AddToScheme(scheme)).To(Succeed())
		mgr, err := ctrl.NewManager(testEnv.Config, ctrl.Options{
			Scheme:  scheme,
			Metrics: metricsserver.Options{BindAddress: "0"},
		})
		Expect(err).NotTo(HaveOccurred())
		mgrCtx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { _ = mgr.Start(mgrCtx) }()
		Expect(mgr.GetCache().WaitForCacheSync(mgrCtx)).To(BeTrue())
		cl := mgr.GetClient()

		rt := newTestAlluxioRuntime("default", "pr5674-envtest")
		rt.Annotations = map[string]string{common.AnnotationDatasetPolicy: common.DatasetPolicyAutoCreate}
		Expect(cl.Create(context.Background(), rt)).To(Succeed())
		Eventually(func() error {
			return cl.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "pr5674-envtest"}, newTestAlluxioRuntime("default", "pr5674-envtest"))
		}, 10*time.Second, 200*time.Millisecond).Should(Succeed())

		reconcileOnce := func() (ctrl.Result, error) {
			fresh := newTestAlluxioRuntime("default", "pr5674-envtest")
			Expect(cl.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "pr5674-envtest"}, fresh)).To(Succeed())
			r := &RuntimeReconciler{
				Client:    cl,
				Log:       fakeutil.NullLogger(),
				Recorder:  record.NewFakeRecorder(32),
				implement: &testRuntimeReconcilerImplement{},
			}
			return r.ReconcileInternal(cruntime.ReconcileRequestContext{
				Context:        context.Background(),
				Client:         cl,
				Log:            fakeutil.NullLogger(),
				Runtime:        fresh,
				NamespacedName: types.NamespacedName{Namespace: "default", Name: "pr5674-envtest"},
				Category:       common.AccelerateCategory,
				RuntimeType:    common.AlluxioRuntime,
				FinalizerName:  runtimeProtectionFinalizer,
			})
		}

		// Reconcile 1: dataset is created, but the ownerRef write-back to the runtime fails.
		_, r1Err := reconcileOnce()

		created := &datav1alpha1.Dataset{}
		Expect(mgr.GetAPIReader().Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "pr5674-envtest"}, created)).To(Succeed())
		Expect(created.Annotations[common.AnnotationDatasetPolicy]).To(Equal(common.DatasetPolicyAutoCreate))
		Expect(created.OwnerReferences).To(HaveLen(1))
		Expect(created.OwnerReferences[0].Controller).NotTo(BeNil())
		Expect(*created.OwnerReferences[0].Controller).To(BeTrue())

		// CANARY: on current head the first reconcile MUST fail with the empty-GVK ownerRef
		// rejection. When the fix lands (GVK set on the created dataset), this flips to FAIL.
		Expect(r1Err).To(HaveOccurred())
		Expect(r1Err.Error()).To(ContainSubstring("metadata.ownerReferences.apiVersion"))

		// Reconcile 2 (after the informer cache sees the dataset): heals and binds.
		Eventually(func() error {
			return cl.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "pr5674-envtest"}, &datav1alpha1.Dataset{})
		}, 15*time.Second, 300*time.Millisecond).Should(Succeed())
		_, r2Err := reconcileOnce()
		Expect(r2Err).NotTo(HaveOccurred())

		updated := newTestAlluxioRuntime("default", "pr5674-envtest")
		Expect(mgr.GetAPIReader().Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "pr5674-envtest"}, updated)).To(Succeed())
		Expect(updated.OwnerReferences).To(HaveLen(1))
		Expect(updated.OwnerReferences[0].Kind).To(Equal("Dataset"))

		_ = cl.Delete(context.Background(), updated)
		_ = cl.Delete(context.Background(), created)
	})
})
