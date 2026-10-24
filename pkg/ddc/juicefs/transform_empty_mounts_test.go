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

// Review verification harness for PR #5674 (fluid-cloudnative/fluid).
// Finding F1 (contract polarity: RED on the PR head, expected to go GREEN once fixed).
//
// Claim under test: JuiceFSEngine.transform must return an error (not panic) when the
// bound Dataset has zero mounts.
//
// Mechanism under test: a Dataset auto-created by RuntimeReconciler.ensureDatasetForRuntime
// is empty (no spec.mounts) until the user populates it. The reconcile chain
// ReconcileRuntime -> TemplateEngine.Setup -> ShouldSetupMaster -> SetupMaster ->
// installJuiceFS -> generateJuicefsValueFile -> transform reaches
// `j.genEdition(dataset.Spec.Mounts[0], ...)` (transform.go) with no empty-mounts guard,
// so an auto-created Dataset panics the reconcile goroutine ("index out of range").
// Note transformFuse has the guard ("do not assign mount point"), but it runs after
// genEdition, which indexes first.

import (
	"testing"

	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestTransformWithEmptyMountsDataset(t *testing.T) {
	// The state the auto-created Dataset is in: exists, bound, no spec.mounts.
	dataset := &datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "hbase",
			Namespace: "fluid",
		},
		Spec: datav1alpha1.DatasetSpec{},
	}
	testRuntime := &datav1alpha1.JuiceFSRuntime{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "hbase",
			Namespace: "fluid",
		},
	}

	client := fake.NewFakeClientWithScheme(testScheme, dataset, testRuntime)

	engine := &JuiceFSEngine{
		name:        "hbase",
		namespace:   "fluid",
		runtimeType: common.JuiceFSRuntime,
		Log:         fake.NullLogger(),
		Client:      client,
	}
	engine.Client = client

	// transform reads j.runtimeInfo.GetOwnerDatasetUID(); initialize it the way the
	// engine does before the reconcile reaches transform.
	if _, err := engine.getRuntimeInfo(); err != nil {
		t.Fatalf("failed to build runtime info: %v", err)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("transform panicked on a Dataset with empty spec.mounts (the auto-created Dataset state): %v", r)
		}
	}()

	_, err := engine.transform(testRuntime)
	if err == nil {
		t.Fatalf("expected transform to fail with an explicit error for empty spec.mounts, got nil")
	}
}
