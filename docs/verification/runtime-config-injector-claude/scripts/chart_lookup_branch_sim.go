//go:build ignore

// Deterministic simulation of the helm `lookup` branch in
// charts/fluid/fluid/templates/webhook/plugins-profile.yaml, using the REAL
// template file and the REAL values.yaml of this branch.
//
// It answers one question (reviewer finding F1, chart half): what does
// `helm upgrade` render into the webhook-plugins ConfigMap when the ConfigMap
// already exists from a previous fluid release (whose profile has no
// `clientless` group) and forceReplacePluginsProfile is left at its default?
//
// helm's `lookup` returns the live object during upgrade/install against a
// cluster; `helm template` (client-only) always returns nil. This program
// stubs `lookup` both ways, plus `include`, `toYaml` and `indent`, and renders
// the actual template text. Run from the repo root:
//
//	go run docs/verification/runtime-config-injector-claude/scripts/chart_lookup_branch_sim.go
package main

import (
	"fmt"
	"os"
	"strings"
	"text/template"

	"gopkg.in/yaml.v2"
)

const templatePath = "charts/fluid/fluid/templates/webhook/plugins-profile.yaml"
const valuesPath = "charts/fluid/fluid/values.yaml"

// preUpgradeProfile is what an installation from before PR #6197 holds in its
// webhook-plugins ConfigMap: no `clientless` group (values.yaml had none).
const preUpgradeProfile = `plugins:
  serverful:
    withDataset:
    - RequireNodeWithFuse
    - NodeAffinityWithCache
    - MountPropagationInjector
    withoutDataset:
    - PreferNodesWithoutCache
  serverless:
    withDataset:
    - FuseSidecar
    - DatasetUsageInjector
    withoutDataset: []
pluginConfig: []
`

func render(lookupResult interface{}, forceReplace bool) (string, error) {
	tmplBytes, err := os.ReadFile(templatePath)
	if err != nil {
		return "", err
	}
	valuesBytes, err := os.ReadFile(valuesPath)
	if err != nil {
		return "", err
	}

	var values map[string]interface{}
	if err := yaml.Unmarshal(valuesBytes, &values); err != nil {
		return "", err
	}
	// deep-copy-ish tweak: forceReplacePluginsProfile
	webhook := values["webhook"].(map[interface{}]interface{})
	webhook["forceReplacePluginsProfile"] = forceReplace

	funcs := template.FuncMap{
		"include": func(name string, _ interface{}) (string, error) {
			// charts define fluid.namespace; default namespace is fluid-system
			if name == "fluid.namespace" {
				return "fluid-system", nil
			}
			return "", fmt.Errorf("unknown template %q", name)
		},
		"toYaml": func(v interface{}) (string, error) {
			b, err := yaml.Marshal(v)
			return string(b), err
		},
		"indent": func(n int, s string) string {
			pad := strings.Repeat(" ", n)
			return pad + strings.ReplaceAll(strings.TrimRight(s, "\n"), "\n", "\n"+pad)
		},
		"lookup": func(_ ...interface{}) (interface{}, error) {
			return lookupResult, nil
		},
	}

	tmpl, err := template.New("plugins-profile").Funcs(funcs).Parse(string(tmplBytes))
	if err != nil {
		return "", err
	}

	var out strings.Builder
	if err := tmpl.Execute(&out, map[string]interface{}{"Values": values}); err != nil {
		return "", err
	}
	return out.String(), nil
}

func expect(what string, got string, contains bool, needle string) bool {
	has := strings.Contains(got, needle)
	ok := has == contains
	status := "PASS"
	if !ok {
		status = "FAIL"
	}
	fmt.Printf("  [%s] %s: rendered profile %q %s\n", status, what, needle, map[bool]string{true: "PRESENT", false: "absent"}[has])
	return ok
}

func main() {
	oldCM := map[string]interface{}{
		"data": map[string]interface{}{"pluginsProfile": preUpgradeProfile},
	}

	allOK := true

	fmt.Println("case A: fresh install (no existing ConfigMap; lookup -> nil, forceReplace=false)")
	out, err := render(nil, false)
	if err != nil {
		fmt.Println("  render error:", err)
		os.Exit(1)
	}
	allOK = expect("fresh install renders the new profile", out, true, "clientless") && allOK

	fmt.Println("case B: helm upgrade over an existing install (lookup -> live ConfigMap with the pre-PR profile, forceReplace=false)")
	out, err = render(oldCM, false)
	if err != nil {
		fmt.Println("  render error:", err)
		os.Exit(1)
	}
	allOK = expect("upgrade keeps the OLD profile", out, false, "clientless") && allOK
	fmt.Println("  rendered pluginsProfile (verbatim):")
	fmt.Println(out)

	fmt.Println("case C: helm upgrade with forceReplacePluginsProfile=true and the same live ConfigMap")
	out, err = render(oldCM, true)
	if err != nil {
		fmt.Println("  render error:", err)
		os.Exit(1)
	}
	allOK = expect("forced replace renders the new profile", out, true, "clientless") && allOK

	if !allOK {
		fmt.Println("VERDICT: unexpected rendering - check the template")
		os.Exit(1)
	}
	fmt.Println("VERDICT: F1 chart mechanism CONFIRMED - on helm upgrade with default values, the")
	fmt.Println("webhook-plugins ConfigMap keeps a profile without the clientless group, while")
	fmt.Println("webhookconfiguration.yaml unconditionally adds the clientless.fluid.io rule.")
}
