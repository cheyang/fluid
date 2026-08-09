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

// Reviewer-private verification harness for PR #6157.
//
// PR #6157 states: "cm.go is the only place in the repo where this nil check is
// missing; transform.go already guards the same fields."
//
// These tests probe the OTHER `runtimeClass.Topology` dereferences that guard the
// sub-component but not `Topology` itself. They are CANARIES: each asserts that the
// panic still happens WITH PR #6157 applied. A canary is "fixed" only when it flips
// (stops panicking) -- at which point invert the assertion.

import (
	"context"
	"testing"

	"github.com/go-logr/logr"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	cruntime "github.com/fluid-cloudnative/fluid/pkg/runtime"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// didPanic reports whether fn panicked, and with what value.
func didPanic(fn func()) (panicked bool, val interface{}) {
	defer func() {
		if r := recover(); r != nil {
			panicked, val = true, r
		}
	}()
	fn()
	return false, nil
}

func verifyScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	_ = datav1alpha1.AddToScheme(s)
	return s
}

// verifyRuntime returns a CacheRuntime with all three components enabled
// (Disabled defaults to false, matching a spec that omits them).
func verifyRuntime() *datav1alpha1.CacheRuntime {
	return &datav1alpha1.CacheRuntime{
		TypeMeta: metav1.TypeMeta{APIVersion: "data.fluid.io/v1alpha1", Kind: datav1alpha1.CacheRuntimeKind},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo",
			Namespace: "default",
			UID:       "demo-uid",
		},
		Spec: datav1alpha1.CacheRuntimeSpec{RuntimeClassName: "test-class"},
	}
}

// verifyClassNoTopology is a CacheRuntimeClass that omits `topology` entirely.
// The CRD marks Topology as +optional, there is no validating webhook for
// CacheRuntimeClass, and CacheEngine.Validate is a no-op -- so this object is
// accepted by the API server as-is.
func verifyClassNoTopology() *datav1alpha1.CacheRuntimeClass {
	return &datav1alpha1.CacheRuntimeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "test-class"},
		Topology:   nil,
		DataOperationSpecs: []datav1alpha1.DataOperationSpec{
			{
				Name:    "DataLoad",
				Command: []string{"/usr/local/bin/dataload"},
				Args:    []string{"--config", "/etc/fluid/config/runtime.json"},
			},
		},
	}
}

func verifyDataset() *datav1alpha1.Dataset {
	return &datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "default"},
		Spec: datav1alpha1.DatasetSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadOnlyMany},
			Mounts:      []datav1alpha1.Mount{{Name: "hbase", MountPoint: "local:///data", Path: "/data"}},
		},
		Status: datav1alpha1.DatasetStatus{
			Runtimes: []datav1alpha1.Runtime{{Name: "demo", Type: common.CacheRuntime}},
		},
	}
}

// F1 -- dataload.go:97. genDataLoadValue derefs runtimeClass.Topology.Worker with
// no Topology==nil guard. Reached from the DataLoad controller, which is a
// completely separate entry point from the runtime controller: transform.go's
// guard is not on this call graph at all.
func TestVerifyF1_GenDataLoadValueNilTopologyStillPanics(t *testing.T) {
	scheme := verifyScheme()
	runtimeObj := verifyRuntime()
	runtimeClass := verifyClassNoTopology()
	dataset := verifyDataset()

	engine := &CacheEngine{
		Client:    fake.NewFakeClientWithScheme(scheme, runtimeObj, runtimeClass, dataset),
		name:      "demo",
		namespace: "default",
	}
	dataload := &datav1alpha1.DataLoad{
		ObjectMeta: metav1.ObjectMeta{Name: "test-load", Namespace: "default"},
		Spec: datav1alpha1.DataLoadSpec{
			Dataset: datav1alpha1.TargetDataset{Name: "demo", Namespace: "default"},
			Policy:  datav1alpha1.Once,
			Target:  []datav1alpha1.TargetPath{{Path: "/data"}},
		},
	}
	ctx := cruntime.ReconcileRequestContext{Context: context.Background()}

	panicked, val := didPanic(func() {
		_, _ = engine.genDataLoadValue(ctx, dataset, runtimeObj, runtimeClass, dataload)
	})

	if !panicked {
		t.Fatalf("CANARY FLIPPED: genDataLoadValue no longer panics on nil Topology. " +
			"dataload.go:97 appears fixed -- invert this assertion.")
	}
	t.Logf("still panics (as expected, unfixed by PR #6157): %v", val)
}

