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

package cacheruntime_test

// F2 — contract: the kubebuilder webhook marker added by PR #6243 must be
// reflected in the committed, controller-gen-generated
// config/webhook/manifests.yaml. The CI lint gate runs `make update-crd`
// (which regenerates that file via controller-gen v0.19.0) and fails the PR
// when regeneration leaves a diff — which is exactly the current state: the
// marker exists in pkg/webhook/handler/validating/webhook.go but the generated
// manifest was never committed.
//
// After the author commits `make manifests` output, this test goes green.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommittedWebhookManifestsIncludeValidatingWebhook(t *testing.T) {
	manifest, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "..", "config", "webhook", "manifests.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatalf("cannot read config/webhook/manifests.yaml: %v", err)
	}
	s := string(data)

	for _, want := range []string{
		"kind: ValidatingWebhookConfiguration",
		"name: update.cacheruntime.fluid.io",
		"path: /validate-data-fluid-io-v1alpha1-cacheruntime",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("config/webhook/manifests.yaml does not contain %q.\nThe kubebuilder webhook marker in pkg/webhook/handler/validating/webhook.go requires `make manifests` (controller-gen v0.19.0) to be re-run and the result committed; the CI lint gate ('make update-crd') fails otherwise.", want)
		}
	}
}
