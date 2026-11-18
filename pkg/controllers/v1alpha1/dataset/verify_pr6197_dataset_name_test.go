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

// Verification harness for the review of PR #6197 (reviewer B / Codex), round 5.
// Additive test file only; no production code is changed.
//
// A-F6 justification, pinned at its source: the round-5 docs claim "Fluid never binds a
// dataset whose name is not a DNS-1035 label". That is true because reconcileDataset
// (pre-existing, #2233) rejects such a name with an error BEFORE any reconcile progress
// (no finalizer, no runtime, no Bound phase), so a digit-leading dataset can never be
// served by the webhook's PVC path either. This contract reconciles a digit-leading
// dataset once and asserts exactly that. Green on base and head alike (the gate is not
// part of this PR); it exists so a future change that starts accepting such names flips
// red and forces a re-review of the webhook's name checks.
package dataset

import (
	"context"
	"strings"
	"testing"

	"github.com/fluid-cloudnative/fluid/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func TestVerifyPR6197NonDNS1035DatasetNeverReconciles(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v1alpha1.AddToScheme(scheme)

	ds := &v1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: "20-hbase", Namespace: "default"},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(ds).Build()
	r := &DatasetReconciler{
		Client:   c,
		Recorder: record.NewFakeRecorder(10),
		Log:      zap.New(zap.UseDevMode(true)),
	}

	// Call reconcileDataset directly: Reconcile first goes through
	// ScaleoutRuntimeControllerOnDemand, whose CRD discovery does not terminate quickly
	// against a fake client; the name gate itself lives in reconcileDataset.
	ctx := reconcileRequestContext{
		Context:        context.Background(),
		Log:            zap.New(zap.UseDevMode(true)),
		NamespacedName: types.NamespacedName{Name: "20-hbase", Namespace: "default"},
	}
	if getErr := c.Get(ctx, ctx.NamespacedName, &ctx.Dataset); getErr != nil {
		t.Fatalf("seed dataset: %v", getErr)
	}
	_, err := r.reconcileDataset(ctx, false)
	if err == nil {
		t.Fatalf("reconciling a digit-leading dataset name must fail the DNS-1035 gate")
	}

	got := &v1alpha1.Dataset{}
	if getErr := c.Get(context.Background(),
		client.ObjectKey{Name: "20-hbase", Namespace: "default"}, got); getErr != nil {
		t.Fatalf("re-get dataset: %v", getErr)
	}
	if len(got.Finalizers) != 0 {
		t.Fatalf("the DNS-1035 gate must fire before any reconcile progress, got finalizers %v", got.Finalizers)
	}
	if got.Status.Phase == v1alpha1.BoundDatasetPhase {
		t.Fatalf("a non-DNS-1035 dataset must never reach Bound")
	}

	// control: a DNS-1035-valid name passes the gate and gets the finalizer
	ok := &v1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: "hbase-20", Namespace: "default"},
	}
	c2 := fake.NewClientBuilder().WithScheme(scheme).WithObjects(ok).Build()
	r2 := &DatasetReconciler{
		Client:   c2,
		Recorder: record.NewFakeRecorder(10),
		Log:      zap.New(zap.UseDevMode(true)),
	}
	ctx2 := reconcileRequestContext{
		Context:        context.Background(),
		Log:            zap.New(zap.UseDevMode(true)),
		NamespacedName: types.NamespacedName{Name: "hbase-20", Namespace: "default"},
	}
	if getErr := c2.Get(ctx2, ctx2.NamespacedName, &ctx2.Dataset); getErr != nil {
		t.Fatalf("seed control dataset: %v", getErr)
	}
	_, err2 := r2.reconcileDataset(ctx2, false)
	if err2 != nil && strings.Contains(err2.Error(), "DNS-1035") {
		t.Fatalf("control broken: a DNS-1035-valid dataset name must pass the gate, got %v", err2)
	}
	got2 := &v1alpha1.Dataset{}
	if getErr := c2.Get(context.Background(),
		client.ObjectKey{Name: "hbase-20", Namespace: "default"}, got2); getErr != nil {
		t.Fatalf("re-get control dataset: %v", getErr)
	}
	if len(got2.Finalizers) == 0 {
		t.Fatalf("control broken: a DNS-1035-valid dataset should get the finalizer on the first reconcile")
	}
}
