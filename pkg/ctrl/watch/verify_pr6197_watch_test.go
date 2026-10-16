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

// Verification harness for the review of PR #6197 (reviewer B / Codex). Additive test
// file only; it does not change any production code.
//
// F4: the RuntimeConfigInjector marks injected pods with done.sidecar.fluid.io/inject, a
// label the fluid-app controller also consumes (pkg/ctrl/watch.ShouldInQueue). These
// contracts prove a client-less config-injected pod is NOT enqueued for fuse sidecar
// lifecycle handling, because it carries no privileged fuse sidecar labels/container.
package watch

import (
	"testing"

	"github.com/fluid-cloudnative/fluid/pkg/common"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestVerifyPR6197ClientlessPodNotWatched(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app",
			Namespace: "verify",
			Labels: map[string]string{
				common.LabelAnnotationInject: common.True,
				common.InjectSidecarDone:     common.True,
			},
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyOnFailure,
			Containers:    []corev1.Container{{Name: "app", Image: "app"}},
		},
	}
	if ShouldInQueue(pod) {
		t.Fatalf("a client-less config-injected pod must not be enqueued for fuse sidecar lifecycle handling")
	}
}

func TestVerifyPR6197FuseSidecarPodStillWatched(t *testing.T) {
	// sanity control: a privileged fuse sidecar pod is still enqueued
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app",
			Namespace: "verify",
			Labels: map[string]string{
				common.InjectSidecarDone: common.True,
				// default serverless platform = privileged fuse sidecar pod
				common.InjectFuseSidecar: common.True,
			},
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyOnFailure,
			Containers: []corev1.Container{
				{Name: "app", Image: "app"},
				{Name: common.FuseContainerName, Image: "fuse"},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{Name: "app", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}}},
				{Name: common.FuseContainerName, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
			},
		},
	}
	if !ShouldInQueue(pod) {
		t.Fatalf("control broken: a privileged fuse sidecar pod must still be enqueued")
	}
}
