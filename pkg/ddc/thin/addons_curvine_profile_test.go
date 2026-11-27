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

package thin

// Review harness for PR #6293 (addons/curvine: use the published Curvine image).
//
// Contract test (polarity: contract — FAILS if the shipped addon manifest stops
// being honored by the thin controller). It decodes the real
// addons/curvine/runtime-profile.yaml (not a re-typed copy) and runs it through
// ThinEngine.transformFuse, asserting the fuse image / command / args the PR
// sets actually reach the values that render the FUSE daemonset:
//
//	image: ghcr.io/curvineio/curvine, imageTag: latest
//	command: [/entrypoint.sh], args: [fluid-thin-runtime]
//
// If someone edits the addon manifest or breaks profile->fuse propagation
// (pkg/ddc/thin/transform_fuse.go parseFromProfileFuse), this test goes red.

import (
	"os"
	"reflect"
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	"sigs.k8s.io/yaml"

	"k8s.io/apimachinery/pkg/types"
)

func TestCurvineAddonProfilePropagation(t *testing.T) {
	const addonManifest = "../../../addons/curvine/runtime-profile.yaml"

	data, err := os.ReadFile(addonManifest)
	if err != nil {
		t.Fatalf("cannot read addon manifest %s: %v", addonManifest, err)
	}

	profileFromManifest := &datav1alpha1.ThinRuntimeProfile{}
	if err := yaml.UnmarshalStrict(data, profileFromManifest); err != nil {
		t.Fatalf("addons/curvine/runtime-profile.yaml does not decode into a ThinRuntimeProfile: %v", err)
	}

	dataset, thinruntime, profile := mockFluidObjectsForTests(types.NamespacedName{Name: "test-dataset", Namespace: "default"})
	// replace the mock profile spec with the exact spec shipped in the addon
	profile.Spec = profileFromManifest.Spec

	engine := mockThinEngineForTests(dataset, thinruntime, profile)
	engine.Client = fake.NewFakeClientWithScheme(datav1alpha1.UnitTestScheme, dataset, thinruntime, profile)

	value := &ThinValue{}
	if err := engine.transformFuse(thinruntime, profile, dataset, value); err != nil {
		t.Fatalf("transformFuse failed with the addon profile: %v", err)
	}

	wantImage := "ghcr.io/curvineio/curvine"
	if value.Fuse.Image != wantImage {
		t.Errorf("fuse image: want %q, got %q", wantImage, value.Fuse.Image)
	}
	if value.Fuse.ImageTag != "latest" {
		t.Errorf("fuse imageTag: want %q, got %q", "latest", value.Fuse.ImageTag)
	}
	if value.Fuse.ImagePullPolicy != "IfNotPresent" {
		t.Errorf("fuse imagePullPolicy: want %q, got %q", "IfNotPresent", value.Fuse.ImagePullPolicy)
	}
	if want := []string{"/entrypoint.sh"}; !reflect.DeepEqual(value.Fuse.Command, want) {
		t.Errorf("fuse command: want %v, got %v", want, value.Fuse.Command)
	}
	if want := []string{"fluid-thin-runtime"}; !reflect.DeepEqual(value.Fuse.Args, want) {
		t.Errorf("fuse args: want %v, got %v", want, value.Fuse.Args)
	}
}
