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

// Reviewer verification harness for PR #6281 (issue #6280).
//
// POLARITY: every test below is a CONTRACT test — it asserts the intended
// behavior documented in docs/en/operation/tiered_locality_schedule.md §3.3:
// "If the application Pod specifies the affinity about tiered locality
// information (defined in 'spec.affinity' or 'spec.nodeselector'), webhook
// will not inject the relevant location affinity, and the user's
// configuration will be kept."
//
// On the BASE branch (253f6bb, without the fix) the two nodeSelector-fallback
// cases and the Mutate-level repro FAIL (this is the reproduction of issue
// #6280). On the PR head (e6d2213) they PASS. The remaining cases are
// controls: they pin behavior that was already correct on base so the fix
// cannot silently regress it (injection must still happen for pods without
// a locality nodeSelector).

import (
	"testing"

	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fluid-cloudnative/fluid/pkg/ddc/base"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestClaudeVerifyHasRepeatedLocalityNodeSelectorFallback is the unit-level
// claim of issue #6280: hasRepeatedLocality must fall through to the
// spec.nodeSelector check when the pod's nodeAffinity has no required terms.
func TestClaudeVerifyHasRepeatedLocalityNodeSelectorFallback(t *testing.T) {
	tieredLocality := &TieredLocality{
		Preferred: []Preferred{
			{Name: "fluid.io/node", Weight: 100},
			{Name: "topology.kubernetes.io/rack", Weight: 50},
		},
		Required: []string{"topology.kubernetes.io/rack"},
	}

	rackNS := map[string]string{"topology.kubernetes.io/rack": "rack-b"}
	disktypePreferred := []corev1.PreferredSchedulingTerm{
		{
			Weight: 100,
			Preference: corev1.NodeSelectorTerm{
				MatchExpressions: []corev1.NodeSelectorRequirement{
					{Key: "disktype", Operator: corev1.NodeSelectorOpIn, Values: []string{"ssd"}},
				},
			},
		},
	}

	tests := []struct {
		name string
		pod  *corev1.Pod
		want bool
		note string
	}{
		{
			// THE BUG (issue #6280): preferred-only affinity on an unrelated
			// label short-circuited the nodeSelector check before the fix.
			name: "preferred-only non-locality affinity + locality nodeSelector",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					NodeSelector: rackNS,
					Affinity: &corev1.Affinity{
						NodeAffinity: &corev1.NodeAffinity{
							PreferredDuringSchedulingIgnoredDuringExecution: disktypePreferred,
						},
					},
				},
			},
			want: true,
			note: "fails on base (returns false, injection happens)",
		},
		{
			// THE BUG (issue #6280): empty node affinity did the same.
			name: "empty node affinity + locality nodeSelector",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					NodeSelector: rackNS,
					Affinity: &corev1.Affinity{
						NodeAffinity: &corev1.NodeAffinity{},
					},
				},
			},
			want: true,
			note: "fails on base (returns false, injection happens)",
		},
		{
			// CONTROL: required affinity on an unrelated label already fell
			// through to nodeSelector on base; must stay true.
			name: "non-locality required affinity + locality nodeSelector",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					NodeSelector: rackNS,
					Affinity: &corev1.Affinity{
						NodeAffinity: &corev1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
								NodeSelectorTerms: []corev1.NodeSelectorTerm{
									{
										MatchExpressions: []corev1.NodeSelectorRequirement{
											{Key: "disktype", Operator: corev1.NodeSelectorOpIn, Values: []string{"ssd"}},
										},
									},
								},
							},
						},
					},
				},
			},
			want: true,
			note: "control: already true on base",
		},
		{
			// CONTROL: a pod without a locality nodeSelector must NOT be
			// treated as repeated (injection must keep happening for it).
			name: "preferred-only non-locality affinity + non-locality nodeSelector",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					NodeSelector: map[string]string{"disktype": "ssd"},
					Affinity: &corev1.Affinity{
						NodeAffinity: &corev1.NodeAffinity{
							PreferredDuringSchedulingIgnoredDuringExecution: disktypePreferred,
						},
					},
				},
			},
			want: false,
			note: "control: must stay false so injection still happens",
		},
		{
			// CONTROL: pod affinity present but no node affinity at all —
			// nodeSelector was already consulted on base.
			name: "nil node affinity + locality nodeSelector",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					NodeSelector: rackNS,
					Affinity: &corev1.Affinity{
						PodAntiAffinity: &corev1.PodAntiAffinity{},
					},
				},
			},
			want: true,
			note: "control: already true on base",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tieredLocality.hasRepeatedLocality(tc.pod)
			assert.Equal(t, tc.want, got, "case %q (%s): hasRepeatedLocality = %v, want %v", tc.name, tc.note, got, tc.want)
		})
	}
}

