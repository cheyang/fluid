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

package fileprefetcher

// Reviewer verification harness (additive only; production code untouched).
//
// Claim verified (PR #6262):
//   P0-b (premise, runs against BASE): when the file-list annotation is absent,
//   the default fileList built from runtimeInfos map keys joins the pvc names
//   in map-iteration order, so the injected FILE_PREFETCHER_FILE_LIST env
//   value varies across invocations.
//
// Polarity: CONTRACT test. On the base branch it FAILS (the failure is the
// reproduction); on the PR head it PASSES.

import (
	"testing"

	"github.com/fluid-cloudnative/fluid/pkg/ddc/base"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestVDefaultFileListDeterministic is a CONTRACT test: repeated config builds
// must yield exactly one distinct GlobPaths value, sorted by pvc name.
func TestVDefaultFileListDeterministic(t *testing.T) {
	runtimeInfos := map[string]base.RuntimeInfoInterface{}
	for _, kv := range [][2]string{
		{"z-pvc", "fluid"},
		{"a-pvc", "fluid"},
		{"m-pvc", "fluid"},
	} {
		ri, err := base.BuildRuntimeInfo(kv[0], kv[1], "alluxio")
		if err != nil {
			t.Fatalf("BuildRuntimeInfo(%s) failed: %v", kv[0], err)
		}
		runtimeInfos[kv[0]] = ri
	}

	prefetcher := &FilePrefetcher{log: logr.Discard()}

	newPod := func() *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "verify-pod",
				Namespace: "fluid",
				Annotations: map[string]string{
					AnnotationFilePrefetcherInject: "true",
					// image is required; no file-list annotation -> default fileList from runtimeInfos
					AnnotationFilePrefetcherImage: "verify-image:v1",
				},
			},
			Spec: corev1.PodSpec{
				Volumes: []corev1.Volume{
					volumeForPVC("vol-a", "a-pvc"),
					volumeForPVC("vol-m", "m-pvc"),
					volumeForPVC("vol-z", "z-pvc"),
				},
			},
		}
	}

	const iterations = 100
	distinct := map[string]bool{}
	for i := 0; i < iterations; i++ {
		config, err := prefetcher.buildFilePrefetcherConfig(newPod(), runtimeInfos)
		if err != nil {
			t.Fatalf("iteration %d: buildFilePrefetcherConfig failed: %v", i, err)
		}
		distinct[config.GlobPaths] = true
	}

	if len(distinct) != 1 {
		t.Fatalf("default file list is non-deterministic: %d distinct GlobPaths values over %d invocations: %v", len(distinct), iterations, distinct)
	}
	const want = "/data/vol-a/**;/data/vol-m/**;/data/vol-z/**"
	if !distinct[want] {
		t.Fatalf("unexpected GlobPaths: got %v, want %q", distinct, want)
	}
}

func volumeForPVC(name, claim string) corev1.Volume {
	return corev1.Volume{
		Name: name,
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: claim,
			},
		},
	}
}
