/*
  Verification harness for PR #6200 (Reviewer B / Codex, round 2026-10-10).
  Additive only — no production code changes.

  This file is intentionally written to compile against BOTH the merge-base
  (d8b37f28, pre-PR) and the PR head (6e3345ef), so the same tests can be run
  on both sides to prove premise + parity:

    - TestZZVerifyPR6200SingleWorkerGetPerCycle  (P0 premise, CONTRACT polarity)
        Asserts the FIXED behavior: exactly 1 worker AdvancedStatefulSet Get
        per CheckAndUpdateRuntimeStatus cycle.
        Expected on base: FAIL (observes 2 Gets/cycle -> premise reproduced).
        Expected on head: PASS.

    - TestZZVerifyPR6200AffinityParity           (regression, CONTRACT polarity)
        Asserts status.CacheAffinity equals the from-first-principles merge of
        the worker pod template nodeSelector + affinity, and that the worker
        replica status matches the workload status. Must PASS on base AND head
        (proves the PR does not change observable status content).

    - TestZZVerifyPR6200WorkerMissingError       (regression, CONTRACT polarity)
        Missing worker workload -> CheckAndUpdateRuntimeStatus returns error.
        Must PASS on base AND head.

    - TestZZVerifyPR6200OutOfBandNodeSelectorReflected (regression, CONTRACT)
        Out-of-band nodeSelector change is reflected in status.CacheAffinity
        on the very next cycle. Must PASS on base AND head (the old two-read
        path was equally fresh; this pins that the new path keeps freshness).
*/

package engine

import (
	"context"
	"testing"
	"time"

	workloadv1alpha1 "github.com/fluid-cloudnative/advanced-statefulset/api/workload/v1alpha1"
	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	zzVerifyNS      = "default"
	zzVerifyRuntime = "zz-verify-runtime"
	zzVerifyMaster  = "zz-verify-runtime-master"
	zzVerifyWorker  = "zz-verify-runtime-worker"
)

// zzVerifyWorkerGetCounter counts Get calls for AdvancedStatefulSets with the
// worker's name. On base there are two such Gets per cycle
// (ConstructComponentStatus + GetNodeAffinity); on the PR head there is one.
type zzVerifyWorkerGetCounter struct {
	ctrlclient.Client
	workerGets int
}

func (c *zzVerifyWorkerGetCounter) Get(ctx context.Context, key types.NamespacedName, obj ctrlclient.Object, opts ...ctrlclient.GetOption) error {
	if _, ok := obj.(*workloadv1alpha1.AdvancedStatefulSet); ok && key.Name == zzVerifyWorker && key.Namespace == zzVerifyNS {
		c.workerGets++
	}
	return c.Client.Get(ctx, key, obj, opts...)
}

func zzVerifyRuntimeObj() *datav1alpha1.CacheRuntime {
	return &datav1alpha1.CacheRuntime{
		ObjectMeta: metav1.ObjectMeta{
			Name:              zzVerifyRuntime,
			Namespace:         zzVerifyNS,
			CreationTimestamp: metav1.NewTime(time.Now().Add(-time.Minute)),
		},
	}
}

func zzVerifyASTS(name string, desired, ready int32, nodeSelector map[string]string, affinity *corev1.Affinity) *workloadv1alpha1.AdvancedStatefulSet {
	replicas := desired
	return &workloadv1alpha1.AdvancedStatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: zzVerifyNS},
		Spec: workloadv1alpha1.AdvancedStatefulSetSpec{
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					NodeSelector: nodeSelector,
					Affinity:     affinity,
				},
			},
		},
		Status: workloadv1alpha1.AdvancedStatefulSetStatus{
			CurrentReplicas:   desired,
			ReadyReplicas:     ready,
			AvailableReplicas: ready,
		},
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
			ComponentIdentity: common.ComponentIdentity{Name: zzVerifyRuntime + "-client", Namespace: zzVerifyNS},
			Enabled:           false,
		},
	}
}

func zzVerifyGetRuntime(t *testing.T, c ctrlclient.Client) *datav1alpha1.CacheRuntime {
	t.Helper()
	r := &datav1alpha1.CacheRuntime{}
	if err := c.Get(context.TODO(), types.NamespacedName{Name: zzVerifyRuntime, Namespace: zzVerifyNS}, r); err != nil {
		t.Fatalf("failed to read back CacheRuntime: %v", err)
	}
	return r
}

