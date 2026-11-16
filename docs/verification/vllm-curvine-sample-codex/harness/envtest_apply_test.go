// L2 integration for PR #6253 (reviewer: codex). ADDITIVE ONLY.
// Starts a real kube-apiserver+etcd via envtest with the repo's CRDs installed and
// applies the exact manifests embedded in the new sample doc, proving they are
// accepted by the API server (structural schema validation) and round-trip intact.
// Requires KUBEBUILDER_ASSETS; fails hard (never silently skips) if unset.
package harness

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/yaml"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
)

func yamlDocs(t *testing.T, body string) [][]byte {
	t.Helper()
	var out [][]byte
	for _, d := range strings.Split(body, "\n---\n") {
		if strings.TrimSpace(d) != "" {
			out = append(out, []byte(d))
		}
	}
	return out
}

func unmarshalInto(t *testing.T, y []byte, obj client.Object) {
	t.Helper()
	j, err := yaml.YAMLToJSON(y)
	if err != nil {
		t.Fatalf("YAMLToJSON: %v", err)
	}
	if err := json.Unmarshal(j, obj); err != nil {
		t.Fatalf("unmarshal %T: %v", obj, err)
	}
}

func TestEnvtestAppliesDocManifests(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Fatal("KUBEBUILDER_ASSETS is not set; integration layer cannot run (this is a harness error, not a pass)")
	}
	root := repoRoot(t)
	if err := datav1alpha1.AddToScheme(scheme.Scheme); err != nil {
		t.Fatalf("add fluid scheme: %v", err)
	}
	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join(root, "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := testEnv.Start()
	if err != nil {
		t.Fatalf("start envtest: %v", err)
	}
	defer func() {
		if err := testEnv.Stop(); err != nil {
			t.Logf("stop envtest: %v", err)
		}
	}()
	c, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// manifests exactly as the en doc's heredocs produce them
	doc := readDoc(t, root, "en", newDocName)
	files := map[string][]string{}
	for _, h := range extractHeredocs(doc) {
		for _, d := range yamlDocs(t, h.body) {
			files[h.fname] = append(files[h.fname], string(d))
		}
	}
	for _, want := range []string{"dataset.yaml", "cacheruntime.yaml", "dataload.yaml", "vllm.yaml"} {
		if len(files[want]) == 0 {
			t.Fatalf("doc produced no %s", want)
		}
	}

	ds := &datav1alpha1.Dataset{}
	unmarshalInto(t, []byte(files["dataset.yaml"][0]), ds)
	cr := &datav1alpha1.CacheRuntime{}
	unmarshalInto(t, []byte(files["cacheruntime.yaml"][0]), cr)
	dl := &datav1alpha1.DataLoad{}
	unmarshalInto(t, []byte(files["dataload.yaml"][0]), dl)
	dep := &appsv1.Deployment{}
	unmarshalInto(t, []byte(files["vllm.yaml"][0]), dep)
	svc := &corev1.Service{}
	unmarshalInto(t, []byte(files["vllm.yaml"][1]), svc)

	for _, obj := range []client.Object{ds, cr, dl, dep, svc} {
		obj.SetNamespace("default")
		if err := c.Create(ctx, obj); err != nil {
			t.Fatalf("apiserver rejected %s %s: %v", obj.GetObjectKind().GroupVersionKind().Kind, obj.GetName(), err)
		}
		t.Logf("created %s/%s", obj.GetObjectKind().GroupVersionKind().Kind, obj.GetName())
	}

	// round-trip the key claims
	gotDS := &datav1alpha1.Dataset{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: "default", Name: "curvine-demo"}, gotDS); err != nil {
		t.Fatalf("get dataset: %v", err)
	}
	if len(gotDS.Spec.Mounts) != 1 || gotDS.Spec.Mounts[0].MountPoint != "s3://test" || gotDS.Spec.Mounts[0].Name != "minio" {
		t.Fatalf("dataset mounts round-trip mismatch: %+v", gotDS.Spec.Mounts)
	}
	if gotDS.Spec.Mounts[0].Options["endpoint_url"] != "http://minio:9000" ||
		gotDS.Spec.Mounts[0].Options["region_name"] != "us-east-1" ||
		gotDS.Spec.Mounts[0].Options["path_style"] != "true" {
		t.Fatalf("dataset mount options mismatch: %+v", gotDS.Spec.Mounts[0].Options)
	}
	if len(gotDS.Spec.Mounts[0].EncryptOptions) != 2 ||
		gotDS.Spec.Mounts[0].EncryptOptions[0].ValueFrom.SecretKeyRef.Name != "curvine-secret" {
		t.Fatalf("dataset encryptOptions mismatch: %+v", gotDS.Spec.Mounts[0].EncryptOptions)
	}

	gotCR := &datav1alpha1.CacheRuntime{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: "default", Name: "curvine-demo"}, gotCR); err != nil {
		t.Fatalf("get cacheruntime: %v", err)
	}
	if gotCR.Spec.RuntimeClassName != "curvine-demo" {
		t.Fatalf("runtimeClassName mismatch: %q", gotCR.Spec.RuntimeClassName)
	}
	lv := gotCR.Spec.Worker.TieredStore.Levels
	if len(lv) != 1 || lv[0].EmptyDir == nil || lv[0].EmptyDir.Quota.String() != "4Gi" || lv[0].Low != "0.5" || lv[0].High != "0.8" {
		t.Fatalf("tieredStore round-trip mismatch: %+v", lv)
	}
	if gotCR.Spec.Master.Options["rpc_port"] != "8995" || gotCR.Spec.Worker.Options["format_worker"] != "true" {
		t.Fatalf("component options mismatch: %+v / %+v", gotCR.Spec.Master.Options, gotCR.Spec.Worker.Options)
	}

	gotDL := &datav1alpha1.DataLoad{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: "default", Name: "curvine-dataload"}, gotDL); err != nil {
		t.Fatalf("get dataload: %v", err)
	}
	if len(gotDL.Spec.Target) != 1 || gotDL.Spec.Target[0].Path != "/minio" ||
		gotDL.Spec.Dataset.Name != "curvine-demo" || gotDL.Spec.Dataset.Namespace != "default" {
		t.Fatalf("dataload spec mismatch: %+v", gotDL.Spec)
	}

	gotDep := &appsv1.Deployment{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: "default", Name: "vllm"}, gotDep); err != nil {
		t.Fatalf("get deployment: %v", err)
	}
	spec := gotDep.Spec.Template.Spec
	if spec.EnableServiceLinks == nil || *spec.EnableServiceLinks != false {
		t.Fatalf("enableServiceLinks not false: %+v", spec.EnableServiceLinks)
	}
	args := spec.Containers[0].Args
	if len(args) == 0 || args[0] != "/data/minio/Qwen2.5-0.5B-Instruct" {
		t.Fatalf("vllm args mismatch: %v", args)
	}
	if spec.Volumes[0].PersistentVolumeClaim == nil || spec.Volumes[0].PersistentVolumeClaim.ClaimName != "curvine-demo" {
		t.Fatalf("model volume does not reference PVC curvine-demo: %+v", spec.Volumes[0])
	}

	gotSvc := &corev1.Service{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: "default", Name: "vllm-svc"}, gotSvc); err != nil {
		t.Fatalf("get service: %v", err)
	}
	if gotSvc.Name == "vllm" { // the VLLM_PORT collision the doc warns about
		t.Fatalf("service must not be named vllm")
	}
}
