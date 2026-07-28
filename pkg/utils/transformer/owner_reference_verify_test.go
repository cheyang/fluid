/*
Copyright 2024 The Fluid Authors.

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

// Verification harness for https://github.com/fluid-cloudnative/fluid/pull/6139
//
// Reviewer-side evidence only. Production code is NOT modified by this file.
// See docs/verification/owner-reference-gvk/README.md for the hypotheses and the
// observed-vs-expected table.
//
// These are plain `go test` functions (not Ginkgo specs) on purpose: the
// re-verify.sh driver parses `go test -json`, which reports one event per Go test
// function. A Ginkgo DescribeTable collapses into a single Test name and would make
// per-finding verdicts unresolvable.

package transformer

import (
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// ---------------------------------------------------------------------------
// H0 — regression guard for the bug this PR fixes (CONTRACT).
//
// Before the PR, an object with a fully empty TypeMeta produced
// APIVersion:"/" and Kind:"". This must not come back.
// Expected on PR head: PASS. Expected on merge base 44cfae9fb: FAIL.
// ---------------------------------------------------------------------------
func TestVerifyH0_EmptyTypeMetaIsRecoveredFromScheme(t *testing.T) {
	cases := []struct {
		name        string
		obj         client.Object
		wantKind    string
		wantAPIVers string
	}{
		{"Dataset", &datav1alpha1.Dataset{ObjectMeta: metav1.ObjectMeta{Name: "d", UID: "u1"}}, "Dataset", "data.fluid.io/v1alpha1"},
		{"DataLoad", &datav1alpha1.DataLoad{ObjectMeta: metav1.ObjectMeta{Name: "l", UID: "u2"}}, "DataLoad", "data.fluid.io/v1alpha1"},
		{"DataMigrate", &datav1alpha1.DataMigrate{ObjectMeta: metav1.ObjectMeta{Name: "m", UID: "u3"}}, "DataMigrate", "data.fluid.io/v1alpha1"},
		{"DataProcess", &datav1alpha1.DataProcess{ObjectMeta: metav1.ObjectMeta{Name: "p", UID: "u4"}}, "DataProcess", "data.fluid.io/v1alpha1"},
		{"AlluxioRuntime", &datav1alpha1.AlluxioRuntime{ObjectMeta: metav1.ObjectMeta{Name: "r", UID: "u5"}}, "AlluxioRuntime", "data.fluid.io/v1alpha1"},
		{"JuiceFSRuntime", &datav1alpha1.JuiceFSRuntime{ObjectMeta: metav1.ObjectMeta{Name: "r", UID: "u6"}}, "JuiceFSRuntime", "data.fluid.io/v1alpha1"},
		{"ThinRuntime", &datav1alpha1.ThinRuntime{ObjectMeta: metav1.ObjectMeta{Name: "r", UID: "u7"}}, "ThinRuntime", "data.fluid.io/v1alpha1"},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			got := GenerateOwnerReferenceFromObject(c.obj)
			if got.Kind != c.wantKind {
				t.Errorf("Kind: got %q, want %q", got.Kind, c.wantKind)
			}
			if got.APIVersion != c.wantAPIVers {
				t.Errorf("APIVersion: got %q, want %q", got.APIVersion, c.wantAPIVers)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// H1 — partially populated TypeMeta skips recovery entirely (CONTRACT).
//
// gvk.Empty() is `len(Group)==0 && len(Version)==0 && len(Kind)==0`
// (vendor/k8s.io/apimachinery/pkg/runtime/schema/group_version.go:149-151), so an
// object with only Kind set — or only APIVersion set — is NOT "empty" and the new
// scheme fallback never runs. The result is still an ownerReference the API server
// rejects.
//
// This test asserts the INTENDED contract: whatever subset of TypeMeta is present,
// the emitted ownerReference must be complete and correct for a scheme-registered
// type. It is expected to FAIL on PR head — that failure IS the reproduction of H1.
// ---------------------------------------------------------------------------
func TestVerifyH1_PartialTypeMetaStillYieldsMalformedRef(t *testing.T) {
	const (
		wantKind    = "DataLoad"
		wantAPIVers = "data.fluid.io/v1alpha1"
	)

	cases := []struct {
		name     string
		typeMeta metav1.TypeMeta
	}{
		// Only Kind set: Group=="" Version=="" Kind=="DataLoad" -> Empty()==false.
		// GroupVersion{}.String() returns Version ("") because Group is empty, so
		// APIVersion comes out as the empty string.
		{"KindOnly", metav1.TypeMeta{Kind: "DataLoad"}},
		// Only APIVersion set: Group/Version populated, Kind=="" -> Empty()==false.
		// Kind stays empty; the API server requires it.
		{"APIVersionOnly", metav1.TypeMeta{APIVersion: "data.fluid.io/v1alpha1"}},
		// Version-only APIVersion (no group). Also non-empty, also no recovery.
		{"BareVersionOnly", metav1.TypeMeta{APIVersion: "v1alpha1"}},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			obj := &datav1alpha1.DataLoad{
				TypeMeta:   c.typeMeta,
				ObjectMeta: metav1.ObjectMeta{Name: "partial", Namespace: "default", UID: "uid-partial"},
			}

			got := GenerateOwnerReferenceFromObject(obj)
			t.Logf("H1 observed: input TypeMeta{Kind:%q APIVersion:%q} -> OwnerReference{Kind:%q APIVersion:%q}",
				c.typeMeta.Kind, c.typeMeta.APIVersion, got.Kind, got.APIVersion)

			if got.Kind != wantKind {
				t.Errorf("Kind: got %q, want %q (recovery skipped because gvk.Empty()==false)", got.Kind, wantKind)
			}
			if got.APIVersion != wantAPIVers {
				t.Errorf("APIVersion: got %q, want %q (recovery skipped because gvk.Empty()==false)", got.APIVersion, wantAPIVers)
			}
		})
	}
}

// TestVerifyH1Canary_ObservedPartialTypeMetaOutput pins the ACTUAL malformed output
// produced by PR head for partially populated TypeMeta (CANARY).
//
// Expected on PR head: PASS (it documents today's behavior).
// Expected once H1 is fixed: FLIPS TO FAIL — at which point invert/delete it.
func TestVerifyH1Canary_ObservedPartialTypeMetaOutput(t *testing.T) {
	cases := []struct {
		name           string
		typeMeta       metav1.TypeMeta
		observedKind   string
		observedAPIVer string
	}{
		{"KindOnly", metav1.TypeMeta{Kind: "DataLoad"}, "DataLoad", ""},
		{"APIVersionOnly", metav1.TypeMeta{APIVersion: "data.fluid.io/v1alpha1"}, "", "data.fluid.io/v1alpha1"},
		{"BareVersionOnly", metav1.TypeMeta{APIVersion: "v1alpha1"}, "", "v1alpha1"},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			obj := &datav1alpha1.DataLoad{
				TypeMeta:   c.typeMeta,
				ObjectMeta: metav1.ObjectMeta{Name: "partial", Namespace: "default", UID: "uid-partial"},
			}
			got := GenerateOwnerReferenceFromObject(obj)
			if got.Kind != c.observedKind || got.APIVersion != c.observedAPIVer {
				t.Errorf("canary flipped: got Kind=%q APIVersion=%q, previously observed Kind=%q APIVersion=%q",
					got.Kind, got.APIVersion, c.observedKind, c.observedAPIVer)
			}
		})
	}
}

// TestVerifyH1_EmptyPredicateSemantics documents the exact upstream predicate that
// causes H1, independently of Fluid code (CONTRACT on apimachinery semantics — it
// passes on both refs and exists to make the mechanism auditable).
func TestVerifyH1_EmptyPredicateSemantics(t *testing.T) {
	if !(schema.GroupVersionKind{}).Empty() {
		t.Error("expected a zero GroupVersionKind to be Empty()")
	}
	if (schema.GroupVersionKind{Kind: "DataLoad"}).Empty() {
		t.Error("GVK with only Kind set is NOT Empty() — this is why the PR's recovery is skipped")
	}
	if (schema.GroupVersionKind{Group: "data.fluid.io", Version: "v1alpha1"}).Empty() {
		t.Error("GVK with only Group/Version set is NOT Empty() — recovery is skipped")
	}
	// And the string form of a Group-less GroupVersion is just the (empty) Version,
	// so a Kind-only TypeMeta yields APIVersion:"" rather than "/".
	if got := (schema.GroupVersionKind{Kind: "DataLoad"}).GroupVersion().String(); got != "" {
		t.Errorf("GroupVersion().String() for Kind-only GVK: got %q, want %q", got, "")
	}
}

// ---------------------------------------------------------------------------
// H2 — the recovery error is swallowed with no diagnostic (CONTRACT, log-capture).
//
// The fallback is `if resolved, err := apiutil.GVKForObject(obj, fluidScheme); err == nil`.
// For a type that is not in fluidScheme the error is dropped on the floor: the
// function returns a malformed ownerReference with no log, event or error return.
//
// This test installs a capturing logr sink as the controller-runtime global logger
// and asserts that at least one message is emitted when resolution fails. It is
// SATISFIABLE by the proposed fix (log the error instead of discarding it), which is
// what makes it a legitimate contract test rather than an unfalsifiable wish.
//
// Expected on PR head: FAIL (zero log records captured).
// ---------------------------------------------------------------------------
func TestVerifyH2_UnregisteredTypeLogsNoDiagnostic(t *testing.T) {
	// A ConfigMap is a legitimate client.Object but is NOT registered in the
	// package-level fluidScheme (only datav1alpha1.AddToScheme is applied).
	obj := unregisteredObject()

	// Establish that fluidScheme really cannot resolve it, i.e. the error branch is
	// the one being taken.
	probe := runtime.NewScheme()
	if err := datav1alpha1.AddToScheme(probe); err != nil {
		t.Fatalf("probe scheme setup: %v", err)
	}
	if _, err := apiutil.GVKForObject(obj, probe); err == nil {
		t.Fatalf("precondition failed: expected the fluid-only scheme to reject %T", obj)
	} else {
		t.Logf("H2 precondition: apiutil.GVKForObject returns an error that the caller drops: %v", err)
	}

	sink := &capturingSink{}
	restore := ctrllog.Log
	ctrllog.SetLogger(logr.New(sink))
	defer func() {
		// Best-effort restore; controller-runtime's DelegatingLogSink is one-shot per
		// process, so only the captured count matters for the assertion.
		_ = restore
	}()

	got := GenerateOwnerReferenceFromObject(obj)
	t.Logf("H2 observed: unregistered %T -> OwnerReference{Kind:%q APIVersion:%q Enabled:%v Controller:%v}; log records captured=%d",
		obj, got.Kind, got.APIVersion, got.Enabled, got.Controller, sink.count())

	if sink.count() == 0 {
		t.Errorf("silent degradation confirmed: GVK resolution failed and produced an "+
			"ownerReference {Kind:%q APIVersion:%q Enabled:%v}, but emitted 0 log records — "+
			"no error return, no event, nothing for an operator to diagnose",
			got.Kind, got.APIVersion, got.Enabled)
	}
}

// TestVerifyH2Canary_UnregisteredTypeReturnsUnusableRef pins the actual malformed
// output for an unregistered type (CANARY).
//
// Note the observed value on PR head is Kind:"" APIVersion:"" — NOT the "/" that the
// pre-PR code produced. A ConfigMap has an empty TypeMeta, so gvk.Empty() is true,
// recovery is attempted, it fails, and gvk stays the zero value whose
// GroupVersion().String() is "" (Group is empty, so String() returns just Version).
//
// Expected on PR head: PASS. On merge base 44cfae9fb it FAILS (APIVersion was "/").
func TestVerifyH2Canary_UnregisteredTypeReturnsUnusableRef(t *testing.T) {
	got := GenerateOwnerReferenceFromObject(unregisteredObject())

	if got.Kind != "" {
		t.Errorf("canary flipped: Kind is now %q (previously observed \"\")", got.Kind)
	}
	if got.APIVersion != "" {
		t.Errorf("canary flipped: APIVersion is now %q (previously observed \"\" on PR head; "+
			"the pre-PR code emitted \"/\")", got.APIVersion)
	}
	// Enabled/Controller are set unconditionally even though the reference is unusable.
	if !got.Enabled || !got.Controller {
		t.Errorf("canary flipped: Enabled=%v Controller=%v (previously both true even for "+
			"an unresolvable owner)", got.Enabled, got.Controller)
	}
}

// capturingSink is a minimal logr.LogSink that counts Info/Error records.
type capturingSink struct {
	n    int
	msgs []string
}

func (s *capturingSink) count() int            { return s.n }
func (s *capturingSink) Init(logr.RuntimeInfo) {}
func (s *capturingSink) Enabled(int) bool      { return true }
func (s *capturingSink) Info(_ int, msg string, _ ...interface{}) {
	s.n++
	s.msgs = append(s.msgs, msg)
}
func (s *capturingSink) Error(_ error, msg string, _ ...interface{}) {
	s.n++
	s.msgs = append(s.msgs, msg)
}
func (s *capturingSink) WithValues(...interface{}) logr.LogSink { return s }
func (s *capturingSink) WithName(string) logr.LogSink           { return s }

// unregisteredObject returns a client.Object whose type is absent from fluidScheme.
// core/v1 is never added to fluidScheme (only datav1alpha1.AddToScheme is), so a
// ConfigMap with an empty TypeMeta exercises the swallowed-error branch.
func unregisteredObject() client.Object {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "cm", Namespace: "default", UID: "uid-cm"},
	}
}

// ---------------------------------------------------------------------------
// H3 — BlockOwnerDeletion:false alongside Controller:true (CANARY, pre-existing).
//
// This is NOT introduced by PR 6139: the merge base emits the same two field values.
// Recorded as a canary so that if the project later decides foreground-deletion
// protection is wanted, the harness flips and forces a decision.
//
// Expected on PR head AND on merge base 44cfae9fb: PASS.
// ---------------------------------------------------------------------------
func TestVerifyH3Canary_BlockOwnerDeletionIsFalseWithControllerTrue(t *testing.T) {
	obj := &datav1alpha1.DataLoad{
		TypeMeta: metav1.TypeMeta{Kind: "DataLoad", APIVersion: "data.fluid.io/v1alpha1"},
		ObjectMeta: metav1.ObjectMeta{
			Name: "dl", Namespace: "default", UID: "uid-h3",
		},
	}

	got := GenerateOwnerReferenceFromObject(obj)
	t.Logf("H3 observed: Controller=%v BlockOwnerDeletion=%v", got.Controller, got.BlockOwnerDeletion)

	if !got.Controller {
		t.Errorf("canary flipped: Controller is now %v (was true)", got.Controller)
	}
	if got.BlockOwnerDeletion {
		t.Errorf("canary flipped: BlockOwnerDeletion is now %v (was false) — "+
			"foreground-deletion protection was added", got.BlockOwnerDeletion)
	}
}
