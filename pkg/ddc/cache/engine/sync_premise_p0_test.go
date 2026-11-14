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

package engine

// P0 — premise reproduction for PR #6243 (runs against the BASE branch, where
// the validating webhook does not exist).
//
// The PR claims (quoted from the PR body):
//   "The CacheRuntime controller syncs only the `replicas`, `runtimeVersion` and
//    `resources` of the master and worker to their AdvancedStatefulSets ... Every
//    other field is read once, when the component is created, so after a change:
//    - most fields: the API server accepts the update and it is silently ignored
//    - `tieredStore`: the update is applied halfway. The quota in the runtime
//      ConfigMap follows the spec, while the tmpfs `sizeLimit` of the Worker Pod
//      keeps its creation-time value"
//
// This spec documents exactly that symptom through the real engine Sync path:
// a spec.worker change to tieredStore/nodeSelector/args is persisted, the runtime
// value ConfigMap picks up the new quota, but the worker AdvancedStatefulSet's
// pod template (nodeSelector, args, tmpfs sizeLimit) keeps its creation-time values.
// It is a SYMPTOM-DOCUMENTATION canary: it passes while the problem exists. The
// admission webhook in PR #6243 prevents such updates at admission time; it does
// not change engine.Sync itself, so this test is expected to keep passing after
// the PR lands.

