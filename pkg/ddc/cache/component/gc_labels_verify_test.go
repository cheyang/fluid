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

package component

// Verification harness for PR #6175 (test-only PR): the e2e scripts in
// test/gha-e2e/mooncake/test.sh and test/gha-e2e/curvine/test.sh assert runtime
// garbage collection with
//
//	kubectl get advancedstatefulset,daemonset,svc -l "cacheruntime.fluid.io/name=<runtime>"
//
// This test pins down the two properties that assertion depends on:
//
//  1. every workload/service the cache component layer creates carries
//     cacheruntime.fluid.io/name=<runtimeName> and
//     cacheruntime.fluid.io/component-name=<runtimeName>-<type>, so the new
//     selector actually matches the created objects (the assertion bites);
//  2. none of them carries the fluid.io/managed-by label, so the selector the
//     curvine case used before this PR ("fluid.io/managed-by=fluid") matched
//     nothing and its GC wait was vacuous.
//
// Additive review-harness test; not part of the PR under review.

import (
	"context"

	workloadv1alpha1 "github.com/fluid-cloudnative/advanced-statefulset/api/workload/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

func verifyComponentForGC(runtimeName string, componentType common.ComponentType) *common.CacheRuntimeComponentValue {
	componentName := common.GetCacheComponentName(runtimeName, componentType)
	return &common.CacheRuntimeComponentValue{
		Name:      componentName,
		Namespace: "default",
		Replicas:  1,
		Owner: &common.OwnerReference{
			APIVersion: "data.fluid.io/v1alpha1",
			Kind:       "CacheRuntime",
			Name:       runtimeName,
			UID:        "verify-uid",
		},
		ComponentType: componentType,
		PodTemplateSpec: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: string(componentType), Image: "verify-image:latest"},
				},
			},
		},
		Service: &common.CacheRuntimeComponentServiceConfig{
			Name: "svc-" + runtimeName + "-" + string(componentType),
		},
	}
}

func expectGCLabels(objectMetaLabels map[string]string, runtimeName string, componentName string) {
	ExpectWithOffset(1, objectMetaLabels).To(HaveKeyWithValue(common.LabelCacheRuntimeName, runtimeName))
	ExpectWithOffset(1, objectMetaLabels).To(HaveKeyWithValue(common.LabelCacheRuntimeComponentName, componentName))
	ExpectWithOffset(1, objectMetaLabels).NotTo(HaveKey(common.LabelAnnotationManagedBy))
}

var _ = Describe("GC selector labels (PR #6175 verification)", func() {
	const runtimeName = "mooncake-demo"

	DescribeTable("objects constructed for a component carry the cacheruntime GC labels and not fluid.io/managed-by",
		func(componentType common.ComponentType) {
			component := verifyComponentForGC(runtimeName, componentType)

			asts := newAdvancedStatefulSetManager(nil).constructAdvancedStatefulSet(component)
			expectGCLabels(asts.Labels, runtimeName, component.Name)
			expectGCLabels(asts.Spec.Template.Labels, runtimeName, component.Name)
			Expect(asts.Name).To(Equal(component.Name))
			Expect(asts.Spec.ServiceName).To(Equal(component.Service.Name))

			svc := constructService(component)
			expectGCLabels(svc.Labels, runtimeName, component.Name)
			Expect(svc.Name).To(Equal(component.Service.Name))
			Expect(svc.Spec.Selector).To(HaveKeyWithValue(common.LabelCacheRuntimeName, runtimeName))

			ds := newDaemonSetManager(nil).constructDaemonSet(component)
			expectGCLabels(ds.Labels, runtimeName, component.Name)
			expectGCLabels(ds.Spec.Template.Labels, runtimeName, component.Name)
			Expect(ds.Name).To(Equal(component.Name))
		},
		Entry("master component", common.ComponentTypeMaster),
		Entry("worker component", common.ComponentTypeWorker),
		Entry("client component", common.ComponentTypeClient),
	)

	It("the reconciler creates the AdvancedStatefulSet and Service with those labels", func() {
		component := verifyComponentForGC(runtimeName, common.ComponentTypeWorker)
		manager := newAdvancedStatefulSetManager(setupTestClient())
		ctx := context.Background()
		Expect(manager.Reconciler(ctx, component)).To(Succeed())

		asts := &workloadv1alpha1.AdvancedStatefulSet{}
		Expect(manager.client.Get(ctx, types.NamespacedName{Name: component.Name, Namespace: "default"}, asts)).To(Succeed())
		expectGCLabels(asts.Labels, runtimeName, component.Name)

		svc := &corev1.Service{}
		Expect(manager.client.Get(ctx, types.NamespacedName{Name: component.Service.Name, Namespace: "default"}, svc)).To(Succeed())
		expectGCLabels(svc.Labels, runtimeName, component.Name)

		dsList := &appsv1.DaemonSetList{}
		Expect(manager.client.List(ctx, dsList)).To(Succeed())
		Expect(dsList.Items).To(BeEmpty())
	})
})
