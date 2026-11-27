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

// Verification harness for fluid-cloudnative/fluid PR #6293 (reviewer: codex).
// Additive test file only; it does not modify production code.
//
// Claim under test (acceptance contract): the shipped example manifest
// addons/curvine/runtime-profile.yaml must (a) parse as a valid
// datav1alpha1.ThinRuntimeProfile and (b) have its fuse image/imageTag/
// imagePullPolicy/command/args honored by ThinEngine.transformFuse, i.e. flow
// into the rendered fuse values. This is the contract the docs PR relies on.

package thin

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"
)

const pr6293AddonProfilePath = "../../../addons/curvine/runtime-profile.yaml"

func loadPR6293AddonProfile(t *testing.T) *datav1alpha1.ThinRuntimeProfile {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(pr6293AddonProfilePath))
	if err != nil {
		t.Fatalf("cannot read shipped addon profile %s: %v", pr6293AddonProfilePath, err)
	}
	profile := &datav1alpha1.ThinRuntimeProfile{}
	if err := yaml.Unmarshal(raw, profile); err != nil {
		t.Fatalf("shipped addon profile does not unmarshal into ThinRuntimeProfile: %v", err)
	}
	return profile
}

// TestVerifyPR6293CurvineAddonProfileParses proves the shipped example is a
// schema-valid ThinRuntimeProfile carrying exactly the fields the docs claim.
func TestVerifyPR6293CurvineAddonProfileParses(t *testing.T) {
	profile := loadPR6293AddonProfile(t)

	if profile.Kind != "ThinRuntimeProfile" {
		t.Fatalf("kind = %q, want ThinRuntimeProfile", profile.Kind)
	}
	if profile.Spec.FileSystemType != "fuse" {
		t.Fatalf("fileSystemType = %q, want fuse", profile.Spec.FileSystemType)
	}
	fuse := profile.Spec.Fuse
	if fuse.Image != "ghcr.io/curvineio/curvine" {
		t.Errorf("fuse.image = %q, want ghcr.io/curvineio/curvine", fuse.Image)
	}
	if fuse.ImageTag != "latest" {
		t.Errorf("fuse.imageTag = %q, want latest", fuse.ImageTag)
	}
	if fuse.ImagePullPolicy != "IfNotPresent" {
		t.Errorf("fuse.imagePullPolicy = %q, want IfNotPresent", fuse.ImagePullPolicy)
	}
	if !reflect.DeepEqual(fuse.Command, []string{"/entrypoint.sh"}) {
		t.Errorf("fuse.command = %v, want [/entrypoint.sh]", fuse.Command)
	}
	if !reflect.DeepEqual(fuse.Args, []string{"fluid-thin-runtime"}) {
		t.Errorf("fuse.args = %v, want [fluid-thin-runtime]", fuse.Args)
	}
}

// TestVerifyPR6293CurvineProfileFlowsIntoFuseValue proves the thin engine
// transform honors the profile's image/command/args (the mechanism the addon
// example depends on end-to-end).
func TestVerifyPR6293CurvineProfileFlowsIntoFuseValue(t *testing.T) {
	profile := loadPR6293AddonProfile(t)
	dataset, thinruntime, _ := mockFluidObjectsForTests(types.NamespacedName{Name: "curvine-demo", Namespace: "default"})
	engine := mockThinEngineForTests(dataset, thinruntime, profile)
	engine.Client = fake.NewFakeClientWithScheme(datav1alpha1.UnitTestScheme, []runtime.Object{dataset, thinruntime, profile}...)

	value := &ThinValue{}
	if err := engine.transformFuse(thinruntime, profile, dataset, value); err != nil {
		t.Fatalf("transformFuse returned error: %v", err)
	}

	if value.Fuse.Image != "ghcr.io/curvineio/curvine" {
		t.Errorf("value.Fuse.Image = %q, want ghcr.io/curvineio/curvine", value.Fuse.Image)
	}
	if value.Fuse.ImageTag != "latest" {
		t.Errorf("value.Fuse.ImageTag = %q, want latest", value.Fuse.ImageTag)
	}
	if value.Fuse.ImagePullPolicy != "IfNotPresent" {
		t.Errorf("value.Fuse.ImagePullPolicy = %q, want IfNotPresent", value.Fuse.ImagePullPolicy)
	}
	if !reflect.DeepEqual(value.Fuse.Command, []string{"/entrypoint.sh"}) {
		t.Errorf("value.Fuse.Command = %v, want [/entrypoint.sh]", value.Fuse.Command)
	}
	if !reflect.DeepEqual(value.Fuse.Args, []string{"fluid-thin-runtime"}) {
		t.Errorf("value.Fuse.Args = %v, want [fluid-thin-runtime]", value.Fuse.Args)
	}
}
