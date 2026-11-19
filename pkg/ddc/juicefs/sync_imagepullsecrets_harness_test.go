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
	"context"
	"fmt"
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
	"sigs.k8s.io/controller-runtime/pkg/client"
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
		wantWorker, wantFuse          []string
		wantCMGlobal, wantCMWorker    []string
		wantFuseGenerationBumped      bool
		wantFuseGenerationStable      bool
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
			// the env secrets on the pods at creation). Round-4 update (head
			// be3337d compares desired secrets against the LIVE template, not the
			// saved value): the first sync after the controller upgrade still
			// records the env default in the configmap, but since the live pods
			// already carry it, the fuse generation must NOT be bumped — a pure
			// bookkeeping catch-up no longer rolls fuse pods. (At d7e3168 this
			// scenario observed a one-time spurious bump; the fix removes it.)
			name:            "configmap predating the feature with env default set records the env default",
			envSecrets:      "env-secret",
			oldCMNoSecrets:  true,
			stsOverride:     []string{"env-secret"},
			dsOverride:      []string{"env-secret"},
			wantWorker:      []string{"env-secret"}, wantFuse: []string{"env-secret"},
			wantCMGlobal:    []string{"env-secret"},
			wantFuseGenerationStable: true,
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
			if c.wantFuseGenerationBumped || c.wantFuseGenerationStable {
				// pre-existing generation, so a bump (or its absence) is observable
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
			} else if c.wantFuseGenerationStable {
				assert.Equal(t, "1", gotGeneration, "fuse generation must not be bumped when the live fuse template already carries the desired secrets (bookkeeping-only catch-up)")
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

/*
Round-4 harness additions (delta d7e3168..be3337d, "fix(juicefs): compare image
pull secrets against the live templates").

Round 3 recorded three recorded-state-vs-cluster desync edges in the new sync
bookkeeping (F-worker-lost-update-missing-scriptcm, F-sync-strategy-flip-advance,
F-sync-override-not-recorded). The round-4 delta closes the *permanence* of all
three by comparing the desired secrets against the LIVE pod template instead of
the saved value, and by recording component overrides unconditionally.

Each test below drives the FULL JuiceFSEngine.SyncRuntime entry point over the
multi-reconcile sequence that produced the round-3 divergence, then asserts the
workload converges on a later reconcile. Polarity: contract tests — each FAILS
on the previous head d7e3168 (divergence permanent), PASSES on be3337d.
*/

// syncDesyncFixture seeds a fake cluster in the previous steady state
// (configmap + live templates carrying oldGlobal) and hands back an engine
// whose runtime CR already carries the user's edit (newGlobal / newWorker).
// workerStrategy is the live worker StatefulSet update strategy;
// includeWorkerScript controls whether the worker-script configmap exists
// (its absence is the round-3 F-worker-lost-update-missing-scriptcm trigger).
func syncDesyncFixture(t *testing.T, oldGlobal, newGlobal, newWorker []string, workerStrategy appsv1.StatefulSetUpdateStrategyType, includeWorkerScript bool) (*JuiceFSEngine, client.Client, mockedObjects) {
	t.Helper()

	dataset, runtime := mockFluidObjectsForTests(types.NamespacedName{Namespace: "fluid", Name: "jfsdemo"})
	runtime.Spec.ImagePullSecrets = refs(oldGlobal...)

	engine := mockJuiceFSEngineForTests(dataset, runtime)
	mockedObjects := mockJuiceFSObjectsForTests(dataset, runtime, engine)

	require.NoError(t, setupRuntimePortAllocatorForTest())

	jfsSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "jfs-secret", Namespace: "fluid"},
		Data: map[string][]byte{
			"token":      []byte("token"),
			"access-key": []byte("ak"),
			"secret-key": []byte("sk"),
		},
	}
	seedClient := fake.NewFakeClientWithScheme(datav1alpha1.UnitTestScheme, dataset.DeepCopy(), runtime.DeepCopy(), jfsSecret)
	engine.Client = seedClient

	// the old configmap value is the real transform() output for the OLD spec
	oldValue, err := engine.transform(runtime)
	require.NoError(t, err)
	oldData, err := yaml.Marshal(oldValue)
	require.NoError(t, err)

	mockedObjects.WorkerSts.Spec.UpdateStrategy.Type = workerStrategy
	mockedObjects.WorkerSts.Spec.Template.Spec.ImagePullSecrets = refs(oldGlobal...)
	mockedObjects.FuseDs.Spec.UpdateStrategy.Type = appsv1.OnDeleteDaemonSetStrategyType
	mockedObjects.FuseDs.Spec.Template.Spec.ImagePullSecrets = refs(oldGlobal...)
	mockedObjects.FuseDs.Spec.Template.Labels[common.LabelRuntimeFuseGeneration] = "1"

	valuesConfigmap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: engine.getHelmValuesConfigMapName(), Namespace: "fluid"},
		Data:       map[string]string{"data": string(oldData)},
	}

	// the user's edit: the runtime CR now carries the NEW spec
	runtime.Spec.ImagePullSecrets = refs(newGlobal...)
	runtime.Spec.Worker.ImagePullSecrets = refs(newWorker...)

	resources := []k8sruntime.Object{
		dataset.DeepCopy(),
		runtime.DeepCopy(),
		mockedObjects.WorkerSts.DeepCopy(),
		mockedObjects.FuseDs.DeepCopy(),
		mockedObjects.PersistentVolume,
		mockedObjects.PersistentVolumeClaim,
		jfsSecret.DeepCopy(),
		valuesConfigmap.DeepCopy(),
	}
	for name, v := range mockedObjects.ConfigMaps {
		if !includeWorkerScript && name == fmt.Sprintf("%s-worker-script", runtime.Name) {
			continue
		}
		resources = append(resources, v.DeepCopy())
	}

	fakeClient := fake.NewFakeClientWithScheme(datav1alpha1.UnitTestScheme, resources...)
	engine.Client = fakeClient
	engine.runtimeInfo.SetFuseName(engine.getFuseName())
	return engine, fakeClient, mockedObjects
}

