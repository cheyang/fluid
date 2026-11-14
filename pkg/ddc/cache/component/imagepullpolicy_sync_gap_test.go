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

// F3 (sync side) — canary documenting that AdvancedStatefulSetManager.
// SyncComponentSpec propagates ONLY the image (image:tag) of the
// VersionSpec, never its ImagePullPolicy. The validating webhook of PR #6243
// treats the whole master/worker `runtimeVersion` as updatable in place, so a
// `spec.master.runtimeVersion.imagePullPolicy` change is admitted but then
// silently ignored — the exact class of no-op update the PR exists to reject.
//
// Polarity: canary. It passes while the gap exists (current behavior). If the
// sync starts propagating imagePullPolicy (or the webhook starts rejecting its
// change), this test flips red and must be inverted.

import (
	"context"
	"testing"

	workloadv1alpha1 "github.com/fluid-cloudnative/advanced-statefulset/api/workload/v1alpha1"
	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestSyncComponentSpecDoesNotPropagateImagePullPolicy(t *testing.T) {
	client := setupTestClient()
	manager := newAdvancedStatefulSetManager(client)
	ctx := context.Background()

	identity := &common.ComponentIdentity{Name: "test-runtime-worker", Namespace: "fluid"}

	// creation-time ASTS, as the creation path rendered it
	replicas := int32(2)
	existing := &workloadv1alpha1.AdvancedStatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: identity.Name, Namespace: identity.Namespace},
		Spec: workloadv1alpha1.AdvancedStatefulSetSpec{
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:            "worker",
						Image:           "test-worker:v1",
						ImagePullPolicy: corev1.PullIfNotPresent,
					}},
				},
			},
		},
	}
	if err := client.Create(ctx, existing); err != nil {
		t.Fatalf("create ASTS failed: %v", err)
	}

	newSpec := componentSpecFor(versionSpec("test-worker", "v2", corev1.PullNever))
	if err := manager.SyncComponentSpec(ctx, identity, newSpec); err != nil {
		t.Fatalf("SyncComponentSpec failed: %v", err)
	}

	asts := &workloadv1alpha1.AdvancedStatefulSet{}
	if err := client.Get(ctx, types.NamespacedName{Name: identity.Name, Namespace: identity.Namespace}, asts); err != nil {
		t.Fatalf("get ASTS failed: %v", err)
	}

	gotImage := asts.Spec.Template.Spec.Containers[0].Image
	if gotImage != "test-worker:v2" {
		t.Errorf("image should have been synced to test-worker:v2, got %q (the harness is not exercising the sync path)", gotImage)
	}
	gotPolicy := asts.Spec.Template.Spec.Containers[0].ImagePullPolicy
	if gotPolicy == corev1.PullNever {
		t.Errorf("ImagePullPolicy WAS propagated (%q); the webhook may now legitimately treat runtimeVersion.imagePullPolicy as updatable — invert this canary", gotPolicy)
	}
	// current behavior: the creation-time pull policy is kept
	if gotPolicy != corev1.PullIfNotPresent {
		t.Errorf("expected the creation-time ImagePullPolicy IfNotPresent to be kept, got %q", gotPolicy)
	}
}

func versionSpec(image, tag string, policy corev1.PullPolicy) datav1alpha1.VersionSpec {
	return datav1alpha1.VersionSpec{Image: image, ImageTag: tag, ImagePullPolicy: string(policy)}
}

func componentSpecFor(version datav1alpha1.VersionSpec) ComponentSpec {
	return ComponentSpec{Version: version}
}
