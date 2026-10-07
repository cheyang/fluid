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

package mutating

// Reviewer verification harness for PR #6197 (RuntimeConfigInjector).
// It encodes the reviewer findings F1/F2 as executable claims:
//
//   F1 (canary): when the loaded plugins profile predates the `clientless` group
//   (the exact state of an existing installation after `helm upgrade`, because
//   charts/fluid/fluid/templates/webhook/plugins-profile.yaml keeps the live
//   ConfigMap via `lookup` unless forceReplacePluginsProfile=true), a pod that
//   opted in with fluid.io/inject=true is routed to an empty plugin list and is
//   admitted with ZERO injection, silently.
//
//   F2 (canary): when the runtime config ConfigMap is missing, the plugin returns
//   a plain error that is NOT a NeedRetryWithApiReaderError, so Handle() logs it
//   and admits the pod WITHOUT the injection (no retry, no rejection), even
//   though the new MutatingWebhookConfiguration rule has failurePolicy: Fail.
//
//   T3 (contract): the full happy path through MutatePod routes to the clientless
//   plugin and injects volume + mount + env + done label.
//
//   T4 (contract): a dataset that does not exist fails with a retryable error,
//   which after the direct-reader retry rejects pod creation (documented behavior).
//
// Canary tests assert the CURRENT behavior; they flip to red when the behavior is
// fixed and must then be inverted. Contract tests assert intended behavior.

import (
	"os"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/webhook/plugins"
	webhookutils "github.com/fluid-cloudnative/fluid/pkg/webhook/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/agiledragon/gomonkey/v2"
)

const harnessNamespace = "fluid-harness"

// preClientlessProfile is the plugins profile of a fluid installation upgraded
// from before this PR: charts/fluid/fluid/templates/webhook/plugins-profile.yaml
// renders the live ConfigMap content when `lookup` finds one, so on `helm upgrade`
// the webhook keeps reading exactly this, with no `clientless` group.
const preClientlessProfile = `
plugins:
  serverful:
    withDataset:
    - RequireNodeWithFuse
    withoutDataset:
    - PreferNodesWithoutCache
  serverless:
    withDataset:
    - FuseSidecar
    withoutDataset:
    - PreferNodesWithoutCache
pluginConfig: []
`

// withClientlessProfile is the profile rendered from this PR's values.yaml.
const withClientlessProfile = `
plugins:
  serverful:
    withDataset:
    - RequireNodeWithFuse
    withoutDataset:
    - PreferNodesWithoutCache
  serverless:
    withDataset:
    - FuseSidecar
    withoutDataset:
    - PreferNodesWithoutCache
  clientless:
    withDataset:
    - RuntimeConfigInjector
    withoutDataset: []
pluginConfig: []
`

var (
	harnessProfile string
	harnessPatch   *gomonkey.Patches
)

func clientlessHarnessPod() *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "clientless-app",
			Namespace: harnessNamespace,
			Labels: map[string]string{
				common.LabelAnnotationInject: common.True,
			},
			Annotations: map[string]string{
				common.LabelAnnotationDatasets: "mooncake",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "app"},
			},
		},
	}
}

func clientlessHarnessDataset() *datav1alpha1.Dataset {
	return &datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mooncake",
			Namespace: harnessNamespace,
		},
		Status: datav1alpha1.DatasetStatus{
			Phase: datav1alpha1.BoundDatasetPhase,
			Runtimes: []datav1alpha1.Runtime{
				{
					Name:      "mooncake",
					Namespace: harnessNamespace,
					Type:      common.CacheRuntime,
				},
			},
		},
	}
}

func clientlessHarnessCacheRuntime() *datav1alpha1.CacheRuntime {
	return &datav1alpha1.CacheRuntime{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mooncake",
			Namespace: harnessNamespace,
		},
	}
}

func clientlessHarnessConfigMap() *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      common.GetCacheRuntimeConfigConfigMapName("mooncake"),
			Namespace: harnessNamespace,
		},
		Data: map[string]string{
			common.RuntimeConfigShellFileName: "#!/bin/sh\nexport MASTER_NAME='mooncake-master'\n",
			common.RuntimeConfigJSONFileName:  `{"master":{"name":"mooncake-master"}}`,
		},
	}
}

func newClientlessHarnessClient(objs ...runtime.Object) *fake.ClientBuilder {
	s := runtime.NewScheme()
	Expect(corev1.AddToScheme(s)).To(Succeed())
	Expect(datav1alpha1.AddToScheme(s)).To(Succeed())
	return fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(objs...)
}

