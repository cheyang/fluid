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

// Integration layer (L2) of the PR #6233 verification harness: same scenario
// as verify_pr6233_pvcnotbound_test.go but against a REAL API server
// (envtest), so the verdict does not rest on fake-client fidelity.
// Base-compatible: references no PR-introduced symbols.
//
// Run with:
//   KUBEBUILDER_ASSETS=<envtest bin dir> go test ./pkg/ddc/thin/ \
//     -run TestVerifyPR6233_Envtest -v

package thin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// TestVerifyPR6233_Envtest_PendingPVCNoErrorLog (contract): with a real API
// server, a dataset whose pvc:// mount is pending must not make
// ShouldUpdateUFS emit Error-level logs, and the notice must be at V(1)+.
// FAILS on base (Error logged — the #6203 symptom), PASSES on head.
func TestVerifyPR6233_Envtest_PendingPVCNoErrorLog(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS not set; skipping envtest layer")
	}

	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
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

	c, err := client.New(cfg, client.Options{Scheme: testScheme})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "fluid-verify"}}
	if err := c.Create(ctx, ns); err != nil {
		t.Fatalf("create namespace: %v", err)
	}

	ds := &datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: "verify-envtest", Namespace: ns.Name},
		Spec: datav1alpha1.DatasetSpec{
			Mounts: []datav1alpha1.Mount{{Name: "my-pvc", MountPoint: "pvc://my-pvc"}},
		},
	}
	if err := c.Create(ctx, ds); err != nil {
		t.Fatalf("create dataset: %v", err)
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "verify-envtest-fuse-conf", Namespace: ns.Name},
		Data:       map[string]string{"config.json": "{}"},
	}
	if err := c.Create(ctx, cm); err != nil {
		t.Fatalf("create configmap: %v", err)
	}

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "my-pvc", Namespace: ns.Name},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources:   corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")}},
		},
	}
	if err := c.Create(ctx, pvc); err != nil {
		t.Fatalf("create pvc: %v", err)
	}
	// Make the provisioning state explicit via the status subresource.
	pvc.Status.Phase = corev1.ClaimPending
	if err := c.Status().Update(ctx, pvc); err != nil {
		t.Fatalf("set pvc status: %v", err)
	}

	t.Setenv(EnvFuseConfigStorage, "configmap")
	rt := &datav1alpha1.ThinRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: "verify-envtest", Namespace: ns.Name},
	}
	sink := &verifyPR6233LogSink{}
	engine := &ThinEngine{
		name:      "verify-envtest",
		namespace: ns.Name,
		runtime:   rt,
		Client:    c,
		Log:       logr.New(sink),
	}

	if got := engine.ShouldUpdateUFS(); got != nil {
		t.Fatalf("ShouldUpdateUFS() = %v, want nil", got)
	}
	if len(sink.errors) != 0 {
		t.Fatalf("pending PVC produced %d Error log records (%v) against a real API server; want 0 — issue #6203 symptom present", len(sink.errors), sink.errorMsgs)
	}
	for i, msg := range sink.infoMsgs {
		if strings.Contains(msg, "not bounded yet") && sink.infoLevels[i] >= 1 {
			return
		}
	}
	t.Fatalf("no V(1)+ 'not bounded yet' notice recorded (msgs=%v levels=%v)", sink.infoMsgs, sink.infoLevels)
}
