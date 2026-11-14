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

package cacheruntime

// Verification harness for fluid-cloudnative/fluid PR #6243, finding F1
// (reviewer: Codex). Not part of the PR; lives on the
// verify/cacheruntime-validating-webhook-codex branch only.
//
// F1: for a CacheRuntime whose STORED form has no spec.client section (e.g. a
// client-less topology applied from YAML, like test/gha-e2e/mooncake), any
// full-object Update — including the controller's own metadata-only updates
// that add a finalizer or an owner reference — is denied by the new webhook.
// Mechanism, in three separately checkable steps:
//
//  1. The stored/admission old object has no spec.client, so its decoded
//     CleanPolicy is "" (CRD defaulting does not materialize absent parent
//     objects; proven against a real API server by the L2 envtest in
//     pkg/webhook/handler/validating/cacheruntime/defaultingtest).
//  2. A Go client (the fluid CacheRuntime controller) holds the runtime as a
//     typed struct; encoding/json does not omit zero-value structs, so its
//     Update payload carries "client":{} — asserted below.
//  3. The API server decodes the payload and applies CRD defaulting: with
//     spec.client present, spec.client.cleanPolicy defaults to
//     "OnRuntimeDeleted" (charts/fluid/fluid/crds/data.fluid.io_cacheruntimes.yaml,
//     `default: OnRuntimeDeleted`). The admission new object therefore differs
//     from the old in spec.client.cleanPolicy, and ValidateSpecUpdate forbids
//     it. The PR's own kind-e2e CI shows exactly this denial from the
//     controller ("Failed to add ownerreference ... spec.client.cleanPolicy:
//     Forbidden: field is immutable") and the mooncake dataset stuck NotBound
//     for 600s on all five Kubernetes versions.
//
// These are plain Go tests (not ginkgo specs) so re-verify.sh can select them
// with -run.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// storedClientlessRuntimeJSON is the shape the API server stores and serves
// for a CacheRuntime applied from YAML without a spec.client section (as in
// test/gha-e2e/mooncake/cacheruntime.yaml). Note: no "client" key at all.
const storedClientlessRuntimeJSON = `{
  "apiVersion": "data.fluid.io/v1alpha1",
  "kind": "CacheRuntime",
  "metadata": {"name": "mooncake-demo", "namespace": "default", "resourceVersion": "1000"},
  "spec": {
    "runtimeClassName": "mooncake-demo",
    "master": {"replicas": 1},
    "worker": {"replicas": 1, "tieredStore": {"levels": [{"low": "0.5", "high": "0.8", "emptyDir": {"quota": "1Gi"}}]}}
  }
}`

func mustDecodeCacheRuntime(t *testing.T, raw string) *datav1alpha1.CacheRuntime {
	t.Helper()
	obj := &datav1alpha1.CacheRuntime{}
	if err := json.Unmarshal([]byte(raw), obj); err != nil {
		t.Fatalf("decode stored form: %v", err)
	}
	return obj
}

// simulateControllerUpdatePayload models what the API server hands to the
// validating webhook as the NEW object when a Go client does a full-object
// Update of a stored client-less CacheRuntime to add a finalizer:
// Go marshaling materializes "client":{} (step 2) and the API server's CRD
// defaulting then fills spec.client.cleanPolicy (step 3).
func simulateControllerUpdatePayload(t *testing.T, stored *datav1alpha1.CacheRuntime) []byte {
	t.Helper()
	inMemory := stored.DeepCopy()
	inMemory.Finalizers = append(inMemory.Finalizers, "fluid-cacheruntime-controller-finalizer")

	payload, err := json.Marshal(inMemory)
	if err != nil {
		t.Fatalf("marshal controller update payload: %v", err)
	}
	if !strings.Contains(string(payload), `"client":{`) {
		t.Fatalf("model broken: expected Go marshaling to materialize a client object, payload: %s", payload)
	}

	// The API server decodes the payload and applies CRD defaulting. With
	// spec.client present, cleanPolicy defaults to OnRuntimeDeleted.
	decoded := &datav1alpha1.CacheRuntime{}
	if err := json.Unmarshal(payload, decoded); err != nil {
		t.Fatalf("re-decode update payload: %v", err)
	}
	decoded.Spec.Client.CleanPolicy = datav1alpha1.OnRuntimeDeletedCleanPolicy

	out, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("marshal admission new object: %v", err)
	}
	return out
}

