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

package juicefs

// Verification harness for PR #6283 / issue #6282.
// Branch: verify/pr6283-juicefs-cmd-options-claude (reviewer branch, additive
// only — no production code is modified by this commit).
//
// POLARITY: every test below is a CONTRACT test — it asserts the intended
// correct behavior. Each one FAILS on the merge base 253f6bb8 (where the
// "-o" splitting bug lives) and PASSES on the PR head cf975a08 (where
// isCommandChanged cuts at the standalone " -o " argument).
//
// TestVerifyP0  - premise of issue #6282, reproduced through the REAL command
//                 generator (genFuseMount), dataset name contains "-o".
// TestVerifyV1  - same premise with an option NAME containing "-o"
//                 (cache-partial-only), direct strings from the issue.
// TestVerifyV2  - guard against over-reporting: same options reordered must
//                 stay "not changed" (passes on base AND head).
// TestVerifyV3  - user-visible lifecycle contract through syncWorkerSpec:
//                 the worker script ConfigMap is rewritten and the worker
//                 StatefulSet is rolled out when only cache-size changes for
//                 a dataset whose name contains "-o".

import (
	"context"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	cruntime "github.com/fluid-cloudnative/fluid/pkg/runtime"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
)

// buildFuseCommandViaRealGenerator builds the fuse mount command exactly the
// way the engine does at runtime, for a dataset named dsName, so the strings
// under test cannot drift away from the produced format.
func buildFuseCommandViaRealGenerator(t *testing.T, dsName, cacheSize string) string {
	t.Helper()
	j := &JuiceFSEngine{
		name:      dsName,
		namespace: "default",
		Log:       fake.NullLogger(),
		Client:    fake.NewFakeClientWithScheme(testScheme),
	}
	value := &JuiceFS{
		Edition: CommunityEdition,
		Source:  "my-vol",
		Fuse: Fuse{
			MountPath: "/runtime-mnt/juicefs/default/" + dsName + "/juicefs-fuse",
		},
	}
	options := map[string]string{
		"cache-dir":  "/dev/shm",
		"cache-size": cacheSize,
	}
	if err := j.genFuseMount(value, options); err != nil {
		t.Fatalf("genFuseMount failed: %v", err)
	}
	return value.Fuse.Command
}

// TestVerifyP0_GenFuseMount_DatasetNameContainsDashO is the premise check
// (claim P0, run against the BASE branch): issue #6282 says
// "a dataset called demo-output ... a changed option such as cache-size is
// never seen: the mount script ConfigMap is not updated".
func TestVerifyP0_GenFuseMount_DatasetNameContainsDashO(t *testing.T) {
	current := buildFuseCommandViaRealGenerator(t, "demo-output", "1024")
	latest := buildFuseCommandViaRealGenerator(t, "demo-output", "2048")
	t.Logf("current: %s", current)
	t.Logf("latest : %s", latest)

	if !strings.Contains(current, " -o ") {
		t.Fatalf("generated command does not contain the standalone \" -o \" separator: %q", current)
	}
	j := JuiceFSEngine{Log: fake.NullLogger()}
	changed, _ := j.isCommandChanged(current, latest)
	if !changed {
		t.Errorf("cache-size 1024 -> 2048 with dataset \"demo-output\" was NOT detected as a command change; " +
			"the mount script ConfigMap would not be updated (issue #6282 symptom)")
	}
}

// TestVerifyV1_OptionNameContainsDashO: issue #6282 also reports
// "The same happens with an option name that contains -o, e.g.
// cache-partial-only,cache-size=1024 vs cache-partial-only,cache-size=2048".
func TestVerifyV1_OptionNameContainsDashO(t *testing.T) {
	current := "exec /bin/mount.juicefs my-vol /runtime-mnt/juicefs/default/demo/juicefs-fuse -o cache-partial-only,cache-size=1024"
	latest := "exec /bin/mount.juicefs my-vol /runtime-mnt/juicefs/default/demo/juicefs-fuse -o cache-partial-only,cache-size=2048"
	j := JuiceFSEngine{Log: fake.NullLogger()}
	changed, _ := j.isCommandChanged(current, latest)
	if !changed {
		t.Errorf("cache-size 1024 -> 2048 with option \"cache-partial-only\" was NOT detected as a command change")
	}
}

