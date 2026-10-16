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

// Chart-level verification harness for PR #6197 finding F1 (reviewer B / Codex).
//
// F1: on `helm upgrade` the preserved `webhook-plugins` ConfigMap used to be re-emitted
// verbatim, so a cluster upgraded from a release without the clientless plugin group kept
// a profile with no `clientless` section while the new `clientless.fluid.io` webhook rule
// already matched labeled pods: zero plugins ran and pods were admitted unmutated.
//
// The fix under test (delta c5ea086b..6d3a6d7e):
//   - templates/webhook/plugins-profile.yaml now merges plugin groups that are missing
//     from a preserved profile (i.e. `clientless`) into it, and
//   - templates/webhook/webhook.yaml gains a checksum/plugins-profile pod annotation so
//     the webhook Deployment rolls and reloads the merged profile.
//
// helm's `lookup` only works against a live cluster, which this debate setup skips, so
// this harness renders the chart's REAL template text (read from the chart on disk) with
// the same function semantics helm v3.16 uses (sprig dict/reflection funcs from
// slim-sprig, helm's own toYaml/fromYaml re-implemented from helm's engine/funcs.go) and
// a stubbed `lookup` that returns a fake existing ConfigMap. The fresh-install branch is
// cross-checked against the real `helm template` binary when it is available.
package chartmerge

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"text/template"

	sprig "github.com/go-task/slim-sprig/v3"

	"github.com/fluid-cloudnative/fluid/pkg/webhook/plugins"
	yamlv2 "gopkg.in/yaml.v2"
	sigsyaml "sigs.k8s.io/yaml"
)

// baseSHA is the merge-base this PR builds on; the legacy profile fixture below was taken
// from its values.yaml and TestLegacyProfileFixtureMatchesBase re-checks it against git.
const baseSHA = "e0fc4c189a6e45ee17a5e812abf3fcbecf566f09"

// legacyProfile mirrors the webhook.pluginsProfile of the release this PR builds on:
// there is no clientless section, and a pluginConfig entry that must survive the merge.
// This is what an upgraded cluster has in its webhook-plugins ConfigMap.
const legacyProfile = `plugins:
  serverful:
    withDataset:
    - FilePrefetcher
    - RequireNodeWithFuse
    - NodeAffinityWithCache
    - MountPropagationInjector
    - DatasetUsageInjector
    withoutDataset:
    - PreferNodesWithoutCache
  serverless:
    withDataset:
    - FilePrefetcher
    - FuseSidecar
    - DatasetUsageInjector
    withoutDataset: []
pluginConfig:
- name: NodeAffinityWithCache
  args: |
    preferred:
      - name: fluid.io/node
        weight: 100
`

func repoRoot(t *testing.T) string {
	t.Helper()
	// docs/verification/clientless-runtime-config-inject/chartmerge -> repo root
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repo root %s has no go.mod: %v", root, err)
	}
	return root
}

// --- helm-compatible template functions (mirrors helm.sh/helm/v3/pkg/engine/funcs.go) ---

// toYAML matches helm's toYAML: sigs.k8s.io/yaml marshal, trailing newline trimmed,
// empty string on error.
func toYAML(v interface{}) string {
	data, err := sigsyaml.Marshal(v)
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(string(data), "\n")
}

// fromYAML matches helm's fromYAML: always returns a map, carrying an "Error" key when
// the input does not parse into one.
func fromYAML(str string) map[string]interface{} {
	m := map[string]interface{}{}
	if err := sigsyaml.Unmarshal([]byte(str), &m); err != nil {
		m["Error"] = err.Error()
	}
	return m
}

type chartRenderer struct {
	t      *testing.T
	tmpl   *template.Template
	values map[string]interface{}
	lookup func() map[string]interface{} // stubbed helm lookup for the webhook-plugins CM
}