func newPR6243Handler(t *testing.T) *SpecUpdateHandler {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := datav1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add scheme: %v", err)
	}
	h := &SpecUpdateHandler{}
	h.Setup(nil, nil, admission.NewDecoder(scheme))
	return h
}

// TestVerifyPR6243ClientlessMetadataUpdateAllowed is the F1 contract test:
// a metadata-only full-object update of a client-less CacheRuntime must be
// allowed. On the PR head it is denied with
// "spec.client.cleanPolicy: Forbidden: field is immutable".
func TestVerifyPR6243ClientlessMetadataUpdateAllowed(t *testing.T) {
	old := mustDecodeCacheRuntime(t, storedClientlessRuntimeJSON)
	if old.Spec.Client.CleanPolicy != "" {
		t.Fatalf("model broken: stored client-less form should have empty cleanPolicy, got %q", old.Spec.Client.CleanPolicy)
	}

	newRaw := simulateControllerUpdatePayload(t, old)

	req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Operation: admissionv1.Update,
		Namespace: "default",
		Name:      "mooncake-demo",
		OldObject: runtime.RawExtension{Raw: []byte(storedClientlessRuntimeJSON)},
		Object:    runtime.RawExtension{Raw: newRaw},
	}}

	resp := newPR6243Handler(t).Handle(context.TODO(), req)
	if !resp.Allowed {
		t.Errorf("F1 reproduced: the webhook denied a metadata-only update of a client-less CacheRuntime: %s",
			resp.Result.Message)
	}
}

// TestVerifyPR6243ClientlessCleanPolicyStillGuarded is the anti-overfix
// guard: a REAL user change of spec.client.cleanPolicy on a client-less
// CacheRuntime must still be denied, so a fix cannot simply drop the check.
func TestVerifyPR6243ClientlessCleanPolicyStillGuarded(t *testing.T) {
	old := mustDecodeCacheRuntime(t, storedClientlessRuntimeJSON)
	inMemory := old.DeepCopy()
	inMemory.Spec.Client.CleanPolicy = datav1alpha1.OnDemandCleanPolicy
	newRaw, err := json.Marshal(inMemory)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Operation: admissionv1.Update,
		Namespace: "default",
		Name:      "mooncake-demo",
		OldObject: runtime.RawExtension{Raw: []byte(storedClientlessRuntimeJSON)},
		Object:    runtime.RawExtension{Raw: newRaw},
	}}

	resp := newPR6243Handler(t).Handle(context.TODO(), req)
	if resp.Allowed {
		t.Errorf("expected denial for a real cleanPolicy change on a client-less CacheRuntime")
	} else if !strings.Contains(resp.Result.Message, "spec.client.cleanPolicy") {
		t.Errorf("expected the denial to name spec.client.cleanPolicy, got: %s", resp.Result.Message)
	}
}

// TestVerifyPR6243ClientfulMetadataUpdateAllowed is the sanity control: a
// CacheRuntime that was created WITH a spec.client section (curvine-style)
// carries the defaulted cleanPolicy in its stored form, so a metadata-only
// update is already allowed on the PR head.
func TestVerifyPR6243ClientfulMetadataUpdateAllowed(t *testing.T) {
	storedWithClient := `{
      "apiVersion": "data.fluid.io/v1alpha1",
      "kind": "CacheRuntime",
      "metadata": {"name": "curvine-demo", "namespace": "default", "resourceVersion": "1000"},
      "spec": {
        "runtimeClassName": "curvine-demo",
        "master": {"replicas": 1},
        "worker": {"replicas": 1},
        "client": {"cleanPolicy": "OnRuntimeDeleted"}
      }
    }`
	old := mustDecodeCacheRuntime(t, storedWithClient)
	inMemory := old.DeepCopy()
	inMemory.Finalizers = append(inMemory.Finalizers, "fluid-cacheruntime-controller-finalizer")
	inMemory.Labels = map[string]string{"fluid.io/dataset": "curvine-demo"}
	newRaw, err := json.Marshal(inMemory)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Operation: admissionv1.Update,
		Namespace: "default",
		Name:      "curvine-demo",
		OldObject: runtime.RawExtension{Raw: []byte(storedWithClient)},
		Object:    runtime.RawExtension{Raw: newRaw},
	}}

	resp := newPR6243Handler(t).Handle(context.TODO(), req)
	if !resp.Allowed {
		t.Errorf("control case failed: metadata-only update of a client-ful CacheRuntime denied: %s",
			resp.Result.Message)
	}
}
