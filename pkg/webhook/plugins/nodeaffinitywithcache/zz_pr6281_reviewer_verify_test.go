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

package nodeaffinitywithcache

// Reviewer-owned verification harness for PR #6281 (issue #6280).
// Reviewer: codex (debate pipeline, Reviewer B). Additive only — no production
// code is modified by the verification branch.
//
// Polarity: all tests in this file are CONTRACT tests — they assert the
// documented behavior (docs/en/operation/tiered_locality_schedule.md:
// "If the application Pod specifies the affinity about tiered locality
// information (defined in 'spec.affinity' or 'spec.nodeselector'), webhook
// will not inject the relevant location affinity"). They FAIL on the base
// branch (bug present) and PASS on the fixed head.
//
// Layers:
//   - unit:        TestVerifyPR6281HasRepeatedLocality* (pure function)
//   - integration: TestVerifyPR6281Mutate* (plugin Mutate against a fake
//                  Kubernetes client with a real AlluxioRuntime status
//                  accessor, exercising YAML args parsing, runtime-info
//                  lookup and the injection helpers end to end)

import (
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/ddc/base"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const verifyPR6281RackLabel = "topology.kubernetes.io/rack"

// verifyPR6281CacheRuntime mirrors the issue #6280 reproduction: the runtime's
// cache affinity pins topology.kubernetes.io/rack to rack-a, so if the webhook
// wrongly injects, the pod ends up requiring rack-a (injected) AND rack-b
// (the pod's own nodeSelector) and stays Pending.
func verifyPR6281CacheRuntime() *datav1alpha1.AlluxioRuntime {
	return &datav1alpha1.AlluxioRuntime{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "verify-pr6281",
			Namespace: "fluid-test",
		},
		Status: datav1alpha1.RuntimeStatus{
			CacheAffinity: &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{
						{
							MatchExpressions: []corev1.NodeSelectorRequirement{
								{
									Key:      verifyPR6281RackLabel,
									Operator: corev1.NodeSelectorOpIn,
									Values:   []string{"rack-a"},
								},
							},
						},
					},
				},
			},
		},
	}
}

func verifyPR6281PreferredDisktypeAffinity() *corev1.Affinity {
	return &corev1.Affinity{
		NodeAffinity: &corev1.NodeAffinity{
			PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{
				{
					Weight: 100,
					Preference: corev1.NodeSelectorTerm{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{
								Key:      "disktype",
								Operator: corev1.NodeSelectorOpIn,
								Values:   []string{"ssd"},
							},
						},
					},
				},
			},
		},
	}
}

func TestVerifyPR6281HasRepeatedLocalityNodeSelector(t *testing.T) {
	tieredLocality := &TieredLocality{
		Preferred: []Preferred{{Name: verifyPR6281RackLabel, Weight: 50}},
		Required:  []string{verifyPR6281RackLabel},
	}

	tests := []struct {
		name string
		pod  *corev1.Pod
		want bool
	}{
		{
			// Issue #6280 case 1: preferred-only affinity on an unrelated label
			// must not make the nodeSelector locality check unreachable.
			name: "preferred-only affinity on unrelated label + locality nodeSelector",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Affinity:     verifyPR6281PreferredDisktypeAffinity(),
					NodeSelector: map[string]string{verifyPR6281RackLabel: "rack-b"},
				},
			},
			want: true,
		},
		{
			// Issue #6280 case 2: an explicitly empty NodeAffinity{} must not
			// make the nodeSelector locality check unreachable either.
			name: "empty node affinity + locality nodeSelector",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Affinity:     &corev1.Affinity{NodeAffinity: &corev1.NodeAffinity{}},
					NodeSelector: map[string]string{verifyPR6281RackLabel: "rack-b"},
				},
			},
			want: true,
		},
		{
			// Over-correction guard: a preferred-only affinity on an unrelated
			// label without any locality nodeSelector must still return false
			// so injection continues to happen.
			name: "preferred-only affinity on unrelated label, no locality anywhere",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Affinity: verifyPR6281PreferredDisktypeAffinity(),
				},
			},
			want: false,
		},
		{
			// Regression guard for the pre-existing path: no affinity at all.
			name: "no affinity + locality nodeSelector",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					NodeSelector: map[string]string{verifyPR6281RackLabel: "rack-b"},
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tieredLocality.hasRepeatedLocality(tt.pod))
		})
	}
}

