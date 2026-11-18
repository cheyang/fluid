// Verification harness for fluid PR #6268 (chore(chart): bump image version to
// v1.1.0-dcb76167b). Additive test-only package; no production code is touched.
//
// Polarity: both tests are CONTRACT tests — they assert the intended correct
// behavior. On the reviewed head they FAIL (that failure IS the reproduction);
// once the PR is fixed they must PASS.
package harness

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	yaml "gopkg.in/yaml.v2"
)

// repoRoot walks up from the harness dir to the repository root (the dir that
// contains charts/fluid/fluid/values.yaml).
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "charts/fluid/fluid/values.yaml")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate repo root from %s", dir)
		}
		dir = parent
	}
}

func readValues(t *testing.T) (defaultVersion, defaultPrefix string, images []string) {
	t.Helper()
	root := repoRoot(t)
	data, err := ioutil.ReadFile(filepath.Join(root, "charts/fluid/fluid/values.yaml"))
	if err != nil {
		t.Fatalf("read values.yaml: %v", err)
	}
	var doc map[interface{}]interface{}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse values.yaml: %v", err)
	}
	// YAML anchors are resolved by the parser: every `imageTag: *defaultVersion`
	// arrives here as the literal tag string.
	if v, ok := doc["version"].(string); ok {
		defaultVersion = v
	}
	if p, ok := doc["imagePrefix"].(string); ok {
		defaultPrefix = p
	}
	if defaultVersion == "" || defaultPrefix == "" {
		t.Fatalf("could not resolve version/imagePrefix anchors (version=%q prefix=%q)", defaultVersion, defaultPrefix)
	}
	seen := map[string]bool{}
	var walk func(n interface{})
	walk = func(n interface{}) {
		switch m := n.(type) {
		case map[interface{}]interface{}:
			name, hasName := m["imageName"].(string)
			tag, hasTag := m["imageTag"].(string)
			if hasName && hasTag && tag == defaultVersion {
				prefix := defaultPrefix
				if p, ok := m["imagePrefix"].(string); ok && p != "" {
					prefix = p
				}
				ref := prefix + "/" + name
				if !seen[ref] {
					seen[ref] = true
					images = append(images, ref)
				}
			}
			for _, v := range m {
				walk(v)
			}
		case []interface{}:
			for _, v := range m {
				walk(v)
			}
		}
	}
	walk(doc)
	return
}

// dockerHubTagExists queries the Docker Hub v2 API for <repo>/<image>:<tag>.
func dockerHubTagExists(repo, tag string) (int, error) {
	url := fmt.Sprintf("https://hub.docker.com/v2/repositories/%s/tags/%s", repo, tag)
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return -1, err
	}
	defer resp.Body.Close()
	ioutil.ReadAll(resp.Body)
	return resp.StatusCode, nil
}

// TestDefaultVersionImagesExistOnDockerHub is the F1 contract test:
// every image whose tag is the chart's defaultVersion must exist on Docker Hub,
// otherwise a default `helm install` of this chart ImagePullBackOffs.
func TestDefaultVersionImagesExistOnDockerHub(t *testing.T) {
	defaultVersion, _, images := readValues(t)
	if len(images) == 0 {
		t.Fatalf("no images consume the defaultVersion anchor; harness is stale")
	}
	t.Logf("defaultVersion=%q; %d managed images", defaultVersion, len(images))
	var missing []string
	for _, ref := range images {
		code, err := dockerHubTagExists(ref, defaultVersion)
		if err != nil {
			t.Fatalf("docker hub query failed for %s: %v", ref, err)
		}
		t.Logf("%s:%s -> HTTP %d", ref, defaultVersion, code)
		if code != http.StatusOK {
			missing = append(missing, fmt.Sprintf("%s:%s (HTTP %d)", ref, defaultVersion, code))
		}
		time.Sleep(200 * time.Millisecond) // be polite to the registry API
	}
	if len(missing) > 0 {
		t.Errorf("defaultVersion %q is NOT PUBLISHED for %d/%d managed images:\n  %s\n"+
			"a default helm install of this chart will ImagePullBackOff",
			defaultVersion, len(missing), len(images), strings.Join(missing, "\n  "))
	}
}

// TestChartAppVersionConsistentWithDefaultVersion is a convention guard:
// Chart.yaml appVersion must equal defaultVersion minus the leading "v".
func TestChartAppVersionConsistentWithDefaultVersion(t *testing.T) {
	defaultVersion, _, _ := readValues(t)
	root := repoRoot(t)
	data, err := ioutil.ReadFile(filepath.Join(root, "charts/fluid/fluid/Chart.yaml"))
	if err != nil {
		t.Fatalf("read Chart.yaml: %v", err)
	}
	var chart map[interface{}]interface{}
	if err := yaml.Unmarshal(data, &chart); err != nil {
		t.Fatalf("parse Chart.yaml: %v", err)
	}
	appVersion := fmt.Sprintf("%v", chart["appVersion"])
	want := strings.TrimPrefix(defaultVersion, "v")
	if appVersion != want {
		t.Errorf("Chart.yaml appVersion=%q does not match values.yaml defaultVersion=%q (want %q)",
			appVersion, defaultVersion, want)
	}
}

// TestHeadCommitSignoffMatchesAuthor is the F2 contract test: it replicates the
// DCO app's core rule for the checked-out HEAD commit — the commit body must
// carry `Signed-off-by: <AuthorName> <<AuthorEmail>>` exactly.
// Override the inspected commit with SIGNOFF_REF (used by the harness-bites
// check to prove the test goes green on a compliant commit).
func TestHeadCommitSignoffMatchesAuthor(t *testing.T) {
	ref := os.Getenv("SIGNOFF_REF")
	if ref == "" {
		ref = "HEAD"
	}
	out, err := exec.Command("git", "log", "-1", "--format=%an%x00%ae%x00%b", ref).Output()
	if err != nil {
		t.Fatalf("git log %s: %v", ref, err)
	}
	parts := strings.SplitN(string(out), "\x00", 3)
	if len(parts) != 3 {
		t.Fatalf("unexpected git log output for %s", ref)
	}
	author := fmt.Sprintf("%s <%s>", strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
	want := "Signed-off-by: " + author
	for _, line := range strings.Split(parts[2], "\n") {
		if strings.TrimSpace(line) == want {
			return // compliant
		}
	}
	t.Errorf("commit %s: DCO sign-off mismatch — author is %q but no matching %q line exists in the commit body",
		ref, author, want)
}
