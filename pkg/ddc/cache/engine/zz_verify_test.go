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
// Round 1 probed the `runtimeClass.Topology` dereferences that guarded the
// sub-component but not `Topology` itself (dataload.go / image.go / sync.go),
// while the PR only fixed cm.go. Round 2: the author moved the check into the
// getRuntimeClass loader (validateRuntimeClassTopology in validate.go), which is
// the sole loader for all five engine entry points. The REACHABLE paths are now
// guarded; the function-level derefs remain unguarded but are unreachable in
// production (every caller obtains the class through the loader).
//
// Polarity after round 2:
//   - F1b / F4 are CONTRACT tests: they assert the fixed behavior (a loader
//     error instead of a panic) and must stay green while the fix is present.
//   - F1 / F2 / F3 remain CANARIES documenting the accepted residual: direct
//     calls still panic because those functions were never guarded themselves.
//     They only flip if someone also adds function-level guards (defense in
//     depth) -- until then, green means "residual unchanged".

import (
	"context"
	"strings"
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
// no Topology==nil guard. CANARY (residual): the production DataLoad path reaches
// genDataLoadValue only through generateDataLoadValueFile, whose class now comes
// from the guarded getRuntimeClass loader (see F1b/F4), so this deref is no longer
// reachable in production. The function itself was never guarded -- direct calls
// still panic. Green = residual unchanged; a flip means someone guarded the
// function itself (invert then).
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

// F1b -- CONTRACT (flipped from a round-1 canary). Entered one hop above F1 at
// generateDataLoadValueFile, which is what the DataLoad operation actually calls.
// The runtimeClass is NOT passed in by the test: the engine loads it from the API
// via getRuntimeClass, whose round-2 validateRuntimeClassTopology guard must reject
// the topology-less class with an error instead of letting the deref panic.
func TestVerifyF1b_GenerateDataLoadValueFileRejectsTopologyLessClass(t *testing.T) {
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
		_, err := engine.generateDataLoadValueFile(ctx, dataload)
		if err == nil {
			t.Errorf("expected the loader to reject the topology-less class with an error, got nil")
		} else if !strings.Contains(err.Error(), "at least one component should be defined") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	if panicked {
		t.Fatalf("REGRESSION: generateDataLoadValueFile panics again on a stored "+
			"topology-less CacheRuntimeClass (val=%v) -- the loader guard is gone", val)
	}
}

// F2 -- image.go:29. getDataOperationImage derefs runtimeClass.Topology.Worker
// with no Topology==nil guard. CANARY (residual): its only production caller is
// genDataLoadValue (dataload.go:123), behind the guarded loader, so it is
// unreachable today. Direct calls still panic; green = residual unchanged.
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
// no Topology==nil guard. CANARY (residual): in production syncRuntimeSpec only
// receives a class loaded through getRuntimeClass, whose round-2 guard rejects a
// topology-less class first -- so this is unreachable today. The deref itself is
// still wrong and becomes live if a future caller passes a class it built or read
// by other means. Green = residual unchanged; a flip means someone guarded the
// function itself (invert then).
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
	t.Logf("still panics on direct call (residual; production path is shielded by the loader): %v", val)
}

// F4 -- CONTRACT on the round-2 choke point. getRuntimeClass is the sole loader
// for all five engine entry points (setup.go:39, cm.go:107, sync.go:50, ufs.go:97,
// dataload.go:56). Its validateRuntimeClassTopology call is what makes every
// previously-panicking path safe; if it is ever removed or bypassed, all of the
// round-1 defects return at once. Both a nil topology and a topology that declares
// no component must be rejected.
func TestVerifyF4_GetRuntimeClassRejectsTopologyLessClass(t *testing.T) {
	cases := map[string]*datav1alpha1.CacheRuntimeClass{
		"topology nil": {
			ObjectMeta: metav1.ObjectMeta{Name: "test-class"},
			Topology:   nil,
		},
		"topology declares no component": {
			ObjectMeta: metav1.ObjectMeta{Name: "test-class"},
			Topology:   &datav1alpha1.RuntimeTopology{},
		},
	}
	for name, class := range cases {
		t.Run(name, func(t *testing.T) {
			scheme := verifyScheme()
			engine := &CacheEngine{
				Client:    fake.NewFakeClientWithScheme(scheme, class),
				name:      "demo",
				namespace: "default",
			}

			panicked, val := didPanic(func() {
				got, err := engine.getRuntimeClass("test-class")
				if err == nil {
					t.Errorf("expected getRuntimeClass to reject the class, got object %v", got)
				} else if !strings.Contains(err.Error(), "at least one component should be defined") {
					t.Errorf("unexpected error: %v", err)
				}
			})
			if panicked {
				t.Fatalf("REGRESSION: getRuntimeClass panics on %s (val=%v)", name, val)
			}
		})
	}
}
