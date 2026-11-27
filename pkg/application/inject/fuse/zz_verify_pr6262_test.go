/*
Verification harness for PR #6262 review (Reviewer B / Codex).
Additive test file only — no production code changes.

Polarity notes:
  - TestVerifyAssembleMountInfosDeterministic: CONTRACT. Fails on base dcb7616
    (unsorted map iteration -> random join order), passes on the PR head.
  - TestVerifyNamespaceSelectionDeterministic: CONTRACT. Fails on base (the
    "first map entry" pick lands in a random namespace across invocations),
    passes on the PR head (lexicographically first key wins every time).
*/
package fuse

import (
	"context"
	"testing"

	"github.com/fluid-cloudnative/fluid/pkg/application/inject/fuse/mutator"
	"github.com/fluid-cloudnative/fluid/pkg/ddc/base"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// CONTRACT for the P0 premise + fix: assembleMountInfos must join paths/types
// in a stable, key-sorted order on every invocation.
func TestVerifyAssembleMountInfosDeterministic(t *testing.T) {
	m := map[string]string{
		"/var/data/z": "jindo",
		"/var/data/a": "alluxio",
		"/var/data/m": "juicefs",
		"/var/data/q": "thin",
		"/var/data/b": "goosefs",
		"/var/data/y": "efc",
	}
	const wantPaths = "/var/data/a:/var/data/b:/var/data/m:/var/data/q:/var/data/y:/var/data/z"
	const wantTypes = "alluxio:goosefs:juicefs:thin:efc:jindo"
	distinct := map[string]bool{}
	for i := 0; i < 300; i++ {
		paths, types := assembleMountInfos(m)
		distinct[paths+"|"+types] = true
		if paths != wantPaths || types != wantTypes {
			t.Fatalf("iteration %d: got (%q, %q), want (%q, %q); %d distinct outputs so far",
				i, paths, types, wantPaths, wantTypes, len(distinct))
		}
	}
}

// CONTRACT: with multiple runtime infos, the namespace chosen for the
// check-mount-ready ConfigMap must be identical across invocations
// (lexicographically first map key's namespace: "a-pvc" -> "ns-a").
func TestVerifyNamespaceSelectionDeterministic(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	chosenNamespaces := map[string]bool{}
	const iterations = 30
	for i := 0; i < iterations; i++ {
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		inj := &Injector{client: fakeClient, log: logr.Discard()}

		riZ, err := base.BuildRuntimeInfo("z-pvc", "ns-z", "jindo")
		if err != nil {
			t.Fatal(err)
		}
		riA, err := base.BuildRuntimeInfo("a-pvc", "ns-a", "alluxio")
		if err != nil {
			t.Fatal(err)
		}
		riM, err := base.BuildRuntimeInfo("m-pvc", "ns-m", "juicefs")
		if err != nil {
			t.Fatal(err)
		}
		infos := map[string]base.RuntimeInfoInterface{
			"z-pvc": riZ,
			"a-pvc": riA,
			"m-pvc": riM,
		}
		podSpecs := &mutator.MutatingPodSpecs{
			MetaObj:    metav1.ObjectMeta{Name: "test-pod"},
			Volumes:    []corev1.Volume{},
			Containers: []corev1.Container{},
		}
		if err := inj.injectCheckMountReadyScript(podSpecs, infos); err != nil {
			t.Fatalf("iteration %d: injectCheckMountReadyScript failed: %v", i, err)
		}
		for _, ns := range []string{"ns-z", "ns-a", "ns-m"} {
			cmList := &corev1.ConfigMapList{}
			if err := fakeClient.List(context.TODO(), cmList, client.InNamespace(ns)); err != nil {
				t.Fatal(err)
			}
			if len(cmList.Items) > 0 {
				chosenNamespaces[ns] = true
			}
		}
	}
	if len(chosenNamespaces) != 1 || !chosenNamespaces["ns-a"] {
		t.Fatalf("namespace selection was non-deterministic over %d runs: got %v, want exactly {ns-a}",
			iterations, chosenNamespaces)
	}
}

// CONTRACT (pre-existing F6, re-exposed at head 5d4117057): a nil runtimeInfo
// map entry must not panic the injector. Sibling plugins guard nil values; the
// round-2 head (08c111a) nil-skipped in the namespace pick; the round-3 rewrite
// reverted the pick loop to the unguarded base form. On base and on 5d4117057
// this test FAILS (recovered nil-pointer panic); it passes only when a nil
// guard is (re-)introduced. The nil state is not producible via in-tree callers
// (round-1 negative analysis stands), so this documents a latent defensive gap,
// not an active bug. recover() turns the panic into a clean test failure so the
// rest of the package suite still runs.
func TestVerifyNamespacePickSkipsNilRuntimeInfo(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	inj := &Injector{client: fakeClient, log: logr.Discard()}

	// Single nil entry: unguarded code dereferences it on the first (only)
	// iteration, so the panic is deterministic rather than map-order dependent.
	infos := map[string]base.RuntimeInfoInterface{"only-pvc": nil}
	podSpecs := &mutator.MutatingPodSpecs{
		MetaObj:    metav1.ObjectMeta{Name: "test-pod"},
		Volumes:    []corev1.Volume{},
		Containers: []corev1.Container{},
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("injectCheckMountReadyScript panicked on a nil runtimeInfo entry (nil guard absent): %v", r)
		}
	}()
	// Any non-panic outcome (nil error or client error) satisfies the contract.
	_ = inj.injectCheckMountReadyScript(podSpecs, infos)
}