var _ = Describe("clientless runtime config injection (reviewer harness for PR #6197)", func() {
	BeforeEach(func() {
		harnessPatch = gomonkey.ApplyFunc(os.ReadFile, func(string) ([]byte, error) {
			return []byte(harnessProfile), nil
		})
	})

	AfterEach(func() {
		harnessPatch.Reset()
	})

	Context("F1: plugins profile without the clientless group (post `helm upgrade` state)", func() {
		It("CANARY: admits an opted-in pod with zero injection and no error", func() {
			harnessProfile = preClientlessProfile

			fakeClient := newClientlessHarnessClient(
				clientlessHarnessDataset(),
				clientlessHarnessCacheRuntime(),
				clientlessHarnessConfigMap(),
			).Build()
			Expect(plugins.RegisterMutatingHandlers(fakeClient)).To(Succeed())

			handler := &FluidMutatingHandler{Client: fakeClient}
			pod := clientlessHarnessPod()

			err := handler.MutatePod(pod, false)
			// Current behavior: the new MutatingWebhookConfiguration rule matches this
			// pod (fluid.io/inject=true), but no clientless plugin is registered, so
			// nothing happens and the pod is admitted WITHOUT its runtime config.
			Expect(err).NotTo(HaveOccurred())
			Expect(pod.Spec.Volumes).To(BeEmpty())
			Expect(pod.Spec.Containers[0].VolumeMounts).To(BeEmpty())
			Expect(pod.Spec.Containers[0].Env).To(BeEmpty())
			Expect(pod.Labels).NotTo(HaveKey(common.InjectSidecarDone))
		})

		It("CONTROL: the same pod IS injected once the profile carries the clientless group", func() {
			// This control proves the canary above detects the difference: the only
			// changed variable is the plugins profile, i.e. the chart upgrade state.
			harnessProfile = withClientlessProfile

			fakeClient := newClientlessHarnessClient(
				clientlessHarnessDataset(),
				clientlessHarnessCacheRuntime(),
				clientlessHarnessConfigMap(),
			).Build()
			Expect(plugins.RegisterMutatingHandlers(fakeClient)).To(Succeed())

			handler := &FluidMutatingHandler{Client: fakeClient}
			pod := clientlessHarnessPod()

			err := handler.MutatePod(pod, false)
			Expect(err).NotTo(HaveOccurred())
			Expect(pod.Spec.Volumes).To(HaveLen(1))
			Expect(pod.Spec.Containers[0].VolumeMounts).To(HaveLen(1))
			Expect(pod.Spec.Containers[0].Env).To(HaveLen(1))
			Expect(pod.Labels).To(HaveKeyWithValue(common.InjectSidecarDone, common.True))
		})
	})

	Context("F2: runtime config ConfigMap missing while the pod opted in", func() {
		It("CANARY: returns a NON-retryable error, so Handle admits the pod without injection", func() {
			harnessProfile = withClientlessProfile

			fakeClient := newClientlessHarnessClient(
				clientlessHarnessDataset(),
				clientlessHarnessCacheRuntime(),
				// no ConfigMap on purpose
			).Build()
			Expect(plugins.RegisterMutatingHandlers(fakeClient)).To(Succeed())

			handler := &FluidMutatingHandler{Client: fakeClient}
			pod := clientlessHarnessPod()

			err := handler.MutatePod(pod, false)
			Expect(err).To(HaveOccurred())

			// Handle() only retries (and only rejects after the retry fails) when the
			// error is a NeedRetryWithApiReaderError. A plain error is logged and the
			// pod is admitted with whatever it had - here: no injection at all.
			Expect(webhookutils.IsNeedRetryWithApiReaderError(err)).To(BeFalse())

			// The plugin resolves everything before touching the pod, so at least the
			// failure leaves no partial mutation behind.
			Expect(pod.Spec.Volumes).To(BeEmpty())
			Expect(pod.Spec.Containers[0].VolumeMounts).To(BeEmpty())
			Expect(pod.Spec.Containers[0].Env).To(BeEmpty())
		})
	})

	Context("T3: happy path through MutatePod", func() {
		It("CONTRACT: routes to the clientless plugin and injects volume, mount, env and done label", func() {
			harnessProfile = withClientlessProfile

			fakeClient := newClientlessHarnessClient(
				clientlessHarnessDataset(),
				clientlessHarnessCacheRuntime(),
				clientlessHarnessConfigMap(),
			).Build()
			Expect(plugins.RegisterMutatingHandlers(fakeClient)).To(Succeed())

			handler := &FluidMutatingHandler{Client: fakeClient}
			pod := clientlessHarnessPod()

			err := handler.MutatePod(pod, false)
			Expect(err).NotTo(HaveOccurred())

			volumeName := common.GetCacheRuntimeConfigConfigMapName("mooncake")
			Expect(pod.Spec.Volumes).To(HaveLen(1))
			Expect(pod.Spec.Volumes[0].Name).To(Equal(volumeName))
			Expect(pod.Spec.Volumes[0].ConfigMap.Name).To(Equal(volumeName))

			Expect(pod.Spec.Containers[0].VolumeMounts).To(HaveLen(1))
			Expect(pod.Spec.Containers[0].VolumeMounts[0].MountPath).To(Equal("/etc/fluid/config/mooncake"))
			Expect(pod.Spec.Containers[0].VolumeMounts[0].ReadOnly).To(BeTrue())

			Expect(pod.Spec.Containers[0].Env).To(HaveLen(1))
			Expect(pod.Spec.Containers[0].Env[0].Name).To(Equal("FLUID_RUNTIME_CONFIG_PATH_MOONCAKE"))
			Expect(pod.Spec.Containers[0].Env[0].Value).To(Equal("/etc/fluid/config/mooncake/runtime.sh"))

			Expect(pod.Labels).To(HaveKeyWithValue(common.InjectSidecarDone, common.True))
		})
	})

	Context("T4: dataset named in the annotation does not exist", func() {
		It("CONTRACT: fails with a retryable error, which rejects pod creation after the retry", func() {
			harnessProfile = withClientlessProfile

			fakeClient := newClientlessHarnessClient().Build()
			Expect(plugins.RegisterMutatingHandlers(fakeClient)).To(Succeed())

			handler := &FluidMutatingHandler{Client: fakeClient}
			pod := clientlessHarnessPod()

			err := handler.MutatePod(pod, false)
			Expect(err).To(HaveOccurred())
			// Retryable: Handle() retries with the direct reader and, when that fails
			// too, returns admission.Errored -> pod creation rejected (documented).
			Expect(webhookutils.IsNeedRetryWithApiReaderError(err)).To(BeTrue())
			Expect(errors.Cause(err)).To(HaveOccurred())
		})
	})
})
