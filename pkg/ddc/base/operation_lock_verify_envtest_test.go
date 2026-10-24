/*
Verification harness for PR #5754 — integration layer (real API server via envtest).
Reviewer B (Codex). Additive test file only.

Runs only when KUBEBUILDER_ASSETS is set (scripts/re-verify.sh exports it when the
binaries are available). Exercises the shipped SetDataOperationInTargetDataset /
ReleaseTargetDataset against a real kube-apiserver with the real Dataset CRD, so
the status-subresource update path is covered, not only the fake client.

Polarity: contract. On merge-base the "blocked" assertion fails (premise P0);
on the PR head it passes. F1 (DataMigrate worker-count cap) is also asserted here.
*/
package base_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/golang/mock/gomock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/dataoperation"
	"github.com/fluid-cloudnative/fluid/pkg/ddc/base"
	cruntime "github.com/fluid-cloudnative/fluid/pkg/runtime"
	"github.com/fluid-cloudnative/fluid/pkg/utils"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
)

func TestVerifyEnvtestOperationLock(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS not set; skipping envtest layer")
	}

	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := testEnv.Start()
	if err != nil {
		t.Fatalf("start envtest: %v", err)
	}
	defer func() {
		if err := testEnv.Stop(); err != nil {
			t.Fatalf("stop envtest: %v", err)
		}
	}()

	if err := datav1alpha1.AddToScheme(scheme.Scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	c, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		t.Fatalf("client: %v", err)
	}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ds := &datav1alpha1.Dataset{ObjectMeta: metav1.ObjectMeta{Name: "ds", Namespace: "default"}}
	if err := c.Create(context.TODO(), ds); err != nil {
		t.Fatalf("create dataset: %v", err)
	}
	// seed the in-progress marker via the status subresource, like a live controller would
	fetched, err := utils.GetDataset(c, "ds", "default")
	if err != nil {
		t.Fatalf("get dataset: %v", err)
	}
	toUpdate := fetched.DeepCopy()
	toUpdate.Status.Conditions = []datav1alpha1.DatasetCondition{}
	toUpdate.SetDataOperationInProgress("DataLoad", "load-1")
	if err := c.Status().Update(context.TODO(), toUpdate); err != nil {
		t.Fatalf("seed status: %v", err)
	}

	ctx := cruntime.ReconcileRequestContext{
		Dataset:  fetched,
		Client:   c,
		Log:      fake.NullLogger(),
		Recorder: record.NewFakeRecorder(16),
	}

	impl := verifyEngine(ctrl)
	op2 := verifyOp(ctrl, dataoperation.DataLoadType, "load-2", "default", 1)

	setRef := func(ref map[string]string) {
		cur, gerr := utils.GetDataset(c, "ds", "default")
		if gerr != nil {
			t.Fatalf("get dataset: %v", gerr)
		}
		cur.Status.Conditions = []datav1alpha1.DatasetCondition{}
		cur.Status.OperationRef = ref
		if uerr := c.Status().Update(context.TODO(), cur); uerr != nil {
			t.Fatalf("seed status: %v", uerr)
		}
	}

	t.Run("SecondDataLoadBlocked", func(t *testing.T) {
		setRef(map[string]string{"DataLoad": "load-1"})
		// contract: second DataLoad blocked on head; admitted on base (premise P0)
		err = base.SetDataOperationInTargetDataset(ctx, op2, impl)
		cur, _ := utils.GetDataset(c, "ds", "default")
		if err == nil {
			t.Fatalf("P0 reproduced against real API server: second DataLoad admitted; OperationRef[DataLoad]=%q", cur.Status.OperationRef["DataLoad"])
		}
		if got := cur.Status.OperationRef["DataLoad"]; got != "load-1" {
			t.Fatalf("status must be untouched when blocked, got %q", got)
		}
		t.Logf("blocked as intended against real API server: %v", err)
	})

	t.Run("F1DataMigrateWorkerCap", func(t *testing.T) {
		// F1: DataMigrate with spec.parallelism=4 admits a second DataMigrate CR
		mig2 := verifyOp(ctrl, dataoperation.DataMigrateType, "mig-2", "default", 4)
		setRef(map[string]string{"DataMigrate": "mig-1"})
		if err := base.SetDataOperationInTargetDataset(ctx, mig2, impl); err == nil {
			cur, _ := utils.GetDataset(c, "ds", "default")
			t.Fatalf("F1 confirmed against real API server: second DataMigrate admitted; OperationRef[DataMigrate]=%q", cur.Status.OperationRef["DataMigrate"])
		}
	})

	t.Run("ReleaseUnblocks", func(t *testing.T) {
		setRef(map[string]string{"DataLoad": "load-1"})
		op1 := verifyOp(ctrl, dataoperation.DataLoadType, "load-1", "default", 1)
		op1.EXPECT().GetTargetDataset().DoAndReturn(func() (*datav1alpha1.Dataset, error) {
			return utils.GetDataset(c, "ds", "default")
		}).AnyTimes()
		op1.EXPECT().RemoveTargetDatasetStatusInProgress(gomock.Any()).AnyTimes()
		if err := base.ReleaseTargetDataset(ctx, op1); err != nil {
			t.Fatalf("release: %v", err)
		}
		if err := base.SetDataOperationInTargetDataset(ctx, op2, impl); err != nil {
			t.Fatalf("second op must be admitted after release: %v", err)
		}
		cur, _ := utils.GetDataset(c, "ds", "default")
		if got := cur.Status.OperationRef["DataLoad"]; got != "load-2" {
			t.Fatalf("after release the second op must hold the ref, got %q", got)
		}
	})
}
