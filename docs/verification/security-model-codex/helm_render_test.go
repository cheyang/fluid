package securitymodeldocs

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	yaml "sigs.k8s.io/yaml"
)

// L2 (integration) layer: renders the actual shipped Helm chart with both
// helmDriver settings and asserts the control-plane permission claims of
// security-model.md §5/§9. Requires the `helm` binary; skips otherwise.

func helmAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm binary not available; skipping chart-render layer")
	}
}

func renderChart(t *testing.T, helmDriver string) []map[string]interface{} {
	t.Helper()
	root := repoRoot(t)
	args := []string{"template", "fluid", filepath.Join(root, "charts", "fluid", "fluid")}
	if helmDriver != "" {
		args = append(args, "--set", "helmDriver="+helmDriver)
	}
	// helm may print kubeconfig WARNING lines on stderr; keep streams separate.
	cmd := exec.Command("helm", args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("helm template (helmDriver=%s) failed: %v\n%s", helmDriver, err, stderr.String())
	}
	var docs []map[string]interface{}
	for _, d := range splitYAMLDocs(string(out)) {
		var v map[string]interface{}
		if err := yaml.Unmarshal([]byte(d), &v); err != nil {
			t.Fatalf("rendered doc does not parse: %v", err)
		}
		if v != nil {
			docs = append(docs, v)
		}
	}
	return docs
}

func rulesOf(doc map[string]interface{}) []map[string]interface{} {
	var out []map[string]interface{}
	rs, _ := doc["rules"].([]interface{})
	for _, r := range rs {
		if rm, ok := r.(map[string]interface{}); ok {
			out = append(out, rm)
		}
	}
	return out
}

func nameOf(doc map[string]interface{}) string {
	md, _ := doc["metadata"].(map[string]interface{})
	n, _ := md["name"].(string)
	return n
}

// TestL2_RBACHasNoWildcards: §5/§9 claim the Helm chart's RBAC rules use no
// wildcard apiGroups, resources or verbs, under both helmDriver settings.
func TestL2_RBACHasNoWildcards(t *testing.T) {
	helmAvailable(t)
	for _, driver := range []string{"configmap", "secret"} {
		for _, doc := range renderChart(t, driver) {
			kind, _ := doc["kind"].(string)
			if kind != "ClusterRole" && kind != "Role" {
				continue
			}
			for _, rule := range rulesOf(doc) {
				for _, field := range []string{"apiGroups", "resources", "verbs"} {
					items, _ := rule[field].([]interface{})
					for _, it := range items {
						if it == "*" {
							t.Errorf("helmDriver=%s %s %s has wildcard %s", driver, kind, nameOf(doc), field)
						}
					}
				}
			}
		}
		t.Logf("helmDriver=%s: no wildcard RBAC rules", driver)
	}
}

// TestL2_ClusterWideSecretsOnlyWithSecretDriver: §5 claims cluster-wide Secret
// API permissions appear only when helmDriver=secret.
func TestL2_ClusterWideSecretsOnlyWithSecretDriver(t *testing.T) {
	helmAvailable(t)
	clusterSecretGrants := func(driver string) []string {
		var names []string
		for _, doc := range renderChart(t, driver) {
			if k, _ := doc["kind"].(string); k != "ClusterRole" {
				continue
			}
			for _, rule := range rulesOf(doc) {
				res, _ := rule["resources"].([]interface{})
				for _, r := range res {
					if r == "secrets" {
						names = append(names, nameOf(doc))
					}
				}
			}
		}
		return names
	}
	if got := clusterSecretGrants("configmap"); len(got) != 0 {
		t.Errorf("helmDriver=configmap: unexpected cluster-wide Secret grants: %v", got)
	}
	secretGrants := clusterSecretGrants("secret")
	if len(secretGrants) == 0 {
		t.Error("helmDriver=secret: expected cluster-wide Secret grants via fluid.helmDriver.rbacs, found none")
	}
	t.Logf("helmDriver=secret: cluster-wide Secret grants on %v", secretGrants)
}

// TestL2_WebhooksFailClosed: §5/§9 claim both injection webhooks render
// failurePolicy: Fail with label selectors.
func TestL2_WebhooksFailClosed(t *testing.T) {
	helmAvailable(t)
	for _, driver := range []string{"configmap", "secret"} {
		found := map[string]string{}
		for _, doc := range renderChart(t, driver) {
			if k, _ := doc["kind"].(string); k != "MutatingWebhookConfiguration" {
				continue
			}
			webhooks, _ := doc["webhooks"].([]interface{})
			for _, wh := range webhooks {
				whm, _ := wh.(map[string]interface{})
				name, _ := whm["name"].(string)
				fp, _ := whm["failurePolicy"].(string)
				if name == "sidecar.fuse.fluid.io" || name == "fuse.serverful.fluid.io" {
					found[name] = fp
					sel, _ := whm["objectSelector"].(map[string]interface{})
					ml, _ := sel["matchLabels"].(map[string]interface{})
					if len(ml) == 0 {
						t.Errorf("helmDriver=%s webhook %s has no label selector", driver, name)
					}
				}
			}
		}
		for _, name := range []string{"sidecar.fuse.fluid.io", "fuse.serverful.fluid.io"} {
			fp, ok := found[name]
			if !ok {
				t.Errorf("helmDriver=%s: webhook %s not rendered", driver, name)
			} else if fp != "Fail" {
				t.Errorf("helmDriver=%s: webhook %s failurePolicy=%s, want Fail", driver, name, fp)
			}
		}
	}
}

// TestL2_ThinControllerLacksSecretRBACByDefault: §5 claims the default Helm
// configuration does not grant thinruntime-controller the Secret API
// permissions needed for the cross-namespace copy.
func TestL2_ThinControllerLacksSecretRBACByDefault(t *testing.T) {
	helmAvailable(t)
	for _, doc := range renderChart(t, "configmap") {
		if k, _ := doc["kind"].(string); k != "ClusterRole" || nameOf(doc) != "thinruntime-controller" {
			continue
		}
		for _, rule := range rulesOf(doc) {
			res, _ := rule["resources"].([]interface{})
			for _, r := range res {
				if r == "secrets" {
					t.Errorf("thinruntime-controller has secrets rule by default: %v", rule)
				}
			}
		}
	}
	// webhook certificate secret access must stay namespace-scoped (a Role)
	for _, doc := range renderChart(t, "configmap") {
		if k, _ := doc["kind"].(string); k != "Role" || nameOf(doc) != "fluid-webhook" {
			continue
		}
		hasSecrets := false
		for _, rule := range rulesOf(doc) {
			res, _ := rule["resources"].([]interface{})
			for _, r := range res {
				if r == "secrets" {
					hasSecrets = true
				}
			}
		}
		if !hasSecrets {
			t.Error("fluid-webhook Role lost its certificate secrets rule; doc claim stale")
		}
	}
}
