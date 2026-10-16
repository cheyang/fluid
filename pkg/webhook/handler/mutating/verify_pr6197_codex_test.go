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

// Verification harness for the review of PR #6197 (runtime config injection for
// client-less cache runtimes), written by reviewer B (Codex). Additive test file
// only; it does not change any production code. Each It() names the finding id it
// proves or disproves and its polarity (contract vs canary).
package mutating

import (
	"context"
	"encoding/json"
	"os"
	"strings"

	"github.com/agiledragon/gomonkey/v2"
	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	"github.com/fluid-cloudnative/fluid/pkg/webhook/plugins"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// verifyProfileWithClientless mirrors the pluginsProfile the PR adds to values.yaml.
var verifyProfileWithClientless = `
plugins:
  serverful:
    withDataset:
    - RequireNodeWithFuse
    - NodeAffinityWithCache
    - MountPropagationInjector
    withoutDataset:
    - PreferNodesWithoutCache
  serverless:
    withDataset:
    - FuseSidecar
    withoutDataset: []
  clientless:
    withDataset:
    - RuntimeConfigInjector
    withoutDataset: []
`

// verifyLegacyProfile mirrors the pluginsProfile of the release this PR builds on
// (taken verbatim from the base values.yaml): there is no clientless section. This
// is what an upgraded cluster keeps in its webhook-plugins ConfigMap, because the
// chart preserves an existing ConfigMap unless forceReplacePluginsProfile is set.
var verifyLegacyProfile = `
plugins:
  serverful:
    withDataset:
    - FilePrefetcher
    - RequireNodeWithFuse
    - NodeAffinityWithCache
    - MountPropagationInjector
    - DatasetUsageInjector
    withoutDataset:
    - PreferNodesWithoutCache
  serverless:
    withDataset:
    - FilePrefetcher
    - FuseSidecar
    - DatasetUsageInjector
    withoutDataset: []
`

// currentProfile is what the mocked os.ReadFile returns for the plugins profile;
// each Context sets it before registering the handlers.
var currentProfile string

func verifyScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	Expect(corev1.AddToScheme(s)).To(Succeed())
	Expect(datav1alpha1.AddToScheme(s)).To(Succeed())
	return s
}

func boundCacheDataset(name, namespace string) (*datav1alpha1.Dataset, *datav1alpha1.CacheRuntime) {
	dataset := &datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Status: datav1alpha1.DatasetStatus{
			Phase: datav1alpha1.BoundDatasetPhase,
			Runtimes: []datav1alpha1.Runtime{
				{Name: name, Namespace: namespace, Type: common.CacheRuntime},
			},
		},
	}
	cacheRuntime := &datav1alpha1.CacheRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
	}
	return dataset, cacheRuntime
}

func runtimeConfigConfigMap(name, namespace string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      common.GetCacheRuntimeConfigConfigMapName(name),
			Namespace: namespace,
		},
		Data: map[string]string{
			common.RuntimeConfigJSONFileName:  `{"master":{"name":"demo-master"}}`,
			common.RuntimeConfigShellFileName: "#!/bin/sh\nexport MASTER_NAME='demo-master'\n",
		},
	}
}

// clientlessAdmissionRequest builds the CREATE admission request for an app pod that
// opts into runtime config injection for the given datasets annotation value.
func clientlessAdmissionRequest(namespace, datasetsAnnotation string) admission.Request {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app",
			Namespace: namespace,
			Labels:    map[string]string{common.LabelAnnotationInject: common.True},
			Annotations: map[string]string{
				common.LabelAnnotationDatasets: datasetsAnnotation,
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app", Image: "app:v1"}},
		},
	}
	raw, err := jsonMarshal(pod)
	Expect(err).NotTo(HaveOccurred())
	return admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Create,
			Namespace: namespace,
			Object:    runtime.RawExtension{Raw: raw},
		},
	}
}

func jsonMarshal(pod *corev1.Pod) ([]byte, error) {
	return json.Marshal(pod)
}