func newChartRenderer(t *testing.T, existingProfile string, forceReplace bool) *chartRenderer {
	t.Helper()
	root := repoRoot(t)

	valuesRaw, err := os.ReadFile(filepath.Join(root, "charts/fluid/fluid/values.yaml"))
	if err != nil {
		t.Fatalf("read values.yaml: %v", err)
	}
	values := map[string]interface{}{}
	if err := sigsyaml.Unmarshal(valuesRaw, &values); err != nil {
		t.Fatalf("parse values.yaml: %v", err)
	}
	if forceReplace {
		webhook := values["webhook"].(map[string]interface{})
		webhook["forceReplacePluginsProfile"] = true
	}

	r := &chartRenderer{t: t, values: values}
	r.lookup = func() map[string]interface{} {
		if existingProfile == "" {
			return map[string]interface{}{} // not found: what helm's lookup returns
		}
		return map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata":   map[string]interface{}{"name": "webhook-plugins", "namespace": "fluid-system"},
			"data":       map[string]interface{}{"pluginsProfile": existingProfile},
		}
	}

	funcMap := sprig.TxtFuncMap()
	funcMap["toYaml"] = toYAML
	funcMap["fromYaml"] = fromYAML
	funcMap["include"] = func(name string, data interface{}) (string, error) {
		var buf strings.Builder
		err := r.tmpl.ExecuteTemplate(&buf, name, data)
		return buf.String(), err
	}
	funcMap["lookup"] = func(apiVersion, kind, namespace, name string) (map[string]interface{}, error) {
		return r.lookup(), nil
	}

	profileTemplate, err := os.ReadFile(filepath.Join(root, "charts/fluid/fluid/templates/webhook/plugins-profile.yaml"))
	if err != nil {
		t.Fatalf("read plugins-profile.yaml: %v", err)
	}

	// The real chart defines fluid.namespace in _helpers.tpl; this stub is faithful to it.
	const helpersStub = `{{- define "fluid.namespace" -}}{{ if .Values.namespace }}{{ .Values.namespace }}{{ else }}{{ .Release.Namespace }}{{ end }}{{- end -}}`

	r.tmpl = template.Must(template.New("plugins-profile.yaml").Funcs(funcMap).Parse(helpersStub))
	r.tmpl = template.Must(r.tmpl.Parse(string(profileTemplate)))
	return r
}

func (r *chartRenderer) dot() map[string]interface{} {
	return map[string]interface{}{
		"Values":  r.values,
		"Release": map[string]interface{}{"Name": "fluid", "Namespace": "fluid-system", "Service": "Helm"},
		"Chart":   map[string]interface{}{"Name": "fluid"},
	}
}

// renderProfile renders the chart's plugins-profile.yaml and returns the resulting
// ConfigMap's data.pluginsProfile string.
func (r *chartRenderer) renderProfile(t *testing.T) string {
	t.Helper()
	var buf strings.Builder
	if err := r.tmpl.ExecuteTemplate(&buf, "plugins-profile.yaml", r.dot()); err != nil {
		t.Fatalf("render plugins-profile.yaml: %v", err)
	}
	var cm struct {
		Data map[string]string `yaml:"data"`
	}
	if err := sigsyaml.Unmarshal([]byte(buf.String()), &cm); err != nil {
		t.Fatalf("rendered ConfigMap does not parse: %v\n%s", err, buf.String())
	}
	profile, ok := cm.Data["pluginsProfile"]
	if !ok {
		t.Fatalf("rendered ConfigMap has no data.pluginsProfile:\n%s", buf.String())
	}
	return profile
}

func profileAsMap(t *testing.T, profile string) map[string]interface{} {
	t.Helper()
	m := map[string]interface{}{}
	if err := sigsyaml.Unmarshal([]byte(profile), &m); err != nil {
		t.Fatalf("profile does not parse: %v", err)
	}
	return m
}

// valuesClientlessGroup returns the clientless plugin group from the chart's values.yaml,
// i.e. what the merge is supposed to add to a preserved profile.
func valuesClientlessGroup(t *testing.T) interface{} {
	t.Helper()
	root := repoRoot(t)
	valuesRaw, err := os.ReadFile(filepath.Join(root, "charts/fluid/fluid/values.yaml"))
	if err != nil {
		t.Fatalf("read values.yaml: %v", err)
	}
	values := map[string]interface{}{}
	if err := sigsyaml.Unmarshal(valuesRaw, &values); err != nil {
		t.Fatalf("parse values.yaml: %v", err)
	}
	return values["webhook"].(map[string]interface{})["pluginsProfile"].(map[string]interface{})["plugins"].(map[string]interface{})["clientless"]
}

