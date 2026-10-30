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

// P0 premise check, CRD half (Reviewer B / Codex). Run ONLY with CODEX_CRD_DIR
// pointing at the BASE branch's CRD directory (which has no scale subresource):
// GET /scale must fail there, while it succeeds with the PR's CRD.
// Polarity: this is a CONTRACT test for the head CRD and the canary probe for
// the premise on the base CRD.

package codexhpa

import (
	"context"
	"os"
	"strings"
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestCacheRuntimeScalePremiseAgainstCRDDir(t *testing.T) {
	if os.Getenv("CODEX_CRD_DIR") == "" {
		t.Skip("set CODEX_CRD_DIR to a CRD dir to probe the scale subresource there")
	}
	env := startScaleEnv(t)

	r := &datav1alpha1.CacheRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: "hpa-demo", Namespace: verifyNS},
		Spec: datav1alpha1.CacheRuntimeSpec{
			RuntimeClassName: "curvine",
			Worker:           datav1alpha1.CacheRuntimeWorkerSpec{Replicas: 2},
		},
	}
	if err := env.client.Create(context.TODO(), r); err != nil {
		t.Fatalf("create: %v", err)
	}

	out, err := env.getScale(t, "hpa-demo")
	if os.Getenv("CODEX_CRD_DIR_EXPECT_SCALE") == "absent" {
		// base CRD: no scale subresource -> GET must fail
		if err == nil {
			t.Fatalf("PREMISE REFUTED: GET /scale unexpectedly succeeded against CRDs at CODEX_CRD_DIR: %s", out)
		}
		t.Logf("PREMISE REPRODUCED (base CRD): GET /scale fails: %s", strings.TrimSpace(out))
		return
	}

	// head CRD: GET must succeed
	if err != nil {
		t.Fatalf("GET /scale failed against CRDs at CODEX_CRD_DIR: %v\n%s", err, out)
	}
	if !strings.Contains(out, `"spec":{"replicas":2}`) {
		t.Fatalf("unexpected scale payload: %s", out)
	}
	got := &datav1alpha1.CacheRuntime{}
	if err := env.client.Get(context.TODO(), types.NamespacedName{Name: "hpa-demo", Namespace: verifyNS}, got); err != nil {
		t.Fatalf("get: %v", err)
	}
}
