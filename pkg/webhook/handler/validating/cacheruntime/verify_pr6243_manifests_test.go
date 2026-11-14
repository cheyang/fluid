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

package cacheruntime

// Verification harness for fluid-cloudnative/fluid PR #6243, finding F2
// (reviewer: Codex). Not part of the PR.
//
// F2: the +kubebuilder:webhook marker in pkg/webhook/handler/validating/webhook.go
// makes `make update-crd` regenerate config/webhook/manifests.yaml, but the PR
// does not commit the result, so the lint CI gate ("CRD validation failed.
// Please use 'make update-crd' to keep CRDs latest") fails. This test asserts
// the committed manifests carry the generated validating webhook entry.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyPR6243WebhookManifestsUpToDate(t *testing.T) {
	manifest := filepath.Join("..", "..", "..", "..", "..", "config", "webhook", "manifests.yaml")
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatalf("read %s: %v", manifest, err)
	}
	if !strings.Contains(string(data), "update.cacheruntime.fluid.io") {
		t.Errorf("F2 reproduced: %s is stale; the +kubebuilder:webhook marker for "+
			"/validate-data-fluid-io-v1alpha1-cacheruntime is not reflected. "+
			"Run 'make update-crd' and commit the result.", manifest)
	}
}
