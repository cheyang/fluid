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
	"fmt"
	"os"
	"path/filepath"
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

		It("[contract] the rejection message names the dataset and the name-length reason (F5, fixed at 268012a5)", func() {
			// F5 canary flipped at 268012a5 (commit c01cbcce carries the per-dataset reason
			// into the aggregate denial); promoted to a contract. Red on 6d3a6d7e.
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
			Expect(resp.Result.Message).To(ContainSubstring("too long"),
				"the denial must state the reason, not only the dataset name")
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
	Context("A-F1: a non-cache dataset listed in fluid.io/datasets (fixed at 268012a5)", func() {
		It("[contract] is rejected with the runtime type named, not 'configmap may not be ready yet'", func() {
			// Before 89d26fe2 an alluxio dataset resolved fine and the pod was then denied
			// with 'configmap ... may not be ready yet' — a state that can never occur for a
			// fuse-served runtime. Red on 6d3a6d7e (message never names the runtime type).
			alluxioDataset := &datav1alpha1.Dataset{
				ObjectMeta: metav1.ObjectMeta{Name: "demo-alx", Namespace: "big-data"},
				Status: datav1alpha1.DatasetStatus{
					Phase: datav1alpha1.BoundDatasetPhase,
					Runtimes: []datav1alpha1.Runtime{
						{Name: "demo-alx", Namespace: "big-data", Type: common.AlluxioRuntime},
					},
				},
			}
			alluxioRuntime := &datav1alpha1.AlluxioRuntime{
				ObjectMeta: metav1.ObjectMeta{Name: "demo-alx", Namespace: "big-data"},
			}
			fakeClient := fake.NewFakeClientWithScheme(s, alluxioDataset, alluxioRuntime)
			Expect(plugins.RegisterMutatingHandlers(fakeClient)).To(Succeed())

			handler := &FluidMutatingHandler{}
			handler.Setup(fakeClient, fakeClient, decoder)

			resp := handler.Handle(context.TODO(), clientlessAdmissionRequest("big-data", "demo-alx"))
			Expect(resp.Allowed).To(BeFalse(), "a non-cache dataset in the annotation must reject the pod")
			Expect(resp.Result.Code).To(Equal(int32(403)))
			Expect(resp.Result.Message).To(ContainSubstring(`dataset "demo-alx" is served by a "alluxio" runtime`))
			Expect(resp.Result.Message).NotTo(ContainSubstring("may not be ready yet"))
		})
	})

	Context("A-F2: a PVC-resolved dataset whose name cannot form a volume name (fixed at 268012a5)", func() {
		It("[contract] is rejected naming the invalid volume name instead of producing an API-server-invalid pod", func() {
			// "demo.v1" is a legal k8s object name (DNS-1123 subdomain) but the derived volume
			// name fails IsDNS1123Label; the annotation path rejects dots up front, the PVC
			// path did not. Red on 6d3a6d7e (pod admitted carrying an invalid volume name).
			dataset, cacheRuntime := boundCacheDataset("demo.v1", "big-data")
			cm := runtimeConfigConfigMap("demo.v1", "big-data")
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pvcsrc-dot",
					Namespace: "big-data",
					Labels:    map[string]string{common.LabelAnnotationManagedBy: "demo.v1"},
				},
			}
			fakeClient := fake.NewFakeClientWithScheme(s, dataset, cacheRuntime, cm, pvc)
			Expect(plugins.RegisterMutatingHandlers(fakeClient)).To(Succeed())

			handler := &FluidMutatingHandler{}
			handler.Setup(fakeClient, fakeClient, decoder)

			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "app-pvc-dot",
					Namespace: "big-data",
					Labels:    map[string]string{common.LabelAnnotationInject: common.True},
				},
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{{
						Name: "data",
						VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "pvcsrc-dot"},
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
			Expect(resp.Allowed).To(BeFalse(),
				"a dataset name that cannot form a valid volume name must reject the pod")
			Expect(resp.Result.Message).To(ContainSubstring("fluid-runtime-config-demo.v1"),
				"the denial must name the invalid volume name")
		})
	})

	Context("B-F7: a pod that already projects the runtime config ConfigMap with its own settings (fixed at 41b47141)", func() {
		// Before 7f5b625c the injection silently replaced a same-named volume of the same
		// ConfigMap, dropping whatever the app projected there (items, mode, optional).
		// The delta rejects such a pod instead, fail-closed with the reason. The
		// API-server-defaulted shape (what a previously injected or GitOps-restored spec
		// carries) must stay admitted. Red on 268012a5 (admitted, volume replaced),
		// green at 41b47141; the defaulted control is green on both.
		volumeNameForDemo := common.GetCacheRuntimeConfigConfigMapName("demo")

		newCustomizedVolumeRequest := func(customize func(*corev1.ConfigMapVolumeSource)) admission.Request {
			vol := corev1.Volume{
				Name: volumeNameForDemo,
				VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: volumeNameForDemo},
				}},
			}
			customize(vol.ConfigMap)
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "app-custom-vol",
					Namespace: "big-data",
					Labels:    map[string]string{common.LabelAnnotationInject: common.True},
					Annotations: map[string]string{
						common.LabelAnnotationDatasets: "demo",
					},
				},
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{vol},
					Containers: []corev1.Container{{
						Name:  "app",
						Image: "app:v1",
						VolumeMounts: []corev1.VolumeMount{{
							Name:      volumeNameForDemo,
							MountPath: filepath.Join(common.RuntimeConfigDir, "demo"),
						}},
					}},
				},
			}
			raw, err := jsonMarshal(pod)
			Expect(err).NotTo(HaveOccurred())
			return admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
				Operation: admissionv1.Create,
				Namespace: "big-data",
				Object:    runtime.RawExtension{Raw: raw},
			}}
		}

		handleWithDemo := func(req admission.Request) admission.Response {
			dataset, cacheRuntime := boundCacheDataset("demo", "big-data")
			cm := runtimeConfigConfigMap("demo", "big-data")
			fakeClient := fake.NewFakeClientWithScheme(s, dataset, cacheRuntime, cm)
			Expect(plugins.RegisterMutatingHandlers(fakeClient)).To(Succeed())
			handler := &FluidMutatingHandler{}
			handler.Setup(fakeClient, fakeClient, decoder)
			return handler.Handle(context.TODO(), req)
		}

		It("[contract] rejects a volume projecting the config ConfigMap with its own items", func() {
			resp := handleWithDemo(newCustomizedVolumeRequest(func(source *corev1.ConfigMapVolumeSource) {
				source.Items = []corev1.KeyToPath{{Key: common.RuntimeConfigShellFileName, Path: "config.sh"}}
			}))
			Expect(resp.Allowed).To(BeFalse(),
				"a customized projection of the runtime config ConfigMap must be rejected, not silently replaced")
			Expect(resp.Result.Code).To(Equal(int32(403)))
			Expect(resp.Result.Message).To(ContainSubstring(volumeNameForDemo))
			Expect(resp.Result.Message).To(ContainSubstring("which the runtime config injection would replace"))
		})

		It("[contract] rejects a volume projecting the config ConfigMap with a non-default mode", func() {
			resp := handleWithDemo(newCustomizedVolumeRequest(func(source *corev1.ConfigMapVolumeSource) {
				mode := int32(0400)
				source.DefaultMode = &mode
			}))
			Expect(resp.Allowed).To(BeFalse())
			Expect(resp.Result.Code).To(Equal(int32(403)))
			Expect(resp.Result.Message).To(ContainSubstring("would replace"))
		})

		It("[contract] rejects a volume projecting the config ConfigMap marked optional", func() {
			resp := handleWithDemo(newCustomizedVolumeRequest(func(source *corev1.ConfigMapVolumeSource) {
				optional := true
				source.Optional = &optional
			}))
			Expect(resp.Allowed).To(BeFalse())
			Expect(resp.Result.Code).To(Equal(int32(403)))
			Expect(resp.Result.Message).To(ContainSubstring("would replace"))
		})

		It("[contract] still accepts the volume exactly as the api server defaults it (idempotency)", func() {
			// What a previously injected spec looks like after the api server filled in its
			// defaults (only defaultMode=0644 is defaulted; optional stays nil): must be
			// admitted and injected. An explicit optional:false is the same default and
			// must be accepted too. Green on 268012a5 and at 41b47141.
			for _, customize := range []func(*corev1.ConfigMapVolumeSource){
				func(source *corev1.ConfigMapVolumeSource) {
					mode := int32(corev1.ConfigMapVolumeSourceDefaultMode)
					source.DefaultMode = &mode
				},
				func(source *corev1.ConfigMapVolumeSource) {
					optional := false
					source.Optional = &optional
				},
			} {
				resp := handleWithDemo(newCustomizedVolumeRequest(customize))
				if !resp.Allowed {
					reason := ""
					if resp.Result != nil {
						reason = resp.Result.Message
					}
					Fail("the api-server-defaulted projection must keep being admitted, got denial: " + reason)
				}
				foundEnv := false
				for _, op := range resp.Patches {
					raw, _ := json.Marshal(op.Value)
					if strings.Contains(string(raw), "FLUID_RUNTIME_CONFIG_PATH_DEMO") {
						foundEnv = true
					}
				}
				Expect(foundEnv).To(BeTrue(), "the env var must be injected for the defaulted volume")
			}
		})
	})

	Context("A-N1: only fluid's own fuse sidecars are skipped (fixed at 268012a5)", func() {
		It("[contract] an app container named fluid-fuse-proxy is injected while fluid-fuse-0 is skipped", func() {
			// Before 9d9bd872 the skip was a bare prefix match, so a user container named
			// fluid-fuse-proxy was silently left without the env var. Red on 6d3a6d7e.
			dataset, cacheRuntime := boundCacheDataset("demo", "big-data")
			cm := runtimeConfigConfigMap("demo", "big-data")
			fakeClient := fake.NewFakeClientWithScheme(s, dataset, cacheRuntime, cm)
			Expect(plugins.RegisterMutatingHandlers(fakeClient)).To(Succeed())

			handler := &FluidMutatingHandler{}
			handler.Setup(fakeClient, fakeClient, decoder)

			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "app-fusey",
					Namespace: "big-data",
					Labels:    map[string]string{common.LabelAnnotationInject: common.True},
					Annotations: map[string]string{
						common.LabelAnnotationDatasets: "demo",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "fluid-fuse-proxy", Image: "app:v1"},
						{Name: "fluid-fuse-0", Image: "app:v1"},
						{Name: "app", Image: "app:v1"},
					},
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

			injected := map[int]bool{}
			for _, op := range resp.Patches {
				var idx int
				if n, _ := fmt.Sscanf(op.Path, "/spec/containers/%d/env", &idx); n == 1 {
					injected[idx] = true
				}
			}
			Expect(injected[0]).To(BeTrue(), "fluid-fuse-proxy is an app container and must be injected")
			Expect(injected[1]).To(BeFalse(), "fluid-fuse-0 is fluid's own fuse sidecar and must be skipped")
			Expect(injected[2]).To(BeTrue(), "the plain app container must be injected")
		})
	})
})