// F1b -- same defect as F1, but entered one hop higher at generateDataLoadValueFile,
// which is what the DataLoad operation actually calls. The runtimeClass is NOT passed
// in by the test: the engine loads it from the API via getRuntimeClass. This removes
// any "the reviewer hand-crafted the object" objection -- a stored CacheRuntimeClass
// with no `topology` is enough.
func TestVerifyF1b_GenerateDataLoadValueFileNilTopologyStillPanics(t *testing.T) {
	scheme := verifyScheme()
	runtimeObj := verifyRuntime()
	runtimeClass := verifyClassNoTopology()
	dataset := verifyDataset()

	engine := &CacheEngine{
		Client:      fake.NewFakeClientWithScheme(scheme, runtimeObj, runtimeClass, dataset),
		name:        "demo",
		namespace:   "default",
		runtimeType: common.CacheRuntime,
		Log:         logr.Discard(),
	}
	dataload := &datav1alpha1.DataLoad{
		ObjectMeta: metav1.ObjectMeta{Name: "test-load", Namespace: "default"},
		Spec: datav1alpha1.DataLoadSpec{
			Dataset: datav1alpha1.TargetDataset{Name: "demo", Namespace: "default"},
			Policy:  datav1alpha1.Once,
			Target:  []datav1alpha1.TargetPath{{Path: "/data"}},
		},
	}
	ctx := cruntime.ReconcileRequestContext{Context: context.Background(), Client: engine.Client}

	panicked, val := didPanic(func() {
		_, _ = engine.generateDataLoadValueFile(ctx, dataload)
	})

	if !panicked {
		t.Fatalf("CANARY FLIPPED: generateDataLoadValueFile no longer panics on a stored "+
			"topology-less CacheRuntimeClass -- the DataLoad path appears fixed, invert this. (val=%v)", val)
	}
	t.Logf("still panics (as expected, unfixed by PR #6157): %v", val)
}

// F2 -- image.go:29. getDataOperationImage derefs runtimeClass.Topology.Worker
// with no Topology==nil guard. Called directly to isolate it from F1, which
// panics first on the shared DataLoad path.
func TestVerifyF2_GetDataOperationImageNilTopologyStillPanics(t *testing.T) {
	engine := &CacheEngine{name: "demo", namespace: "default"}

	panicked, val := didPanic(func() {
		_, _ = engine.getDataOperationImage(verifyRuntime(), verifyClassNoTopology())
	})

	if !panicked {
		t.Fatalf("CANARY FLIPPED: getDataOperationImage no longer panics on nil Topology. " +
			"image.go:29 appears fixed -- invert this assertion.")
	}
	t.Logf("still panics (as expected, unfixed by PR #6157): %v", val)
}

// F3 -- sync.go:190/214. syncRuntimeSpec derefs runtimeClass.Topology.Master with
// no Topology==nil guard. Today this is latent: on the Sync path, PR #6157's new
// error in generateRuntimeConfigData (reached earlier, at sync.go:55) short-circuits
// Sync before syncRuntimeSpec runs. The deref is still unguarded, so it becomes live
// if that ordering ever changes.
func TestVerifyF3_SyncRuntimeSpecNilTopologyStillPanics(t *testing.T) {
	scheme := verifyScheme()
	runtimeObj := verifyRuntime()
	runtimeClass := verifyClassNoTopology()

	engine := &CacheEngine{
		Client:    fake.NewFakeClientWithScheme(scheme, runtimeObj, runtimeClass),
		name:      "demo",
		namespace: "default",
		Log:       logr.Discard(),
	}
	ctx := cruntime.ReconcileRequestContext{Context: context.Background()}

	panicked, val := didPanic(func() {
		_ = engine.syncRuntimeSpec(ctx, runtimeObj, runtimeClass)
	})

	if !panicked {
		t.Fatalf("CANARY FLIPPED: syncRuntimeSpec no longer panics on nil Topology. " +
			"sync.go:190 appears fixed -- invert this assertion.")
	}
	t.Logf("still panics (as expected, unfixed by PR #6157): %v", val)
}