// patchTouchesVolumes reports whether the admission response patches in any volume,
// i.e. whether the RuntimeConfigInjector actually mutated the pod.
func patchTouchesVolumes(resp admission.Response) bool {
	for _, p := range resp.Patches {
		if strings.Contains(p.Path, "volumes") {
			return true
		}
	}
	return false
}

var _ = Describe("PR6197 verification: clientless runtime config injection", func() {
	var (
		s       *runtime.Scheme
		patch   *gomonkey.Patches
		decoder *admission.Decoder
	)

	BeforeEach(func() {
		s = verifyScheme()
		decoder = admission.NewDecoder(scheme.Scheme)
		currentProfile = verifyProfileWithClientless
		mockReadFile := func(content string) ([]byte, error) {
			return []byte(currentProfile), nil
		}
		patch = gomonkey.ApplyFunc(os.ReadFile, mockReadFile)
	})

	AfterEach(func() {
		patch.Reset()
	})

	Context("positive control: fresh install profile, everything in place", func() {
		It("[contract] injects the runtime config volume, mount and env into the app pod", func() {
			dataset, cacheRuntime := boundCacheDataset("demo", "big-data")
			cm := runtimeConfigConfigMap("demo", "big-data")
			fakeClient := fake.NewFakeClientWithScheme(s, dataset, cacheRuntime, cm)
			Expect(plugins.RegisterMutatingHandlers(fakeClient)).To(Succeed())

			handler := &FluidMutatingHandler{}
			handler.Setup(fakeClient, fakeClient, decoder)

			resp := handler.Handle(context.TODO(), clientlessAdmissionRequest("big-data", "demo"))
			Expect(resp.Allowed).To(BeTrue())
			Expect(patchTouchesVolumes(resp)).To(BeTrue(),
				"expected the response to patch a runtime config volume into the pod")
		})
	})

	Context("F1 (characterization): handler behavior when the loaded plugins profile has no clientless section", func() {
		It("[characterization] the labeled pod is admitted silently unmutated although everything else is in place", func() {
			currentProfile = verifyLegacyProfile

			dataset, cacheRuntime := boundCacheDataset("demo", "big-data")
			cm := runtimeConfigConfigMap("demo", "big-data")
			fakeClient := fake.NewFakeClientWithScheme(s, dataset, cacheRuntime, cm)
			Expect(plugins.RegisterMutatingHandlers(fakeClient)).To(Succeed())

			handler := &FluidMutatingHandler{}
			handler.Setup(fakeClient, fakeClient, decoder)

			resp := handler.Handle(context.TODO(), clientlessAdmissionRequest("big-data", "demo"))
			Expect(resp.Allowed).To(BeTrue())
			// The webhook matched the pod, ran zero plugins, and admitted it unchanged.
			// This is the handler-side consequence that F1 described; the fix lives in the
			// chart (plugins-profile.yaml merges the clientless group into a preserved
			// profile on upgrade), verified by the chartmerge package.
			Expect(patchTouchesVolumes(resp)).To(BeFalse(),
				"legacy profile has no clientless plugins, so nothing can be injected")
		})
	})

	Context("F3: the runtime config is not available at admission time", func() {
		It("[contract] a missing runtime config ConfigMap rejects the pod (no more fail-open)", func() {
			dataset, cacheRuntime := boundCacheDataset("demo", "big-data")
			// note: no ConfigMap
			fakeClient := fake.NewFakeClientWithScheme(s, dataset, cacheRuntime)
			Expect(plugins.RegisterMutatingHandlers(fakeClient)).To(Succeed())

			handler := &FluidMutatingHandler{}
			handler.Setup(fakeClient, fakeClient, decoder)

			resp := handler.Handle(context.TODO(), clientlessAdmissionRequest("big-data", "demo"))
			Expect(resp.Allowed).To(BeFalse(),
				"missing ConfigMap must reject the pod after the fix")
			Expect(resp.Result.Code).To(Equal(int32(403)))
			Expect(resp.Result.Message).To(ContainSubstring(common.GetCacheRuntimeConfigConfigMapName("demo")))
		})

		It("[contract] a ConfigMap without runtime.sh rejects the pod too (old cacheruntime-controller)", func() {
			dataset, cacheRuntime := boundCacheDataset("demo", "big-data")
			cm := runtimeConfigConfigMap("demo", "big-data")
			delete(cm.Data, common.RuntimeConfigShellFileName)
			fakeClient := fake.NewFakeClientWithScheme(s, dataset, cacheRuntime, cm)
			Expect(plugins.RegisterMutatingHandlers(fakeClient)).To(Succeed())

			handler := &FluidMutatingHandler{}
			handler.Setup(fakeClient, fakeClient, decoder)

			resp := handler.Handle(context.TODO(), clientlessAdmissionRequest("big-data", "demo"))
			Expect(resp.Allowed).To(BeFalse())
			Expect(resp.Result.Message).To(ContainSubstring(common.RuntimeConfigShellFileName))
		})

		It("[contract] a missing dataset still rejects the pod", func() {
			// no dataset object at all
			fakeClient := fake.NewFakeClientWithScheme(s)
			Expect(plugins.RegisterMutatingHandlers(fakeClient)).To(Succeed())

			handler := &FluidMutatingHandler{}
			handler.Setup(fakeClient, fakeClient, decoder)

			resp := handler.Handle(context.TODO(), clientlessAdmissionRequest("big-data", "demo"))
			Expect(resp.Allowed).To(BeFalse(),
				"CollectRuntimeInfosFromAnnotations error rejects the pod")
			Expect(resp.Result.Message).To(ContainSubstring("demo"))
		})
	})

	Context("F2: dataset names of 43-63 characters are accepted by the annotation but cannot be injected", func() {
		It("[contract] a 50-char dataset is now rejected at admission instead of silently skipped", func() {
			longName := strings.Repeat("a", 50)
			dataset, cacheRuntime := boundCacheDataset(longName, "big-data")
			cm := runtimeConfigConfigMap(longName, "big-data")
			fakeClient := fake.NewFakeClientWithScheme(s, dataset, cacheRuntime, cm)
			Expect(plugins.RegisterMutatingHandlers(fakeClient)).To(Succeed())

			handler := &FluidMutatingHandler{}
			handler.Setup(fakeClient, fakeClient, decoder)
			resp := handler.Handle(context.TODO(), clientlessAdmissionRequest("big-data", longName))
			Expect(resp.Allowed).To(BeFalse(),
				"the collector must reject the over-long name instead of letting the plugin fail")
			Expect(resp.Result.Code).To(Equal(int32(403)))
		})

		It("[characterization] the rejection message names the dataset but not the name-length reason (F5)", func() {
			longName := strings.Repeat("a", 50)
			dataset, cacheRuntime := boundCacheDataset(longName, "big-data")
			cm := runtimeConfigConfigMap(longName, "big-data")
			fakeClient := fake.NewFakeClientWithScheme(s, dataset, cacheRuntime, cm)
			Expect(plugins.RegisterMutatingHandlers(fakeClient)).To(Succeed())

			handler := &FluidMutatingHandler{}
			handler.Setup(fakeClient, fakeClient, decoder)
			resp := handler.Handle(context.TODO(), clientlessAdmissionRequest("big-data", longName))
			Expect(resp.Allowed).To(BeFalse())
			Expect(resp.Result.Message).To(ContainSubstring(longName))
			// F5 (minor): the "too long" reason only shows up in the webhook log; the API-facing
			// message is the generic "failed to get the following datasets", indistinguishable
			// from a not-found dataset, although the docs say the reason is in the error.
			Expect(resp.Result.Message).NotTo(ContainSubstring("too long"))
		})
	})

	Context("PVC-sourced datasets of a clientless-labeled pod (delta: mergeClientlessRuntimeInfos)", func() {
		It("[contract] a cache-runtime dataset resolved from a mounted PVC is injected under its dataset name", func() {
			// Pod mounts PVC "pvcsrc-demo" which is managed by dataset "pvcdemo"; no annotation.
			dataset, cacheRuntime := boundCacheDataset("pvcdemo", "big-data")
			cm := runtimeConfigConfigMap("pvcdemo", "big-data")
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pvcsrc-demo",
					Namespace: "big-data",
					Labels:    map[string]string{common.LabelAnnotationManagedBy: "pvcdemo"},
				},
			}
			fakeClient := fake.NewFakeClientWithScheme(s, dataset, cacheRuntime, cm, pvc)
			Expect(plugins.RegisterMutatingHandlers(fakeClient)).To(Succeed())

			handler := &FluidMutatingHandler{}
			handler.Setup(fakeClient, fakeClient, decoder)

			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "app-pvc",
					Namespace: "big-data",
					Labels:    map[string]string{common.LabelAnnotationInject: common.True},
				},
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{{
						Name: "data",
						VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "pvcsrc-demo"},
						},
					}},
					Containers: []corev1.Container{{Name: "app", Image: "app:v1"}},
				},
			}
			raw, err := jsonMarshal(pod)
			Expect(err).NotTo(HaveOccurred())
			resp := handler.Handle(context.TODO(), admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Operation: admissionv1.Create,
					Namespace: "big-data",
					Object:    runtime.RawExtension{Raw: raw},
				},
			})
			Expect(resp.Allowed).To(BeTrue())
			Expect(patchTouchesVolumes(resp)).To(BeTrue(),
				"the dataset behind the PVC must get its runtime config injected under the dataset name")
		})

		It("[contract] a mounted PVC of a fuse runtime is left alone (admitted without injection)", func() {
			// Pod mounts a PVC of an Alluxio dataset: no cache runtime config exists for it,
			// so the clientless plugins must see no runtime info rather than erroring on the
			// PVC name.
			alluxioDataset := &datav1alpha1.Dataset{
				ObjectMeta: metav1.ObjectMeta{Name: "pvcalx", Namespace: "big-data"},
				Status: datav1alpha1.DatasetStatus{
					Phase: datav1alpha1.BoundDatasetPhase,
					Runtimes: []datav1alpha1.Runtime{
						{Name: "pvcalx", Namespace: "big-data", Type: common.AlluxioRuntime},
					},
				},
			}
			alluxioRuntime := &datav1alpha1.AlluxioRuntime{
				ObjectMeta: metav1.ObjectMeta{Name: "pvcalx", Namespace: "big-data"},
			}
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pvcsrc-alx",
					Namespace: "big-data",
					Labels:    map[string]string{common.LabelAnnotationManagedBy: "pvcalx"},
				},
			}
			fakeClient := fake.NewFakeClientWithScheme(s, alluxioDataset, alluxioRuntime, pvc)
			Expect(plugins.RegisterMutatingHandlers(fakeClient)).To(Succeed())

			handler := &FluidMutatingHandler{}
			handler.Setup(fakeClient, fakeClient, decoder)

			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "app-pvc-alx",
					Namespace: "big-data",
					Labels:    map[string]string{common.LabelAnnotationInject: common.True},
				},
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{{
						Name: "data",
						VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "pvcsrc-alx"},
						},
					}},
					Containers: []corev1.Container{{Name: "app", Image: "app:v1"}},
				},
			}
			raw, err := jsonMarshal(pod)
			Expect(err).NotTo(HaveOccurred())
			resp := handler.Handle(context.TODO(), admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Operation: admissionv1.Create,
					Namespace: "big-data",
					Object:    runtime.RawExtension{Raw: raw},
				},
			})
			Expect(resp.Allowed).To(BeTrue())
			Expect(patchTouchesVolumes(resp)).To(BeFalse(),
				"a fuse-runtime PVC has no runtime config to inject; the pod must pass through untouched")
		})
	})
})
