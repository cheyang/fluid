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

// Verification harness for review findings on PR #6156
// ("fix(jindo): fall back to dataset mounts when DataLoad has no target").
//
// This file is ADDITIVE reviewer-side evidence. It does not modify production code.
//
// Layer: L1 (deterministic / unit).
//
// Polarity legend (see docs/verification/pr6156-jindo-dataload-no-target/README.md):
//   [contract] asserts the INTENDED behavior -> FAILS on the PR as submitted, PASSES once fixed.
//   [canary]   asserts the CURRENT (wrong/limited) behavior -> PASSES now, FLIPS to fail once
//              fixed, at which point the assertion must be inverted.

package jindo

import (
	"strings"
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	cdataload "github.com/fluid-cloudnative/fluid/pkg/dataload"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	jindoutils "github.com/fluid-cloudnative/fluid/pkg/utils/jindo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

// pr6156Runtime is a minimal JindoRuntime accepted by genDataLoadValue.
func pr6156Runtime() *datav1alpha1.JindoRuntime {
	return &datav1alpha1.JindoRuntime{
		Spec: datav1alpha1.JindoRuntimeSpec{
			TieredStore: datav1alpha1.TieredStore{
				Levels: []datav1alpha1.Level{{MediumType: "MEM"}},
			},
		},
	}
}

// pr6156Dataset builds a dataset whose mounts are real JindoFS-supported UFS URIs (oss://),
// unlike the local:// fixtures in the PR's own test, which jindo's transform skips entirely.
func pr6156Dataset(mounts []datav1alpha1.Mount) *datav1alpha1.Dataset {
	return &datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: "test-dataset", Namespace: "fluid"},
		Spec:       datav1alpha1.DatasetSpec{Mounts: mounts},
	}
}

func pr6156NoTargetDataLoad() *datav1alpha1.DataLoad {
	return &datav1alpha1.DataLoad{
		ObjectMeta: metav1.ObjectMeta{Name: "test-dataload", Namespace: "fluid"},
		Spec: datav1alpha1.DataLoadSpec{
			Dataset: datav1alpha1.TargetDataset{Name: "test-dataset", Namespace: "fluid"},
			// Spec.Target deliberately unset -- this is the scenario the PR addresses.
		},
	}
}

func pr6156Engine() JindoEngine {
	return JindoEngine{namespace: "fluid", Log: fake.NullLogger()}
}

// ---------------------------------------------------------------------------
// F1 [contract] -- A no-target DataLoad must yield target paths that are actually
// addressable inside the JindoFS unified namespace.
//
// The JindoFS engine serves exactly ONE namespace ("jindo") backed by exactly ONE
// UFS URI (see TestPR6156_L1_F2_JindoCollapsesAllMountsIntoOneNamespace below), and
// charts/fluid-dataloader/jindo/values.yaml documents the intended default for
// targetPaths as a single entry (path: "/", replicas: 1, fluidNative: false).
//
// So for a no-target DataLoad the engine must emit either nothing (letting the chart
// default apply) or the single root path "/". Emitting one path per dataset mount is
// not representable in this engine.
//
// On PR #6156 this FAILS: it emits /mnt0 and /mnt1.
func TestPR6156_L1_F1_NoTargetMustYieldAddressableJindoPaths(t *testing.T) {
	dataset := pr6156Dataset([]datav1alpha1.Mount{
		{Name: "spark", MountPoint: "oss://bucket1/spark/", Path: "/mnt0"},
		{Name: "hive", MountPoint: "oss://bucket2/hive/", Path: "/mnt1"},
	})
	engine := pr6156Engine()

	got, err := engine.genDataLoadValue("fluid:v0.0.1", pr6156Runtime(), dataset, pr6156NoTargetDataLoad())
	if err != nil {
		t.Fatalf("genDataLoadValue returned error: %v", err)
	}
	paths := got.DataLoadInfo.TargetPaths
	t.Logf("observed TargetPaths = %+v", paths)

	// C1a: JindoFS exposes a single namespace over a single UFS, so more than one
	// target path cannot be addressed.
	if len(paths) > 1 {
		t.Errorf("C1a FAIL: got %d target paths %+v; JindoFS serves one namespace over one UFS, "+
			"so at most 1 path is addressable", len(paths), paths)
	}

	// C1b: whatever single path is emitted must be the namespace root "/", matching the
	// documented chart default.
	for _, p := range paths {
		if p.Path != "/" {
			t.Errorf("C1b FAIL: target path %q is not the JindoFS namespace root; the jindo engine "+
				"never mounts a per-mount subtree, so %q does not exist in jfs://jindo", p.Path, p.Path)
		}
	}
}