func TestZZVerifyPR6200SingleWorkerGetPerCycle(t *testing.T) {
	base := fake.NewFakeClientWithScheme(CacheEngineTestScheme,
		zzVerifyRuntimeObj(),
		zzVerifyASTS(zzVerifyMaster, 1, 1, nil, nil),
		zzVerifyASTS(zzVerifyWorker, 1, 1, map[string]string{"disktype": "ssd"}, nil),
	)
	counting := &zzVerifyWorkerGetCounter{Client: base}
	e := &CacheEngine{Client: counting, name: zzVerifyRuntime, namespace: zzVerifyNS, Log: fake.NullLogger()}

	const cycles = 2
	for i := 0; i < cycles; i++ {
		ready, err := e.CheckAndUpdateRuntimeStatus(zzVerifyStatusValue())
		if err != nil {
			t.Fatalf("cycle %d: CheckAndUpdateRuntimeStatus: %v", i, err)
		}
		if !ready {
			t.Fatalf("cycle %d: expected runtime ready", i)
		}
	}

	perCycle := float64(counting.workerGets) / cycles
	t.Logf("observed worker AdvancedStatefulSet Gets: %d over %d cycles (%.1f per cycle)", counting.workerGets, cycles, perCycle)
	if counting.workerGets != cycles {
		t.Errorf("P0 premise check: expected exactly 1 worker Get per status cycle (%d total over %d cycles), observed %d total — duplicate read present",
			cycles, cycles, counting.workerGets)
	}

	r := zzVerifyGetRuntime(t, counting)
	if r.Status.CacheAffinity == nil {
		t.Fatalf("expected status.CacheAffinity to be set")
	}
}

// zzVerifyExpectedMergedAffinity encodes MergeNodeSelectorAndNodeAffinity's
// contract from first principles for: nodeSelector={disktype:ssd} and
// podAffinity.NodeAffinity.Required=[{zone In [a]}], Preferred=[p].
func zzVerifyExpectedMergedAffinity() *corev1.NodeAffinity {
	return &corev1.NodeAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
			NodeSelectorTerms: []corev1.NodeSelectorTerm{
				{
					MatchExpressions: []corev1.NodeSelectorRequirement{
						{Key: "zone", Operator: corev1.NodeSelectorOpIn, Values: []string{"a"}},
						{Key: "disktype", Operator: corev1.NodeSelectorOpIn, Values: []string{"ssd"}},
					},
				},
			},
		},
		PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{
			{
				Weight: 100,
				Preference: corev1.NodeSelectorTerm{
					MatchExpressions: []corev1.NodeSelectorRequirement{
						{Key: "rack", Operator: corev1.NodeSelectorOpIn, Values: []string{"r1"}},
					},
				},
			},
		},
	}
}

func zzVerifyPodAffinity() *corev1.Affinity {
	return &corev1.Affinity{
		NodeAffinity: &corev1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{
					{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{Key: "zone", Operator: corev1.NodeSelectorOpIn, Values: []string{"a"}},
						},
					},
				},
			},
			PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{
				{
					Weight: 100,
					Preference: corev1.NodeSelectorTerm{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{Key: "rack", Operator: corev1.NodeSelectorOpIn, Values: []string{"r1"}},
						},
					},
				},
			},
		},
	}
}

