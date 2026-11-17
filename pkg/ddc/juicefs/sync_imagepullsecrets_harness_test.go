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
Round-3 harness for PR #6245 (delta 3271f28..d7e3168, "fix(juicefs): sync image
pull secrets to worker and fuse on runtime updates").

Round 2 recorded F-copilot-sync-path-no-propagation (tracked as #6248): the sync
path's field allowlist (checkAndSetWorkerChanges / checkAndSetFuseChanges) omitted
imagePullSecrets, so editing secrets on a live runtime never reached the worker
StatefulSet / fuse DaemonSet. The round-3 delta adds exactly that.

This contract test drives the FULL controller entry point — JuiceFSEngine.SyncRuntime
(the same function pkg/ddc/base/syncs.go invokes on every reconcile) — against a fake
cluster seeded with the previous state, then edits the runtime CR the way a user would
and asserts the user-visible contract:

  - the worker StatefulSet pod template gets the new effective secrets
    (spec.worker.imagePullSecrets, else spec.imagePullSecrets);
  - the fuse DaemonSet pod template gets the new effective secrets
    (spec.fuse.imagePullSecrets, else spec.imagePullSecrets), and the fuse
    generation label is bumped so fuse pods roll (same treatment the fuse image
    change path gets);
  - the values configmap (the controller's source of truth for "what was synced")
    records the new runtime-level and component-level secrets;
  - a secrets change confined to one component does not touch the other's pods;
  - removal semantics match the creation path: removing spec.imagePullSecrets
    falls back to the controller env default (IMAGE_PULL_SECRETS), removing
    everything clears the pod templates;
  - when nothing changes, nothing is written.

The OLD configmap value is produced by the real transform() on the old spec (not a
hand-built value), so old and latest differ only in the secrets fields.

Polarity: contract test. FAILS on the round-2 head (3271f28, no sync code: pods
keep the old credentials); PASSES on the fixed code.
*/

package juicefs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	goruntime "runtime"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/net"
	"sigs.k8s.io/yaml"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/ddc/base/portallocator"
	cruntime "github.com/fluid-cloudnative/fluid/pkg/runtime"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	"github.com/fluid-cloudnative/fluid/pkg/utils/kubeclient"
)

func refs(names ...string) []corev1.LocalObjectReference {
	if len(names) == 0 {
		return nil
	}
	out := make([]corev1.LocalObjectReference, 0, len(names))
	for _, n := range names {
		out = append(out, corev1.LocalObjectReference{Name: n})
	}
	return out
}

func namesOf(l []corev1.LocalObjectReference) []string {
	if len(l) == 0 {
		return nil
	}
	out := make([]string, 0, len(l))
	for _, r := range l {
		out = append(out, r.Name)
	}
	return out
}

// harnessEffectiveSecrets mirrors the production precedence rule (component list
// wins over the runtime-wide one) so this file compiles against both the fixed
// head and previous heads that predate the sync code.
func harnessEffectiveSecrets(component, global []corev1.LocalObjectReference) []corev1.LocalObjectReference {
	if len(component) > 0 {
		return component
	}
	return global
}

func TestJuicefsSyncImagePullSecretsPropagation(t *testing.T) {
	cases := []struct {
		name string

		// controller env IMAGE_PULL_SECRETS; "" means leave unset
		envSecrets string

		// previous runtime spec (what the configmap + pods were built from)
		oldGlobal, oldWorker, oldFuse []string
		// edited runtime spec
		newGlobal, newWorker, newFuse []string

		// upgrade-path fixture shaping: a configmap written by the pre-feature
		// controller carries no secrets fields at all
		oldCMNoSecrets bool
		// live pod-template secrets before the sync (default: old effective)
		stsOverride, dsOverride []string

		// expectations
		wantWorker, wantFuse         []string
		wantCMGlobal, wantCMWorker   []string
		wantFuseGenerationBumped     bool
	}{
		{
			name:       "runtime-wide secrets added reach both components and the configmap",
			newGlobal:  []string{"a", "b"},
			wantWorker: []string{"a", "b"}, wantFuse: []string{"a", "b"},
			wantCMGlobal: []string{"a", "b"},
			wantFuseGenerationBumped: true,
		},
		{
			name:      "worker override removed falls back to the runtime-wide secrets",
			oldGlobal: []string{"g"}, oldWorker: []string{"w"},
			newGlobal: []string{"g"},
			wantWorker: []string{"g"}, wantFuse: []string{"g"},
			wantCMGlobal: []string{"g"},
		},
		{
			name:      "fuse override replaced only changes fuse and bumps its generation",
			oldGlobal: []string{"g"}, oldFuse: []string{"f1"},
			newGlobal: []string{"g"}, newFuse: []string{"f2"},
			wantWorker: []string{"g"}, wantFuse: []string{"f2"},
			wantCMGlobal: []string{"g"},
			wantFuseGenerationBumped: true,
		},
		{
			name:      "secrets removed entirely clears both pod templates",
			oldGlobal: []string{"g"},
			wantWorker: nil, wantFuse: nil,
			wantFuseGenerationBumped: true,
		},
		{
			// replacement semantics must match the creation path: removing the
			// spec-level secrets reverts the pods to the controller env default
			name:       "spec secrets removed with env default set reverts pods to the env default",
			envSecrets: "env-secret",
			oldGlobal:  []string{"x"},
			wantWorker: []string{"env-secret"}, wantFuse: []string{"env-secret"},
			wantCMGlobal: []string{"env-secret"},
			wantFuseGenerationBumped: true,
		},
		{
			name:      "worker override isolates the worker while the runtime-wide change moves only fuse",
			oldGlobal: []string{"g"}, oldWorker: []string{"w"},
			newGlobal: []string{"g2"}, newWorker: []string{"w"},
			wantWorker: []string{"w"}, wantFuse: []string{"g2"},
			wantCMGlobal: []string{"g2"}, wantCMWorker: []string{"w"},
			wantFuseGenerationBumped: true,
		},
		{
			name:      "no change leaves the workloads and the configmap alone",
			oldGlobal: []string{"g"}, newGlobal: []string{"g"},
			wantWorker: []string{"g"}, wantFuse: []string{"g"},
			wantCMGlobal: []string{"g"},
		},
		{
			// upgrade path: a configmap written by the pre-feature controller records
			// no secrets at all while the env default is set (the chart already put
			// the env secrets on the pods at creation). The first sync after the
			// controller upgrade records the env default and (observed, one-time)
			// bumps the fuse generation.
			name:            "configmap predating the feature with env default set records the env default",
			envSecrets:      "env-secret",
			oldCMNoSecrets:  true,
			stsOverride:     []string{"env-secret"},
			dsOverride:      []string{"env-secret"},
			wantWorker:      []string{"env-secret"}, wantFuse: []string{"env-secret"},
			wantCMGlobal:    []string{"env-secret"},
			wantFuseGenerationBumped: true,
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			if c.envSecrets == "" {
				t.Setenv(common.EnvImagePullSecretsKey, "")
			} else {
				t.Setenv(common.EnvImagePullSecretsKey, c.envSecrets)
			}

			dataset, juicefsruntime := mockFluidObjectsForTests(types.NamespacedName{Namespace: "fluid", Name: "jfsdemo"})
			juicefsruntime.Spec.ImagePullSecrets = refs(c.oldGlobal...)
			juicefsruntime.Spec.Worker.ImagePullSecrets = refs(c.oldWorker...)
			juicefsruntime.Spec.Fuse.ImagePullSecrets = refs(c.oldFuse...)

			engine := mockJuiceFSEngineForTests(dataset, juicefsruntime)
			mockedObjects := mockJuiceFSObjectsForTests(dataset, juicefsruntime, engine)

			require.NoError(t, setupRuntimePortAllocatorForTest())

			// seed client for the OLD transform: dataset + secret + runtime
			jfsSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "jfs-secret", Namespace: "fluid"},
				Data: map[string][]byte{
					"token":      []byte("token"),
					"access-key": []byte("ak"),
					"secret-key": []byte("sk"),
				},
			}
			seedClient := fake.NewFakeClientWithScheme(datav1alpha1.UnitTestScheme, dataset.DeepCopy(), juicefsruntime.DeepCopy(), jfsSecret)
			engine.Client = seedClient

			// the old configmap value is what the previous controller run saved:
			// the real transform() output for the OLD spec.
			oldValue, err := engine.transform(juicefsruntime)
			require.NoError(t, err)
			if c.oldCMNoSecrets {
				oldValue.ImagePullSecrets = nil
				oldValue.Worker.ImagePullSecrets = nil
				oldValue.Fuse.ImagePullSecrets = nil
			}
			oldData, err := yaml.Marshal(oldValue)
			require.NoError(t, err)

			// existing workloads carry the old effective secrets; OnDelete update
			// strategies, as the sync path requires
			oldWorkerEffective := harnessEffectiveSecrets(refs(c.oldWorker...), refs(c.oldGlobal...))
			oldFuseEffective := harnessEffectiveSecrets(refs(c.oldFuse...), refs(c.oldGlobal...))
			if c.stsOverride != nil {
				oldWorkerEffective = refs(c.stsOverride...)
			}
			if c.dsOverride != nil {
				oldFuseEffective = refs(c.dsOverride...)
			}
			mockedObjects.WorkerSts.Spec.UpdateStrategy.Type = appsv1.OnDeleteStatefulSetStrategyType
			mockedObjects.WorkerSts.Spec.Template.Spec.ImagePullSecrets = oldWorkerEffective
			mockedObjects.FuseDs.Spec.UpdateStrategy.Type = appsv1.OnDeleteDaemonSetStrategyType
			mockedObjects.FuseDs.Spec.Template.Spec.ImagePullSecrets = oldFuseEffective
			if c.wantFuseGenerationBumped {
				// pre-existing generation, so a bump is observable
				mockedObjects.FuseDs.Spec.Template.Labels[common.LabelRuntimeFuseGeneration] = "1"
			}

			valuesConfigmap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: engine.getHelmValuesConfigMapName(), Namespace: "fluid"},
				Data:       map[string]string{"data": string(oldData)},
			}

			// the user's edit: the runtime CR now carries the NEW spec
			juicefsruntime.Spec.ImagePullSecrets = refs(c.newGlobal...)
			juicefsruntime.Spec.Worker.ImagePullSecrets = refs(c.newWorker...)
			juicefsruntime.Spec.Fuse.ImagePullSecrets = refs(c.newFuse...)

			resources := []k8sruntime.Object{
				dataset.DeepCopy(),
				juicefsruntime.DeepCopy(),
				mockedObjects.WorkerSts.DeepCopy(),
				mockedObjects.FuseDs.DeepCopy(),
				mockedObjects.PersistentVolume,
				mockedObjects.PersistentVolumeClaim,
				jfsSecret.DeepCopy(),
				valuesConfigmap.DeepCopy(),
			}
			for _, v := range mockedObjects.ConfigMaps {
				resources = append(resources, v.DeepCopy())
			}

			client := fake.NewFakeClientWithScheme(datav1alpha1.UnitTestScheme, resources...)
			engine.Client = client
			engine.runtimeInfo.SetFuseName(engine.getFuseName())

			_, err = engine.SyncRuntime(cruntime.ReconcileRequestContext{})
			require.NoError(t, err, "SyncRuntime must succeed")

			// 1. worker StatefulSet pod template
			updatedSts, err := kubeclient.GetStatefulSet(client, mockedObjects.WorkerSts.Name, mockedObjects.WorkerSts.Namespace)
			require.NoError(t, err)
			assert.Equal(t, c.wantWorker, namesOf(updatedSts.Spec.Template.Spec.ImagePullSecrets),
				"worker sts pod template imagePullSecrets")

			// 2. fuse DaemonSet pod template + generation
			updatedDs, err := kubeclient.GetDaemonset(client, mockedObjects.FuseDs.Name, mockedObjects.FuseDs.Namespace)

			require.NoError(t, err)
			assert.Equal(t, c.wantFuse, namesOf(updatedDs.Spec.Template.Spec.ImagePullSecrets),
				"fuse ds pod template imagePullSecrets")
			gotGeneration := updatedDs.Spec.Template.Labels[common.LabelRuntimeFuseGeneration]
			if c.wantFuseGenerationBumped {
				assert.Equal(t, "2", gotGeneration, "fuse generation must be bumped so fuse pods roll")
			} else {
				assert.NotEqual(t, "2", gotGeneration, "fuse generation must not be bumped when fuse secrets do not change")
			}

			// 3. values configmap records what was synced
			cm, err := engine.GetValuesConfigMap()
			require.NoError(t, err)
			require.NotNil(t, cm)
			syncedValue := &JuiceFS{}
			require.NoError(t, yaml.Unmarshal([]byte(cm.Data["data"]), syncedValue))
			assert.Equal(t, c.wantCMGlobal, namesOf(syncedValue.ImagePullSecrets), "configmap runtime-level secrets")
			assert.Equal(t, c.wantCMWorker, namesOf(syncedValue.Worker.ImagePullSecrets), "configmap worker-level secrets")

			// 4. idempotence: a second SyncRuntime with the same spec must not
			// re-bump the fuse generation (no spurious fuse rollouts)
			_, err = engine.SyncRuntime(cruntime.ReconcileRequestContext{})
			require.NoError(t, err)
			updatedDs2, err := kubeclient.GetDaemonset(client, mockedObjects.FuseDs.Name, mockedObjects.FuseDs.Namespace)
			require.NoError(t, err)
			assert.Equal(t, gotGeneration, updatedDs2.Spec.Template.Labels[common.LabelRuntimeFuseGeneration],
				"second sync must not bump the fuse generation again")
		})
	}
}

