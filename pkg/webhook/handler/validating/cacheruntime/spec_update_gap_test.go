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

package cacheruntime_test

// F3 (admission side) — canary documenting that ValidateSpecUpdate treats the
// WHOLE master/worker `runtimeVersion` as updatable, including
// `imagePullPolicy`, which the controller never syncs
// (AdvancedStatefulSetManager.updateImage patches container.Image only).
//
// Polarity: canary. It passes while the over-permissive allowlist exists. If
// runtimeVersion.imagePullPolicy becomes immutable (or becomes actually
// synced), this flips red and must be inverted.

import (
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/webhook/handler/validating/cacheruntime"
)

func TestValidateSpecUpdateAllowsImagePullPolicyChange(t *testing.T) {
	oldSpec := imagePullPolicyBaseSpec()
	newSpec := imagePullPolicyBaseSpec()
	newSpec.Master.RuntimeVersion.ImagePullPolicy = "Never"
	newSpec.Worker.RuntimeVersion.ImagePullPolicy = "Always"

	errs := cacheruntime.ValidateSpecUpdate(&oldSpec, &newSpec)
	if len(errs) != 0 {
		t.Fatalf("a runtimeVersion.imagePullPolicy change was rejected (%v), but the controller sync never propagates imagePullPolicy — the allowlist and the sync disagree the other way too", errs)
	}
}

func imagePullPolicyBaseSpec() datav1alpha1.CacheRuntimeSpec {
	return datav1alpha1.CacheRuntimeSpec{
		RuntimeClassName: "test-class",
		Master: datav1alpha1.CacheRuntimeMasterSpec{
			RuntimeComponentCommonSpec: datav1alpha1.RuntimeComponentCommonSpec{
				RuntimeVersion: datav1alpha1.VersionSpec{Image: "test-master", ImageTag: "v1", ImagePullPolicy: "IfNotPresent"},
			},
			Replicas: 1,
		},
		Worker: datav1alpha1.CacheRuntimeWorkerSpec{
			RuntimeComponentCommonSpec: datav1alpha1.RuntimeComponentCommonSpec{
				RuntimeVersion: datav1alpha1.VersionSpec{Image: "test-worker", ImageTag: "v1", ImagePullPolicy: "IfNotPresent"},
			},
			Replicas: 1,
		},
	}
}
