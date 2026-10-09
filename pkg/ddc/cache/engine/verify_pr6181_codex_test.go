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

// Verification harness for PR #6181 / issue #6174 (Reviewer B, codex).
//
// Claim under test (contract polarity - FAILS on the buggy base branch, PASSES once fixed):
//   A CacheRuntime whose spec.worker.tieredStore declares real levels must produce a
//   RuntimeInfo whose tiered store info reflects those levels, so that
//   SyncScheduleInfoToCacheNodes writes non-zero cache capacity labels
//   (fluid.io/s-h-cache-{m,d,t}-<ns>-<name>) onto nodes running worker pods.
//
// On the base branch (pre-fix), getRuntimeInfo built the RuntimeInfo with an empty
// TieredStore, so tieredstore.GetLevelStorageMap returns an empty map and the node only
// gets the total label written as "0B" with no memory/disk labels at all.
//
// This file is additive verification code; it changes no production code.

package engine

import (
	"context"
	"testing"

	workloadv1alpha1 "github.com/fluid-cloudnative/advanced-statefulset/api/workload/v1alpha1"
	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/utils/dataset/lifecycle"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	"github.com/fluid-cloudnative/fluid/pkg/utils/tieredstore"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime2 "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// verifyPR6181IssueRuntime reproduces the exact CacheRuntime spec from issue #6174:
// a single emptyDir level with a 1Gi quota.
func verifyPR6181IssueRuntime() *datav1alpha1.CacheRuntime {
	return &datav1alpha1.CacheRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "default"},
		Spec: datav1alpha1.CacheRuntimeSpec{
			RuntimeClassName: "demo-class",
			Worker: datav1alpha1.CacheRuntimeWorkerSpec{
				Replicas: 2,
				TieredStore: datav1alpha1.RuntimeTieredStore{
					Levels: []datav1alpha1.RuntimeTieredStoreLevel{
						{
							EmptyDir: &datav1alpha1.EmptyDirMediumSource{
								Quota: resource.MustParse("1Gi"),
							},
							High: "0.8",
							Low:  "0.5",
						},
					},
				},
			},
		},
	}
}

// verifyPR6181MixedRuntime declares process memory plus a two-path hostPath level,
// to check the MEM/HDD bucketing and the per-path quota preservation.
func verifyPR6181MixedRuntime() *datav1alpha1.CacheRuntime {
	return &datav1alpha1.CacheRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "default"},
		Spec: datav1alpha1.CacheRuntimeSpec{
			RuntimeClassName: "demo-class",
			Worker: datav1alpha1.CacheRuntimeWorkerSpec{
				Replicas: 1,
				TieredStore: datav1alpha1.RuntimeTieredStore{
					Levels: []datav1alpha1.RuntimeTieredStoreLevel{
						{
							ProcessMemory: &datav1alpha1.ProcessMemoryMediumSource{
								Quota: resource.MustParse("4Gi"),
							},
						},
						{
							HostPath: &datav1alpha1.HostPathMediumSource{
								Paths:  []string{"/mnt/cache1", "/mnt/cache2"},
								Quotas: []resource.Quantity{resource.MustParse("1Gi"), resource.MustParse("3Gi")},
							},
						},
					},
				},
			},
		},
	}
}

func verifyPR6181NewEngine(c client.Client) *CacheEngine {
	return &CacheEngine{
		Client:      c,
		Log:         logr.Discard(),
		name:        "demo",
		namespace:   "default",
		runtimeType: common.CacheRuntime,
	}
}

// verifyPR6181WorkerObjects fabricates the worker AdvancedStatefulSet, one scheduled
// worker pod on "node1", and "node1" itself, mimicking a bound cache runtime whose
// worker has been placed.
func verifyPR6181WorkerObjects() []runtime2.Object {
	stsUID := types.UID("verify-pr6181-sts-uid")
	workerName := common.GetCacheComponentName("demo", common.ComponentTypeWorker)
	selector := map[string]string{"app": "demo-worker"}

	sts := &workloadv1alpha1.AdvancedStatefulSet{
		// TypeMeta is required: the fake client only round-trips the GVK it is
		// created with, and CacheRuntimeInfo.GetWorkerPods -> resolveControllerRef
		// matches the pod's owner reference against it.
		TypeMeta: metav1.TypeMeta{
			Kind:       "AdvancedStatefulSet",
			APIVersion: "workload.fluid.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      workerName,
			Namespace: "default",
			UID:       stsUID,
		},
		Spec: workloadv1alpha1.AdvancedStatefulSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: selector},
		},
	}

	controller := true
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      workerName + "-0",
			Namespace: "default",
			Labels:    selector,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "workload.fluid.io/v1alpha1",
					Kind:       "AdvancedStatefulSet",
					Name:       workerName,
					UID:        stsUID,
					Controller: &controller,
				},
			},
		},
		Spec: corev1.PodSpec{NodeName: "node1"},
	}

	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}}

	return []runtime2.Object{sts, pod, node}
}