// TestPluginsProfileMerge_UpgradeLegacyProfile is the F1 contract: an upgrade that finds
// a preserved legacy profile must emit it with the clientless group merged in, keeping
// every pre-existing entry.
func TestPluginsProfileMerge_UpgradeLegacyProfile(t *testing.T) {
	r := newChartRenderer(t, legacyProfile, false)
	merged := r.renderProfile(t)
	mergedMap := profileAsMap(t, merged)
	legacyMap := profileAsMap(t, legacyProfile)

	pluginsMap, ok := mergedMap["plugins"].(map[string]interface{})
	if !ok {
		t.Fatalf("merged profile plugins is not a map: %s", merged)
	}
	clientless, ok := pluginsMap["clientless"]
	if !ok {
		t.Fatalf("merged profile has no clientless group — F1 still present:\n%s", merged)
	}
	if !reflect.DeepEqual(clientless, valuesClientlessGroup(t)) {
		t.Fatalf("merged clientless group differs from the chart's values.yaml group:\n%v\nvs\n%v",
			clientless, valuesClientlessGroup(t))
	}
	// every legacy plugin group must survive
	for _, group := range []string{"serverful", "serverless"} {
		if !reflect.DeepEqual(pluginsMap[group], legacyMap["plugins"].(map[string]interface{})[group]) {
			t.Fatalf("legacy %s group was not preserved:\ngot  %v\nwant %v", group, pluginsMap[group], legacyMap["plugins"].(map[string]interface{})[group])
		}
	}
	// the pluginConfig section must survive too
	if !reflect.DeepEqual(mergedMap["pluginConfig"], legacyMap["pluginConfig"]) {
		t.Fatalf("pluginConfig was not preserved:\ngot  %v\nwant %v", mergedMap["pluginConfig"], legacyMap["pluginConfig"])
	}
	// and the merged profile must still load into the webhook's own profile type
	var parsed plugins.PluginsProfile
	if err := yamlv2.Unmarshal([]byte(merged), &parsed); err != nil {
		t.Fatalf("merged profile does not unmarshal into plugins.PluginsProfile: %v", err)
	}
	if len(parsed.Plugins.Clientless.WithDataset) != 1 || parsed.Plugins.Clientless.WithDataset[0] != "RuntimeConfigInjector" {
		t.Fatalf("webhook would not register RuntimeConfigInjector from the merged profile: %+v", parsed.Plugins.Clientless)
	}
	// rendering must be deterministic (no diff churn between helm runs)
	if again := newChartRenderer(t, legacyProfile, false).renderProfile(t); again != merged {
		t.Fatalf("merge render is not deterministic:\n%s\nvs\n%s", merged, again)
	}
}

// TestPluginsProfileMerge_PreservesUserCustomizations: a preserved profile whose existing
// groups a user edited must keep those edits and still gain clientless.
func TestPluginsProfileMerge_PreservesUserCustomizations(t *testing.T) {
	custom := strings.Replace(legacyProfile, "- PreferNodesWithoutCache", "- PreferNodesWithoutCache\n    - MyCustomPlugin", 1)
	merged := profileAsMap(t, newChartRenderer(t, custom, false).renderProfile(t))
	serverful := merged["plugins"].(map[string]interface{})["serverful"].(map[string]interface{})
	if !reflect.DeepEqual(serverful["withoutDataset"], []interface{}{"PreferNodesWithoutCache", "MyCustomPlugin"}) {
		t.Fatalf("user customization lost: %v", serverful["withoutDataset"])
	}
	if _, ok := merged["plugins"].(map[string]interface{})["clientless"]; !ok {
		t.Fatalf("clientless group not merged into a customized profile")
	}
}

