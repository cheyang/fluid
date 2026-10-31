package zzcritiqueprobe

// envtest probe (Reviewer A). Pins the /scale endpoint behavior for worker-less
// CacheRuntime payloads — the repro for findings F-workerless-scale-docs and
// Q-blockless-worker-scale-target of PR #6223 (round 2: originally a phase-2
// cross-critique scratch dir, now part of the verification branch). Run:
//   KUBEBUILDER_ASSETS=$HOME/.local/share/kubebuilder-envtest/k8s/1.31.0-linux-amd64 \
//   go test ./zz-critique-probe/ -run TestCritiqueProbe -v -count=1
// Polarity: CANARY — it pins the currently-observed (payload-dependent) API-server
// behavior. T1/T3 (worker key absent) → GET /scale fails; T2/T5 (worker block
// present, component disabled or empty) → replicas:1 materializes and GET /scale
// succeeds against a selector that can never match pods; T4 → scale PUT creates
// .spec.worker.replicas even where the path was absent.

import (
	"context"
	"encoding/json"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

var gvr = schema.GroupVersionResource{Group: "data.fluid.io", Version: "v1alpha1", Resource: "cacheruntimes"}

func TestCritiqueProbe(t *testing.T) {
	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{"../config/crd/bases"},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := testEnv.Start()
	if err != nil {
		t.Fatalf("envtest start: %v", err)
	}
	defer func() { _ = testEnv.Stop() }()

	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("dynamic client: %v", err)
	}
	ctx := context.Background()
	ns := "default"

	mk := func(name string, spec map[string]interface{}) {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gvr.GroupVersion().WithKind("CacheRuntime"))
		obj.SetName(name)
		obj.SetNamespace(ns)
		obj.Object["spec"] = spec
		if _, err := dyn.Resource(gvr).Namespace(ns).Create(ctx, obj, metav1.CreateOptions{}); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	specOf := func(name string) map[string]interface{} {
		got, err := dyn.Resource(gvr).Namespace(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("get %s: %v", name, err)
		}
		sp, _, _ := unstructured.NestedMap(got.Object, "spec")
		return sp
	}
	getScale := func(name string) (string, error) {
		sc, err := dyn.Resource(gvr).Namespace(ns).Get(ctx, name, metav1.GetOptions{}, "scale")
		if err != nil {
			return "", err
		}
		b, _ := json.Marshal(sc.Object)
		return string(b), nil
	}
	wrep := func(name string) (interface{}, bool) {
		v, found, _ := unstructured.NestedFieldNoCopy(specOf(name), "worker", "replicas")
		return v, found
	}

	// T1: worker-less create (no worker key in the request body)
	mk("probe-noworker", map[string]interface{}{"runtimeClassName": "curvine"})
	v1, f1 := wrep("probe-noworker")
	t.Logf("T1 no-worker-key: spec.worker=%v, spec.worker.replicas=%v (found=%v)",
		specOf("probe-noworker")["worker"], v1, f1)

	// T5: explicit empty worker block (what the typed Go client always sends)
	mk("probe-emptyworker", map[string]interface{}{
		"runtimeClassName": "curvine",
		"worker":           map[string]interface{}{},
	})
	v5, f5 := wrep("probe-emptyworker")
	t.Logf("T5 empty-worker-block: spec.worker.replicas=%v (found=%v)", v5, f5)
	if s, err := getScale("probe-noworker"); err != nil {
		t.Logf("T1 GET /scale FAILED: %v", err)
	} else {
		t.Logf("T1 GET /scale SUCCEEDED: spec.replicas/status/sel = %s", s)
	}

	// T2: worker component disabled via spec flag
	mk("probe-disabled", map[string]interface{}{
		"runtimeClassName": "curvine",
		"worker":           map[string]interface{}{"disabled": true},
	})
	t.Logf("T2 worker-disabled: spec.worker=%v", specOf("probe-disabled")["worker"])
	if s, err := getScale("probe-disabled"); err != nil {
		t.Logf("T2 GET /scale FAILED: %v", err)
	} else {
		t.Logf("T2 GET /scale SUCCEEDED: spec.replicas/status/sel = %s", s)
	}

	// T3: existing runtime, worker block removed via merge patch (Copilot C4420756616 scenario)
	mk("probe-removed", map[string]interface{}{
		"runtimeClassName": "curvine",
		"worker":           map[string]interface{}{"replicas": int64(2)},
	})
	if _, err := dyn.Resource(gvr).Namespace(ns).Patch(ctx, "probe-removed", types.MergePatchType,
		[]byte(`{"spec":{"worker":null}}`), metav1.PatchOptions{}); err != nil {
		t.Fatalf("T3 patch remove worker: %v", err)
	}
	t.Logf("T3 after worker:null merge patch: spec.worker=%v", specOf("probe-removed")["worker"])
	if s, err := getScale("probe-removed"); err != nil {
		t.Logf("T3 GET /scale FAILED: %v", err)
	} else {
		t.Logf("T3 GET /scale SUCCEEDED: spec.replicas/status/sel = %s", s)
	}

	// T4: scale-subresource write on the worker-less CR (what an HPA would attempt)
	if s, err := getScale("probe-noworker"); err != nil {
		t.Logf("T1 GET /scale FAILED: %v", err)
	} else {
		t.Logf("T1 GET /scale SUCCEEDED: %s", s)
	}
	if s, err := getScale("probe-emptyworker"); err != nil {
		t.Logf("T5 GET /scale FAILED: %v", err)
	} else {
		t.Logf("T5 GET /scale SUCCEEDED: %s", s)
	}

	// T4: scale-subresource write lands on spec.worker.replicas (independent AC3 check)
	sc := &unstructured.Unstructured{}
	sc.SetGroupVersionKind(schema.GroupVersion{Group: "autoscaling", Version: "v1"}.WithKind("Scale"))
	sc.SetName("probe-noworker")
	sc.SetNamespace(ns)
	if err := unstructured.SetNestedField(sc.Object, int64(4), "spec", "replicas"); err != nil {
		t.Fatalf("T4 set replicas: %v", err)
	}
	if _, err := dyn.Resource(gvr).Namespace(ns).Update(ctx, sc, metav1.UpdateOptions{}, "scale"); err != nil {
		t.Logf("T4 scale PUT FAILED: %v", err)
	} else {
		v, found := wrep("probe-noworker")
		t.Logf("T4 scale PUT ok; spec.worker.replicas=%v (found=%v)", v, found)
	}
}
