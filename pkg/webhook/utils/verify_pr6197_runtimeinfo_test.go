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

// Verification harness for the review of PR #6197 (reviewer B / Codex), round 5.
// Additive test file only; no production code is changed.
//
// A-F6 (open at 41b47141): the annotation path rejected digit-leading dataset names via
// IsDNS1035Label while the PVC path applied no name-shape check, so - the claim went - a
// digit-leading dataset worked via a mounted PVC but not via fluid.io/datasets. Round 5
// (dd4aeb81e) answers with docs + a code comment: "the dataset controller only accepts
// DNS-1035 names ... a name starting with a digit is a valid volume name but never names
// a dataset that gets bound". These contracts prove that justification end to end:
//   1. the annotation path rejects a digit-leading name with the documented reason, and
//   2. the PVC path cannot serve it either, because the dataset can never reach Bound
//      (the dataset-controller side is pinned in
//      pkg/controllers/v1alpha1/dataset/verify_pr6197_dataset_name_test.go).
// Polarity: contract. The behavior exists on both heads (the check is pre-existing), so
// these are green everywhere; they pin the author's claim so a future refactor that
// starts binding digit-leading datasets flips them red.
package utils

import (
	"strings"
	"testing"

	"github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func TestVerifyPR6197DigitLeadingDatasetRejectedEverywhere(t *testing.T) {
	scheme := setupScheme()
	log := zap.New(zap.UseDevMode(true))

	// A digit-leading dataset that the API server accepts (metadata.name is a DNS-1123
	// subdomain) but the dataset controller never binds: it sits in NotBound forever.
	dataset := &v1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: "20-hbase", Namespace: "default"},
		Status:     v1alpha1.DatasetStatus{Phase: v1alpha1.NotBoundDatasetPhase},
	}

	// 1. annotation path: rejected up front with the documented DNS-1035 reason
	annClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(dataset).Build()
	_, err := CollectRuntimeInfosFromAnnotations(annClient,
		map[string]string{common.LabelAnnotationDatasets: "20-hbase"}, "default", log, false)
	if err == nil || !strings.Contains(err.Error(), "20-hbase") ||
		!strings.Contains(err.Error(), "a DNS-1035 label must consist of") {
		t.Fatalf("annotation path must reject a digit-leading dataset name with the DNS-1035 reason, got %v", err)
	}

	// 2. PVC path: no name-shape check there, but it cannot serve the dataset either,
	// because such a dataset never reaches Bound (checkDatasetBound rejects it).
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "20-hbase",
			Namespace: "default",
			Labels:    map[string]string{common.LabelAnnotationStorageCapacityPrefix: "true"},
		},
	}
	pvcClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pvc, dataset).Build()
	_, err = CollectRuntimeInfosFromPVCs(pvcClient, []string{"20-hbase"}, "default", log, false)
	if err == nil || !strings.Contains(err.Error(), "not bound") {
		t.Fatalf("PVC path must reject a digit-leading dataset as not bound, got %v", err)
	}
}
