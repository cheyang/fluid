/*
Verification harness for the review of PR #6187 (additive; production code untouched).

Claim under test (contract polarity — red on base, green on the PR head):

	When transformFuseConfig fails inside updateFuseConfigOnChange, the error must
	reach the only caller, ShouldUpdateUFS, which logs it at Error level, while still
	returning a nil *utils.UFSToUpdate (no behavioural change beyond observability).
*/
package thin

import (
	"strings"
	"testing"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
)

// recordingSink is a logr.LogSink that records Error calls so tests can assert
// on what the engine logged.
type recordingSink struct {
	errorMsgs []string
	errs      []error
}

func (s *recordingSink) Init(logr.RuntimeInfo)                  {}
func (s *recordingSink) Enabled(int) bool                       { return true }
func (s *recordingSink) Info(int, string, ...interface{})       {}
func (s *recordingSink) WithName(string) logr.LogSink           { return s }
func (s *recordingSink) WithValues(...interface{}) logr.LogSink { return s }
func (s *recordingSink) Error(err error, msg string, kv ...interface{}) {
	s.errs = append(s.errs, err)
	s.errorMsgs = append(s.errorMsgs, msg)
}

func (s *recordingSink) loggedErrorContaining(substr string) bool {
	for _, err := range s.errs {
		if err != nil && strings.Contains(err.Error(), substr) {
			return true
		}
	}
	return false
}

// verifyPR6187Objects builds the scenario from the PR: a Dataset whose only mount
// is pvc://missing-pvc (no such PersistentVolumeClaim exists), plus the fuse
// ConfigMap that updateFuseConfigOnChange would update. transformFuseConfig must
// fail in extractVolumeInfo with "failed to extract volume info".
func verifyPR6187Objects() (*datav1alpha1.Dataset, *corev1.ConfigMap, *datav1alpha1.ThinRuntime) {
	dataset := &datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: datav1alpha1.DatasetSpec{
			Mounts: []datav1alpha1.Mount{
				{MountPoint: "pvc://missing-pvc", Name: "missing-pvc"},
			},
		},
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "test-fuse-conf", Namespace: "default"},
		Data:       map[string]string{"config.json": "{}"},
	}
	thinRuntime := &datav1alpha1.ThinRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
	}
	return dataset, cm, thinRuntime
}

func verifyPR6187Scheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add corev1 to scheme: %v", err)
	}
	if err := datav1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add datav1alpha1 to scheme: %v", err)
	}
	return scheme
}

func verifyPR6187Engine(c client.Client, sink *recordingSink) ThinEngine {
	_, _, thinRuntime := verifyPR6187Objects()
	return ThinEngine{
		Client:    c,
		name:      "test",
		namespace: "default",
		runtime:   thinRuntime,
		Log:       logr.New(sink),
	}
}

// L1 — caller level with a fake client. Contract test: FAILS on the base branch
// (error swallowed, nothing logged), PASSES on the PR head (error propagated and
// logged). The nil-UFSToUpdate assertion holds on both, pinning the PR's claim
// that behaviour beyond logging is unchanged.
func TestVerifyPR6187ShouldUpdateUFSLogsTransformError(t *testing.T) {
	dataset, cm, _ := verifyPR6187Objects()
	c := fake.NewFakeClientWithScheme(verifyPR6187Scheme(t), dataset, cm)
	sink := &recordingSink{}
	engine := verifyPR6187Engine(c, sink)

	ufsToUpdate := engine.ShouldUpdateUFS()
	if ufsToUpdate != nil {
		t.Errorf("ShouldUpdateUFS() = %v, want nil (transform failure must not schedule an update)", ufsToUpdate)
	}
	if !sink.loggedErrorContaining("failed to extract volume info") {
		t.Errorf("transform failure was not surfaced: expected an Error log containing %q, got errs=%v msgs=%v",
			"failed to extract volume info", sink.errs, sink.errorMsgs)
	}
}