import (
	"context"
	"strings"

	workloadv1alpha1 "github.com/fluid-cloudnative/advanced-statefulset/api/workload/v1alpha1"
	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	cruntime "github.com/fluid-cloudnative/fluid/pkg/runtime"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("P0 premise: spec updates beyond the synced fields are silently ignored", Label("pkg.ddc.cache.engine.sync_premise_p0_test.go"), func() {

	// The whole scenario lives in one It so the creation-time state, the spec
	// mutation and all three assertions share one fixture.
	It("applies a tieredStore update halfway: ConfigMap follows the spec, the worker workload does not", func() {
		scheme := CacheEngineTestScheme

		dataset := &datav1alpha1.Dataset{
			ObjectMeta: metav1.ObjectMeta{Name: "test-runtime", Namespace: "default", UID: "test-dataset-uid"},
			Spec:       datav1alpha1.DatasetSpec{},
		}

		runtimeObj := &datav1alpha1.CacheRuntime{
			TypeMeta:   metav1.TypeMeta{APIVersion: "data.fluid.io/v1alpha1", Kind: "CacheRuntime"},
			ObjectMeta: metav1.ObjectMeta{Name: "test-runtime", Namespace: "default", UID: "test-runtime-uid"},
			Spec: datav1alpha1.CacheRuntimeSpec{
				RuntimeClassName: "test-class",
				Master:           datav1alpha1.CacheRuntimeMasterSpec{Replicas: 1},
				Worker: datav1alpha1.CacheRuntimeWorkerSpec{
					Replicas: 2,
					// creation-time tieredStore: 1Gi tmpfs
					TieredStore: datav1alpha1.RuntimeTieredStore{
						Levels: []datav1alpha1.RuntimeTieredStoreLevel{{
							EmptyDir: &datav1alpha1.EmptyDirMediumSource{Quota: resource.MustParse("1Gi")},
						}},
					},
				},
				Client: datav1alpha1.CacheRuntimeClientSpec{},
			},
		}
		runtimeObj.Status.Master.Phase = datav1alpha1.RuntimePhaseNone
		runtimeObj.Status.Worker.Phase = datav1alpha1.RuntimePhaseNone
		runtimeObj.Status.Client.Phase = datav1alpha1.RuntimePhaseNone

		runtimeClass := &datav1alpha1.CacheRuntimeClass{
			ObjectMeta:     metav1.ObjectMeta{Name: "test-class"},
			FileSystemType: "test-fs",
			Topology: &datav1alpha1.RuntimeTopology{
				Master: &datav1alpha1.RuntimeComponentDefinition{
					Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "master", Image: "test-master:latest"}}}},
				},
				Worker: &datav1alpha1.RuntimeComponentDefinition{
					Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "worker", Image: "test-worker:latest"}}}},
				},
				Client: &datav1alpha1.RuntimeComponentDefinition{
					Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "client", Image: "test-client:latest"}}}},
				},
			},
		}

		masterReplicas := int32(1)
		masterSts := &workloadv1alpha1.AdvancedStatefulSet{
			ObjectMeta: metav1.ObjectMeta{Name: "test-runtime-master", Namespace: "default"},
			Spec: workloadv1alpha1.AdvancedStatefulSetSpec{
				Replicas: &masterReplicas,
				Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "master", Image: "test-master:latest"}}}},
			},
			Status: workloadv1alpha1.AdvancedStatefulSetStatus{ReadyReplicas: 1, CurrentReplicas: 1, AvailableReplicas: 1},
		}

		// The worker ASTS as creation rendered it: creation-time nodeSelector,
		// args, and the tmpfs volume from the 1Gi tieredStore.
		workerReplicas := int32(2)
		workerSts := &workloadv1alpha1.AdvancedStatefulSet{
			ObjectMeta: metav1.ObjectMeta{Name: "test-runtime-worker", Namespace: "default"},
			Spec: workloadv1alpha1.AdvancedStatefulSetSpec{
				Replicas: &workerReplicas,
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{},
					Spec: corev1.PodSpec{
						NodeSelector: map[string]string{"cache": "true"},
						Containers: []corev1.Container{{
							Name:  "worker",
							Image: "test-worker:latest",
							Args:  []string{"--creation-time"},
							VolumeMounts: []corev1.VolumeMount{{
								Name:      "fluid-emptydir-tieredstore-level0",
								MountPath: GetEmptyDirTieredStoreMountPath(0),
							}},
						}},
						Volumes: []corev1.Volume{{
							Name: "fluid-emptydir-tieredstore-level0",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{
									Medium:    corev1.StorageMediumMemory,
									SizeLimit: &[]resource.Quantity{resource.MustParse("1Gi")}[0],
								},
							},
						}},
					},
				},
			},
			Status: workloadv1alpha1.AdvancedStatefulSetStatus{ReadyReplicas: 2, CurrentReplicas: 2, AvailableReplicas: 2},
		}

		clientDs := &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Name: "test-runtime-client", Namespace: "default"},
			Spec: appsv1.DaemonSetSpec{
				Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "client", Image: "test-client:latest"}}}},
			},
			Status: appsv1.DaemonSetStatus{NumberReady: 0, DesiredNumberScheduled: 0},
		}

		client := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(dataset, runtimeObj, runtimeClass, masterSts, workerSts, clientDs).
			WithStatusSubresource(dataset, runtimeObj).
			Build()

		engine := &CacheEngine{
			name:      "test-runtime",
			namespace: "default",
			Client:    client,
			Log:       ctrl.Log.WithName("p0-test"),
		}

		ctx := cruntime.ReconcileRequestContext{
			Client:         client,
			Context:        context.Background(),
			Log:            ctrl.Log.WithName("p0-test"),
			RuntimeType:    "cache",
			NamespacedName: types.NamespacedName{Name: "test-runtime", Namespace: "default"},
		}

		// --- the user updates the spec: tieredStore quota 1Gi -> 4Gi, plus a
		// nodeSelector and args change; the API server accepts it.
		updated := runtimeObj.DeepCopy()
		updated.Spec.Worker.TieredStore.Levels[0].EmptyDir.Quota = resource.MustParse("4Gi")
		updated.Spec.Worker.NodeSelector = map[string]string{"cache": "false"}
		updated.Spec.Worker.Args = []string{"--after-update"}
		Expect(client.Update(ctx.Context, updated)).To(Succeed())

		persisted := &datav1alpha1.CacheRuntime{}
		Expect(client.Get(ctx.Context, types.NamespacedName{Name: "test-runtime", Namespace: "default"}, persisted)).To(Succeed())
		Expect(persisted.Spec.Worker.TieredStore.Levels[0].EmptyDir.Quota.String()).To(Equal("4Gi"), "the API server accepted the update")

		// --- the controller syncs
		Expect(engine.Sync(ctx)).To(Succeed())

		// (a) the runtime value ConfigMap follows the spec: quota 4Gi
		cm := &corev1.ConfigMap{}
		Expect(client.Get(ctx.Context, types.NamespacedName{Name: "fluid-runtime-config-test-runtime", Namespace: "default"}, cm)).To(Succeed())
		Expect(cm.Data).ToNot(BeEmpty())
		var sawNewQuota, sawOldQuota bool
		for _, v := range cm.Data {
			if strings.Contains(v, `"quotas":["4Gi"]`) {
				sawNewQuota = true
			}
			if strings.Contains(v, `"quotas":["1Gi"]`) {
				sawOldQuota = true
			}
		}
		Expect(sawNewQuota).To(BeTrue(), "the runtime ConfigMap quota should follow the new spec (the 'half' that is applied)")
		Expect(sawOldQuota).To(BeFalse(), "the runtime ConfigMap should not keep the old quota")

		// (b) the worker workload keeps its creation-time values: tmpfs sizeLimit,
		// nodeSelector and args are NOT updated by the sync.
		after := &workloadv1alpha1.AdvancedStatefulSet{}
		Expect(client.Get(ctx.Context, types.NamespacedName{Name: "test-runtime-worker", Namespace: "default"}, after)).To(Succeed())
		Expect(after.Spec.Template.Spec.Volumes[0].EmptyDir.SizeLimit.String()).To(Equal("1Gi"),
			"the worker tmpfs sizeLimit keeps its creation-time value (the 'half' that is NOT applied)")
		Expect(after.Spec.Template.Spec.NodeSelector).To(Equal(map[string]string{"cache": "true"}),
			"nodeSelector is read once at creation and silently ignored afterwards")
		Expect(after.Spec.Template.Spec.Containers[0].Args).To(Equal([]string{"--creation-time"}),
			"args are read once at creation and silently ignored afterwards")
	})
})
