/*
Integration-layer verification for PR #6244 (reviewer: codex). Additive test
file only. Runs the public SyncScheduleInfoToCacheNodes against a REAL API
server (envtest) through a manager CACHED client - exactly the client engines
use in production (e.Client) - to prove that:

  - stale capacity labels on an already-labelled cache node are rewritten, and
  - a label for a removed tier is DELETED via a real strategic-merge patch with
    null values against a real apiserver (not fake-client bookkeeping),
  - a steady node is not re-patched (no label churn).

A direct (non-cached) client is deliberately NOT used: typed Gets through it
return objects with empty TypeMeta, which makes GetPodsForStatefulSet's
owner-reference kind check fail - a client artifact, not production behavior
(the manager's cached client preserves TypeMeta; verified separately).

Run:
  KUBEBUILDER_ASSETS=/home/ecs-user/envtest-assets/k8s/1.31.0-linux-amd64 \
    go test ./pkg/utils/dataset/lifecycle/ -run TestPR6244Envtest -count=1 -v
*/

package lifecycle

import (
	"context"
	"os"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/ddc/base"
)

func TestPR6244Envtest(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS not set; skipping envtest layer")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	env := &envtest.Environment{}
	cfg, err := env.Start()
	if err != nil {
		t.Fatalf("start envtest: %v", err)
	}
	defer func() { _ = env.Stop() }()

	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{
		corev1.AddToScheme, appsv1.AddToScheme, datav1alpha1.AddToScheme,
	} {
		if err := add(scheme); err != nil {
			t.Fatalf("scheme: %v", err)
		}
	}

	mgr, err := manager.New(cfg, manager.Options{
		Scheme:  scheme,
		Metrics: metricsserver.Options{BindAddress: "0"},
	})
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	go func() { _ = mgr.Start(ctx) }()
	if !mgr.GetCache().WaitForCacheSync(ctx) {
		t.Fatal("cache sync")
	}
	cli := mgr.GetClient()

	// runtime "spark"/"fluid" (alluxio), mem 1GiB + ssd 2GiB
	tieredStore := datav1alpha1.TieredStore{Levels: []datav1alpha1.Level{
		{MediumType: common.Memory, Quota: resource.NewQuantity(1<<30, resource.BinarySI)},
		{MediumType: common.SSD, Quota: resource.NewQuantity(2<<30, resource.BinarySI)},
	}}
	runtimeInfo, err := base.BuildRuntimeInfo("spark", "fluid", "alluxio", base.WithTieredStore(tieredStore))
	if err != nil {
		t.Fatalf("BuildRuntimeInfo: %v", err)
	}

	if err := cli.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "fluid"}}); err != nil {
		t.Fatalf("create ns: %v", err)
	}
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Namespace: "fluid", Name: "spark-worker"},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "spark-worker"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "spark-worker"}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "c", Image: "i"}}},
			},
		},
	}
	if err := cli.Create(ctx, sts); err != nil {
		t.Fatalf("create sts: %v", err)
	}
	if err := cli.Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "spark-worker-0",
			Namespace: "fluid",
			Labels:    map[string]string{"app": "spark-worker"},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "apps/v1", Kind: "StatefulSet", Name: "spark-worker",
				UID: sts.UID, Controller: ptr.To(true),
			}},
		},
		Spec: corev1.PodSpec{
			NodeName:   "node-1",
			Containers: []corev1.Container{{Name: "worker", Image: "busybox"}},
		},
	}); err != nil {
		t.Fatalf("create worker: %v", err)
	}

	const (
		runtimeLabel = "fluid.io/s-alluxio-fluid-spark"
		memLabel     = "fluid.io/s-h-alluxio-m-fluid-spark"
		diskLabel    = "fluid.io/s-h-alluxio-d-fluid-spark"
		totalLabel   = "fluid.io/s-h-alluxio-t-fluid-spark"
	)

	// node-1 simulates a cache node labelled before #6181: runtime label present,
	// total stuck at 0B, no mem/disk labels.
	if err := cli.Create(ctx, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: map[string]string{
		runtimeLabel: "true",
		totalLabel:   "0B",
	}}}); err != nil {
		t.Fatalf("create node-1: %v", err)
	}

	// wait until the cached client sees the fresh objects
	waitFor(t, 30*time.Second, func() bool {
		pods, err := runtimeInfo.GetWorkerPods(cli)
		return err == nil && len(pods) == 1
	}, "cached client to see the worker pod")

	if err := SyncScheduleInfoToCacheNodes(runtimeInfo, cli); err != nil {
		t.Fatalf("sync: %v", err)
	}

	// P0a against a real apiserver: stale 0B labels get refreshed.
	waitFor(t, 60*time.Second, func() bool {
		got := &corev1.Node{}
		if err := cli.Get(ctx, client.ObjectKey{Name: "node-1"}, got); err != nil {
			return false
		}
		return got.Labels[memLabel] == "1GiB" && got.Labels[diskLabel] == "2GiB" && got.Labels[totalLabel] == "3GiB"
	}, "stale capacity labels to be refreshed")

	// steady state: a second sync must leave the labels byte-identical
	before := &corev1.Node{}
	if err := cli.Get(ctx, client.ObjectKey{Name: "node-1"}, before); err != nil {
		t.Fatal(err)
	}
	if err := SyncScheduleInfoToCacheNodes(runtimeInfo, cli); err != nil {
		t.Fatalf("sync (steady): %v", err)
	}
	after := &corev1.Node{}
	if err := cli.Get(ctx, client.ObjectKey{Name: "node-1"}, after); err != nil {
		t.Fatal(err)
	}
	if before.ResourceVersion != after.ResourceVersion {
		t.Errorf("steady-state sync re-patched node-1 (rv %s -> %s)", before.ResourceVersion, after.ResourceVersion)
	}

	// P0b against a real apiserver: drop the memory tier; the mem label must be
	// DELETED (strategic-merge patch with a null value) and the total recomputed.
	diskOnly, err := base.BuildRuntimeInfo("spark", "fluid", "alluxio", base.WithTieredStore(
		datav1alpha1.TieredStore{Levels: []datav1alpha1.Level{
			{MediumType: common.SSD, Quota: resource.NewQuantity(2<<30, resource.BinarySI)},
		}}))
	if err != nil {
		t.Fatalf("BuildRuntimeInfo diskOnly: %v", err)
	}

	if err := SyncScheduleInfoToCacheNodes(diskOnly, cli); err != nil {
		t.Fatalf("sync diskOnly: %v", err)
	}
	waitFor(t, 60*time.Second, func() bool {
		got := &corev1.Node{}
		if err := cli.Get(ctx, client.ObjectKey{Name: "node-1"}, got); err != nil {
			return false
		}
		_, memStillThere := got.Labels[memLabel]
		return !memStillThere && got.Labels[totalLabel] == "2GiB" && got.Labels[diskLabel] == "2GiB"
	}, "removed-tier label to be deleted")

	got := &corev1.Node{}
	if err := cli.Get(ctx, client.ObjectKey{Name: "node-1"}, got); err != nil {
		t.Fatal(err)
	}
	t.Logf("node-1 final labels: %v", got.Labels)
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(500 * time.Millisecond)
	}
}