// TestVerifyPR6181RuntimeInfoStorageMap is the core premise/fix check:
// the RuntimeInfo built by the cache engine must expose the worker tiered store.
//
// Polarity: contract. On base (pre-fix) this FAILS with an empty storage map;
// on the PR head it PASSES.
func TestVerifyPR6181RuntimeInfoStorageMap(t *testing.T) {
	cases := []struct {
		name       string
		runtime    *datav1alpha1.CacheRuntime
		wantMemory string // expected summed MEM quota, "" means no entry expected
		wantDisk   string // expected summed SSD/HDD quota, "" means no entry expected
	}{
		{name: "issue repro: 1Gi emptyDir", runtime: verifyPR6181IssueRuntime(), wantMemory: "", wantDisk: "1Gi"},
		{name: "mixed processMemory + hostPath", runtime: verifyPR6181MixedRuntime(), wantMemory: "4Gi", wantDisk: "4Gi"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := fake.NewFakeClientWithScheme(CacheEngineTestScheme, tc.runtime)
			e := verifyPR6181NewEngine(c)

			info, err := e.getRuntimeInfo()
			if err != nil {
				t.Fatalf("getRuntimeInfo() error = %v", err)
			}

			storage := tieredstore.GetLevelStorageMap(info)

			check := func(storeType common.CacheStoreType, want string) {
				got, found := storage[storeType]
				if want == "" {
					if found {
						t.Errorf("storage[%s] = %s, want no entry", storeType, got.String())
					}
					return
				}
				if !found {
					t.Errorf("storage[%s] missing, want %s (full map: %v)", storeType, want, storage)
					return
				}
				wantQ := resource.MustParse(want)
				if got.Cmp(wantQ) != 0 {
					t.Errorf("storage[%s] = %s, want %s", storeType, got.String(), want)
				}
			}
			check(common.MemoryCacheStore, tc.wantMemory)
			check(common.DiskCacheStore, tc.wantDisk)
		})
	}
}

// TestVerifyPR6181NodeCapacityLabels runs the real label-writing path the issue
// describes: engine.getRuntimeInfo -> lifecycle.SyncScheduleInfoToCacheNodes ->
// labelCacheNode -> labelNodeWithCapacityInfo, against a fake client holding the
// CacheRuntime, its worker AdvancedStatefulSet, one scheduled worker pod and the node.
//
// Polarity: contract. On base (pre-fix) this FAILS: the node ends up with
// fluid.io/s-h-cache-t-default-demo=0B and no -m-/-d- labels, exactly the symptom
// reported in issue #6174. On the PR head it PASSES.
func TestVerifyPR6181NodeCapacityLabels(t *testing.T) {
	cases := []struct {
		name      string
		runtime   *datav1alpha1.CacheRuntime
		wantTotal string
		wantMem   string // "" means label must be absent
		wantDisk  string // "" means label must be absent
	}{
		{name: "issue repro: 1Gi emptyDir", runtime: verifyPR6181IssueRuntime(), wantTotal: "1GiB", wantMem: "", wantDisk: "1GiB"},
		{name: "mixed processMemory + hostPath", runtime: verifyPR6181MixedRuntime(), wantTotal: "8GiB", wantMem: "4GiB", wantDisk: "4GiB"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			objs := []runtime2.Object{tc.runtime}
			objs = append(objs, verifyPR6181WorkerObjects()...)
			c := fake.NewFakeClientWithScheme(CacheEngineTestScheme, objs...)
			e := verifyPR6181NewEngine(c)

			info, err := e.getRuntimeInfo()
			if err != nil {
				t.Fatalf("getRuntimeInfo() error = %v", err)
			}

			if err := lifecycle.SyncScheduleInfoToCacheNodes(info, c); err != nil {
				t.Fatalf("SyncScheduleInfoToCacheNodes() error = %v", err)
			}

			node := &corev1.Node{}
			if err := c.Get(context.TODO(), types.NamespacedName{Name: "node1"}, node); err != nil {
				t.Fatalf("get node1: %v", err)
			}

			totalLabel := info.GetLabelNameForTotal()
			memLabel := info.GetLabelNameForMemory()
			diskLabel := info.GetLabelNameForDisk()
			t.Logf("node labels: total %s=%q, mem %s=%q, disk %s=%q",
				totalLabel, node.Labels[totalLabel], memLabel, node.Labels[memLabel], diskLabel, node.Labels[diskLabel])

			if got := node.Labels[totalLabel]; got != tc.wantTotal {
				t.Errorf("total capacity label %s = %q, want %q", totalLabel, got, tc.wantTotal)
			}
			checkLabel := func(key, want string) {
				got, found := node.Labels[key]
				if want == "" {
					if found {
						t.Errorf("label %s = %q, want it absent", key, got)
					}
					return
				}
				if !found || got != want {
					t.Errorf("label %s = %q (present=%v), want %q", key, got, found, want)
				}
			}
			checkLabel(memLabel, tc.wantMem)
			checkLabel(diskLabel, tc.wantDisk)
		})
	}
}

