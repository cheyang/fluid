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

// Verification harness for the review of PR #6197 (reviewer B / Codex), round 5.
// Additive test file only; no production code is changed.
//
// A-F4 (open at 41b47141): the first clientless admission reads the runtime-config
// ConfigMap through the webhook's cached client, starting a cluster-wide ConfigMap
// informer. Round 5 (bdd955926) answers with a code comment: "the webhook already caches
// config maps for the fuse injection, so this adds no informer". This contract proves the
// premise of that answer: the fuse injection path (injectCheckMountReadyScript, reached
// by every serverless pod that mounts a dataset) issues a ConfigMap Get through the
// plugin's client - which in production is the webhook's mgr.GetClient(), i.e. the same
// cached client - so the ConfigMap informer already exists in any cluster that uses fuse
// injection, and the clientless read adds no new one.
// Polarity: contract; green on base and head (the fuse path is pre-existing).
package fuse

import (
	"context"
	"sync"
	"testing"

	"github.com/fluid-cloudnative/fluid/pkg/application/inject/fuse/mutator"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/ddc/base"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// getCountingClient wraps a client and counts Get calls per object type.
type getCountingClient struct {
	client.Client
	mu   sync.Mutex
	gets map[string]int
}

func (c *getCountingClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	c.mu.Lock()
	c.gets[reflect.TypeOf(obj).String()]++
	c.mu.Unlock()
	return c.Client.Get(ctx, key, obj, opts...)
}

func TestVerifyPR6197FusePathReadsConfigMapsThroughPluginClient(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	counting := &getCountingClient{
		Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
		gets:   map[string]int{},
	}
	injector := &Injector{client: counting, log: logr.Discard()}

	info, err := base.BuildRuntimeInfo("demo", "default", common.ThinRuntime)
	if err != nil {
		t.Fatalf("build runtime info: %v", err)
	}
	runtimeInfos := map[string]base.RuntimeInfoInterface{"demo": info}
	podSpecs := &mutator.MutatingPodSpecs{
		MetaObj: metav1.ObjectMeta{Name: "app", Namespace: "default"},
		Volumes: []corev1.Volume{{
			Name: "data",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "demo"},
			},
		}},
		Containers: []corev1.Container{{
			Name:         "app",
			VolumeMounts: []corev1.VolumeMount{{Name: "data", MountPath: "/data"}},
		}},
	}

	if err := injector.injectCheckMountReadyScript(podSpecs, runtimeInfos); err != nil {
		t.Fatalf("injectCheckMountReadyScript: %v", err)
	}
	if counting.gets["*v1.ConfigMap"] == 0 {
		t.Fatalf("the fuse injection path must read ConfigMaps through the plugin's client " +
			"(the webhook's cached client in production); if that changes, the A-F4 justification " +
			"'the webhook already caches config maps for the fuse injection' no longer holds")
	}
}
