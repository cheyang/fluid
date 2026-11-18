/*
Verification harness for PR #6262 review (Reviewer B / Codex).
Additive test file only — no production code changes.

Polarity: INVARIANCE probe for finding F2. The PR sorts runtimeInfos keys when
building datasetNames, but datasetNames is consumed only through
utils.ContainsString membership checks (and one V(1) log line), so the order
never reaches the mutated pod. This test must PASS on BOTH base and head,
documenting that the hunk (and the PR's new "deterministic order" test, which
asserts nothing about order) has no observable behavioral effect.
*/
package mountpropagationinjector

import (
	"encoding/json"
	"testing"

	"github.com/fluid-cloudnative/fluid/pkg/ddc/base"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestVerifyMountPropagationMutateOrderIndependent(t *testing.T) {
	mkInfos := func() map[string]base.RuntimeInfoInterface {
		ri, err := base.BuildRuntimeInfo("test", "fluid", "alluxio")
		if err != nil {
			t.Fatal(err)
		}
		ri2, err := base.BuildRuntimeInfo("test2", "fluid", "juicefs")
		if err != nil {
			t.Fatal(err)
		}
		ri3, err := base.BuildRuntimeInfo("test3", "fluid", "jindo")
		if err != nil {
			t.Fatal(err)
		}
		return map[string]base.RuntimeInfoInterface{
			"test3": ri3,
			"test":  ri,
			"test2": ri2,
		}
	}
	newPod := func() *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "p"},
			Spec: corev1.PodSpec{
				Volumes: []corev1.Volume{
					{Name: "vol-test", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "test"}}},
					{Name: "vol-test2", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "test2"}}},
					{Name: "vol-test3", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "test3"}}},
				},
				Containers: []corev1.Container{
					{Name: "app", VolumeMounts: []corev1.VolumeMount{
						{Name: "vol-test", MountPath: "/mnt/test"},
						{Name: "vol-test2", MountPath: "/mnt/test2"},
						{Name: "vol-test3", MountPath: "/mnt/test3"},
					}},
				},
			},
		}
	}

	plugin := &MountPropagationInjector{}
	var firstSpec string
	for i := 0; i < 100; i++ {
		pod := newPod()
		shouldStop, err := plugin.Mutate(pod, mkInfos())
		if err != nil || shouldStop {
			t.Fatalf("iteration %d: Mutate failed: err=%v shouldStop=%v", i, err, shouldStop)
		}
		specJSON, err := json.Marshal(pod.Spec)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			firstSpec = string(specJSON)
			// all three mounts must have HostToContainer propagation set
			for _, vm := range pod.Spec.Containers[0].VolumeMounts {
				if vm.MountPropagation == nil || *vm.MountPropagation != corev1.MountPropagationHostToContainer {
					t.Fatalf("mount %s missing HostToContainer propagation", vm.Name)
				}
			}
			continue
		}
		if string(specJSON) != firstSpec {
			t.Fatalf("iteration %d: mutated pod spec varied across runs", i)
		}
	}
}