// TestClaudeVerifyMutateSkipWhenNodeSelectorHasLocality reproduces the exact
// user-visible symptom from issue #6280 at Mutate level: a pod labeled
// `sched: required` pinning its own rack via nodeSelector (rack-b) with only
// a preferred node affinity on an unrelated label must NOT get the runtime's
// rack affinity (rack-a) injected on top — the old behavior produced two
// contradictory requirements (nodeSelector rack-b AND required rack-a) and
// left the pod Pending.
func TestClaudeVerifyMutateSkipWhenNodeSelectorHasLocality(t *testing.T) {
	alluxioRuntime := newCacheAlluxioRuntime()
	cl := fake.NewFakeClientWithScheme(testScheme, alluxioRuntime)

	runtimeInfo, err := base.BuildRuntimeInfo(alluxioRuntime.Name, alluxioRuntime.Namespace, "alluxio")
	require.NoError(t, err)
	runtimeInfo.SetFuseNodeSelector(map[string]string{})

	plugin, err := NewPlugin(cl, `
preferred:
- name: fluid.io/node
  weight: 100
- name: topology.kubernetes.io/rack
  weight: 50
required:
- topology.kubernetes.io/rack
`)
	require.NoError(t, err)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
			Labels: map[string]string{
				"fluid.io/dataset." + alluxioRuntime.Name + ".sched": "required",
			},
		},
		Spec: corev1.PodSpec{
			NodeSelector: map[string]string{
				"topology.kubernetes.io/rack": "rack-b",
			},
			Affinity: &corev1.Affinity{
				NodeAffinity: &corev1.NodeAffinity{
					PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{
						{
							Weight: 100,
							Preference: corev1.NodeSelectorTerm{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{Key: "disktype", Operator: corev1.NodeSelectorOpIn, Values: []string{"ssd"}},
								},
							},
						},
					},
				},
			},
		},
	}

	_, err = plugin.Mutate(pod, map[string]base.RuntimeInfoInterface{
		alluxioRuntime.Name: runtimeInfo,
	})
	require.NoError(t, err)

	// No required locality affinity may be injected (the bug injected
	// rack In [rack-a], contradicting the nodeSelector rack-b).
	assert.Nil(t, pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution,
		"locality affinity must not be injected when the pod pins the locality label via nodeSelector")
	// The user's own preferred term is kept untouched.
	assert.Len(t, pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution, 1,
		"the pod's own preferred terms must be kept")
	// The user's nodeSelector is kept untouched (user's configuration kept).
	assert.Equal(t, map[string]string{"topology.kubernetes.io/rack": "rack-b"}, pod.Spec.NodeSelector)
}

// TestClaudeVerifyMutateStillInjectsWithoutLocalityNodeSelector is the
// counterweight control: the fix must not stop injection for pods that do
// NOT pin a locality label. Same pod shape as above minus the rack
// nodeSelector — the required rack affinity MUST be injected here.
func TestClaudeVerifyMutateStillInjectsWithoutLocalityNodeSelector(t *testing.T) {
	alluxioRuntime := newCacheAlluxioRuntime()
	cl := fake.NewFakeClientWithScheme(testScheme, alluxioRuntime)

	runtimeInfo, err := base.BuildRuntimeInfo(alluxioRuntime.Name, alluxioRuntime.Namespace, "alluxio")
	require.NoError(t, err)
	runtimeInfo.SetFuseNodeSelector(map[string]string{})

	plugin, err := NewPlugin(cl, `
preferred:
- name: fluid.io/node
  weight: 100
required:
- topology.kubernetes.io/rack
`)
	require.NoError(t, err)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
			Labels: map[string]string{
				"fluid.io/dataset." + alluxioRuntime.Name + ".sched": "required",
			},
		},
		Spec: corev1.PodSpec{
			Affinity: &corev1.Affinity{
				NodeAffinity: &corev1.NodeAffinity{
					PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{
						{
							Weight: 100,
							Preference: corev1.NodeSelectorTerm{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{Key: "disktype", Operator: corev1.NodeSelectorOpIn, Values: []string{"ssd"}},
								},
							},
						},
					},
				},
			},
		},
	}

	_, err = plugin.Mutate(pod, map[string]base.RuntimeInfoInterface{
		alluxioRuntime.Name: runtimeInfo,
	})
	require.NoError(t, err)

	require.NotNil(t, pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution,
		"required locality affinity must still be injected for pods without a locality nodeSelector")
	assert.Equal(t, []corev1.NodeSelectorTerm{
		{
			MatchExpressions: []corev1.NodeSelectorRequirement{
				{Key: "topology.kubernetes.io/rack", Operator: corev1.NodeSelectorOpIn, Values: []string{"rack-a"}},
			},
		},
	}, pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms)
}