// F-worker-lost-update-missing-scriptcm: when the worker-script configmap is
// missing, the first sync advances the values configmap without updating the
// worker StatefulSet (the transient divergence — still observable at head, see
// the round-4 findings). The PERMANENCE is what the fix removes: once the
// configmap comes back, the next reconcile must converge the worker template,
// because the comparison is against the live template rather than the saved
// value. At d7e3168 the second reconcile was a no-op and the worker stayed on
// the old credentials forever.
func TestJuicefsSyncSecretsMissingScriptConfigmapRecovers(t *testing.T) {
	t.Setenv(common.EnvImagePullSecretsKey, "")

	engine, client, mockedObjects := syncDesyncFixture(t, []string{"g1"}, []string{"g2"}, nil, appsv1.OnDeleteStatefulSetStrategyType, false)

	workerScriptCm := &corev1.ConfigMap{}
	for name, v := range mockedObjects.ConfigMaps {
		if name == fmt.Sprintf("%s-worker-script", "jfsdemo") {
			workerScriptCm = v.DeepCopy()
		}
	}
	require.NotEmpty(t, workerScriptCm.Name, "fixture must hold the worker-script configmap for the recovery step")

	// pass 1: the script configmap is missing; syncWorkerSpec early-returns
	// after the in-memory bookkeeping. No error surfaces.
	_, err := engine.SyncRuntime(cruntime.ReconcileRequestContext{})
	require.NoError(t, err, "SyncRuntime must not error while the script configmap is missing")
	sts, err := kubeclient.GetStatefulSet(client, mockedObjects.WorkerSts.Name, mockedObjects.WorkerSts.Namespace)
	require.NoError(t, err)
	assert.Equal(t, []string{"g1"}, namesOf(sts.Spec.Template.Spec.ImagePullSecrets),
		"pass 1: the worker sts is not updated while the script configmap is missing (known transient divergence)")

	// pass 2: the abnormal cluster state clears; the worker must converge.
	require.NoError(t, client.Create(context.TODO(), workerScriptCm))
	_, err = engine.SyncRuntime(cruntime.ReconcileRequestContext{})
	require.NoError(t, err)
	sts, err = kubeclient.GetStatefulSet(client, mockedObjects.WorkerSts.Name, mockedObjects.WorkerSts.Namespace)
	require.NoError(t, err)
	assert.Equal(t, []string{"g2"}, namesOf(sts.Spec.Template.Spec.ImagePullSecrets),
		"pass 2: once the script configmap is back, the worker sts must catch up to the new secrets (permanent at d7e3168, fixed at be3337d)")
}

