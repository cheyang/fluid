/*
Verification harness for PR #6262 review (Reviewer B / Codex).
Additive test file only — no production code changes.

Polarity: CONTRACT. Both tests fail on base dcb7616 (map iteration order leaks
into the sidecar's VolumeMounts/Env slices and into the default file list),
and pass on the PR head (key-sorted iteration).
*/
package fileprefetcher

import (
	"reflect"
	"testing"

	"github.com/fluid-cloudnative/fluid/pkg/ddc/base"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CONTRACT: the assembled prefetcher sidecar must be byte-identical across
// invocations, with VolumeMounts sorted by volume name (status mount last)
// and ExtraEnvs appended in key order.
func TestVerifySidecarContainerDeterministic(t *testing.T) {
	p := &FilePrefetcher{}
	config := filePrefetcherConfig{
		Image:          "test-image:v1",
		AsyncPrefetch:  true,
		TimeoutSeconds: 30,
		VolumeMountPaths: map[string]string{
			"vol-z": "/data/vol-z",
			"vol-a": "/data/vol-a",
			"vol-m": "/data/vol-m",
			"vol-q": "/data/vol-q",
		},
		ExtraEnvs: map[string]string{
			"ENV_Z": "val_z",
			"ENV_A": "val_a",
			"ENV_M": "val_m",
			"ENV_Q": "val_q",
		},
	}

	first, _ := p.buildFilePrefetcherSidecarContainer(config)
	for i := 0; i < 200; i++ {
		got, _ := p.buildFilePrefetcherSidecarContainer(config)
		if !reflect.DeepEqual(first, got) {
			t.Fatalf("iteration %d: non-deterministic container spec:\nfirst: %+v\ngot:   %+v", i, first, got)
		}
	}

	var vmNames []string
	for _, vm := range first.VolumeMounts {
		vmNames = append(vmNames, vm.Name)
	}
	wantVM := []string{"vol-a", "vol-m", "vol-q", "vol-z", filePrefetcherStatusVolumeName}
	if !reflect.DeepEqual(vmNames, wantVM) {
		t.Fatalf("volume mount order = %v, want %v", vmNames, wantVM)
	}

	var extraEnvNames []string
	for _, env := range first.Env {
		switch env.Name {
		case envKeyFilePrefetcherFileList, envKeyFilePrefetcherAsyncPrefetch, envKeyFilePrefetcherTimeoutSeconds:
			continue
		}
		extraEnvNames = append(extraEnvNames, env.Name)
	}
	wantEnvs := []string{"ENV_A", "ENV_M", "ENV_Q", "ENV_Z"}
	if !reflect.DeepEqual(extraEnvNames, wantEnvs) {
		t.Fatalf("extra env order = %v, want %v", extraEnvNames, wantEnvs)
	}
}

// CONTRACT: the default file list (pvc://<name> for every runtime info) must
// be assembled in sorted PVC-name order, so the derived GlobPaths env value
// is stable across invocations.
func TestVerifyDefaultFileListDeterministic(t *testing.T) {
	p := &FilePrefetcher{}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				AnnotationFilePrefetcherImage: "test-image:v1",
			},
		},
		Spec: corev1.PodSpec{
			Volumes: []corev1.Volume{
				{Name: "z-vol", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "z-pvc"}}},
				{Name: "a-vol", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "a-pvc"}}},
				{Name: "m-vol", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "m-pvc"}}},
			},
		},
	}
	infos := map[string]base.RuntimeInfoInterface{
		"z-pvc": &base.RuntimeInfo{},
		"a-pvc": &base.RuntimeInfo{},
		"m-pvc": &base.RuntimeInfo{},
	}

	const want = "/data/a-vol/**;/data/m-vol/**;/data/z-vol/**"
	for i := 0; i < 200; i++ {
		cfg, err := p.buildFilePrefetcherConfig(pod, infos)
		if err != nil {
			t.Fatalf("iteration %d: buildFilePrefetcherConfig failed: %v", i, err)
		}
		if cfg.GlobPaths != want {
			t.Fatalf("iteration %d: GlobPaths = %q, want %q", i, cfg.GlobPaths, want)
		}
	}
}
