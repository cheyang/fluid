/*
Additive review-verification harness for PR #6288 (reviewer: codex).
Production code is untouched by this file.
See docs/verification/alluxio-sync-imagepullsecrets-codex/README.md.

Mechanism test for F1's blast radius (contract polarity; expected to PASS on
both base and PR head — it documents the coupling that turns a perpetually
"changed" SyncRuntime into a stalled sync loop):

  base.Sync:  updated, err = t.Implement.SyncRuntime(ctx)
              if updated { return }   // skips health check, runtime status,
                                      // dataset cache, dataset mounts, UFS
                                      // updates, cache-node schedule info

So whenever SyncRuntime reports changed=true, every later stage of the sync
loop is skipped for that reconcile. Combined with the alluxio finding F1
(default-config runtimes report changed=true forever), those stages never run
again for the affected runtime.
*/

package base_test

import (
	"context"
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/ddc/base"
	enginemock "github.com/fluid-cloudnative/fluid/pkg/ddc/base/mock"
	"github.com/fluid-cloudnative/fluid/pkg/runtime"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	"github.com/golang/mock/gomock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apimachineryRuntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

func verifyCodexNewSyncContext() runtime.ReconcileRequestContext {
	fakeDataset := &datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: "verify-codex", Namespace: "default"},
	}
	s := apimachineryRuntime.NewScheme()
	s.AddKnownTypes(datav1alpha1.GroupVersion, fakeDataset)
	fakeClient := fake.NewFakeClientWithScheme(s, fakeDataset)
	return runtime.ReconcileRequestContext{
		Context:        context.Background(),
		NamespacedName: types.NamespacedName{Namespace: "default", Name: "verify-codex"},
		Client:         fakeClient,
		Log:            fake.NullLogger(),
		RuntimeType:    "verify-codex-runtime-type",
		FinalizerName:  "verify-codex-finalizer",
		Runtime:        &datav1alpha1.AlluxioRuntime{},
	}
}

// SyncRuntime reporting (true, nil) must end the sync round early: none of the
// later stages may run. This is the pre-existing base.Sync contract that F1
// escalates into a permanent stall.
func TestVerifyCodexPR6288SyncShortCircuitsWhenSyncRuntimeChanged(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	impl := enginemock.NewMockImplement(ctrl)
	fakeCtx := verifyCodexNewSyncContext()
	engine := base.NewTemplateEngine(impl, "verify-codex", fakeCtx)

	impl.EXPECT().SyncMetadata().Return(nil).Times(1)
	impl.EXPECT().SyncReplicas(gomock.Any()).Return(nil).Times(1)
	impl.EXPECT().SyncRuntime(gomock.Any()).Return(true, nil).Times(1)

	// Everything after `if updated { return }` must NOT run.
	impl.EXPECT().CheckRuntimeHealthy().Times(0)
	impl.EXPECT().CheckAndUpdateRuntimeStatus().Times(0)
	impl.EXPECT().UpdateCacheOfDataset().Times(0)
	impl.EXPECT().ShouldSyncDatasetMounts().Times(0)
	impl.EXPECT().SyncDatasetMounts().Times(0)
	impl.EXPECT().ShouldUpdateUFS().Times(0)
	impl.EXPECT().SyncScheduleInfoToCacheNodes().Times(0)

	if err := engine.Sync(fakeCtx); err != nil {
		t.Fatalf("Sync() error = %v, want nil", err)
	}
}

// Control: SyncRuntime reporting (false, nil) must let the full sync loop run.
func TestVerifyCodexPR6288SyncContinuesWhenSyncRuntimeUnchanged(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	impl := enginemock.NewMockImplement(ctrl)
	fakeCtx := verifyCodexNewSyncContext()
	engine := base.NewTemplateEngine(impl, "verify-codex", fakeCtx)

	gomock.InOrder(
		impl.EXPECT().SyncMetadata().Return(nil).Times(1),
		impl.EXPECT().SyncReplicas(gomock.Any()).Return(nil).Times(1),
		impl.EXPECT().SyncRuntime(gomock.Any()).Return(false, nil).Times(1),
		impl.EXPECT().CheckRuntimeHealthy().Return(nil).Times(1),
		impl.EXPECT().CheckAndUpdateRuntimeStatus().Return(true, nil).Times(1),
		impl.EXPECT().UpdateCacheOfDataset().Return(nil).Times(1),
		impl.EXPECT().ShouldSyncDatasetMounts().Return(false, nil).Times(1),
		impl.EXPECT().ShouldUpdateUFS().Return(nil).Times(1),
		impl.EXPECT().SyncScheduleInfoToCacheNodes().Return(nil).Times(1),
	)

	if err := engine.Sync(fakeCtx); err != nil {
		t.Fatalf("Sync() error = %v, want nil", err)
	}
}
