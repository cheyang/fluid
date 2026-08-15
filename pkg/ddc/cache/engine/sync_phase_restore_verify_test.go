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

// Verification harness for PR #6162 ("fix(cache): restore Dataset to Bound after
// CacheRuntime recovers from an outage").
//
// This file is reviewer-side evidence, not a proposed production change. It exercises
// CacheEngine.Sync at the boundary the PR touches and counts the pod-exec RPCs the
// restore path issues.
//
// Every test below is a CONTRACT test (asserts intended behavior): it FAILS on the
// code under review if the finding is real, and PASSES once fixed. No bug-canaries,
// so "all green" here does mean "all findings addressed".
//
//	[F1-a] no cache-state RPC while the permitSync() rate limiter is closed   -> RED on PR head
//	[F1-b] phase is still restored while the limiter is closed                -> GREEN on PR head, RED on master
//	[F1-c] at most one cache-state RPC per limiter window across a flap       -> RED on PR head, GREEN on master
//	[F2 ] the DatasetReady condition is restored alongside the phase          -> GREEN on PR head, RED on master
//
// The RPC being counted is CacheEngine.GetCacheStates -> CacheFileUtil.Execute ->
// kubeclient.ExecCommandInContainerWithTimeout, i.e. a synchronous exec into the master
// pod with a floor of common.MinExecutionTimeoutSeconds (20s). Patching NewCacheFileUtil
// is the same seam pkg/ddc/cache/engine/sync_test.go already uses.
package engine