func verifyPR6281BuildPlugin(t *testing.T) (*NodeAffinityWithCache, map[string]base.RuntimeInfoInterface) {
	t.Helper()
	runtimeObj := verifyPR6281CacheRuntime()
	cl := fake.NewFakeClientWithScheme(testScheme, runtimeObj)

	runtimeInfo, err := base.BuildRuntimeInfo(runtimeObj.Name, runtimeObj.Namespace, "alluxio")
	require.NoError(t, err)
	runtimeInfo.SetFuseNodeSelector(map[string]string{})

	handler, err := NewPlugin(cl, `
preferred:
- name: topology.kubernetes.io/rack
  weight: 50
required:
- topology.kubernetes.io/rack
`)
	require.NoError(t, err)
	plugin, ok := handler.(*NodeAffinityWithCache)
	require.True(t, ok, "NewPlugin must return *NodeAffinityWithCache")

	return plugin, map[string]base.RuntimeInfoInterface{runtimeObj.Name: runtimeInfo}
}

func verifyPR6281SchedRequiredPod(affinity *corev1.Affinity, nodeSelector map[string]string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "verify-pr6281-pod",
			Namespace: "fluid-test",
			Labels: map[string]string{
				"fluid.io/dataset.verify-pr6281.sched": "required",
			},
		},
		Spec: corev1.PodSpec{
			NodeSelector: nodeSelector,
			Affinity:     affinity,
		},
	}
}

func TestVerifyPR6281MutateSkipsInjectionWhenPodNodeSelectorHasLocality(t *testing.T) {
	plugin, runtimeInfos := verifyPR6281BuildPlugin(t)

	// Exact issue #6280 reproduction: pod pins rack-b itself via nodeSelector
	// and only carries a preferred affinity on an unrelated label.
	pod := verifyPR6281SchedRequiredPod(
		verifyPR6281PreferredDisktypeAffinity(),
		map[string]string{verifyPR6281RackLabel: "rack-b"},
	)

	_, err := plugin.Mutate(pod, runtimeInfos)
	require.NoError(t, err)

	assert.Nil(t, pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution,
		"webhook must not inject a required locality affinity on top of the pod's own nodeSelector")
	assert.Len(t, pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution, 1,
		"the pod's own preferred affinity must be kept untouched")
	assert.Equal(t, map[string]string{verifyPR6281RackLabel: "rack-b"}, pod.Spec.NodeSelector,
		"the pod's own nodeSelector must be kept untouched")
}

func TestVerifyPR6281MutateStillInjectsWhenPodHasNoLocality(t *testing.T) {
	plugin, runtimeInfos := verifyPR6281BuildPlugin(t)

	// Same pod shape minus the locality nodeSelector: the fix must not turn
	// "no required terms" into "skip everything". Injection must still happen.
	pod := verifyPR6281SchedRequiredPod(
		verifyPR6281PreferredDisktypeAffinity(),
		nil,
	)

	_, err := plugin.Mutate(pod, runtimeInfos)
	require.NoError(t, err)

	require.NotNil(t, pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution,
		"webhook must still inject the required locality affinity when the pod sets no locality itself")
	require.Len(t, pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms, 1)
	term := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0]
	require.Len(t, term.MatchExpressions, 1)
	assert.Equal(t, verifyPR6281RackLabel, term.MatchExpressions[0].Key)
	assert.Equal(t, corev1.NodeSelectorOpIn, term.MatchExpressions[0].Operator)
	assert.Equal(t, []string{"rack-a"}, term.MatchExpressions[0].Values)
	// The pod's dataset label is sched: "required", so the dataset is moved out
	// of the preferred set (getRequiredAndPreferredSchedulingRuntimes) and only
	// required terms are injected; the pod's own preferred term stays alone.
	assert.Len(t, pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution, 1,
		"the pod's own preferred term must be kept, nothing appended for a sched:required dataset")
}
