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

/*
Verification harness for PR #6245 (hierarchical imagePullSecrets for JuiceFSRuntime).

This file is ADDITIVE reviewer harness code; it intentionally does not modify any
production file. Each entry runs the full JuiceFSEngine.transform() — the same
entry point generateJuicefsValueFile() uses before handing the marshaled result to
helm — and asserts:

  1. the in-memory value struct precedence (env default < spec.imagePullSecrets
     < spec.worker.imagePullSecrets / spec.fuse.imagePullSecrets), and
  2. the sigs.k8s.io/yaml marshaling of that struct, i.e. the exact keys the
     juicefs chart templates read (.Values.imagePullSecrets, .Values.worker.imagePullSecrets,
     .Values.fuse.imagePullSecrets).

When HARNESS_OUT is set, the marshaled values of each scenario are also written to
$HARNESS_OUT/<scenario>.values.yaml together with an expected.json describing the
pod-level imagePullSecrets the chart is expected to render for the worker StatefulSet
and the fuse DaemonSet. scripts/render-chart.sh (on the verification branch) feeds
those files to `helm template` and checks the rendered pods — that closes the
controller -> values -> chart -> pod contract end to end.

Polarity: contract tests. They FAIL on code that breaks the documented precedence
and PASS on correct code.
*/

package juicefs

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	goruntime "runtime"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/net"
	"sigs.k8s.io/yaml"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/ddc/base"
	"github.com/fluid-cloudnative/fluid/pkg/ddc/base/portallocator"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
)

type imagePullSecretsScenario struct {
	name string

	// controller default, as deployed on the runtime controller (IMAGE_PULL_SECRETS env)
	envImagePullSecrets string
	// "" means leave the env var unset entirely
	unsetEnv bool

	specImagePullSecrets []string
	workerSecrets        []string
	fuseSecrets          []string

	// expectations: nil means "no imagePullSecrets key at all"
	wantGlobal  []string
	wantWorker  []string
	wantFuse    []string
	wantNoKeys  bool // true when no imagePullSecrets key may appear anywhere in the marshaled values
}