// F1 [contract] -- same claim for mounts WITHOUT an explicit spec.mounts[*].path, which is
// the branch the PR description calls out ("mounts without an explicit path are handled
// consistently") but which the PR's own added test never reaches, because both of its
// fixtures set an absolute Path.
//
// On PR #6156 this FAILS: it emits /spark and /hive from the mount NAMES.
func TestPR6156_L1_F1_NoTargetNoMountPathMustYieldAddressableJindoPaths(t *testing.T) {
	dataset := pr6156Dataset([]datav1alpha1.Mount{
		{Name: "spark", MountPoint: "oss://bucket1/spark/"},
		{Name: "hive", MountPoint: "oss://bucket2/hive/"},
	})
	engine := pr6156Engine()

	got, err := engine.genDataLoadValue("fluid:v0.0.1", pr6156Runtime(), dataset, pr6156NoTargetDataLoad())
	if err != nil {
		t.Fatalf("genDataLoadValue returned error: %v", err)
	}
	paths := got.DataLoadInfo.TargetPaths
	t.Logf("observed TargetPaths (mounts without .path) = %+v", paths)

	if len(paths) > 1 {
		t.Errorf("C1a FAIL: got %d target paths %+v for a 2-mount dataset", len(paths), paths)
	}
	for _, p := range paths {
		if p.Path != "/" {
			t.Errorf("C1b FAIL: name-derived path %q is not addressable in jfs://jindo", p.Path)
		}
	}
}

// ---------------------------------------------------------------------------
// F1-premise [canary] -- The PR's stated premise is that an empty Spec.Target makes
// TargetPaths empty and therefore "the DataLoad job loaded no data".
//
// That premise is refuted at this layer: DataLoadInfo.TargetPaths carries
// `json:"targetPaths,omitempty"`, so an empty slice is OMITTED from the generated
// values file. `helm install -f <values> <chart>` then coalesces the chart's own
// default (path: "/") on top, so the pre-PR behaviour is "load the whole dataset",
// not "load nothing". L2 (helm render) confirms this end to end.
//
// PASSES on master and on the PR (the marshalling is unchanged by the PR).
// Flips only if someone drops `omitempty`, which would itself be the real fix for
// the no-op the PR believes it is fixing.
func TestPR6156_L1_F1Premise_EmptyTargetPathsIsOmittedSoChartDefaultApplies(t *testing.T) {
	// Exactly what master's genDataLoadValue produces for a no-target DataLoad.
	value := &cdataload.DataLoadValue{
		Name: "test-dataload",
		DataLoadInfo: cdataload.DataLoadInfo{
			BackoffLimit:  3,
			TargetDataset: "test-dataset",
			Image:         "fluid:v0.0.1",
			TargetPaths:   []cdataload.TargetPath{}, // empty, as master produces
		},
	}

	data, err := yaml.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	rendered := string(data)
	t.Logf("rendered values:\n%s", rendered)

	if strings.Contains(rendered, "targetPaths") {
		t.Errorf("canary flipped: expected `targetPaths` to be OMITTED for an empty slice "+
			"(json omitempty), but it is present:\n%s", rendered)
	}
}

