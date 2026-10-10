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

// Verification harness for PR #6200 (reviewer: codex, round 3).
// Additive test-only file; production code is untouched.
//
// This file is intentionally SELF-CONTAINED (zz-prefixed helpers only) so it
// compiles both on the PR head and on the merge-base. That is what allows the
// same contract test to serve as the P0 premise check:
//   - on base (d8b37f28): FAILS, workerGets == 2 per cycle  -> premise confirmed
//   - on head (7b6392c2): PASSES, workerGets == 1 per cycle -> fix verified

package engine

import (
	"context"
	"testing"

	workloadv1alpha1 "github.com/fluid-cloudnative/advanced-statefulset/api/workload/v1alpha1"
	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	zzVerifyNS      = "zz-verify-ns"
	zzVerifyRuntime = "zz-verify-runtime"
	zzVerifyMaster  = "zz-verify-master"
	zzVerifyWorker  = "zz-verify-worker"
	zzVerifyClient  = "zz-verify-client"
)

// zzWorkerGetCounter counts Get calls against the worker AdvancedStatefulSet.
type zzWorkerGetCounter struct {
	ctrlclient.Client
	workerGets int
}

func (c *zzWorkerGetCounter) Get(ctx context.Context, key types.NamespacedName, obj ctrlclient.Object, opts ...ctrlclient.GetOption) error {
	if _, ok := obj.(*workloadv1alpha1.AdvancedStatefulSet); ok && key.Name == zzVerifyWorker {
		c.workerGets++
	}
	return c.Client.Get(ctx, key, obj, opts...)
}

func zzVerifyScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{
		datav1alpha1.AddToScheme,
		workloadv1alpha1.AddToScheme,
		appsv1.AddToScheme,
		corev1.AddToScheme,
	} {
		if err := add(scheme); err != nil {
			t.Fatalf("add to scheme: %v", err)
		}
	}
	return scheme
}

func zzVerifyRuntimeObj() *datav1alpha1.CacheRuntime {
	return &datav1alpha1.CacheRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: zzVerifyRuntime, Namespace: zzVerifyNS},
	}
}

func zzVerifyASTS(name string, nodeSelector map[string]string) *workloadv1alpha1.AdvancedStatefulSet {
	return &workloadv1alpha1.AdvancedStatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: zzVerifyNS},
		Spec: workloadv1alpha1.AdvancedStatefulSetSpec{
			Replicas: ptr.To(int32(1)),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{NodeSelector: nodeSelector},
			},
		},
		Status: workloadv1alpha1.AdvancedStatefulSetStatus{
			ReadyReplicas:     1,
			CurrentReplicas:   1,
			AvailableReplicas: 1,
		},
	}
}

func zzVerifyEngine(c ctrlclient.Client) *CacheEngine {
	return &CacheEngine{
		Client:    c,
		name:      zzVerifyRuntime,
		namespace: zzVerifyNS,
		Log:       fake.NullLogger(),
	}
}

func zzVerifyStatusValue() *common.CacheRuntimeStatusValue {
	return &common.CacheRuntimeStatusValue{
		Master: &common.ComponentStatusInfo{
			ComponentIdentity: common.ComponentIdentity{Name: zzVerifyMaster, Namespace: zzVerifyNS},
			Enabled:           true,
		},
		Worker: &common.ComponentStatusInfo{
			ComponentIdentity: common.ComponentIdentity{Name: zzVerifyWorker, Namespace: zzVerifyNS},
			Enabled:           true,
		},
		Client: &common.ComponentStatusInfo{
			ComponentIdentity: common.ComponentIdentity{Name: zzVerifyClient, Namespace: zzVerifyNS},
			Enabled:           false,
		},
	}
}

// TestVerifyPR6200SingleWorkerReadPerCycle is a CONTRACT test (polarity: contract).
//
// Claim: each CacheRuntime status update cycle reads the worker
// AdvancedStatefulSet exactly once (P0 premise: on the base branch it is read
// twice — once via ConstructComponentStatus, once via GetNodeAffinity).
//
// Expected: base -> RED (workerGets == 2 after cycle 1); head -> GREEN.
func TestVerifyPR6200SingleWorkerReadPerCycle(t *testing.T) {
	scheme := zzVerifyScheme(t)
	baseClient := fake.NewFakeClientWithScheme(scheme,
		zzVerifyRuntimeObj(),
		zzVerifyASTS(zzVerifyMaster, nil),
		zzVerifyASTS(zzVerifyWorker, map[string]string{"disktype": "ssd"}),
	)
	counting := &zzWorkerGetCounter{Client: baseClient}
	engine := zzVerifyEngine(counting)

	for cycle := 1; cycle <= 2; cycle++ {
		ready, err := engine.CheckAndUpdateRuntimeStatus(zzVerifyStatusValue())
		if err != nil {
			t.Fatalf("cycle %d: CheckAndUpdateRuntimeStatus: %v", cycle, err)
		}
		if !ready {
			t.Fatalf("cycle %d: expected runtime ready", cycle)
		}
		if counting.workerGets != cycle {
			t.Fatalf("cycle %d: worker AdvancedStatefulSet Get calls = %d, want exactly %d (1 per status cycle)",
				cycle, counting.workerGets, cycle)
		}
	}

	// The single read must still populate CacheAffinity on the runtime status.
	updated := &datav1alpha1.CacheRuntime{}
	if err := baseClient.Get(context.TODO(), types.NamespacedName{Name: zzVerifyRuntime, Namespace: zzVerifyNS}, updated); err != nil {
		t.Fatalf("get updated runtime: %v", err)
	}
	if updated.Status.CacheAffinity == nil {
		t.Fatalf("expected status.CacheAffinity to be populated after status cycle")
	}
}