// TestPluginsProfileMerge_KeepsProfileThatAlreadyHasClientless: once a profile carries a
// (possibly user-edited) clientless group, the merge leaves it alone.
func TestPluginsProfileMerge_KeepsProfileThatAlreadyHasClientless(t *testing.T) {
	withClientless := legacyProfile[:strings.Index(legacyProfile, "pluginConfig:")] +
		"  clientless:\n    withDataset:\n    - RuntimeConfigInjector\n    - MyExtraPlugin\n    withoutDataset: []\n" +
		legacyProfile[strings.Index(legacyProfile, "pluginConfig:"):]
	got := profileAsMap(t, newChartRenderer(t, withClientless, false).renderProfile(t))
	want := profileAsMap(t, withClientless)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("a profile that already has clientless must be kept as it is:\ngot  %v\nwant %v", got, want)
	}
}

// TestPluginsProfileMerge_KeptVerbatimWhenUnmergeable: profiles that are not a map of
// plugin groups cannot be merged into and must be kept as they are (and must not fail
// the template render).
func TestPluginsProfileMerge_KeptVerbatimWhenUnmergeable(t *testing.T) {
	for name, profile := range map[string]string{
		"invalid yaml":    "plugins: [unclosed",
		"scalar plugins":  "plugins: not-a-map",
		"no plugins key":  "unrelated: true",
		"scalar document": "just-a-string",
	} {
		t.Run(name, func(t *testing.T) {
			got := newChartRenderer(t, profile, false).renderProfile(t)
			if strings.TrimSpace(got) != strings.TrimSpace(profile) {
				t.Fatalf("unmergeable profile must be kept verbatim:\ngot  %q\nwant %q", got, profile)
			}
		})
	}
}

// TestPluginsProfileMerge_SecondUpgradeIsStable: once the merged profile is stored in the
// ConfigMap, the next upgrade must render byte-identical profile data (no diff churn).
func TestPluginsProfileMerge_SecondUpgradeIsStable(t *testing.T) {
	merged := newChartRenderer(t, legacyProfile, false).renderProfile(t)
	again := newChartRenderer(t, merged, false).renderProfile(t)
	if again != merged {
		t.Fatalf("second upgrade changed the stored profile:\n%q\nvs\n%q", again, merged)
	}
}

// TestPluginsProfileMerge_FreshInstallAndForceReplace: without an existing ConfigMap, or
// with forceReplacePluginsProfile, the chart's own values profile (which carries the
// clientless group) is used.
func TestPluginsProfileMerge_FreshInstallAndForceReplace(t *testing.T) {
	fresh := newChartRenderer(t, "", false).renderProfile(t)
	freshMap := profileAsMap(t, fresh)
	if _, ok := freshMap["plugins"].(map[string]interface{})["clientless"]; !ok {
		t.Fatalf("fresh install profile has no clientless group:\n%s", fresh)
	}

	forced := newChartRenderer(t, legacyProfile, true).renderProfile(t)
	if forced != fresh {
		t.Fatalf("forceReplacePluginsProfile must render the values profile:\n%s\nvs\n%s", forced, fresh)
	}
}

// TestWebhookChecksumAnnotation: webhook.yaml must carry a checksum/plugins-profile
// annotation so the Deployment rolls (and reloads the profile) when the rendered profile
// changes; rendering the checksum expression the same way webhook.yaml does must yield a
// value that changes when the profile content changes and then stays stable.
func TestWebhookChecksumAnnotation(t *testing.T) {
	root := repoRoot(t)
	webhookYAML, err := os.ReadFile(filepath.Join(root, "charts/fluid/fluid/templates/webhook/webhook.yaml"))
	if err != nil {
		t.Fatalf("read webhook.yaml: %v", err)
	}
	if !strings.Contains(string(webhookYAML), "checksum/plugins-profile") {
		t.Fatalf("webhook.yaml lost the checksum/plugins-profile annotation")
	}

	checksumOf := func(existingProfile string) string {
		r := newChartRenderer(t, existingProfile, false)
		// render the exact expression webhook.yaml uses, with BasePath pointing at the
		// parsed plugins-profile template name
		var buf strings.Builder
		tmpl := template.Must(r.tmpl.New("checksum").Parse(
			`{{ include "plugins-profile.yaml" . | sha256sum }}`))
		if err := tmpl.Execute(&buf, r.dot()); err != nil {
			t.Fatalf("render checksum: %v", err)
		}
		return buf.String()
	}

	legacy := checksumOf(legacyProfile)
	mergedProfile := newChartRenderer(t, legacyProfile, false).renderProfile(t)
	merged := checksumOf(mergedProfile)
	if legacy == merged {
		t.Fatalf("checksum does not change when the profile changes: webhook would not roll")
	}
	if again := checksumOf(mergedProfile); again != merged {
		t.Fatalf("checksum not stable once the merged profile is stored: %s vs %s", again, merged)
	}
}

