// Package sigsyamlverify is an ADDITIVE review-verification harness for
// https://github.com/fluid-cloudnative/fluid/pull/6240 (bump sigs.k8s.io/yaml
// 1.4.0 -> 1.6.0, which swaps the underlying YAML engine from the embedded
// sigs.k8s.io/yaml/goyaml.v2 copy to go.yaml.in/yaml/v2 v2.4.4).
//
// It exercises the sigs.k8s.io/yaml API surface exactly the way fluid's own
// code uses it (helm-values Marshal in pkg/ddc/*/master_internal.go and
// load_data.go, configmap/profile/manifest Unmarshal in
// pkg/ddc/juicefs/port_parser.go, pkg/webhook/plugins, pkg/utils/raw.go).
//
// Each case prints a canonical "CASE <name> | <value>" line. The same test
// file is grafted onto the merge-base and onto the PR head by
// docs/verification/pr6240-sigs-yaml-bump-codex/scripts/run-behavior-matrix.sh
// and the outputs are diffed: byte-identical output == no behavioral change.
//
// Known, intended divergence (documented in the README): case C6-errunwrap
// changes from "not-unwrappable" (v1.4.0) to "unwrappable" (v1.6.0) because
// sigs.k8s.io/yaml PR #106 changed jsonUnmarshal's error wrap from %v to %w.
// The golden table below therefore excludes C6-errunwrap; everything else must
// match on both revisions.
package sigsyamlverify

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

// ---------- fluid-shaped fixtures ----------

// helmValues mimics the value structs marshaled in pkg/ddc/*/master_internal.go
type helmValues struct {
	FullnameOverride string                 `json:"fullnameOverride"`
	Replicas         int                    `json:"replicas"`
	HostNetwork      bool                   `json:"hostNetwork"`
	Image            string                 `json:"image"`
	ImageTag         string                 `json:"imageTag"`
	Ports            map[string]int         `json:"ports"`
	Tolerations      []map[string]string    `json:"tolerations"`
	Properties       map[string]string      `json:"properties"`
	Fuse             fuseSection            `json:"fuse"`
	Extra            map[string]interface{} `json:"extra,omitempty"`
}

type fuseSection struct {
	Enabled     bool     `json:"enabled"`
	MetricsPort *int     `json:"metricsPort,omitempty"`
	MountPath   string   `json:"mountPath"`
	Args        []string `json:"args"`
}

// juiceFSValues mimics the configmap shape parsed by pkg/ddc/juicefs/port_parser.go
var juiceFSStyleYAML = `worker:
  hostNetwork: true
  metricsPort: 20001
fuse:
  hostNetwork: false
  metricsPort: 20002
`

type juiceFSSection struct {
	HostNetwork bool `json:"hostNetwork"`
	MetricsPort *int `json:"metricsPort,omitempty"`
}
type juiceFSValues struct {
	Worker juiceFSSection `json:"worker"`
	Fuse   juiceFSSection `json:"fuse"`
}

// datasetManifest mimics the object shape decoded in pkg/utils/raw.go
const datasetManifest = `apiVersion: data.fluid.io/v1alpha1
kind: Dataset
metadata:
  name: demo
  namespace: default
spec:
  mounts:
  - mountPoint: https://mirrors.tuna.tsinghua.edu.cn/apache
    name: hbase
  accessModes:
  - ReadOnlyMany
`

type datasetMeta struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}
type datasetMount struct {
	MountPoint string `json:"mountPoint"`
	Name       string `json:"name"`
}
type datasetSpec struct {
	Mounts     []datasetMount `json:"mounts"`
	AccessMode []string       `json:"accessModes,omitempty"`
}
type datasetObj struct {
	APIVersion string      `json:"apiVersion"`
	Kind       string      `json:"kind"`
	Metadata   datasetMeta `json:"metadata"`
	Spec       datasetSpec `json:"spec"`
}

func record(t *testing.T, name, value string) {
	t.Helper()
	value = strings.ReplaceAll(value, "\n", "\\n")
	fmt.Printf("CASE %s | %s\n", name, value)
	if want, ok := golden[name]; ok {
		if value != want {
			t.Errorf("case %s: behavior differs from recorded golden\n got: %s\nwant: %s", name, value, want)
		}
	}
}

