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

// Verification harness for the review of
// https://github.com/fluid-cloudnative/fluid/pull/6175 (additive test file; no
// production code is touched).
//
// Claim P0 (premise of the PR): a CacheRuntimeClass whose topology declares
// only master and worker (no client component), combined with a CacheRuntime
// that leaves spec.client at its default, used to panic the
// cacheruntime-controller with a nil pointer dereference (fixed by #6157,
// commit d1ae1ace). The mooncake e2e case is a regression guard for that.
//
// This test is a CONTRACT test on the PR head (and on master, which contains
// #6157): generateRuntimeConfigData must not panic and must produce a config
// with master+worker but no client section. Grafted onto d1ae1ace^ (pre-fix)
// the same test panics, which reproduces the premise on the base-of-the-fix.
//
// The test additionally pins the JSON wiring that
// test/gha-e2e/mooncake/image/custom-entrypoint.sh reads with jq:
//   .master.service.name, .worker.service.name,
//   .worker.tieredStoreLevels[0].quotas[0]
// If VERIFY_RUNTIME_JSON_OUT is set, the generated runtime.json is written to
// that path so the shell-side harness can run custom-entrypoint.sh against it.

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func verifyP0Scheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := datav1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return scheme
}

// verifyP0Objects mirrors test/gha-e2e/mooncake/{cacheruntimeclass,cacheruntime,dataset}.yaml:
// a client-less topology (master+worker only), a CacheRuntime that does not
// mention spec.client at all (so Client.Disabled is the zero value, false),
// and a Dataset with a single mooncakefs mount.
func verifyP0Objects() (*datav1alpha1.CacheRuntime, *datav1alpha1.CacheRuntimeClass, *datav1alpha1.Dataset) {
	headless := &datav1alpha1.HeadlessRuntimeComponentService{}
	runtimeObj := &datav1alpha1.CacheRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: "mooncake-demo", Namespace: "default"},
		Spec: datav1alpha1.CacheRuntimeSpec{
			RuntimeClassName: "mooncake-demo",
			Master:           datav1alpha1.CacheRuntimeMasterSpec{Replicas: 1},
			Worker: datav1alpha1.CacheRuntimeWorkerSpec{
				Replicas: 1,
				TieredStore: datav1alpha1.RuntimeTieredStore{
					Levels: []datav1alpha1.RuntimeTieredStoreLevel{
						{
							EmptyDir: &datav1alpha1.EmptyDirMediumSource{
								Quota: resource.MustParse("1Gi"),
							},
							High: "0.8",
							Low:  "0.5",
						},
					},
				},
			},
			// no spec.client at all: zero value => Disabled=false
		},
	}
	runtimeClass := &datav1alpha1.CacheRuntimeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "mooncake-demo"},
		Topology: &datav1alpha1.RuntimeTopology{
			Master: &datav1alpha1.RuntimeComponentDefinition{
				Service: datav1alpha1.RuntimeComponentService{
					ComponentServiceConfig: datav1alpha1.ComponentServiceConfig{Headless: headless},
				},
			},
			Worker: &datav1alpha1.RuntimeComponentDefinition{
				Service: datav1alpha1.RuntimeComponentService{
					ComponentServiceConfig: datav1alpha1.ComponentServiceConfig{Headless: headless},
				},
			},
			// no client component: this is the client-less topology
		},
	}
	dataset := &datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: "mooncake-demo", Namespace: "default"},
		Spec: datav1alpha1.DatasetSpec{
			Mounts: []datav1alpha1.Mount{
				{Name: "mc", MountPoint: "mooncakefs:///"},
			},
		},
	}
	return runtimeObj, runtimeClass, dataset
}

func TestClientLessTopologyGeneratesRuntimeConfig_P0Verify(t *testing.T) {
	scheme := verifyP0Scheme(t)
	runtimeObj, runtimeClass, dataset := verifyP0Objects()
	c := fake.NewFakeClientWithScheme(scheme, runtimeObj, runtimeClass, dataset)
	e := &CacheEngine{Client: c, name: "mooncake-demo", namespace: "default"}

	// A panic here is exactly the regression the e2e case guards; on pre-#6157
	// code this test panics with a nil pointer dereference.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("generateRuntimeConfigData panicked on a client-less topology: %v", r)
		}
	}()

	data, err := e.generateRuntimeConfigData(context.Background(), runtimeObj)
	if err != nil {
		t.Fatalf("expected no error for a client-less topology, got %v", err)
	}

	raw, ok := data["runtime.json"] // same name as e.getRuntimeConfigFileName()
	if !ok {
		t.Fatalf("expected runtime config data key %q, got keys %v", "runtime.json", data)
	}

	if out := os.Getenv("VERIFY_RUNTIME_JSON_OUT"); out != "" {
		if err := os.WriteFile(out, []byte(raw), 0644); err != nil {
			t.Fatalf("failed to write runtime.json fixture: %v", err)
		}
		t.Logf("wrote runtime.json fixture to %s", out)
	}

	var cfg common.CacheRuntimeConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("runtime.json is not a CacheRuntimeConfig: %v", err)
	}

	if cfg.Client != nil {
		t.Fatalf("client-less topology must not produce a client config, got %+v", cfg.Client)
	}
	if cfg.Master == nil || cfg.Worker == nil {
		t.Fatalf("expected master and worker configs, got master=%+v worker=%+v", cfg.Master, cfg.Worker)
	}

	// jq wiring asserted by custom-entrypoint.sh:
	//   MASTER_SVC=$(jq -r '.master.service.name')
	//   WORKER_SVC=$(jq -r '.worker.service.name')
	//   QUOTA=$(jq -r '.worker.tieredStoreLevels[0].quotas[0]')
	if cfg.Master.Service.Name != "svc-mooncake-demo-master" {
		t.Fatalf("expected .master.service.name=svc-mooncake-demo-master, got %q", cfg.Master.Service.Name)
	}
	if cfg.Worker.Service.Name != "svc-mooncake-demo-worker" {
		t.Fatalf("expected .worker.service.name=svc-mooncake-demo-worker, got %q", cfg.Worker.Service.Name)
	}
	if len(cfg.Worker.TieredStoreLevels) == 0 || len(cfg.Worker.TieredStoreLevels[0].Quotas) == 0 {
		t.Fatalf("expected .worker.tieredStoreLevels[0].quotas to be non-empty, got %+v", cfg.Worker.TieredStoreLevels)
	}
	if got := cfg.Worker.TieredStoreLevels[0].Quotas[0]; got != "1Gi" {
		t.Fatalf("expected .worker.tieredStoreLevels[0].quotas[0]=1Gi, got %q", got)
	}

	// The shell script reads the raw JSON with jq, so pin the exact key casing too.
	var generic map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &generic); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"master", "worker"} {
		if _, ok := generic[key]; !ok {
			t.Fatalf("runtime.json missing top-level key %q", key)
		}
	}
	if _, ok := generic["client"]; ok {
		t.Fatalf("runtime.json must not contain a client section for a client-less topology")
	}
}