var _ = Describe("imagePullSecrets hierarchy (PR 6245 harness)", func() {
	BeforeEach(func() {
		pr := net.ParsePortRangeOrDie("14000-15999")
		Expect(portallocator.SetupRuntimePortAllocator(nil, pr, "bitmap", dummy)).To(Succeed())
	})

	scenarios := []imagePullSecretsScenario{
		{
			// P0-adjacent: existing installs (no new spec fields) must render exactly
			// as before the PR — the controller env default keeps flowing to all pods.
			name:                 "s1-env-default-only",
			envImagePullSecrets:  "default-secret",
			wantGlobal:           []string{"default-secret"},
			wantWorker:           nil,
			wantFuse:             nil,
		},
		{
			// spec.imagePullSecrets replaces the controller default (Alluxio-compatible semantics).
			name:                 "s2-runtime-replaces-env-default",
			envImagePullSecrets:  "default-secret",
			specImagePullSecrets: []string{"runtime-secret"},
			wantGlobal:           []string{"runtime-secret"},
			wantWorker:           nil,
			wantFuse:             nil,
		},
		{
			// component-level secrets win over the runtime-level ones (the feature asked for by #4222).
			name:                 "s3-component-overrides-runtime",
			envImagePullSecrets:  "default-secret",
			specImagePullSecrets: []string{"runtime-secret"},
			workerSecrets:        []string{"worker-secret"},
			fuseSecrets:          []string{"fuse-secret"},
			wantGlobal:           []string{"runtime-secret"},
			wantWorker:           []string{"worker-secret"},
			wantFuse:             []string{"fuse-secret"},
		},
		{
			// only worker-level set: worker pod gets its own, fuse pod gets nothing.
			name:          "s4-worker-only-no-default",
			unsetEnv:      true,
			workerSecrets: []string{"worker-secret"},
			wantGlobal:    nil,
			wantWorker:    []string{"worker-secret"},
			wantFuse:      nil,
		},
		{
			// only fuse-level set: fuse pod gets its own, worker pod gets nothing.
			name:       "s5-fuse-only-no-default",
			unsetEnv:   true,
			fuseSecrets: []string{"fuse-secret"},
			wantGlobal: nil,
			wantWorker: nil,
			wantFuse:   []string{"fuse-secret"},
		},
	}

	for _, scenario := range scenarios {
		sc := scenario
		It("transforms "+sc.name, func() {
			if sc.unsetEnv {
				os.Unsetenv(common.EnvImagePullSecretsKey)
			} else {
				GinkgoT().Setenv(common.EnvImagePullSecretsKey, sc.envImagePullSecrets)
			}

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "fluid"},
				Data:       map[string][]byte{"metaurl": []byte("redis://127.0.0.1:6379/0")},
			}
			dataset := &datav1alpha1.Dataset{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "fluid"},
				Spec: datav1alpha1.DatasetSpec{
					Mounts: []datav1alpha1.Mount{{
						MountPoint: "juicefs:///mnt/test",
						Name:       "test",
						EncryptOptions: []datav1alpha1.EncryptOption{{
							Name: JuiceMetaUrl,
							ValueFrom: datav1alpha1.EncryptOptionSource{
								SecretKeyRef: datav1alpha1.SecretKeySelector{Name: "test", Key: "metaurl"},
							},
						}},
					}},
				},
			}
			fakeClient := fake.NewFakeClientWithScheme(testScheme, secret.DeepCopy(), dataset.DeepCopy())

			runtimeInfo, err := base.BuildRuntimeInfo("test", "fluid", "juicefs")
			Expect(err).NotTo(HaveOccurred())

			toRefs := func(names []string) []corev1.LocalObjectReference {
				if len(names) == 0 {
					return nil
				}
				refs := make([]corev1.LocalObjectReference, 0, len(names))
				for _, n := range names {
					refs = append(refs, corev1.LocalObjectReference{Name: n})
				}
				return refs
			}

			jfsRuntime := &datav1alpha1.JuiceFSRuntime{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "fluid"},
				Spec: datav1alpha1.JuiceFSRuntimeSpec{
					Fuse:   datav1alpha1.JuiceFSFuseSpec{ImagePullSecrets: toRefs(sc.fuseSecrets)},
					Worker: datav1alpha1.JuiceFSCompTemplateSpec{Replicas: 1, ImagePullSecrets: toRefs(sc.workerSecrets)},
				},
			}
			if len(sc.specImagePullSecrets) > 0 {
				jfsRuntime.Spec.ImagePullSecrets = toRefs(sc.specImagePullSecrets)
			}

			engine := JuiceFSEngine{
				name:        "test",
				namespace:   "fluid",
				Client:      fakeClient,
				Log:         fake.NullLogger(),
				runtime:     jfsRuntime,
				runtimeInfo: runtimeInfo,
			}

			value, err := engine.transform(jfsRuntime)
			Expect(err).NotTo(HaveOccurred())

			// 1. value-struct precedence
			Expect(value.ImagePullSecrets).To(Equal(toRefs(sc.wantGlobal)))
			Expect(value.Worker.ImagePullSecrets).To(Equal(toRefs(sc.wantWorker)))
			Expect(value.Fuse.ImagePullSecrets).To(Equal(toRefs(sc.wantFuse)))

			// 2. marshaled values: the exact keys the chart templates consume
			data, err := yaml.Marshal(value)
			Expect(err).NotTo(HaveOccurred())

			var asMap map[string]interface{}
			err = yaml.Unmarshal(data, &asMap)
			Expect(err).NotTo(HaveOccurred())

			checkKey := func(container map[string]interface{}, key string, want []string) {
				// "no secrets" may marshal as either an absent key (omitempty) or an
				// empty list (the inlined common.ImageInfo has no omitempty), so the
				// contract is on the secret names, not raw key presence.
				names := []string{}
				if raw, ok := container[key]; ok && raw != nil {
					list, ok := raw.([]interface{})
					Expect(ok).To(BeTrue(), "%q must marshal as a list, got %T, values:\n%s", key, raw, string(data))
					for _, item := range list {
						names = append(names, item.(map[string]interface{})["name"].(string))
					}
				}
				if want == nil {
					Expect(names).To(BeEmpty(), "expected no %q entries, got %v, values:\n%s", key, names, string(data))
				} else {
					Expect(names).To(Equal(want), "wrong %q entries, values:\n%s", key, string(data))
				}
			}

			checkKey(asMap, "imagePullSecrets", sc.wantGlobal)
			workerMap, ok := asMap["worker"].(map[string]interface{})
			Expect(ok).To(BeTrue(), "worker key missing in marshaled values")
			checkKey(workerMap, "imagePullSecrets", sc.wantWorker)
			fuseMap, ok := asMap["fuse"].(map[string]interface{})
			Expect(ok).To(BeTrue(), "fuse key missing in marshaled values")
			checkKey(fuseMap, "imagePullSecrets", sc.wantFuse)

			// 3. render the actual juicefs chart with these values and check the
			// pod-level imagePullSecrets the worker StatefulSet / fuse DaemonSet get.
			// This closes the controller -> values -> chart -> pod contract.
			renderChartAndAssert(sc, data)

			// 4. optionally dump values + expectations for offline inspection
			outDir := os.Getenv("HARNESS_OUT")
			if outDir != "" {
				Expect(os.MkdirAll(outDir, 0o755)).To(Succeed())
				Expect(os.WriteFile(filepath.Join(outDir, sc.name+".values.yaml"), data, 0o644)).To(Succeed())
			}
		})
	}
})

