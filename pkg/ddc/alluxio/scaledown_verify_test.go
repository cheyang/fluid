/*
Copyright 2024 The Fluid Authors.

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

// Additive verification harness for the review of
// https://github.com/fluid-cloudnative/fluid/pull/6061
// (graceful AlluxioRuntime worker scale-down). No production code is modified.
// See docs/verification/alluxio-scaledown-e2e/.
//
// Deliberately gomonkey-FREE: the package's existing *_test.go files patch
// methods with gomonkey, which crashes on darwin/arm64. These tests use only
// real calls so they run deterministically on any machine.

package alluxio

import (
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/ddc/base/portallocator"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	"k8s.io/apimachinery/pkg/util/net"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// FINDING F2 — contract (expect FAIL on PR head; PASSES once fixed).
//
// getWorkerWebPort() is used to build the address that
// `alluxio fsadmin decommissionWorker --addresses <hostIP>:<port>` targets.
// For the DEFAULT AlluxioRuntime the network mode is host network
// (IsHostNetwork("")==true), so transform.allocatePorts() assigns the worker
// web port DYNAMICALLY from the operator port range when the user has not
// pinned spec.worker.ports["web"]. That allocated port is never written back
// to runtime.Spec.Worker.Ports, yet getWorkerWebPort only reads that map (or
// falls back to defaultWorkerWebPort=30000). So the decommission command is
// sent to the wrong port, the drain silently no-ops, and scale-down falls
// through to defaultWorkerDecommissionDeadline and proceeds UNGRACEFULLY —
// the exact data-loss path this feature exists to prevent.
//
// This test allocates ports the same way the engine does at deploy time, then
// asserts getWorkerWebPort reports the port the worker actually listens on.
func TestVerifyF2WorkerWebPortMatchesAllocatedPort(t *testing.T) {
	// A port range that deliberately EXCLUDES the 30000 default, so a
	// mismatch is unambiguous.
	noReserved := func(c client.Client) ([]int, error) { return []int{}, nil }
	pr := net.ParsePortRangeOrDie("20000-21000")
	if err := portallocator.SetupRuntimePortAllocator(nil, pr, "bitmap", noReserved); err != nil {
		t.Fatalf("failed to set up port allocator: %v", err)
	}

	// Default runtime: networkMode "" => host network => dynamic allocation,
	// and no pinned spec.worker.ports["web"].
	runtime := &datav1alpha1.AlluxioRuntime{}
	e := &AlluxioEngine{runtime: runtime, Log: fake.NullLogger()}

	value := &Alluxio{Properties: map[string]string{}}
	if err := e.allocatePorts(value, runtime); err != nil {
		t.Fatalf("allocatePorts failed: %v", err)
	}

	actual := value.Worker.Ports.Web      // the port the worker process actually binds
	reported := e.getWorkerWebPort(runtime) // the port the decommission command targets

	t.Logf("worker actually listens on web port %d; getWorkerWebPort() reports %d", actual, reported)
	if reported != actual {
		t.Errorf("F2 CONFIRMED: decommission would target %d but the worker's web port is %d "+
			"(default host-network deployment) — graceful drain silently no-ops and scale-down "+
			"degrades to ungraceful after the deadline", reported, actual)
	}
}

// FINDING F2 (companion) — proves the mismatch is specifically the
// host-network dynamic-allocation path, and that container-network mode is
// unaffected (getWorkerWebPort's 30000 default is correct there because
// generateStaticPorts always assigns 30000). Contract: both assertions should
// hold once getWorkerWebPort is made network-mode aware.
func TestVerifyF2ContainerNetworkModeIsUnaffected(t *testing.T) {
	runtime := &datav1alpha1.AlluxioRuntime{
		Spec: datav1alpha1.AlluxioRuntimeSpec{
			Worker: datav1alpha1.AlluxioCompTemplateSpec{
				NetworkMode: datav1alpha1.ContainerNetworkMode,
			},
		},
	}
	e := &AlluxioEngine{runtime: runtime, Log: fake.NullLogger()}
	value := &Alluxio{Properties: map[string]string{}}
	// container network => generateStaticPorts path (no allocator needed)
	e.generateStaticPorts(value)

	if value.Worker.Ports.Web != e.getWorkerWebPort(runtime) {
		t.Errorf("container-network: static worker web port %d != getWorkerWebPort() %d",
			value.Worker.Ports.Web, e.getWorkerWebPort(runtime))
	}
	if value.Worker.Ports.Web != defaultWorkerWebPort {
		t.Errorf("sanity: expected static worker web port to be %d, got %d",
			defaultWorkerWebPort, value.Worker.Ports.Web)
	}
}