import (
	"context"
	"time"

	"github.com/agiledragon/gomonkey/v2"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	workloadv1alpha1 "github.com/fluid-cloudnative/advanced-statefulset/api/workload/v1alpha1"
	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	cruntime "github.com/fluid-cloudnative/fluid/pkg/runtime"
	"github.com/fluid-cloudnative/fluid/pkg/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const verifyReportSummaryJSON = `{"cached":"1073741824","cachedPercentage":"50","cacheCapacity":"2147483648","cacheHitRatio":"90","fileNum":"100","ufsTotal":"2147483648"}`

var _ = Describe("VERIFY PR#6162 Dataset phase restore", Label("verify.pr6162"), func() {
	const (
		rtName = "test-runtime"
		rtNs   = "default"
	)

	var (
		engine       *CacheEngine
		ctx          cruntime.ReconcileRequestContext
		dataset      *datav1alpha1.Dataset
		runtimeObj   *datav1alpha1.CacheRuntime
		runtimeClass *datav1alpha1.CacheRuntimeClass
		patches      *gomonkey.Patches

		// execCount counts would-be kubelet exec RPCs issued during a Sync.
		execCount int
	)

	// newAdvancedSts builds a master/worker AdvancedStatefulSet whose readiness is what
	// CheckAndUpdateRuntimeStatus collapses into the runtimeReady flag driving sync.go:87.
	newAdvancedSts := func(suffix string, desired, ready int32) *workloadv1alpha1.AdvancedStatefulSet {
		replicas := desired
		return &workloadv1alpha1.AdvancedStatefulSet{
			ObjectMeta: metav1.ObjectMeta{Name: rtName + "-" + suffix, Namespace: rtNs},
			Spec: workloadv1alpha1.AdvancedStatefulSetSpec{
				Replicas: &replicas,
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: suffix, Image: "test-" + suffix + ":latest"}},
					},
				},
			},
			Status: workloadv1alpha1.AdvancedStatefulSetStatus{
				ReadyReplicas:     ready,
				CurrentReplicas:   desired,
				AvailableReplicas: ready,
			},
		}
	}

	// buildClient wires a fake client holding the fixture plus workloads at the given
	// worker readiness. workerReady == workerDesired means the runtime reports Ready.
	buildClient := func(workerReady int32) {
		clientDs := &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Name: rtName + "-client", Namespace: rtNs},
			Spec: appsv1.DaemonSetSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "client", Image: "test-client:latest"}},
					},
				},
			},
			Status: appsv1.DaemonSetStatus{NumberReady: 0, DesiredNumberScheduled: 0},
		}

		engine.Client = fake.NewClientBuilder().
			WithScheme(CacheEngineTestScheme).
			WithObjects(
				dataset, runtimeObj, runtimeClass,
				newAdvancedSts("master", 1, 1),
				newAdvancedSts("worker", 2, workerReady),
				clientDs,
			).
			WithStatusSubresource(dataset, runtimeObj).
			Build()

		ctx.Client = engine.Client
	}

	// setWorkerReady flips worker readiness in place, modelling a worker pod dropping out
	// and coming back — the transient outage the PR is about.
	setWorkerReady := func(ready int32) {
		sts := &workloadv1alpha1.AdvancedStatefulSet{}
		Expect(engine.Client.Get(context.Background(),
			types.NamespacedName{Name: rtName + "-worker", Namespace: rtNs}, sts)).To(Succeed())
		sts.Status.ReadyReplicas = ready
		sts.Status.AvailableReplicas = ready
		Expect(engine.Client.Update(context.Background(), sts)).To(Succeed())
	}

	currentDataset := func() *datav1alpha1.Dataset {
		ds := &datav1alpha1.Dataset{}
		Expect(engine.Client.Get(context.Background(),
			types.NamespacedName{Name: rtName, Namespace: rtNs}, ds)).To(Succeed())
		return ds
	}

	BeforeEach(func() {
		execCount = 0

		dataset = &datav1alpha1.Dataset{
			ObjectMeta: metav1.ObjectMeta{Name: rtName, Namespace: rtNs, UID: "test-dataset-uid"},
			Spec:       datav1alpha1.DatasetSpec{},
		}

		runtimeObj = &datav1alpha1.CacheRuntime{
			TypeMeta: metav1.TypeMeta{APIVersion: "data.fluid.io/v1alpha1", Kind: "CacheRuntime"},
			ObjectMeta: metav1.ObjectMeta{
				Name: rtName, Namespace: rtNs, UID: "test-runtime-uid",
			},
			Spec: datav1alpha1.CacheRuntimeSpec{
				RuntimeClassName: "test-class",
				Master:           datav1alpha1.CacheRuntimeMasterSpec{Replicas: 1},
				Worker:           datav1alpha1.CacheRuntimeWorkerSpec{Replicas: 2},
				Client:           datav1alpha1.CacheRuntimeClientSpec{},
			},
		}
		runtimeObj.Status.Master.Phase = datav1alpha1.RuntimePhaseNone
		runtimeObj.Status.Worker.Phase = datav1alpha1.RuntimePhaseNone
		runtimeObj.Status.Client.Phase = datav1alpha1.RuntimePhaseNone

		// ReportSummary present on the master topology is what makes GetCacheStates
		// reach the exec seam rather than bailing out early.
		runtimeClass = &datav1alpha1.CacheRuntimeClass{
			ObjectMeta:     metav1.ObjectMeta{Name: "test-class"},
			FileSystemType: "test-fs",
			Topology: &datav1alpha1.RuntimeTopology{
				Master: &datav1alpha1.RuntimeComponentDefinition{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{Name: "master", Image: "test-master:latest"}},
						},
					},
					ExecutionEntries: &datav1alpha1.ExecutionEntries{
						ReportSummary: &datav1alpha1.ExecutionCommonEntry{
							Command:        []string{"summary"},
							TimeoutSeconds: 10,
						},
					},
				},
				Worker: &datav1alpha1.RuntimeComponentDefinition{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{Name: "worker", Image: "test-worker:latest"}},
						},
					},
				},
				Client: &datav1alpha1.RuntimeComponentDefinition{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{Name: "client", Image: "test-client:latest"}},
						},
					},
				},
			},
		}

		engine = &CacheEngine{
			name:      rtName,
			namespace: rtNs,
			Log:       ctrl.Log.WithName("verify-pr6162"),
			// Build() uses defaultSyncRetryDuration (5s); the shared sync_test.go fixture
			// leaves it at 0, which disables the limiter and hides this finding entirely.
			syncRetryDuration: defaultSyncRetryDuration,
		}

		ctx = cruntime.ReconcileRequestContext{
			Context:        context.Background(),
			Log:            ctrl.Log.WithName("verify-pr6162"),
			RuntimeType:    "cache",
			NamespacedName: types.NamespacedName{Name: rtName, Namespace: rtNs},
		}

		patches = gomonkey.ApplyFunc(NewCacheFileUtil,
			func(podName, containerName, namespace string, log logr.Logger) CacheFileUtil {
				return &MockExecutions{
					MockExecute: func(command []string, timeout time.Duration) (string, error) {
						execCount++
						return verifyReportSummaryJSON, nil
					},
				}
			})
	})

	AfterEach(func() {
		if patches != nil {
			patches.Reset()
		}
	})

	Context("when the runtime is Ready but the Dataset was left Failed by an outage", func() {
		BeforeEach(func() {
			dataset.Status.Phase = datav1alpha1.FailedDatasetPhase
			// UpdateDatasetStatus(Failed) leaves DatasetReady/False behind; reproduce that
			// so IsSetupDone() is true and Setup()/BindToDataset() would not re-run.
			dataset.Status.Conditions = []datav1alpha1.DatasetCondition{
				utils.NewDatasetCondition(datav1alpha1.DatasetReady, datav1alpha1.DatasetReadyReason,
					"The ddc runtime is not ready.", corev1.ConditionFalse),
			}
			buildClient(2) // worker fully ready => runtimeReady == true
		})

		// [F1-a] CONTRACT. sync.go:38 documents permitSyncEngineStatus as the guard that
		// "avoids frequent rpcs with engines with rate limited retries". Restoring the phase
		// must not smuggle a pod exec past a closed limiter.
		It("[contract][F1-a] must not issue a cache-state RPC while permitSync() is closed", func() {
			engine.timeOfLastSync = time.Now() // inside syncRetryDuration => limiter closed
			Expect(engine.permitSync()).To(BeFalse(), "precondition: the rate limiter must be closed")

			Expect(engine.Sync(ctx)).To(Succeed())

			Expect(execCount).To(Equal(0),
				"a cache-state pod-exec RPC was issued while the permitSync() rate limiter was closed")
		})

		// [F1-b] CONTRACT. The recovery itself must not depend on the limiter being open,
		// otherwise recovery is merely delayed rather than fixed. Green on PR head; this is
		// the PR's actual win, at a boundary its own test does not cover.
		It("[contract][F1-b] must restore the phase to Bound even while permitSync() is closed", func() {
			engine.timeOfLastSync = time.Now()
			Expect(engine.permitSync()).To(BeFalse())

			Expect(engine.Sync(ctx)).To(Succeed())

			Expect(currentDataset().Status.Phase).To(Equal(datav1alpha1.BoundDatasetPhase))
		})

		// [F2] CONTRACT. Phase alone is not enough: IsSetupDone() and every consumer of
		// DatasetReady read the condition. Green on PR head (UpdateDatasetStatus sets both).
		It("[contract][F2] must restore the DatasetReady condition to True, not only the phase", func() {
			Expect(engine.Sync(ctx)).To(Succeed())

			updated := currentDataset()
			Expect(updated.Status.Phase).To(Equal(datav1alpha1.BoundDatasetPhase))

			idx, cond := utils.GetDatasetCondition(updated.Status.Conditions, datav1alpha1.DatasetReady)
			Expect(idx).NotTo(Equal(-1), "DatasetReady condition missing after restore")
			Expect(cond.Status).To(Equal(corev1.ConditionTrue),
				"DatasetReady left False while the phase says Bound")
		})
	})

	Context("when the runtime flaps not-ready -> ready repeatedly inside one limiter window", func() {
		BeforeEach(func() {
			dataset.Status.Phase = datav1alpha1.BoundDatasetPhase
			buildClient(2)
		})

		// [F1-c] CONTRACT. This is the PR's own scenario (a worker pod restarting), driven
		// three times. Reconciles fire on every AdvancedStatefulSet status change, and the
		// engine's client is informer-backed, so each ready-reconcile that still reads
		// Failed pays another unthrottled exec. Bounded by the 5s limiter, the whole flap
		// should cost at most one RPC.
		It("[contract][F1-c] must issue at most one cache-state RPC per limiter window", func() {
			const flaps = 3
			for i := 0; i < flaps; i++ {
				setWorkerReady(0) // outage: runtime not ready => Dataset goes Failed
				Expect(engine.Sync(ctx)).To(Succeed())
				Expect(currentDataset().Status.Phase).To(Equal(datav1alpha1.FailedDatasetPhase))

				setWorkerReady(2) // recovery: runtime Ready again
				Expect(engine.Sync(ctx)).To(Succeed())
			}

			// The loop runs in well under syncRetryDuration, so exactly one window elapsed.
			Expect(execCount).To(BeNumerically("<=", 1),
				"%d cache-state pod-exec RPCs across %d flaps within a single %s rate-limit window",
				execCount, flaps, defaultSyncRetryDuration)
		})
	})
})