// TestHelmTemplateFreshInstall anchors the harness to the real helm binary for the branch
// it can render without a cluster (no existing ConfigMap => lookup is empty).
func TestHelmTemplateFreshInstall(t *testing.T) {
	helmPath, err := exec.LookPath("helm")
	if err != nil {
		t.Skip("helm binary not available")
	}
	root := repoRoot(t)
	out, err := exec.Command(helmPath, "template", "fluid", filepath.Join(root, "charts/fluid/fluid")).Output()
	if err != nil {
		t.Fatalf("helm template failed: %v", err)
	}
	rendered := string(out)
	if !strings.Contains(rendered, "clientless.fluid.io") {
		t.Fatalf("rendered webhook configuration has no clientless.fluid.io rule")
	}
	if !strings.Contains(rendered, "RuntimeConfigInjector") {
		t.Fatalf("rendered plugins profile has no RuntimeConfigInjector")
	}
	if !strings.Contains(rendered, "checksum/plugins-profile") {
		t.Fatalf("rendered webhook deployment has no checksum/plugins-profile annotation")
	}
	// cross-check the harness against helm on the fresh-install profile content
	mine := profileAsMap(t, newChartRenderer(t, "", false).renderProfile(t))
	var helmCM struct {
		Data map[string]string `yaml:"data"`
	}
	found := false
	for _, doc := range strings.Split(rendered, "\n---") {
		if !strings.Contains(doc, "name: webhook-plugins") {
			continue
		}
		if err := sigsyaml.Unmarshal([]byte(doc), &helmCM); err != nil {
			t.Fatalf("helm-rendered webhook-plugins ConfigMap does not parse: %v", err)
		}
		found = true
	}
	if !found {
		t.Fatalf("helm render has no webhook-plugins ConfigMap")
	}
	if helms := profileAsMap(t, helmCM.Data["pluginsProfile"]); !reflect.DeepEqual(helms, mine) {
		t.Fatalf("harness fresh-install render differs from real helm:\nhelm: %v\nmine: %v", helms, mine)
	}
}

// TestLegacyProfileFixtureMatchesBase re-checks the embedded legacy fixture against the
// merge-base values.yaml so the upgrade tests cannot drift away from reality.
func TestLegacyProfileFixtureMatchesBase(t *testing.T) {
	root := repoRoot(t)
	out, err := exec.Command("git", "-C", root, "show", baseSHA+":charts/fluid/fluid/values.yaml").Output()
	if err != nil {
		t.Skipf("base %s not available in this clone: %v", baseSHA, err)
	}
	baseValues := map[string]interface{}{}
	if err := sigsyaml.Unmarshal(out, &baseValues); err != nil {
		t.Fatalf("parse base values.yaml: %v", err)
	}
	baseProfile := baseValues["webhook"].(map[string]interface{})["pluginsProfile"]
	baseMap := profileAsMap(t, toYAML(baseProfile))
	fixtureMap := profileAsMap(t, legacyProfile)
	// the fixture is a hand-trimmed copy (pluginConfig args shortened); require the plugin
	// groups to match exactly and pluginConfig to be present in both.
	if !reflect.DeepEqual(baseMap["plugins"], fixtureMap["plugins"]) {
		t.Fatalf("legacy fixture plugins drifted from the base chart:\nbase: %v\nfixture: %v", baseMap["plugins"], fixtureMap["plugins"])
	}
	if len(baseMap["pluginConfig"].([]interface{})) == 0 || len(fixtureMap["pluginConfig"].([]interface{})) == 0 {
		t.Fatalf("pluginConfig missing from base chart or fixture")
	}
}