func orFallback(component, global []string) []string {
	if len(component) > 0 {
		return component
	}
	if len(global) > 0 {
		return global
	}
	return nil
}

// renderChartAndAssert runs `helm template` on the real charts/juicefs with the
// marshaled JuiceFS values (exactly what generateJuicefsValueFile hands to helm)
// and asserts the pod-level imagePullSecrets of the worker StatefulSet and the
// fuse DaemonSet, applying the chart's documented fallback
// (component imagePullSecrets -> global imagePullSecrets).
func renderChartAndAssert(sc imagePullSecretsScenario, valuesYAML []byte) {
	helmBin, err := exec.LookPath("helm")
	if err != nil {
		Fail("helm is required by this verification harness (charts rendering layer): " + err.Error())
	}

	// repo root = three levels above this package (pkg/ddc/juicefs)
	_, thisFile, _, _ := goruntime.Caller(0)
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(thisFile))))
	chartDir := filepath.Join(repoRoot, "charts", "juicefs")
	if _, err := os.Stat(chartDir); err != nil {
		Fail("charts/juicefs not found at " + chartDir + ": " + err.Error())
	}

	valuesFile, err := os.CreateTemp("", "juicefs-harness-values-*.yaml")
	Expect(err).NotTo(HaveOccurred())
	defer os.Remove(valuesFile.Name())
	Expect(os.WriteFile(valuesFile.Name(), valuesYAML, 0o644)).To(Succeed())

	cmd := exec.Command(helmBin, "template", "test", chartDir,
		"-f", valuesFile.Name(), "--namespace", "fluid")
	out, err := cmd.CombinedOutput()
	Expect(err).NotTo(HaveOccurred(), "helm template failed:\n%s", string(out))

	// helm may print walk warnings before the first document separator
	if idx := bytes.Index(out, []byte("---")); idx > 0 {
		out = out[idx:]
	}

	rendered := map[string][]corev1.LocalObjectReference{}
	for _, doc := range strings.Split(string(out), "\n---") {
		if !strings.Contains(doc, "kind: StatefulSet") && !strings.Contains(doc, "kind: DaemonSet") {
			continue
		}
		var obj struct {
			Kind string `json:"kind" yaml:"kind"`
			Metadata struct {
				Name string `json:"name" yaml:"name"`
			} `json:"metadata" yaml:"metadata"`
			Spec struct {
				Template struct {
					Spec struct {
						ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets" yaml:"imagePullSecrets"`
					} `json:"spec" yaml:"spec"`
				} `json:"template" yaml:"template"`
			} `json:"spec" yaml:"spec"`
		}
		if err := yaml.Unmarshal([]byte(doc), &obj); err != nil {
			continue
		}
		switch obj.Kind {
		case "StatefulSet":
			if strings.HasSuffix(obj.Metadata.Name, "-worker") {
				rendered["worker"] = obj.Spec.Template.Spec.ImagePullSecrets
			}
		case "DaemonSet":
			if strings.HasSuffix(obj.Metadata.Name, "-fuse") {
				rendered["fuse"] = obj.Spec.Template.Spec.ImagePullSecrets
			}
		}
	}

	names := func(refs []corev1.LocalObjectReference) []string {
		ns := []string{}
		for _, r := range refs {
			ns = append(ns, r.Name)
		}
		return ns
	}

	// the chart's fallback: component secrets, else the global ones
	wantWorker := orFallback(sc.wantWorker, sc.wantGlobal)
	wantFuse := orFallback(sc.wantFuse, sc.wantGlobal)
	if wantWorker == nil {
		wantWorker = []string{}
	}
	if wantFuse == nil {
		wantFuse = []string{}
	}

	// both workloads always render for these scenarios; the contract is on the
	// pod-level imagePullSecrets each of them ends up with
	Expect(rendered).To(HaveKey("worker"), "worker StatefulSet not rendered")
	Expect(names(rendered["worker"])).To(Equal(wantWorker),
		"%s: worker pod imagePullSecrets", sc.name)
	Expect(rendered).To(HaveKey("fuse"), "fuse DaemonSet not rendered")
	Expect(names(rendered["fuse"])).To(Equal(wantFuse),
		"%s: fuse pod imagePullSecrets", sc.name)
}