// F-sync-strategy-flip-advance: with a non-OnDelete worker update strategy the
// first sync flips the strategy and returns, while the values configmap
// advances. The next reconcile must then converge the worker template.
// At d7e3168 the worker stayed on the old credentials permanently.
func TestJuicefsSyncSecretsStrategyFlipCatchesUp(t *testing.T) {
	t.Setenv(common.EnvImagePullSecretsKey, "")

	engine, client, mockedObjects := syncDesyncFixture(t, []string{"g1"}, []string{"g2"}, nil, appsv1.RollingUpdateStatefulSetStrategyType, true)

	// pass 1: strategy is not OnDelete; syncWorkerSpec flips it and returns.
	_, err := engine.SyncRuntime(cruntime.ReconcileRequestContext{})
	require.NoError(t, err)
	sts, err := kubeclient.GetStatefulSet(client, mockedObjects.WorkerSts.Name, mockedObjects.WorkerSts.Namespace)
	require.NoError(t, err)
	assert.Equal(t, appsv1.OnDeleteStatefulSetStrategyType, sts.Spec.UpdateStrategy.Type, "pass 1: the strategy is flipped to OnDelete")

	// pass 2: the sts update event reconciles again; the worker must converge.
	_, err = engine.SyncRuntime(cruntime.ReconcileRequestContext{})
	require.NoError(t, err)
	sts, err = kubeclient.GetStatefulSet(client, mockedObjects.WorkerSts.Name, mockedObjects.WorkerSts.Namespace)
	require.NoError(t, err)
	assert.Equal(t, []string{"g2"}, namesOf(sts.Spec.Template.Spec.ImagePullSecrets),
		"pass 2: after the strategy flip, the worker sts must catch up to the new secrets (permanent at d7e3168, fixed at be3337d)")
}

