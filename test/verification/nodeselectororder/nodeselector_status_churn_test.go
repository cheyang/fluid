/*
L2 (envtest, real API server) verification for fluid PR #6217 / issue #6216.
Additive reviewer harness. Production code untouched.

Simulates the reconcile-loop in pkg/ddc/alluxio/status.go against a real API
server: each cycle rebuilds status.CacheAffinity via
kubeclient.MergeNodeSelectorAndNodeAffinity from a multi-label nodeSelector map
and issues Status().Update() whenever reflect.DeepEqual detects a drift.

Polarity: CONTRACT. Fails on base (update churn), passes on the PR head
(exactly one status write, then stable).
*/

package nodeselectororder

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/utils/kubeclient"
)

var churnSelector = map[string]string{
	"kubernetes.io/hostname":           "node-1",
	"topology.kubernetes.io/zone":      "zone-a",
	"fluid.io/dataset":                 "my-dataset",
	"node.kubernetes.io/instance-type": "m5.large",
	"beta.kubernetes.io/arch":          "amd64",
	"app.kubernetes.io/name":           "worker",
}

func TestCacheAffinityStatusChurn(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS not set; skipping envtest layer")
	}

	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
	}
	cfg, err := testEnv.Start()
	if err != nil {
		t.Fatalf("failed to start envtest: %v", err)
	}
	defer func() {
		if err := testEnv.Stop(); err != nil {
			t.Logf("failed to stop envtest: %v", err)
		}
	}()

	if err := datav1alpha1.AddToScheme(scheme.Scheme); err != nil {
		t.Fatalf("failed to register scheme: %v", err)
	}
	c, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	runtime := &datav1alpha1.AlluxioRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: "churn-check", Namespace: "default"},
	}
	if err := c.Create(ctx, runtime); err != nil {
		t.Fatalf("failed to create AlluxioRuntime: %v", err)
	}

	// Reconcile-loop simulation: identical to the structure of
	// pkg/ddc/alluxio/status.go (rebuild affinity -> DeepEqual -> conditional update).
	const cycles = 30
	updates := 0
	var lastWritten *corev1.NodeAffinity
	for i := 0; i < cycles; i++ {
		fresh := &datav1alpha1.AlluxioRuntime{}
		if err := c.Get(ctx, client.ObjectKey{Name: "churn-check", Namespace: "default"}, fresh); err != nil {
			t.Fatalf("cycle %d: get failed: %v", i, err)
		}
		toUpdate := fresh.DeepCopy()
		toUpdate.Status.CacheAffinity = kubeclient.MergeNodeSelectorAndNodeAffinity(churnSelector, nil)
		if !reflect.DeepEqual(fresh.Status, toUpdate.Status) {
			if err := c.Status().Update(ctx, toUpdate); err != nil {
				t.Fatalf("cycle %d: status update failed: %v", i, err)
			}
			updates++
			lastWritten = toUpdate.Status.CacheAffinity
		}
	}

	if updates != 1 {
		t.Fatalf("expected exactly 1 status update (initial write) over %d reconcile cycles, got %d (status churn)", cycles, updates)
	}

	// Storage must preserve the written list order exactly, so a deterministic
	// producer converges. (If the API server reordered lists, even fixed code
	// would flap; this assertion attributes any churn to the producer.)
	fresh := &datav1alpha1.AlluxioRuntime{}
	if err := c.Get(ctx, client.ObjectKey{Name: "churn-check", Namespace: "default"}, fresh); err != nil {
		t.Fatalf("final get failed: %v", err)
	}
	if !reflect.DeepEqual(fresh.Status.CacheAffinity, lastWritten) {
		t.Fatalf("API server did not round-trip CacheAffinity order:\nwrote: %v\nread:  %v", lastWritten, fresh.Status.CacheAffinity)
	}
	t.Logf("cycles=%d statusUpdates=%d (1 = converged after initial write)", cycles, updates)
}
