// Integration layer for the PR #6240 verification harness.
//
// Starts a real kube-apiserver via envtest (KUBEBUILDER_ASSETS must point at a
// kubebuilder asset dir), installs fluid's CRDs, and round-trips YAML
// manifests through sigs.k8s.io/yaml -> unstructured -> the real API server.
// This exercises the exact vendored stack this PR changes (sigs.k8s.io/yaml is
// also used by k8s.io/apimachinery's YAML serializer and client-go).
package sigsyamlverify

import (
	"context"
	"os"
	"reflect"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/yaml"
)

const datasetYAML = `apiVersion: data.fluid.io/v1alpha1
kind: Dataset
metadata:
  name: verify-yaml-bump
  namespace: default
spec:
  mounts:
  - mountPoint: https://mirrors.tuna.tsinghua.edu.cn/apache
    name: hbase
  accessModes:
  - ReadOnlyMany
`

const alluxioRuntimeYAML = `apiVersion: data.fluid.io/v1alpha1
kind: AlluxioRuntime
metadata:
  name: verify-yaml-bump
  namespace: default
spec:
  replicas: 2
  tieredstore:
    levels:
    - mediumtype: MEM
      path: /dev/shm
      quota: 2Gi
      high: "0.95"
      low: "0.7"
`

func yamlToUnstructured(t *testing.T, doc string) *unstructured.Unstructured {
	t.Helper()
	j, err := yaml.YAMLToJSON([]byte(doc))
	if err != nil {
		t.Fatalf("YAMLToJSON: %v", err)
	}
	obj := &unstructured.Unstructured{}
	if err := obj.UnmarshalJSON(j); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	return obj
}

func TestEnvtestYamlRoundTrip(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS not set; skipping envtest integration layer")
	}
	env := &envtest.Environment{
		CRDDirectoryPaths:     []string{"../../../config/crd/bases"},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := env.Start()
	if err != nil {
		t.Fatalf("envtest start: %v", err)
	}
	defer func() {
		if err := env.Stop(); err != nil {
			t.Errorf("envtest stop: %v", err)
		}
	}()

	c, err := client.New(cfg, client.Options{})
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	for name, doc := range map[string]string{"Dataset": datasetYAML, "AlluxioRuntime": alluxioRuntimeYAML} {
		obj := yamlToUnstructured(t, doc)
		if err := c.Create(ctx, obj); err != nil {
			t.Fatalf("%s create: %v", name, err)
		}
		got := &unstructured.Unstructured{}
		got.SetGroupVersionKind(obj.GroupVersionKind())
		if err := c.Get(ctx, client.ObjectKeyFromObject(obj), got); err != nil {
			t.Fatalf("%s get: %v", name, err)
		}
		wantSpec, _, _ := unstructured.NestedMap(obj.Object, "spec")
		gotSpec, _, _ := unstructured.NestedMap(got.Object, "spec")
		if !reflect.DeepEqual(wantSpec, gotSpec) {
			t.Errorf("%s spec round-trip mismatch:\n want: %#v\n got:  %#v", name, wantSpec, gotSpec)
		}
		t.Logf("CASE envtest-%s-roundtrip | OK kind=%s name=%s", name, got.GetKind(), got.GetName())
	}
}