// TestVerifyV2_ReorderedOptionsStayUnchanged guards the fix against
// over-reporting: identical options in a different order (metrics excluded as
// before) must remain "not changed" even when the dataset name contains "-o".
// This one passes on the base branch as well; it exists so a future rewrite
// of the parser cannot silently start rolling out workers on every reconcile.
func TestVerifyV2_ReorderedOptionsStayUnchanged(t *testing.T) {
	current := "exec /bin/mount.juicefs my-vol /runtime-mnt/juicefs/default/demo-output/juicefs-fuse -o cache-dir=/dev/shm,cache-size=1024,metrics=0.0.0.0:9567"
	latest := "exec /bin/mount.juicefs my-vol /runtime-mnt/juicefs/default/demo-output/juicefs-fuse -o metrics=0.0.0.0:9567,cache-size=1024,cache-dir=/dev/shm"
	j := JuiceFSEngine{Log: fake.NullLogger()}
	changed, _ := j.isCommandChanged(current, latest)
	if changed {
		t.Errorf("same options in a different order were reported as a command change; " +
			"this would roll out the worker StatefulSet on every reconcile")
	}
}

// TestVerifyV3_SyncWorkerSpec_UpdatesScriptConfigmap exercises the full
// lifecycle contract from the issue at the controller layer (fake API
// server): "the mount script ConfigMap is not updated and the worker
// StatefulSet template does not get its restart annotation". After the fix,
// changing only cache-size for dataset "demo-output" must rewrite the worker
// script ConfigMap's mount line and report the worker as changed.
func TestVerifyV3_SyncWorkerSpec_UpdatesScriptConfigmap(t *testing.T) {
	const (
		name = "demo-output"
		ns   = "default"
	)
	oldCommand := "exec /bin/mount.juicefs my-vol /runtime-mnt/juicefs/default/demo-output/juicefs-fuse -o cache-dir=/dev/shm,cache-size=1024,metrics=0.0.0.0:9567"
	newCommand := "exec /bin/mount.juicefs my-vol /runtime-mnt/juicefs/default/demo-output/juicefs-fuse -o cache-dir=/dev/shm,cache-size=2048,metrics=0.0.0.0:9567"

	runtimeObj := &datav1alpha1.JuiceFSRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
	}
	workerSts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: name + "-worker", Namespace: ns},
		Spec: appsv1.StatefulSetSpec{
			// must already be OnDelete, otherwise syncWorkerSpec switches the
			// strategy and returns before the command comparison runs
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.OnDeleteStatefulSetStrategyType,
			},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "juicefs"}},
				},
			},
		},
	}
	workerScriptCm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: name + "-worker-script", Namespace: ns},
		Data: map[string]string{
			"script.sh": "#!/bin/bash\nset -e\necho \"juicefs mount start.\"\n" + oldCommand + "\n",
		},
	}

	client := fake.NewFakeClientWithScheme(testScheme, runtimeObj, workerSts, workerScriptCm)
	e := &JuiceFSEngine{
		runtime:   runtimeObj,
		name:      name,
		namespace: ns,
		Log:       fake.NullLogger(),
		Client:    client,
	}

	changed, err := e.syncWorkerSpec(cruntime.ReconcileRequestContext{}, runtimeObj, &JuiceFS{}, &JuiceFS{
		Worker: Worker{Command: newCommand},
	})
	if err != nil {
		t.Fatalf("syncWorkerSpec returned err = %v, want nil", err)
	}
	if !changed {
		t.Errorf("syncWorkerSpec changed = false, want true: the changed cache-size was not propagated to the worker")
	}

	// user-visible contract #1: the mount script ConfigMap is updated
	gotCm := &corev1.ConfigMap{}
	if err := client.Get(context.TODO(), types.NamespacedName{Namespace: ns, Name: name + "-worker-script"}, gotCm); err != nil {
		t.Fatalf("failed to re-read worker script configmap: %v", err)
	}
	lines := strings.Split(gotCm.Data["script.sh"], "\n")
	lastLine := ""
	for i := len(lines) - 1; i >= 0; i-- {
		if lines[i] != "" {
			lastLine = lines[i]
			break
		}
	}
	if lastLine != newCommand {
		t.Errorf("worker script configmap mount line was not updated:\n got: %q\nwant: %q", lastLine, newCommand)
	}

	// user-visible contract #2: the worker StatefulSet template gets its
	// restart annotation so the new script is picked up
	gotSts := &appsv1.StatefulSet{}
	if err := client.Get(context.TODO(), types.NamespacedName{Namespace: ns, Name: name + "-worker"}, gotSts); err != nil {
		t.Fatalf("failed to re-read worker statefulset: %v", err)
	}
	if _, ok := gotSts.Spec.Template.ObjectMeta.Annotations["kubectl.kubernetes.io/restartedAt"]; !ok {
		t.Errorf("worker statefulset did not get the kubectl.kubernetes.io/restartedAt restart annotation")
	}
}