// TestVerifyPR6181LabelFormatSanity pins the label key shape used above so a refactor
// of the label naming surfaces here instead of silently changing what the test proves.
func TestVerifyPR6181LabelFormatSanity(t *testing.T) {
	c := fake.NewFakeClientWithScheme(CacheEngineTestScheme, verifyPR6181IssueRuntime())
	e := verifyPR6181NewEngine(c)
	info, err := e.getRuntimeInfo()
	if err != nil {
		t.Fatalf("getRuntimeInfo() error = %v", err)
	}
	if got, want := info.GetLabelNameForTotal(), "fluid.io/s-h-cache-t-default-demo"; got != want {
		t.Errorf("GetLabelNameForTotal() = %q, want %q", got, want)
	}
	if got, want := info.GetLabelNameForMemory(), "fluid.io/s-h-cache-m-default-demo"; got != want {
		t.Errorf("GetLabelNameForMemory() = %q, want %q", got, want)
	}
	if got, want := info.GetLabelNameForDisk(), "fluid.io/s-h-cache-d-default-demo"; got != want {
		t.Errorf("GetLabelNameForDisk() = %q, want %q", got, want)
	}
}

// TestVerifyPR6181StaleLabelsAfterSpecChange pins a limitation the PR itself discloses
// in "Special notes": capacity labels are only written when a node first enters the
// cache node set (calculateNodeDifferences visits only newly added nodes, and
// addScheduleInfoToNode skips nodes already carrying the runtime label). Editing
// spec.worker.tieredStore afterwards - or upgrading an existing CacheRuntime to a
// build containing this fix - leaves the stale capacity labels in place.
//
// Polarity: BUG CANARY. It asserts the current (stale-label) behavior. It PASSES
// while the limitation exists and must be inverted (labels expected to update) once
// re-labelling of already-assigned nodes is implemented.
func TestVerifyPR6181StaleLabelsAfterSpecChange(t *testing.T) {
	runtime := verifyPR6181IssueRuntime() // 1Gi emptyDir
	objs := []runtime2.Object{runtime}
	objs = append(objs, verifyPR6181WorkerObjects()...)
	c := fake.NewFakeClientWithScheme(CacheEngineTestScheme, objs...)

	// First reconcile: node1 gets labelled from the 1Gi tiered store.
	e1 := verifyPR6181NewEngine(c)
	info1, err := e1.getRuntimeInfo()
	if err != nil {
		t.Fatalf("getRuntimeInfo() error = %v", err)
	}
	if err := lifecycle.SyncScheduleInfoToCacheNodes(info1, c); err != nil {
		t.Fatalf("first SyncScheduleInfoToCacheNodes() error = %v", err)
	}
	totalLabel := info1.GetLabelNameForTotal()
	diskLabel := info1.GetLabelNameForDisk()

	node := &corev1.Node{}
	if err := c.Get(context.TODO(), types.NamespacedName{Name: "node1"}, node); err != nil {
		t.Fatalf("get node1: %v", err)
	}
	if got := node.Labels[totalLabel]; got != "1GiB" {
		t.Fatalf("precondition failed: after first sync %s = %q, want %q", totalLabel, got, "1GiB")
	}

	// User edits the runtime: quota doubled to 2Gi.
	updated := &datav1alpha1.CacheRuntime{}
	if err := c.Get(context.TODO(), types.NamespacedName{Name: "demo", Namespace: "default"}, updated); err != nil {
		t.Fatalf("get runtime: %v", err)
	}
	updated.Spec.Worker.TieredStore.Levels[0].EmptyDir.Quota = resource.MustParse("2Gi")
	if err := c.Update(context.TODO(), updated); err != nil {
		t.Fatalf("update runtime: %v", err)
	}

	// A later reconcile (fresh engine, so no cached runtimeInfo) re-syncs schedule info.
	e2 := verifyPR6181NewEngine(c)
	info2, err := e2.getRuntimeInfo()
	if err != nil {
		t.Fatalf("second getRuntimeInfo() error = %v", err)
	}

	// The new runtimeInfo does see 2Gi...
	storage := tieredstore.GetLevelStorageMap(info2)
	if got := storage[common.DiskCacheStore]; got == nil || got.Cmp(resource.MustParse("2Gi")) != 0 {
		t.Fatalf("precondition failed: new runtimeInfo disk storage = %v, want 2Gi", storage)
	}

	// ...but the already-labelled node is never revisited.
	if err := lifecycle.SyncScheduleInfoToCacheNodes(info2, c); err != nil {
		t.Fatalf("second SyncScheduleInfoToCacheNodes() error = %v", err)
	}
	if err := c.Get(context.TODO(), types.NamespacedName{Name: "node1"}, node); err != nil {
		t.Fatalf("get node1: %v", err)
	}
	t.Logf("after quota edit 1Gi->2Gi: %s=%q %s=%q", totalLabel, node.Labels[totalLabel], diskLabel, node.Labels[diskLabel])
	if got := node.Labels[totalLabel]; got != "1GiB" {
		t.Errorf("canary flipped: %s = %q after quota edit, want stale %q (limitation fixed? invert this test)", totalLabel, got, "1GiB")
	}
	if got := node.Labels[diskLabel]; got != "1GiB" {
		t.Errorf("canary flipped: %s = %q after quota edit, want stale %q (limitation fixed? invert this test)", diskLabel, got, "1GiB")
	}
}
