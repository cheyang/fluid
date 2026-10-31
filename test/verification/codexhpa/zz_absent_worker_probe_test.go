package codexhpa

// Contract test for the narrowed F-workerless-scale-docs behavior (PR #6223):
// when spec.worker is TRULY ABSENT from the stored object (created via
// unstructured without the worker key), structural defaulting does NOT
// materialize .spec.worker.replicas, so GET /scale must FAIL with the
// missing-path error. This is the behavior the (corrected) PR body claims.
// Pinned from envtest observations on k8s 1.33.0 (round-2 probe) and
// claude's T1/T3 probes on k8s 1.31.0. If the PR is changed to backfill the
// worker block (so GET /scale succeeds), this test flips red and must be
// inverted together with a docs update.

import (
	"context"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

func TestCacheRuntimeAbsentWorkerBlockScaleFails(t *testing.T) {
	env := startScaleEnv(t)
	ctx := context.TODO()

	cr := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "data.fluid.io/v1alpha1",
		"kind":       "CacheRuntime",
		"metadata":   map[string]interface{}{"name": "truly-absent-worker", "namespace": verifyNS},
		"spec":       map[string]interface{}{"runtimeClassName": "curvine"},
	}}
	if err := env.client.Create(ctx, cr); err != nil {
		t.Fatalf("create unstructured CR without spec.worker: %v", err)
	}

	back := &unstructured.Unstructured{}
	back.SetGroupVersionKind(schema.GroupVersionKind{Group: "data.fluid.io", Version: "v1alpha1", Kind: "CacheRuntime"})
	if err := env.client.Get(ctx, types.NamespacedName{Name: "truly-absent-worker", Namespace: verifyNS}, back); err != nil {
		t.Fatalf("get back: %v", err)
	}
	workerVal, workerFound, _ := unstructured.NestedMap(back.Object, "spec", "worker")
	replicasVal, replicasFound, _ := unstructured.NestedInt64(back.Object, "spec", "worker", "replicas")
	t.Logf("OBSERVED stored spec.worker present=%v value=%v; spec.worker.replicas present=%v value=%v", workerFound, workerVal, replicasFound, replicasVal)
	if workerFound {
		t.Fatalf("PIN VIOLATED: spec.worker block materialized by defaulting (value=%v); the narrowed F-workerless-scale-docs contract says block-absent stays absent", workerVal)
	}
	if replicasFound {
		t.Fatalf("PIN VIOLATED: spec.worker.replicas materialized (=%d) for a worker-less payload", replicasVal)
	}

	out, err := env.getScale(t, "truly-absent-worker")
	if err == nil {
		t.Fatalf("PIN VIOLATED: GET /scale with truly-absent spec.worker unexpectedly SUCCEEDED: %s (PR-body claim and round-2 probes say it must fail)", out)
	}
	t.Logf("OBSERVED GET /scale with truly-absent spec.worker FAILED as expected: %v\n%s", err, out)
	if !strings.Contains(err.Error()+out, ".spec.worker.replicas") {
		t.Fatalf("GET /scale failed but not for the pinned reason; want the missing-path error mentioning .spec.worker.replicas, got: %v\n%s", err, out)
	}
}