// ---------------------------------------------------------------------------
// F2 [canary] -- The JindoFS engine collapses ALL dataset mounts into a single
// hardcoded namespace "jindo" backed by a single UFS URI: every mount writes the same
// `jfs.namespaces.jindo.<mode>.uri` key, so the LAST mount wins. The per-mount
// namespace accumulation is commented out in transform.go
// (`//jfsNamespace = jfsNamespace + mount.Name + ","`).
//
// This is why the PR's per-mount target paths are not addressable: there is no
// /spark or /mnt0 subtree in jfs://jindo -- the namespace root IS the single UFS root.
//
// PASSES today. Flips if real multi-mount support is implemented, at which point the
// PR's approach could become viable and this assertion must be inverted.
func TestPR6156_L1_F2_JindoCollapsesAllMountsIntoOneNamespace(t *testing.T) {
	dataset := pr6156Dataset([]datav1alpha1.Mount{
		{Name: "spark", MountPoint: "oss://bucket1/spark/", Path: "/mnt0",
			Options: map[string]string{"fs.oss.endpoint": "oss-cn-hangzhou.aliyuncs.com"}},
		{Name: "hive", MountPoint: "oss://bucket2/hive/", Path: "/mnt1",
			Options: map[string]string{"fs.oss.endpoint": "oss-cn-hangzhou.aliyuncs.com"}},
	})
	engine := pr6156Engine()

	value := &Jindo{}
	if err := engine.transformMaster(pr6156Runtime(), "/mnt/disk1", value, dataset); err != nil {
		t.Fatalf("transformMaster: %v", err)
	}
	props := value.Master.MasterProperties
	t.Logf("jfs.namespaces          = %q", props["jfs.namespaces"])
	t.Logf("jfs.namespaces.jindo.oss.uri = %q", props["jfs.namespaces.jindo.oss.uri"])

	if props["jfs.namespaces"] != "jindo" {
		t.Errorf("canary flipped: jfs.namespaces = %q, expected the single hardcoded \"jindo\"",
			props["jfs.namespaces"])
	}

	// Only the LAST mount's URI survives -> multi-mount is not representable.
	uri := props["jfs.namespaces.jindo.oss.uri"]
	if !strings.Contains(uri, "bucket2") {
		t.Errorf("canary flipped: expected only the LAST mount (bucket2) to survive in the single "+
			"namespace URI, got %q", uri)
	}
	if strings.Contains(uri, "bucket1") {
		t.Errorf("canary flipped: first mount (bucket1) unexpectedly still present in %q -- "+
			"multi-mount may now be supported", uri)
	}

	// There is no per-mount namespace entry for either mount name.
	for _, name := range []string{"spark", "hive"} {
		if _, ok := props["jfs.namespaces."+name+".oss.uri"]; ok {
			t.Errorf("canary flipped: found a per-mount namespace jfs.namespaces.%s.* -- "+
				"per-mount paths may now be addressable", name)
		}
	}
}

// ---------------------------------------------------------------------------
// F3 [canary] -- pkg/ddc/jindo (JindoFS, smartdata:3.8.0) is NOT the engine a
// JindoRuntime gets by default: GetDefaultEngineImpl() returns "jindocache" unless the
// operator is started with JINDO_ENGINE_TYPE=jindo|jindofsx. So the PR fixes a legacy,
// opt-in engine while the default engine (jindocache) and jindofsx keep the gap the PR
// describes.
//
// PASSES today. Flips when the default engine changes or when jindocache/jindofsx gain
// the same fallback.
func TestPR6156_L1_F3_DefaultJindoEngineIsNotTheOnePatched(t *testing.T) {
	t.Setenv("JINDO_ENGINE_TYPE", "")
	impl := jindoutils.GetDefaultEngineImpl()
	t.Logf("GetDefaultEngineImpl() with JINDO_ENGINE_TYPE unset = %q", impl)
	if impl != "jindocache" {
		t.Errorf("canary flipped: default jindo engine impl is now %q, not \"jindocache\"", impl)
	}
	if impl == "jindo" {
		t.Errorf("canary flipped: pkg/ddc/jindo is now the default engine")
	}
}
