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

package juicefs

import (
	"reflect"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

// These tests pin the externally observable contract of sigs.k8s.io/yaml as used
// by fluid's engine code (pkg/ddc/*, pkg/dataprocess): yaml.Marshal for Helm
// values generation (pkg/ddc/juicefs/utils.go:243, pkg/ddc/alluxio/master_internal.go:81,
// pkg/dataprocess/generate_values.go:51) and yaml.Unmarshal for runtime config
// parsing (pkg/ddc/juicefs/port_parser.go:72, pkg/ddc/juicefs/datasetinfo_parser.go:52).
//
// Polarity: CONTRACT. They encode the behavior the 1.4.0 -> 1.6.0 bump
// (PR #6240) must preserve, and act as the regression net for any future
// sigs.k8s.io/yaml / go.yaml.in bump that changes marshal/unmarshal semantics.

// TestSigsYamlMarshalGolden pins the exact YAML emitted for a Helm-values-shaped
// structure mirroring pkg/ddc/juicefs/utils.go transform output: sorted map keys,
// quoting of YAML-1.1-ambiguous scalars, null rendering, nested maps and lists.
func TestSigsYamlMarshalGolden(t *testing.T) {
	value := map[string]interface{}{
		"replicas":        2,
		"image":           "juicedata/juicefs-csi-driver",
		"imageTag":        "v1.2.0",
		"imagePullPolicy": "IfNotPresent",
		"resources": map[string]interface{}{
			"limits":   map[string]interface{}{"cpu": "2", "memory": "4Gi"},
			"requests": map[string]interface{}{"cpu": "1", "memory": "1Gi"},
		},
		"configs":  []interface{}{"a", true, nil, 3.14, int64(9007199254740993)},
		"nullKey":  nil,
		"octalStr": "0755",
		"boolish":  "true",
		"empty":    "",
		"special":  "on: yes #not-a-comment",
	}

	got, err := yaml.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	want := `boolish: "true"
configs:
- a
- true
- null
- 3.14
- 9007199254740993
empty: ""
image: juicedata/juicefs-csi-driver
imagePullPolicy: IfNotPresent
imageTag: v1.2.0
nullKey: null
octalStr: "0755"
replicas: 2
resources:
  limits:
    cpu: "2"
    memory: 4Gi
  requests:
    cpu: "1"
    memory: 1Gi
special: 'on: yes #not-a-comment'
`
	if string(got) != want {
		t.Fatalf("Marshal golden mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestSigsYamlUnmarshalConfigGolden pins decoding of a runtime-config string of
// the kind pkg/ddc/juicefs/datasetinfo_parser.go and port_parser.go consume:
// struct decoding via json tags, scalar type resolution into interface{}
// (ints as int, bools including YAML-1.1 "on", null as nil, floats as float64).
func TestSigsYamlUnmarshalConfigGolden(t *testing.T) {
	type fakeConfig struct {
		Name    string `json:"name"`
		MetaURL string `json:"metaUrl"`
		Timeout int    `json:"timeout"`
	}

	var cfg fakeConfig
	in := "name: juicefs\nmetaUrl: redis://host:6379\ntimeout: 30\n"
	if err := yaml.Unmarshal([]byte(in), &cfg); err != nil {
		t.Fatalf("Unmarshal struct returned error: %v", err)
	}
	if cfg != (fakeConfig{Name: "juicefs", MetaURL: "redis://host:6379", Timeout: 30}) {
		t.Fatalf("Unmarshal struct mismatch: %+v", cfg)
	}

	var m map[string]interface{}
	in2 := "enabled: on\ncount: 42\nratio: 0.5\nnothing: ~\ntext: v1.2.0\nlist:\n- 1\n- two\n"
	if err := yaml.Unmarshal([]byte(in2), &m); err != nil {
		t.Fatalf("Unmarshal map returned error: %v", err)
	}
	// NOTE: sigs.k8s.io/yaml decodes via JSON, so scalars in interface{} maps are
	// float64 (not int) — pinned here deliberately.
	want := map[string]interface{}{
		"enabled": true,
		"count":   float64(42),
		"ratio":   float64(0.5),
		"nothing": nil,
		"text":    "v1.2.0",
		"list":    []interface{}{float64(1), "two"},
	}
	if !reflect.DeepEqual(m, want) {
		t.Fatalf("Unmarshal map mismatch:\ngot:  %#v\nwant: %#v", m, want)
	}
}

// TestSigsYamlRoundTripStability pins round-trip fidelity and YAML anchor/merge-key
// semantics (the merge-key code path is what the v1.5.0 changelog touched).
func TestSigsYamlRoundTripStability(t *testing.T) {
	src := "base: &b\n  a: 1\n  b: 2\nmerged:\n  <<: *b\n  b: 3\n"
	var m map[string]interface{}
	if err := yaml.Unmarshal([]byte(src), &m); err != nil {
		t.Fatalf("Unmarshal merge-key doc returned error: %v", err)
	}
	merged, ok := m["merged"].(map[string]interface{})
	if !ok {
		t.Fatalf("merged is not a map: %#v", m["merged"])
	}
	wantMerged := map[string]interface{}{"a": float64(1), "b": float64(3)}
	if !reflect.DeepEqual(merged, wantMerged) {
		t.Fatalf("merge-key semantics mismatch: got %#v want %#v", merged, wantMerged)
	}

	// round-trip of the merged doc must be stable
	out, err := yaml.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal round-trip returned error: %v", err)
	}
	var back map[string]interface{}
	if err := yaml.Unmarshal(out, &back); err != nil {
		t.Fatalf("Unmarshal round-trip returned error: %v", err)
	}
	if !reflect.DeepEqual(m, back) {
		t.Fatalf("round-trip not stable:\nmarshal: %s\ngot:  %#v\nwant: %#v", out, back, m)
	}
}

// TestSigsYamlErrorTextStable pins the error message surface, since the v1.5.0
// changelog changed jsonUnmarshal to wrap with %w instead of %v. The rendered
// text must stay identical, and fluid error handling must not observe new text.
func TestSigsYamlErrorTextStable(t *testing.T) {
	var m map[string]interface{}
	err := yaml.Unmarshal([]byte("a: [unclosed"), &m)
	if err == nil {
		t.Fatalf("expected an error for malformed YAML, got nil")
	}
	if !strings.HasPrefix(err.Error(), "error converting YAML to JSON: yaml: ") {
		t.Fatalf("YAMLToJSON error prefix changed: %q", err.Error())
	}

	type cfg struct {
		Timeout int `json:"timeout"`
	}
	var c cfg
	err = yaml.Unmarshal([]byte("timeout: notanumber\n"), &c)
	if err == nil {
		t.Fatalf("expected an error for type mismatch, got nil")
	}
	if !strings.HasPrefix(err.Error(), "error unmarshaling JSON: while decoding JSON: json: ") {
		t.Fatalf("jsonUnmarshal error prefix changed: %q", err.Error())
	}
}
