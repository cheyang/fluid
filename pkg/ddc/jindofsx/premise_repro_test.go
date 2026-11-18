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

package jindofsx

// P0 premise repro for fluid#6261 / issue #2077.
//
// Polarity: contract test. On the BASE branch (1321a217) this exact call
// panics inside resource.MustParse("10Gi,5Gi") — see
// docs/verification/jindo-quota-sum/results/base-premise.txt for the captured
// stack trace, which matches issue #2077 byte for byte. On the PR head it
// must complete without panicking.
//
// Note: the engine needs a (fake) Client on the head because the fix lets
// execution proceed into the runtime-update branch; on base it panicked
// before ever reaching it.

import (
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPremiseQuotaListMustNotPanic(t *testing.T) {
	rt := &datav1alpha1.JindoRuntime{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "spark",
			Namespace: "fluid",
		},
		Spec: datav1alpha1.JindoRuntimeSpec{
			TieredStore: datav1alpha1.TieredStore{
				Levels: []datav1alpha1.Level{{
					MediumType: common.Memory,
					Path:       "/dev/shm,/mnt/ram",
					QuotaList:  "10Gi,5Gi",
				}},
			},
			Worker: datav1alpha1.JindoCompTemplateSpec{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("20Gi"),
					},
				},
			},
		},
	}
	engine := &JindoFSxEngine{
		name:      "spark",
		namespace: "fluid",
		Client:    fake.NewFakeClientWithScheme(testScheme, rt.DeepCopy()),
		Log:       fake.NullLogger(),
	}
	// "10g,5g" is the jindo-unit form transform() joins and hands down
	err := engine.transformWorkerResources(rt, &Jindo{}, "10g,5g")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}
