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

package fuse

// Reviewer verification harness (additive only; production code untouched).
//
// Claims verified here (PR #6262):
//   P0-a (premise, runs against BASE): assembleMountInfos returns varying
//        colon-joined strings across invocations when the input map has >1 key,
//        because it iterates the map directly.
//   F-ns: injectCheckMountReadyScript picks a random runtimeInfo's namespace
//        across invocations (map iteration), so the check-mount ConfigMap is
//        created in a non-deterministic namespace.
//
// Polarity: CONTRACT tests. They assert the intended deterministic behavior.
// On the base (unpatched) branch they FAIL (that failure is the reproduction);
// on the PR head they PASS.

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

func multiRuntimeInfosInDistinctNamespaces(t *testing.T) map[string]base.RuntimeInfoInterface {
	t.Helper()
	ris := map[string]base.RuntimeInfoInterface{}
	for _, kv := range [][2]string{
		{"z-pvc", "ns-z"},
		{"a-pvc", "ns-a"},
		{"m-pvc", "ns-m"},
	} {
		ri, err := base.BuildRuntimeInfo(kv[0], kv[1], "alluxio")
		if err != nil {
			t.Fatalf("BuildRuntimeInfo(%s,%s) failed: %v", kv[0], kv[1], err)
		}
		ris[kv[0]] = ri
	}
	return ris
}

// TestVAssembleMountInfosDeterministic is a CONTRACT test:
// repeated invocations must yield exactly one distinct (mountPathStr, mountTypeStr) pair.
func TestVAssembleMountInfosDeterministic(t *testing.T) {
	path2RuntimeTypeMap := map[string]string{
		"/var/data/z": "jindo",
		"/var/data/a": "alluxio",
		"/var/data/m": "juicefs",
	}
	const iterations = 200

	distinct := map[string]string{}
	for i := 0; i < iterations; i++ {
		mountPathStr, mountTypeStr := assembleMountInfos(path2RuntimeTypeMap)
		distinct[mountPathStr] = mountTypeStr
	}

	if len(distinct) != 1 {
		t.Fatalf("assembleMountInfos is non-deterministic: observed %d distinct outputs over %d invocations: %v", len(distinct), iterations, distinct)
	}
	for got, gotType := range distinct {
		if got != "/var/data/a:/var/data/m:/var/data/z" {
			t.Fatalf("unexpected mountPathStr: got %q (types %q), want sorted \"/var/data/a:/var/data/m:/var/data/z\"", got, gotType)
		}
	}
}

// TestVInjectCheckMountReadyScriptNamespaceDeterministic is a CONTRACT test:
// repeated injections must always create the check-mount ConfigMap in the
// namespace of the lexicographically-first runtimeInfo key (a-pvc -> ns-a).
func TestVInjectCheckMountReadyScriptNamespaceDeterministic(t *testing.T) {
	runtimeInfos := multiRuntimeInfosInDistinctNamespaces(t)
	const iterations = 40

	seenNamespaces := map[string]int{}
	for i := 0; i < iterations; i++ {
		scheme := runtime.NewScheme()
		if err := corev1.AddToScheme(scheme); err != nil {
			t.Fatalf("scheme setup failed: %v", err)
		}
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		injector := &Injector{client: fakeClient, log: logr.Discard()}

		podSpecs := &mutator.MutatingPodSpecs{
			MetaObj: metav1.ObjectMeta{Name: "verify-pod", Namespace: "ns-a"},
			Volumes: []corev1.Volume{},
			Containers: []corev1.Container{
				{
					Name: "app",
					VolumeMounts: []corev1.VolumeMount{
						{Name: "vol-a", MountPath: "/data/a"},
					},
				},
			},
		}
		if err := injector.injectCheckMountReadyScript(podSpecs, runtimeInfos); err != nil {
			t.Fatalf("iteration %d: injectCheckMountReadyScript failed: %v", i, err)
		}

		cmList := &corev1.ConfigMapList{}
		if err := fakeClient.List(context.TODO(), cmList, client.InNamespace("ns-a")); err != nil {
			t.Fatalf("iteration %d: list configmaps failed: %v", i, err)
		}
		created := 0
		for _, cm := range cmList.Items {
			seenNamespaces[cm.Namespace]++
			created++
		}
		if created == 0 {
			// The ConfigMap may have been created in another namespace; find it.
			all := &corev1.ConfigMapList{}
			if err := fakeClient.List(context.TODO(), all); err != nil {
				t.Fatalf("iteration %d: list all configmaps failed: %v", i, err)
			}
			for _, cm := range all.Items {
				seenNamespaces[cm.Namespace]++
			}
		}
	}

	if len(seenNamespaces) != 1 {
		t.Fatalf("ConfigMap created in non-deterministic namespaces: %v", seenNamespaces)
	}
	for ns := range seenNamespaces {
		if ns != "ns-a" {
			t.Fatalf("ConfigMap created in %q, want deterministically \"ns-a\" (lexicographically-first key a-pvc)", ns)
		}
	}
}