func TestYamlBehaviorMatrix(t *testing.T) {
	// C1: Marshal of a helm-values-shaped struct (pkg/ddc/*/master_internal.go path)
	port := 20003
	hv := helmValues{
		FullnameOverride: "demo",
		Replicas:         3,
		HostNetwork:      true,
		Image:            "fluid/alluxio",
		ImageTag:         "release-2.9.0",
		Ports:            map[string]int{"rpc": 19998, "web": 19999},
		Tolerations:      []map[string]string{{"key": "fluid", "operator": "Equal", "value": "true"}},
		Properties:       map[string]string{"alluxio.user.file.writetype": "CACHE_THROUGH", "quoted": "a:b # c"},
		Fuse:             fuseSection{Enabled: true, MetricsPort: &port, MountPath: "/runtime-mnt/alluxio", Args: []string{"fuse", "--fuse-opts=kernel_cache,ro"}},
	}
	out, err := yaml.Marshal(hv)
	if err != nil {
		t.Fatalf("C1 marshal: %v", err)
	}
	record(t, "C1-marshal-helmvalues", string(out))
	var hvBack helmValues
	if err := yaml.Unmarshal(out, &hvBack); err != nil {
		t.Fatalf("C1 roundtrip unmarshal: %v", err)
	}
	if !reflect.DeepEqual(hv, hvBack) {
		t.Fatalf("C1 roundtrip mismatch: %+v vs %+v", hv, hvBack)
	}

	// C2: Unmarshal of juicefs-style configmap data (port_parser.go path)
	var jv juiceFSValues
	if err := yaml.Unmarshal([]byte(juiceFSStyleYAML), &jv); err != nil {
		t.Fatalf("C2 unmarshal: %v", err)
	}
	record(t, "C2-unmarshal-juicefs", fmt.Sprintf("worker=%v,%d fuse=%v,%d",
		jv.Worker.HostNetwork, *jv.Worker.MetricsPort, jv.Fuse.HostNetwork, *jv.Fuse.MetricsPort))
	if *jv.Worker.MetricsPort != 20001 || *jv.Fuse.MetricsPort != 20002 {
		t.Fatalf("C2 wrong ports: %+v", jv)
	}

	// C3: YAML 1.1 scalar resolution through YAMLToJSON (the classic gotcha matrix)
	scalars := `a_yes: yes
b_no: "no"
c_on: on
d_hex: 0x10
e_underscore: 1_000
f_sexagesimal: 1:30
h_octalish: 0123
i_exp: 1e3
j_null: null
k_tilde: ~
l_empty: ""
m_date: 2024-01-02
`
	j, err := yaml.YAMLToJSON([]byte(scalars))
	if err != nil {
		t.Fatalf("C3 YAMLToJSON: %v", err)
	}
	record(t, "C3-yaml11-scalars", string(j))

	// C3b: .inf resolves to +Inf which JSON cannot encode -> YAMLToJSON errors
	_, errInf := yaml.YAMLToJSON([]byte("g_inf: .inf\n"))
	record(t, "C3b-inf-err", fmt.Sprintf("err=%v", errInf))

	// C4: Marshal of a free-form map (dataload value path)
	freeform := map[string]interface{}{
		"image":    "fluid:0.9",
		"replicas": 2,
		"enabled":  false,
		"labels":   map[string]interface{}{"a.b/c": "x", "num": 7},
		"list":     []interface{}{1, "two", true},
	}
	out4, err := yaml.Marshal(freeform)
	if err != nil {
		t.Fatalf("C4 marshal: %v", err)
	}
	record(t, "C4-marshal-map", string(out4))

	// C5: error text shapes
	var tabDoc interface{}
	errTab := yaml.Unmarshal([]byte("a:\n\tb: 1\n"), &tabDoc)
	record(t, "C5-tab-indent-err", fmt.Sprintf("err=%v", errTab))

	var dupStrict map[string]interface{}
	errDupStrict := yaml.UnmarshalStrict([]byte("a: 1\na: 2\n"), &dupStrict)
	record(t, "C5-dup-strict-err", fmt.Sprintf("err=%v", errDupStrict))

	var dupLax map[string]interface{}
	errDupLax := yaml.Unmarshal([]byte("a: 1\na: 2\n"), &dupLax)
	record(t, "C5-dup-lax", fmt.Sprintf("err=%v val=%v", errDupLax, dupLax))

	var typeMismatch juiceFSValues
	errTM := yaml.Unmarshal([]byte("worker:\n  hostNetwork: notabool\n"), &typeMismatch)
	record(t, "C5-typemismatch-err", fmt.Sprintf("err=%v", errTM))
	if errTM == nil {
		t.Fatalf("C5 expected a type mismatch error")
	}

	// C6: error UNWRAP semantics — the one intended behavioral delta of this PR
	// (sigs.k8s.io/yaml#106: jsonUnmarshal now wraps with %w, so errors.As can
	// reach the stdlib json error). Print-only, excluded from the golden table.
	var ute *json.UnmarshalTypeError
	unwrappable := errors.As(errTM, &ute)
	record(t, "C6-errunwrap", fmt.Sprintf("errors.As(*json.UnmarshalTypeError)=%v", unwrappable))

	// C7: empty / null documents
	var emptyObj juiceFSValues
	errEmpty := yaml.Unmarshal([]byte(""), &emptyObj)
	var nullObj juiceFSValues
	errNull := yaml.Unmarshal([]byte("null"), &nullObj)
	record(t, "C7-empty-null", fmt.Sprintf("emptyErr=%v nullErr=%v", errEmpty, errNull))

	// C8: anchors and moderately nested aliases
	anchorDoc := `base: &b
  x: 1
  y: [1, 2, 3]
ref:
  <<: *b
  z: 2
`
	var anchorObj map[string]interface{}
	errAnchor := yaml.Unmarshal([]byte(anchorDoc), &anchorObj)
	canon, _ := json.Marshal(anchorObj)
	record(t, "C8-anchors", fmt.Sprintf("err=%v val=%s", errAnchor, canon))

	// C9: integer precision at the edges (JSONToYAML preserves ints)
	bigInts := `{"max64": 9223372036854775807, "min64": -9223372036854775808, "over": 18446744073709551615, "plain": 42}`
	y9, err := yaml.JSONToYAML([]byte(bigInts))
	if err != nil {
		t.Fatalf("C9 JSONToYAML: %v", err)
	}
	record(t, "C9-int-precision", string(y9))

	// C10: Dataset-manifest typed unmarshal (pkg/utils/raw.go shape)
	var ds datasetObj
	if err := yaml.Unmarshal([]byte(datasetManifest), &ds); err != nil {
		t.Fatalf("C10 unmarshal: %v", err)
	}
	record(t, "C10-dataset-manifest", fmt.Sprintf("%s/%s kind=%s mounts=%d am=%v",
		ds.Metadata.Namespace, ds.Metadata.Name, ds.Kind, len(ds.Spec.Mounts), ds.Spec.AccessMode))
	if ds.Metadata.Name != "demo" || len(ds.Spec.Mounts) != 1 || ds.Spec.Mounts[0].MountPoint == "" {
		t.Fatalf("C10 wrong content: %+v", ds)
	}

	// C11: DisallowUnknownFields option (the function this PR moves between files)
	var dsStrict datasetObj
	errUnknown := yaml.Unmarshal([]byte(datasetManifest+"  unknownField: x\n"), &dsStrict, yaml.DisallowUnknownFields)
	record(t, "C11-disallow-unknown", fmt.Sprintf("err=%v", errUnknown))
	if errUnknown == nil || !strings.Contains(errUnknown.Error(), "unknown") {
		t.Fatalf("C11 expected unknown-field error, got %v", errUnknown)
	}

	// C12: JSONToYAML key ordering / unicode
	ujson := `{"zeta": "最后一个", "alpha": "héllo", "mid": [3,1,2]}`
	y12, err := yaml.JSONToYAML([]byte(ujson))
	if err != nil {
		t.Fatalf("C12: %v", err)
	}
	record(t, "C12-ordering-unicode", string(y12))
}