// TestJuicefsSampleDocsSyncClaim pins the round-3 docs consistency requirement:
// since sync_runtime.go now propagates imagePullSecrets edits to the worker and
// fuse on existing runtimes (commit d7e3168), the user-facing sample docs must
// not keep telling users that changing the secrets requires recreating the
// runtime. Polarity: contract test — RED on head d7e3168 (stale claim present),
// GREEN once the sentence is updated.
func TestJuicefsSampleDocsSyncClaim(t *testing.T) {
	_, thisFile, _, _ := goruntime.Caller(0)
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(thisFile))))

	syncCode, err := os.ReadFile(filepath.Join(repoRoot, "pkg", "ddc", "juicefs", "sync_runtime.go"))
	require.NoError(t, err)
	syncsSecrets := strings.Contains(string(syncCode), "image pull secrets changed")
	require.True(t, syncsSecrets, "expected the sync path to sync imagePullSecrets; if it does not, this test's premise is stale")

	for _, lang := range []string{"en", "zh"} {
		doc, err := os.ReadFile(filepath.Join(repoRoot, "docs", lang, "samples", "image_pull_secrets.md"))
		require.NoError(t, err, "docs/%s/samples/image_pull_secrets.md must exist", lang)
		lower := strings.ToLower(string(doc))
		assert.NotContains(t, lower, "needs the runtime to be recreated",
			"docs/%s still claims changing imagePullSecrets requires recreating the runtime, but the sync path now propagates the change", lang)
		assert.NotContains(t, lower, "需要重新创建 runtime",
			"docs/%s still claims changing imagePullSecrets requires recreating the runtime, but the sync path now propagates the change", lang)
	}
}

// setupRuntimePortAllocatorForTest isolates the port allocator setup so the
// sync scenarios do not depend on ginkgo's BeforeEach.
func setupRuntimePortAllocatorForTest() error {
	pr := net.ParsePortRangeOrDie("14000-15999")
	return portallocator.SetupRuntimePortAllocator(nil, pr, "bitmap", dummy)
}