// F-sync-override-not-recorded: rotating the runtime-wide secrets a->b while
// adding a worker override set to a is an effective no-op for the worker, but
// the override must still be recorded; removing the override later must then
// drop the worker onto b. At d7e3168 the override was never recorded, so the
// removal left the worker pulling with a permanently.
func TestJuicefsSyncSecretsRotationThenOverrideRemoval(t *testing.T) {
	t.Setenv(common.EnvImagePullSecretsKey, "")

	engine, client, mockedObjects := syncDesyncFixture(t, []string{"a"}, []string{"b"}, []string{"a"}, appsv1.OnDeleteStatefulSetStrategyType, true)

	// pass 1: rotation plus override. The worker template legitimately stays on
	// a (its effective list did not change) — but the override must be recorded.
	_, err := engine.SyncRuntime(cruntime.ReconcileRequestContext{})
	require.NoError(t, err)
	sts, err := kubeclient.GetStatefulSet(client, mockedObjects.WorkerSts.Name, mockedObjects.WorkerSts.Namespace)
	require.NoError(t, err)
	assert.Equal(t, []string{"a"}, namesOf(sts.Spec.Template.Spec.ImagePullSecrets), "pass 1: worker keeps its effective list")
	cm, err := engine.GetValuesConfigMap()
	require.NoError(t, err)
	synced := &JuiceFS{}
	require.NoError(t, yaml.Unmarshal([]byte(cm.Data["data"]), synced))
	assert.Equal(t, []string{"b"}, namesOf(synced.ImagePullSecrets), "pass 1: configmap records the rotated global secrets")
	assert.Equal(t, []string{"a"}, namesOf(synced.Worker.ImagePullSecrets),
		"pass 1: configmap records the worker override even though the effective list did not change (not recorded at d7e3168)")

	// pass 2: the user removes the override; the worker must fall back to b.
	// getRuntime() re-fetches the CR from the client, so updating it suffices.
	edited := &datav1alpha1.JuiceFSRuntime{}
	require.NoError(t, client.Get(context.TODO(), types.NamespacedName{Namespace: "fluid", Name: "jfsdemo"}, edited))
	edited.Spec.Worker.ImagePullSecrets = nil
	require.NoError(t, client.Update(context.TODO(), edited))
	_, err = engine.SyncRuntime(cruntime.ReconcileRequestContext{})
	require.NoError(t, err)
	sts, err = kubeclient.GetStatefulSet(client, mockedObjects.WorkerSts.Name, mockedObjects.WorkerSts.Namespace)
	require.NoError(t, err)
	assert.Equal(t, []string{"b"}, namesOf(sts.Spec.Template.Spec.ImagePullSecrets),
		"pass 2: removing the override must move the worker to the runtime-wide secrets (stuck on a at d7e3168)")
	cm, err = engine.GetValuesConfigMap()
	require.NoError(t, err)
	synced = &JuiceFS{}
	require.NoError(t, yaml.Unmarshal([]byte(cm.Data["data"]), synced))
	assert.Nil(t, namesOf(synced.Worker.ImagePullSecrets), "pass 2: configmap clears the removed override")
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

// TestJuicefsSampleDocsFuseRestartClaim pins the round-6 docs consistency
// requirement (finding F-docs-fuse-restart-overstated): a secrets change on an
// existing runtime must NOT be documented as restarting the running fuse pods.
// Mechanism (all three pillars re-checked at head a0baafae0):
//   - charts/juicefs/values.yaml ships the fuse DaemonSet updateStrategy.type:
//     OnDelete, and syncFuseSpec flips any non-OnDelete strategy back to OnDelete
//     and returns before syncing the template, so a template update never
//     restarts running pods;
//   - the fuse generation bump is consumed only by CSI NodeUnstage's lazy clean
//     (getCleanFuseFunc), under cleanPolicy OnFuseChanged / OnDemand;
//   - SetupFuseCleanPolicy maps the unset policy to OnRuntimeDeleted, so on the
//     default configuration running fuse pods keep the old secrets until they
//     are deleted.
// Polarity: contract test — RED on 2150b49ef (the round-5 replacement sentence
// said "the fuse pods are restarted" / "fuse pod 会重启"), GREEN on a0baafae0.
func TestJuicefsSampleDocsFuseRestartClaim(t *testing.T) {
	_, thisFile, _, _ := goruntime.Caller(0)
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(thisFile))))

	// Anchor the mechanism in the code so a future behavior change makes this
	// test's premise stale instead of silently pinning a wrong sentence.
	chartValues, err := os.ReadFile(filepath.Join(repoRoot, "charts", "juicefs", "values.yaml"))
	require.NoError(t, err)
	require.True(t, strings.Contains(string(chartValues), "type: OnDelete"),
		"expected charts/juicefs to ship the fuse DaemonSet OnDelete update strategy; if it does not, this test's premise is stale")

	syncCode, err := os.ReadFile(filepath.Join(repoRoot, "pkg", "ddc", "juicefs", "sync_runtime.go"))
	require.NoError(t, err)
	require.True(t, strings.Contains(string(syncCode), "not safe to sync fuse spec"),
		"expected syncFuseSpec to flip non-OnDelete fuse strategies back to OnDelete and return before the template sync; if it does not, this test's premise is stale")

	baseRuntime, err := os.ReadFile(filepath.Join(repoRoot, "pkg", "ddc", "base", "runtime.go"))
	require.NoError(t, err)
	require.True(t, strings.Contains(string(baseRuntime), "Default to set the fuse clean policy to OnRuntimeDeleted"),
		"expected SetupFuseCleanPolicy to default the unset policy to OnRuntimeDeleted; if it does not, this test's premise is stale")

	en, err := os.ReadFile(filepath.Join(repoRoot, "docs", "en", "samples", "image_pull_secrets.md"))
	require.NoError(t, err)
	zh, err := os.ReadFile(filepath.Join(repoRoot, "docs", "zh", "samples", "image_pull_secrets.md"))
	require.NoError(t, err)

	// The overstated claims must be gone in both languages.
	assert.NotContains(t, string(en), "the fuse pods are restarted",
		"docs/en still claims a secrets change restarts the running fuse pods; the fuse DaemonSet is OnDelete and the default clean policy is OnRuntimeDeleted, so no running pod restarts")
	assert.NotContains(t, string(zh), "fuse pod 会重启",
		"docs/zh still claims a secrets change restarts the running fuse pods; the fuse DaemonSet is OnDelete and the default clean policy is OnRuntimeDeleted, so no running pod restarts")

	// The corrected sentence must state the actual default behavior.
	assert.Contains(t, string(en), "OnDelete", "docs/en should name the fuse DaemonSet OnDelete update strategy")
	assert.Contains(t, string(en), "OnRuntimeDeleted", "docs/en should name the default fuse clean policy")
	assert.Contains(t, string(en), "until they are deleted", "docs/en should say running fuse pods keep the old secrets until they are deleted")
	assert.Contains(t, string(zh), "OnDelete", "docs/zh should name the fuse DaemonSet OnDelete update strategy")
	assert.Contains(t, string(zh), "OnRuntimeDeleted", "docs/zh should name the default fuse clean policy")
	assert.Contains(t, string(zh), "直到被删除", "docs/zh should say running fuse pods keep the old secrets until they are deleted")
}

// setupRuntimePortAllocatorForTest isolates the port allocator setup so the
// sync scenarios do not depend on ginkgo's BeforeEach.
func setupRuntimePortAllocatorForTest() error {
	pr := net.ParsePortRangeOrDie("14000-15999")
	return portallocator.SetupRuntimePortAllocator(nil, pr, "bitmap", dummy)
}