func TestZZVerifyPR6200AffinityParity(t *testing.T) {
	base := fake.NewFakeClientWithScheme(CacheEngineTestScheme,
		zzVerifyRuntimeObj(),
		zzVerifyASTS(zzVerifyMaster, 1, 1, nil, nil),
		zzVerifyASTS(zzVerifyWorker, 3, 2, map[string]string{"disktype": "ssd"}, zzVerifyPodAffinity()),
	)
	e := &CacheEngine{Client: base, name: zzVerifyRuntime, namespace: zzVerifyNS, Log: fake.NullLogger()}

	ready, err := e.CheckAndUpdateRuntimeStatus(zzVerifyStatusValue())
	if err != nil {
		t.Fatalf("CheckAndUpdateRuntimeStatus: %v", err)
	}
	if !ready {
		t.Fatalf("expected runtime ready (partial worker readiness still counts ready)")
	}

	r := zzVerifyGetRuntime(t, e)

	// Worker replica status must mirror the workload status.
	w := r.Status.Worker
	if w.DesiredReplicas != 3 || w.ReadyReplicas != 2 || w.CurrentReplicas != 3 || w.AvailableReplicas != 2 {
		t.Errorf("worker status mismatch: got %+v", w)
	}
	if w.Phase != datav1alpha1.RuntimePhasePartialReady {
		t.Errorf("worker phase: expected PartialReady, got %q", w.Phase)
	}

	got := r.Status.CacheAffinity
	want := zzVerifyExpectedMergedAffinity()
	if got == nil || got.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		t.Fatalf("CacheAffinity or its Required term missing: %+v", got)
	}
	gotTerms := got.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	wantTerms := want.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	if len(gotTerms) != len(wantTerms) {
		t.Fatalf("required terms count: got %d want %d (%+v)", len(gotTerms), len(wantTerms), gotTerms)
	}
	for i := range wantTerms {
		if len(gotTerms[i].MatchExpressions) != len(wantTerms[i].MatchExpressions) {
			t.Fatalf("term %d expressions: got %+v want %+v", i, gotTerms[i].MatchExpressions, wantTerms[i].MatchExpressions)
		}
		for j, we := range wantTerms[i].MatchExpressions {
			ge := gotTerms[i].MatchExpressions[j]
			if ge.Key != we.Key || ge.Operator != we.Operator || len(ge.Values) != len(we.Values) || ge.Values[0] != we.Values[0] {
				t.Errorf("term %d expr %d: got %+v want %+v", i, j, ge, we)
			}
		}
	}
	if len(got.PreferredDuringSchedulingIgnoredDuringExecution) != 1 ||
		got.PreferredDuringSchedulingIgnoredDuringExecution[0].Weight != 100 {
		t.Errorf("preferred terms not preserved: %+v", got.PreferredDuringSchedulingIgnoredDuringExecution)
	}
}

func TestZZVerifyPR6200WorkerMissingError(t *testing.T) {
	base := fake.NewFakeClientWithScheme(CacheEngineTestScheme,
		zzVerifyRuntimeObj(),
		zzVerifyASTS(zzVerifyMaster, 1, 1, nil, nil),
		// no worker workload
	)
	e := &CacheEngine{Client: base, name: zzVerifyRuntime, namespace: zzVerifyNS, Log: fake.NullLogger()}

	ready, err := e.CheckAndUpdateRuntimeStatus(zzVerifyStatusValue())
	if err == nil {
		t.Fatalf("expected error when worker component is missing")
	}
	if ready {
		t.Fatalf("expected ready=false when worker component is missing")
	}
}

func TestZZVerifyPR6200OutOfBandNodeSelectorReflected(t *testing.T) {
	base := fake.NewFakeClientWithScheme(CacheEngineTestScheme,
		zzVerifyRuntimeObj(),
		zzVerifyASTS(zzVerifyMaster, 1, 1, nil, nil),
		zzVerifyASTS(zzVerifyWorker, 1, 1, map[string]string{"disktype": "ssd"}, nil),
	)
	e := &CacheEngine{Client: base, name: zzVerifyRuntime, namespace: zzVerifyNS, Log: fake.NullLogger()}

	if _, err := e.CheckAndUpdateRuntimeStatus(zzVerifyStatusValue()); err != nil {
		t.Fatalf("cycle 1: %v", err)
	}

	// out-of-band nodeSelector update
	worker := &workloadv1alpha1.AdvancedStatefulSet{}
	if err := base.Get(context.TODO(), types.NamespacedName{Name: zzVerifyWorker, Namespace: zzVerifyNS}, worker); err != nil {
		t.Fatalf("get worker: %v", err)
	}
	worker.Spec.Template.Spec.NodeSelector = map[string]string{"disktype": "nvme"}
	if err := base.Update(context.TODO(), worker); err != nil {
		t.Fatalf("update worker: %v", err)
	}

	if _, err := e.CheckAndUpdateRuntimeStatus(zzVerifyStatusValue()); err != nil {
		t.Fatalf("cycle 2: %v", err)
	}

	r := zzVerifyGetRuntime(t, e)
	terms := r.Status.CacheAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	if len(terms) != 1 || len(terms[0].MatchExpressions) != 1 {
		t.Fatalf("unexpected terms: %+v", terms)
	}
	expr := terms[0].MatchExpressions[0]
	if expr.Key != "disktype" || expr.Values[0] != "nvme" {
		t.Errorf("expected updated nodeSelector disktype=nvme, got %+v", expr)
	}
}
